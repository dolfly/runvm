// Package doctor answers "will this work on this machine?" with probes that
// actually boot each backend, because the failure modes here (missing
// libunicorn, missing WebKitGTK, a missing bionic sysroot) are runtime
// discoveries that a version check cannot predict.
package doctor

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/dolfly/runvm/internal/android"
	"github.com/dolfly/runvm/internal/h5"
)

// Check is one probe result.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// Report is the whole environment verdict.
type Report struct {
	Version string  `json:"version"`
	Go      string  `json:"go"`
	OS      string  `json:"os"`
	Arch    string  `json:"arch"`
	Checks  []Check `json:"checks"`
}

// OK reports whether every check passed.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// Run probes the environment. It boots a real emulator and a real page, so it
// takes a second or two; that is the point.
func Run(version string) Report {
	r := Report{Version: version, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}

	assets := android.AssetsDir()
	r.Checks = append(r.Checks, Check{
		Name:   "bionic sysroot",
		OK:     android.AssetsPresent(assets),
		Detail: assets,
		Hint:   "set RUNVM_ASSETS to golem's assets directory (assets/android/sdk23)",
	})

	if ok := android.AssetsPresent(assets); ok {
		start := time.Now()
		sess, err := android.Open("", android.OpenOptions{})
		switch {
		case err == nil:
			_ = sess.Close()
			r.Checks = append(r.Checks, Check{Name: "cpu engine", OK: true,
				Detail: fmt.Sprintf("booted bionic in %s", time.Since(start).Round(time.Millisecond))})
		default:
			// Booting with no main library is the shallowest possible test:
			// if this fails, the engine itself is unavailable.
			r.Checks = append(r.Checks, Check{Name: "cpu engine", OK: false, Detail: err.Error(),
				Hint: engineHint()})
		}
	}

	start := time.Now()
	page, err := h5.Open("about:blank", h5.OpenOptions{Ephemeral: true, ReadyTimeout: 20 * time.Second})
	if err != nil {
		r.Checks = append(r.Checks, Check{Name: "webview", OK: false, Detail: err.Error(),
			Hint: "Linux needs WebKitGTK and Xvfb: apt install libwebkit2gtk-4.1-0 xvfb libgl1-mesa-dri"})
	} else {
		_, evalErr := page.Eval(context.Background(), "1 + 1", 5*time.Second)
		_ = page.Close()
		c := Check{Name: "webview", OK: evalErr == nil,
			Detail: fmt.Sprintf("about:blank ready in %s", time.Since(start).Round(time.Millisecond))}
		if evalErr != nil {
			c.Detail = evalErr.Error()
			c.Hint = "the page host started but JavaScript evaluation failed"
		}
		r.Checks = append(r.Checks, c)
	}
	return r
}

// Print renders the report for a terminal.
// engineHint names the install for the platform instead of guessing.
func engineHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install unicorn, or set GOLEM_UNICORN to libunicorn.dylib"
	case "windows":
		return "set GOLEM_UNICORN to unicorn.dll (golem ships one under assets\\windows\\<arch>)"
	default:
		return "apt install libunicorn2, or set GOLEM_UNICORN to libunicorn.so.2"
	}
}

func (r Report) Print(w io.Writer) {
	status := "ok"
	if !r.OK() {
		status = "degraded"
	}
	fmt.Fprintf(w, "runvm %s — go %s, %s/%s — %s\n\n", r.Version, r.Go, r.OS, r.Arch, status)
	for _, c := range r.Checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		fmt.Fprintf(w, " %s %-16s %s\n", mark, c.Name, c.Detail)
		if !c.OK && c.Hint != "" {
			fmt.Fprintf(w, "   %-16s hint: %s\n", "", c.Hint)
		}
	}
}
