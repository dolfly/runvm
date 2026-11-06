// Package tools wires the MCP surface to the two backends.
//
// Every tool is thin on purpose: validate arguments, call one backend method,
// return JSON. Policy that matters for correctness (locking, lifetime,
// parsing) lives in the backends, so a second transport could reuse it.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/dolfly/runvm/internal/android"
	"github.com/dolfly/runvm/internal/h5"
	"github.com/dolfly/runvm/internal/mcp"
	"github.com/dolfly/runvm/internal/session"
)

// Set holds the live sessions behind the tools.
type Set struct {
	Version string
	Android *session.Store[*android.Session]
	H5      *session.Store[*h5.Session]
}

// New builds the MCP server and the session set it operates on.
func New(version string) (*mcp.Server, *Set) {
	s := &Set{
		Version: version,
		Android: session.New[*android.Session]("android"),
		H5:      session.New[*h5.Session]("h5"),
	}
	srv := mcp.NewServer("runvm", version)

	srv.Add(mcp.Tool{
		Name:        "runvm_status",
		Description: "Report which backends are live and how many sessions are open. Cheap: does not boot anything.",
		InputSchema: object(nil),
		Handler:     s.status,
	})

	srv.Add(mcp.Tool{
		Name: "android_open",
		Description: "Load an Android native library (.so) into a golem emulator and return a session id. " +
			"Runs the library's DT_INIT/init_array and JNI_OnLoad, exactly as a device would on load. " +
			"Use android_symbols afterwards to see what is callable.",
		InputSchema: object(map[string]any{
			"so":     str("Path to the .so on this machine (AArch64, ARMv7 or x86_64)"),
			"engine": str(`CPU backend: "unicorn" or empty for the default`),
			"dex":    str("Optional classes.dex for real class/method metadata in JNI lookups"),
			"pool": num("Engines to warm, 1-16 (default 1). Above 1, calls to this session run in parallel " +
				"on separate engines that share the library's read-only pages"),
			"max_uses": num("Recycle an engine after this many calls (default 0: never)"),
		}, "so"),
		Handler: s.androidOpen,
	})

	srv.Add(mcp.Tool{
		Name:        "android_symbols",
		Description: "List the exported functions of a loaded library. Reads the ELF directly, so it also works before any call.",
		InputSchema: object(map[string]any{
			"session": str("Session id from android_open"),
			"filter":  str("Case-insensitive substring filter on the symbol name"),
			"limit":   num("Maximum symbols to return (default 200)"),
		}, "session"),
		Handler: s.androidSymbols,
	})

	srv.Add(mcp.Tool{
		Name: "android_hook",
		Description: "Replace an exported function with one that returns a constant, so a dependency can be " +
			"stubbed out while the rest of the library still runs for real. Re-hooking a symbol with a " +
			"different value is refused: open a new session instead.",
		InputSchema: object(map[string]any{
			"session": str("Session id from android_open"),
			"symbol":  str("Exported function to replace"),
			"returns": num("Constant the stub returns"),
		}, "session", "symbol", "returns"),
		Handler: s.androidHook,
	})

	srv.Add(mcp.Tool{
		Name: "android_java_stub",
		Description: "Answer a Java callback the library makes through JNI, instead of needing a real Android " +
			"runtime. Give the Java class and method and the value to return: a string answers object calls, " +
			"a number answers int/long/boolean calls, null answers 'no such object'.",
		InputSchema: object(map[string]any{
			"session": str("Session id from android_open"),
			"class":   str(`Java class, e.g. "java/lang/System" or "android.content.Context"`),
			"method":  str(`Method name, e.g. "currentTimeMillis" (a full signature works too)`),
			"returns": map[string]any{"description": "Value the method returns: string, number, boolean or null",
				"type": []string{"string", "number", "boolean", "null"}},
		}, "session", "class", "method"),
		Handler: s.androidJavaStub,
	})

	srv.Add(mcp.Tool{
		Name:        "android_java_stubs",
		Description: "List the Java callbacks this session answers.",
		InputSchema: object(map[string]any{"session": str("Session id from android_open")}, "session"),
		Handler:     s.androidJavaStubs,
	})

	srv.Add(mcp.Tool{
		Name: "android_call",
		Description: "Call an exported function and return its value. Arguments are integers, strings (char*), " +
			`or {"buffer": n} for an out-parameter of n bytes that is read back after the call. Up to 8 arguments.`,
		InputSchema: object(map[string]any{
			"session": str("Session id from android_open"),
			"symbol":  str("Exported function name"),
			"args": array("Arguments in ABI order", map[string]any{
				"type": []string{"number", "string", "object"},
			}),
		}, "session", "symbol"),
		Handler: s.androidCall,
	})

	srv.Add(mcp.Tool{
		Name:        "android_close",
		Description: "Tear down an android session and free its emulator.",
		InputSchema: object(map[string]any{"session": str("Session id from android_open")}, "session"),
		Handler:     s.androidClose,
	})

	srv.Add(mcp.Tool{
		Name: "h5_open",
		Description: "Open a page in a real WebKit view (headless, unthrottled) and return a session id. " +
			"The target may be a URL, a local file path, or about:blank. Uses webframe underneath.",
		InputSchema: object(map[string]any{
			"target": str(`URL, file path, or "about:blank"`),
			"env": objectFields(map[string]any{"type": "string"},
				"Strings exposed to the page as webframe.env (before any page script)"),
			"ephemeral": boolean("Keep cookies, storage and cache in memory only (default true)"),
			"visible":   boolean("Draw the page in a window instead of offscreen; required for h5_screenshot"),
			"width":     num("Window width when visible (default 1280)"),
			"height":    num("Window height when visible (default 800)"),
		}, "target"),
		Handler: s.h5Open,
	})

	srv.Add(mcp.Tool{
		Name: "h5_eval",
		Description: "Evaluate JavaScript in the page and return the value. Promises are awaited; " +
			"strings print quoted, objects print as JSON. Use it to drive the page and assert on the result.",
		InputSchema: object(map[string]any{
			"session":    str("Session id from h5_open"),
			"js":         str("JavaScript to evaluate (an expression or a statement block)"),
			"timeout_ms": num("How long to wait for the result (default 30000)"),
		}, "session", "js"),
		Handler: s.h5Eval,
	})

	srv.Add(mcp.Tool{
		Name:        "h5_console",
		Description: "Return buffered console output and page exceptions from the running page, oldest first.",
		InputSchema: object(map[string]any{
			"session": str("Session id from h5_open"),
			"clear":   boolean("Clear the buffer after reading (default true)"),
		}, "session"),
		Handler: s.h5Console,
	})

	srv.Add(mcp.Tool{
		Name: "h5_screenshot",
		Description: "Capture the page as a PNG and return it as an image. The session must have been opened " +
			"with visible=true on Linux, where the host draws in a private Xvfb it started itself.",
		InputSchema: object(map[string]any{
			"session": str("Session id from h5_open (opened with visible: true)"),
			"path":    str("Optional file path to also write the PNG to"),
		}, "session"),
		Handler: s.h5Screenshot,
	})

	srv.Add(mcp.Tool{
		Name:        "h5_close",
		Description: "Close a page session and stop its host process.",
		InputSchema: object(map[string]any{"session": str("Session id from h5_open")}, "session"),
		Handler:     s.h5Close,
	})

	return srv, s
}

// CloseAll releases every live session; call it on shutdown.
func (s *Set) CloseAll() {
	for _, a := range s.Android.CloseAll() {
		_ = a.Close()
	}
	for _, w := range s.H5.CloseAll() {
		_ = w.Close()
	}
}

func (s *Set) status(context.Context, json.RawMessage) (any, error) {
	assets := android.AssetsDir()
	return map[string]any{
		"version": s.Version,
		"go":      runtime.Version(),
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
		"android": map[string]any{
			"sessions":  s.Android.Len(),
			"assets":    assets,
			"assets_ok": android.AssetsPresent(assets),
		},
		"h5": map[string]any{
			"sessions": s.H5.Len(),
		},
		"sessions": map[string]any{
			"android": s.androidDetails(),
			"h5":      s.H5.IDs(),
		},
	}, nil
}

type androidOpenArgs struct {
	SO      string `json:"so"`
	Engine  string `json:"engine"`
	DEX     string `json:"dex"`
	Pool    int    `json:"pool"`
	MaxUses int64  `json:"max_uses"`
}

func (s *Set) androidDetails() []map[string]any {
	ids := s.Android.IDs()
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		sess, ok := s.Android.Get(id)
		if !ok {
			continue
		}
		st := sess.Stats()
		entry := map[string]any{
			"session": id,
			"pool":    sess.PoolSize(),
			"engines": map[string]any{
				"in_use":  st.InUse,
				"idle":    st.Idle,
				"created": sess.EnginesCreated(),
			},
		}
		if hooks := sess.Hooks(); len(hooks) > 0 {
			entry["hooks"] = hooks
		}
		out = append(out, entry)
	}
	return out
}

func (s *Set) androidOpen(_ context.Context, raw json.RawMessage) (any, error) {
	var a androidOpenArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.Engine == "" {
		a.Engine = os.Getenv("RUNVM_ENGINE")
	}
	info, infoErr := android.Describe(a.SO)
	exports, exportsErr := android.Exports(a.SO)

	sess, err := android.Open(a.SO, android.OpenOptions{
		Engine:  a.Engine,
		DEX:     a.DEX,
		Pool:    a.Pool,
		MaxUses: a.MaxUses,
	})
	if err != nil {
		return nil, err
	}
	id := s.Android.Add(sess)

	out := map[string]any{
		"session": id,
		"path":    a.SO,
		"engine":  sess.Engine(),
		"pool":    sess.PoolSize(),
	}
	if infoErr == nil {
		out["arch"] = info.Arch
		out["kind"] = info.Kind
		out["bytes"] = info.Bytes
	}
	if exportsErr == nil {
		out["export_count"] = len(exports)
		out["exports_preview"] = preview(exports, 10)
	}
	return out, nil
}

func (s *Set) androidSymbols(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Filter  string `json:"filter"`
		Limit   int    `json:"limit"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.androidSession(a.Session)
	if err != nil {
		return nil, err
	}
	all, err := android.Exports(sess.Path())
	if err != nil {
		return nil, fmt.Errorf("android_symbols: %w", err)
	}
	matched := android.SymbolFilter(all, a.Filter)
	limit := a.Limit
	if limit <= 0 {
		limit = 200
	}
	out := map[string]any{"total": len(matched), "symbols": preview(matched, limit)}
	if len(matched) > limit {
		out["truncated"] = true
	}
	return out, nil
}

func (s *Set) androidCall(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Symbol  string `json:"symbol"`
		Args    []any  `json:"args"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.androidSession(a.Session)
	if err != nil {
		return nil, err
	}
	args, err := android.ParseArgs(a.Args)
	if err != nil {
		return nil, fmt.Errorf("android_call: %w", err)
	}
	res, err := sess.Call(ctx, a.Symbol, args)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"symbol": a.Symbol,
		"value":  res.Value,
		"hex":    android.Hex(res.Value),
		"i32":    int32(uint32(res.Value)),
	}
	if len(res.Buffers) > 0 {
		out["buffers"] = res.Buffers
	}
	return out, nil
}

func (s *Set) androidHook(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string  `json:"session"`
		Symbol  string  `json:"symbol"`
		Returns float64 `json:"returns"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.androidSession(a.Session)
	if err != nil {
		return nil, err
	}
	if a.Returns < 0 || a.Returns != float64(uint64(a.Returns)) {
		return nil, fmt.Errorf("android_hook: returns must be a non-negative integer")
	}
	if err := sess.Hook(a.Symbol, uint64(a.Returns)); err != nil {
		return nil, err
	}
	return map[string]any{"hooked": a.Symbol, "returns": uint64(a.Returns), "hooks": sess.Hooks()}, nil
}

func (s *Set) androidJavaStub(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Class   string `json:"class"`
		Method  string `json:"method"`
		Returns any    `json:"returns"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.androidSession(a.Session)
	if err != nil {
		return nil, err
	}
	st, err := sess.JavaStub(a.Class, a.Method, a.Returns)
	if err != nil {
		return nil, fmt.Errorf("android_java_stub: %w", err)
	}
	return map[string]any{"stub": st.Class + "->" + st.Method, "returns": st.String()}, nil
}

func (s *Set) androidJavaStubs(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.androidSession(a.Session)
	if err != nil {
		return nil, err
	}
	return map[string]any{"stubs": sess.JavaStubs()}, nil
}

func (s *Set) androidClose(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, ok := s.Android.Delete(a.Session)
	if !ok {
		return nil, fmt.Errorf("android_close: unknown session %q", a.Session)
	}
	if err := sess.Close(); err != nil {
		return nil, fmt.Errorf("android_close: %w", err)
	}
	return map[string]any{"closed": a.Session}, nil
}

func (s *Set) androidSession(id string) (*android.Session, error) {
	sess, ok := s.Android.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown android session %q (open one with android_open)", id)
	}
	return sess, nil
}

func (s *Set) h5Open(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Target    string            `json:"target"`
		Env       map[string]string `json:"env"`
		Ephemeral *bool             `json:"ephemeral"`
		Visible   *bool             `json:"visible"`
		Width     int               `json:"width"`
		Height    int               `json:"height"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	ephemeral := true
	if a.Ephemeral != nil {
		ephemeral = *a.Ephemeral
	}
	sess, err := h5.Open(a.Target, h5.OpenOptions{
		Env:       a.Env,
		Ephemeral: ephemeral,
		Visible:   a.Visible != nil && *a.Visible,
		Width:     a.Width,
		Height:    a.Height,
	})
	if err != nil {
		return nil, err
	}
	id := s.H5.Add(sess)
	return map[string]any{"session": id, "target": sess.Target()}, nil
}

func (s *Set) h5Eval(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session   string `json:"session"`
		JS        string `json:"js"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.h5Session(a.Session)
	if err != nil {
		return nil, err
	}
	result, err := sess.Eval(ctx, a.JS, time.Duration(a.TimeoutMS)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result}, nil
}

func (s *Set) h5Console(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Clear   *bool  `json:"clear"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.h5Session(a.Session)
	if err != nil {
		return nil, err
	}
	clear := true
	if a.Clear != nil {
		clear = *a.Clear
	}
	lines := sess.Console(clear)
	return map[string]any{"count": len(lines), "lines": lines}, nil
}

func (s *Set) h5Screenshot(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Path    string `json:"path"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.h5Session(a.Session)
	if err != nil {
		return nil, err
	}
	png, err := sess.Screenshot()
	if err != nil {
		return nil, err
	}
	caption := fmt.Sprintf("%s — %d bytes PNG", sess.Target(), len(png))
	if a.Path != "" {
		if err := os.WriteFile(a.Path, png, 0o644); err != nil {
			return nil, fmt.Errorf("h5_screenshot: write %q: %w", a.Path, err)
		}
		caption = fmt.Sprintf("%s — saved to %s (%d bytes)", sess.Target(), a.Path, len(png))
	}
	return mcp.Image{MimeType: "image/png", Data: png, Caption: caption}, nil
}

func (s *Set) h5Close(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, ok := s.H5.Delete(a.Session)
	if !ok {
		return nil, fmt.Errorf("h5_close: unknown session %q", a.Session)
	}
	if err := sess.Close(); err != nil {
		return nil, fmt.Errorf("h5_close: %w", err)
	}
	return map[string]any{"closed": a.Session}, nil
}

func (s *Set) h5Session(id string) (*h5.Session, error) {
	sess, ok := s.H5.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown h5 session %q (open one with h5_open)", id)
	}
	return sess, nil
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func preview[T any](in []T, n int) []T {
	if len(in) < n {
		return in
	}
	return in[:n]
}

// JSON Schema helpers. Hand-written so the schemas stay readable and the
// server carries no schema-library dependency.
func object(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func objectFields(value map[string]any, desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc, "additionalProperties": value}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func array(desc string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": items}
}
