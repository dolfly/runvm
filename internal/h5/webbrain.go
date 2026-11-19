package h5

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WebBrain page-control capability.
//
// runvm's H5 backend is a plain WebKit view with no extension runtime, so the
// WebBrain content scripts (assets/h5/webbrain/accessibility-tree.js and
// content.js, copied verbatim from webbrain-one/webbrain under GPL-3.0-or-later)
// are loaded as ordinary page scripts through shim.js, which stubs
// chrome.runtime, captures the onMessage handlers the scripts register, and
// exposes window.__webbrainRun(action, params). The webbrain MCP tool then
// dispatches the same actions the extension's agent loop uses.

// webbrainDir resolves the directory holding the WebBrain scripts. It follows
// the same asset convention as the Android backend: RUNVM_ASSETS overrides the
// default relative "assets".
func webbrainDir() string {
	base := "assets"
	if v := os.Getenv("RUNVM_ASSETS"); v != "" {
		base = v
	}
	return filepath.Join(base, "h5", "webbrain")
}

// webbrainFiles is the load order: the shim must install the chrome.runtime
// stub and listener capture before the two scripts register their handlers,
// and accessibility-tree.js must load before content.js references it.
var webbrainFiles = []string{"shim.js", "accessibility-tree.js", "content.js"}

// webbrainPresent reports whether the script directory is usable.
func webbrainPresent(dir string) bool {
	for _, f := range webbrainFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// webbrainReadyJS is the cheap guard the webbrain tool evaluates before every
// call: if a full navigation dropped the injected globals, re-inject first.
const webbrainReadyJS = `(window.__webbrainReady === true && typeof window.__generateAccessibilityTree === 'function') ? 'ready' : 'missing'`

const webbrainActionTimeout = 60 * time.Second

// InjectWebbrain evaluates the WebBrain content scripts in the page (in load
// order) and reports which file failed, if any.
func InjectWebbrain(ctx context.Context, s *Session) error {
	dir := webbrainDir()
	if !webbrainPresent(dir) {
		return fmt.Errorf("webbrain assets not found in %s (set RUNVM_ASSETS to the repo root, or run from the repo root)", dir)
	}
	for _, name := range webbrainFiles {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("webbrain: read %s: %w", name, err)
		}
		if _, err := s.Eval(ctx, string(raw), webbrainActionTimeout); err != nil {
			return fmt.Errorf("webbrain: inject %s: %w", name, err)
		}
	}
	return nil
}

// EnsureWebbrain re-injects the scripts if a navigation dropped them. It is
// cheap on the hot path: one eval that returns 'ready' when the globals are
// already present.
func EnsureWebbrain(ctx context.Context, s *Session) error {
	state, err := s.Eval(ctx, webbrainReadyJS, 10*time.Second)
	if err != nil {
		return fmt.Errorf("webbrain: probe: %w", err)
	}
	if state == `"ready"` {
		return nil
	}
	if err := InjectWebbrain(ctx, s); err != nil {
		return err
	}
	// Verify the injection actually took before handing control to the caller.
	state, err = s.Eval(ctx, webbrainReadyJS, 10*time.Second)
	if err != nil || state != `"ready"` {
		return fmt.Errorf("webbrain: injection did not stick (state=%s, err=%v)", state, err)
	}
	return nil
}

// WebbrainAct dispatches one WebBrain content-script action through the shim
// and returns the handler's response object. params is passed to the page as
// the action's msg.params; values are JSON-serialized.
func WebbrainAct(ctx context.Context, s *Session, action string, params map[string]any, timeout time.Duration) (map[string]any, error) {
	if err := EnsureWebbrain(ctx, s); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = webbrainActionTimeout
	}
	args, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("webbrain: marshal params: %w", err)
	}
	// Eval prints strings quoted and objects as JSON; ask for JSON.stringify so
	// the result is one level of quoting no matter what the handler returned.
	// Plain eval() forbids top-level await, so wrap in an async IIFE — the
	// host's settle() awaits the returned promise.
	js := fmt.Sprintf(`(async()=>JSON.stringify(await window.__webbrainRun(%q, %s, %d)))()`, action, args, int(timeout.Milliseconds()))
	raw, err := s.Eval(ctx, js, timeout+10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("webbrain: %s: %w", action, err)
	}
	var payload string
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("webbrain: %s: decode response envelope: %w (raw=%s)", action, err, raw)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return nil, fmt.Errorf("webbrain: %s: decode handler payload: %w (raw=%s)", action, err, payload)
	}
	return out, nil
}
