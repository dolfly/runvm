// Package tools wires the MCP surface to the two backends.
//
// Every tool is thin on purpose: validate arguments, call one backend method,
// return JSON. Policy that matters for correctness (locking, lifetime,
// parsing) lives in the backends, so a second transport could reuse it.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dolfly/runvm/internal/android"
	"github.com/dolfly/runvm/internal/h5"
	"github.com/dolfly/runvm/internal/ios"
	"github.com/dolfly/runvm/internal/mcp"
	"github.com/dolfly/runvm/internal/resources"
	"github.com/dolfly/runvm/internal/session"
)

// Limits keeps one forgotten session from becoming a leaked emulator.
type Limits struct {
	// MaxSessions caps android+ios+h5 sessions together; 0 means unlimited.
	MaxSessions int
	// IdleTimeout closes sessions unused for this long; 0 disables reaping.
	IdleTimeout time.Duration
}

// Set holds the live sessions behind the tools.
type Set struct {
	Version string
	Limits  Limits
	Android *session.Store[*android.Session]
	IOS     *session.Store[*ios.Session]
	H5      *session.Store[*h5.Session]
	// Resources is the writable resource store behind runvm://files/ URIs and
	// the runvm_upload / runvm_write tools. It may be nil, in which case the
	// upload/write tools answer with a clear error instead of pretending to
	// succeed.
	Resources *resources.Store
}

// New builds the MCP server and the session set it operates on.
func New(version string, limits Limits) (*mcp.Server, *Set) {
	s := &Set{
		Version: version,
		Limits:  limits,
		Android: session.New[*android.Session]("android"),
		IOS:     session.New[*ios.Session]("ios"),
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
		Name: "runvm_upload",
		Description: "Copy a local file into the writable resource store (runvm://files/<path>). " +
			"Returns the runvm:// URI, byte count and sha256. Fails with a clear error if the " +
			"path escapes the resources root, is a symlink, or the file exceeds the size limit " +
			"(RUNVM_RESOURCES_MAX_SIZE, default 64 MiB) — it never silently truncates.",
		InputSchema: object(map[string]any{
			"path":   str("Path to the local file on this machine to upload"),
			"target": str("Optional relative path/name under runvm://files/ (default: the source file's base name)"),
		}, "path"),
		Handler: s.upload,
	})

	srv.Add(mcp.Tool{
		Name: "runvm_write",
		Description: "Write or overwrite a resource under runvm://files/<path> from text (content) or " +
			"base64-encoded binary (base64). Exactly one of content or base64 must be given. Returns the " +
			"runvm:// URI, byte count and sha256. Same path/size safety as runvm_upload.",
		InputSchema: object(map[string]any{
			"target":  str("Relative path/name under runvm://files/ to write"),
			"content": str("Text content to store (mutually exclusive with base64)"),
			"base64":  str("Base64-encoded binary content to store (mutually exclusive with content)"),
		}, "target"),
		Handler: s.write,
	})

	srv.Add(mcp.Tool{
		Name: "android_open",
		Description: "Load an Android native library (.so) into a golem emulator and return a session id. " +
			"Runs the library's DT_INIT/init_array and JNI_OnLoad, exactly as a device would on load. " +
			"Use android_symbols afterwards to see what is callable. A Mach-O dylib is routed to the " +
			"iOS path automatically (ios_open/ios_call), because it is a different container format.",
		InputSchema: object(map[string]any{
			"so":     str("Path to the .so on this machine (AArch64, ARMv7 or x86_64). A Mach-O dylib also works and yields an ios-N session"),
			"engine": str(`CPU backend: "unicorn" or empty for the default`),
			"dex":    str("Optional classes.dex for real class/method metadata in JNI lookups"),
			"pool": num("Engines to warm, 1-16 (default 1). Above 1, calls to this session run in parallel " +
				"on separate engines that share the library's read-only pages"),
			"max_uses": num("Recycle an engine after this many calls (default 0: never)"),
		}, "so"),
		Handler: s.androidOpen,
	})

	srv.Add(mcp.Tool{
		Name: "ios_open",
		Description: "Load an iOS/Darwin dylib (Mach-O, arm64) into a golem emulator and return a session id. " +
			"No Java runtime, no JNI, no bionic sysroot: the Darwin personality is chosen from the file's " +
			"magic. Only symbols the dylib defines itself resolve. An imported name (libSystem, Foundation, " +
			"Objective-C) has no implementation here: calling it directly fails with 'imported but has no " +
			"implementation', while a call the dylib makes to it internally gets golem's optimistic stub, " +
			"which answers 0. ios_symbols and ios_imports list both sides before you call.",
		InputSchema: object(map[string]any{
			"dylib": str("Path to the Mach-O arm64 dylib on this machine. It must use non-lazy binds " +
				"(clang -Wl,-bind_at_load): the default lazy-bind form is refused, because runvm does not " +
				"emulate dyld's stub binder"),
			"engine":   str(`CPU backend: "unicorn" or empty for the default`),
			"pool":     num("Engines to warm, 1-16 (default 1). Above 1, calls run in parallel on separate engines"),
			"max_uses": num("Recycle an engine after this many calls (default 0: never)"),
		}, "dylib"),
		Handler: s.iosOpen,
	})

	srv.Add(mcp.Tool{
		Name: "ios_symbols",
		Description: "List the symbols an iOS dylib exports and imports. Exports are callable with ios_call. " +
			"Imports are the names libSystem/ObjC would have to provide: calling one directly reports " +
			"'imported but has no implementation', while a call the dylib makes to one internally gets " +
			"golem's optimistic stub (0). The file is read from disk rather than through the running " +
			"emulator, but the session is still what names it — after ios_close there is no path to read.",
		InputSchema: object(map[string]any{
			"session": str("Session id from ios_open"),
			"filter":  str("Case-insensitive substring filter on the symbol name"),
			"limit":   num("Maximum symbols to return per section (default 200)"),
		}, "session"),
		Handler: s.iosSymbols,
	})

	srv.Add(mcp.Tool{
		Name: "ios_imports",
		Description: "List the symbols an iOS dylib imports — what libSystem, Foundation or Objective-C would " +
			"have to provide for the library to be fully functional. runvm supplies none of them: calling one " +
			"of these names directly returns an error, and a call the dylib makes to one internally gets " +
			"golem's optimistic stub (0). Use this to explain a result that looks wrong.",
		InputSchema: object(map[string]any{
			"session": str("Session id from ios_open"),
		}, "session"),
		Handler: s.iosImports,
	})

	srv.Add(mcp.Tool{
		Name: "ios_call",
		Description: "Call an exported symbol of an iOS dylib and return its value. Arguments are integers, " +
			"strings (char*) or {\"buffer\": n} for an out-parameter read back after the call. Up to 8. " +
			"Calling an imported name reports that explicitly instead of returning a number. If the function " +
			"is defined in the dylib but calls an unimplemented import itself, that import answers 0 — " +
			"golem's optimistic stub, not an error; ios_imports lists what is missing.",
		InputSchema: object(map[string]any{
			"session": str("Session id from ios_open"),
			"symbol":  str("Exported function name, without the leading underscore"),
			"args": array("Arguments in ABI order", map[string]any{
				"type": []string{"number", "string", "object"},
			}),
		}, "session", "symbol"),
		Handler: s.iosCall,
	})

	srv.Add(mcp.Tool{
		Name:        "ios_close",
		Description: "Tear down an iOS session and free its engines.",
		InputSchema: object(map[string]any{"session": str("Session id from ios_open")}, "session"),
		Handler:     s.iosClose,
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
			"visible":   boolean("Draw the page in a window instead of offscreen; required for h5_screenshot on Linux/Windows"),
			"webbrain":  boolean("Inject WebBrain's page-control scripts (accessibility tree + DOM actions) after load, so the webbrain tool can drive the page"),
			"width":     num("View width in points (default 1280 when visible). Also the page width of an h5_pdf print, so a hidden 1280x720 view prints a 16:9 document"),
			"height":    num("View height in points (default 800 when visible)"),
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
		Name: "h5_wait_for",
		Description: "Poll the page until a JavaScript expression is truthy (or a selector matches), then " +
			"return the value. This is the tool for asynchronous UI: wait for the result to appear instead " +
			"of sleeping and hoping.",
		InputSchema: object(map[string]any{
			"session":    str("Session id from h5_open"),
			"expression": str("JavaScript expression polled until truthy, e.g. document.querySelector('#done')"),
			"selector":   str("CSS selector; shorthand for expression=document.querySelector(<selector>)"),
			"timeout_ms": num("How long to wait (default 10000)"),
		}, "session"),
		Handler: s.h5WaitFor,
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
		Name: "h5_pdf",
		Description: "Render the page into a single-page PDF and return its path. On macOS this is a real " +
			"printed PDF (vector: text stays text, the whole scrollable page is included, nothing is cut " +
			"off at the fold, and the page does not have to be visible). On Linux and Windows it is a " +
			"raster PDF, because those WebView backends expose no print API yet; the result says which. " +
			"The raster path needs visible=true on Linux.",
		InputSchema: object(map[string]any{
			"session": str("Session id from h5_open (opened with visible: true)"),
			"path":    str("Where to write the PDF (default: <target-slug>.pdf in the working directory)"),
		}, "session"),
		Handler: s.h5PDF,
	})

	srv.Add(mcp.Tool{
		Name:        "h5_close",
		Description: "Close a page session and stop its host process.",
		InputSchema: object(map[string]any{"session": str("Session id from h5_open")}, "session"),
		Handler:     s.h5Close,
	})

	srv.Add(mcp.Tool{
		Name: "webbrain",
		Description: "Drive a page with WebBrain's own page-control scripts (accessibility tree + DOM actions), " +
			"loaded from assets/h5/webbrain when the session was opened with \"webbrain\": true. " +
			"This is the same perception/action stack the WebBrain browser-extension agent uses: " +
			"the page is read as a flat accessibility tree with stable ref_id references, and actions " +
			"target those ref_ids (or viewport coordinates / visible text). Actions: " +
			"page (title/url/meta), tree (accessibility tree text), interactive (clickable/typeable elements), " +
			"snapshot (page + compact tree in one call), click, type, press, scroll, check. " +
			"Prefer ref_id from tree/interactive for precision; coordinates and text matching are fallbacks.",
		InputSchema: object(map[string]any{
			"session":    str("Session id from h5_open (opened with webbrain: true)"),
			"action":     str("page | tree | interactive | snapshot | click | type | press | scroll | check"),
			"ref_id":     str("Stable ref_id from a tree/interactive/snapshot read; for click/type/scroll/check"),
			"text":       str("Text to type (type) or to click by visible text (click with text_match)"),
			"clear":      boolean("Clear the field before typing (type)"),
			"filter":     str("Tree filter: all | visible | interactive (tree, default all)"),
			"max_depth":  num("Tree depth cap (tree, default 15)"),
			"max_chars":  num("Tree output cap in chars (tree/snapshot, default 30000)"),
			"x":          num("Viewport x (click/scroll)"),
			"y":          num("Viewport y (click/scroll)"),
			"selector":   str("CSS selector to click (click; no :contains()/:has-text())"),
			"text_match": str("exact | contains — how text matches a click target (click, default exact)"),
			"key":        str("Key to press: Escape, Tab, Enter, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, ; (press)"),
			"repeat":     num("Key repeat, 1-3 (press, default 1)"),
			"checked":    boolean("Target checked state (check)"),
			"timeout_ms": num("Action timeout (default 60000)"),
		}, "session", "action"),
		Handler: s.webbrainTool,
	})

	s.registerCatalog(srv)
	return srv, s
}

// registerCatalog adds the read-only surface: live state as resources, and the
// two workflows people actually run (test a page, poke a native library) as
// prompts. Both are optional in MCP, so they are registered after the tools
// and advertised only because they exist.
func (s *Set) registerCatalog(srv *mcp.Server) {
	srv.AddResource(mcp.Resource{
		URI:         "runvm://tools",
		Name:        "Tool catalog",
		Description: "Every tool this server exposes, with its input schema, as JSON.",
		MimeType:    "application/json",
		Read: func(context.Context) (any, error) {
			tools := make([]map[string]any, 0)
			for _, t := range srv.Tools() {
				tools = append(tools, map[string]any{
					"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema,
				})
			}
			return map[string]any{"tools": tools}, nil
		},
	})

	srv.AddResource(mcp.Resource{
		URI:         "runvm://status",
		Name:        "Server status",
		Description: "Backends, limits and live sessions.",
		MimeType:    "application/json",
		Read: func(ctx context.Context) (any, error) {
			return s.status(ctx, nil)
		},
	})

	// Writable resources appear only when the store is configured and a file
	// has been uploaded or written. Each entry reads from disk on demand, so
	// resources/read always reflects the latest overwrite.
	srv.AddResourceLister(func() []mcp.Resource {
		if s.Resources == nil {
			return nil
		}
		metas, err := s.Resources.List()
		if err != nil {
			return nil
		}
		out := make([]mcp.Resource, 0, len(metas))
		for _, m := range metas {
			name := m.Name
			out = append(out, mcp.Resource{
				URI:         "runvm://files/" + name,
				Name:        "Resource " + name,
				Description: fmt.Sprintf("%d bytes, modified %s", m.Size, m.ModTime.Format(time.RFC3339)),
				MimeType:    m.MimeType,
				Size:        m.Size,
				Read: func(context.Context) (any, error) {
					data, err := s.Resources.Read(name)
					if err != nil {
						return nil, err
					}
					if resources.IsText(data) {
						return string(data), nil
					}
					return data, nil
				},
			})
		}
		return out
	})

	// Screenshots appear in the catalog only after one has been taken.
	srv.AddResourceLister(func() []mcp.Resource {
		out := make([]mcp.Resource, 0)
		for _, id := range s.H5.IDs() {
			sess, ok := s.H5.Get(id)
			if !ok {
				continue
			}
			if _, _, has := sess.LastScreenshot(); !has {
				continue
			}
			sessionID := id
			out = append(out, mcp.Resource{
				URI:         "runvm://screenshot/" + sessionID,
				Name:        "Screenshot " + sessionID,
				Description: "Most recent capture of " + sess.Target(),
				MimeType:    "image/png",
				Read: func(context.Context) (any, error) {
					s, ok := s.H5.Get(sessionID)
					if !ok {
						return nil, fmt.Errorf("screenshot: session %s is gone", sessionID)
					}
					png, _, has := s.LastScreenshot()
					if !has {
						return nil, fmt.Errorf("screenshot: session %s has none", sessionID)
					}
					return png, nil
				},
			})
		}
		return out
	})

	srv.AddResourceLister(func() []mcp.Resource {
		out := make([]mcp.Resource, 0)
		for _, id := range s.H5.IDs() {
			sess, ok := s.H5.Get(id)
			if !ok {
				continue
			}
			if _, has := sess.LastPDF(); !has {
				continue
			}
			sessionID := id
			out = append(out, mcp.Resource{
				URI:         "runvm://pdf/" + sessionID,
				Name:        "PDF export " + sessionID,
				Description: "Most recent PDF export of " + sess.Target(),
				MimeType:    "application/pdf",
				Read: func(context.Context) (any, error) {
					s, ok := s.H5.Get(sessionID)
					if !ok {
						return nil, fmt.Errorf("pdf: session %s is gone", sessionID)
					}
					doc, has := s.LastPDF()
					if !has {
						return nil, fmt.Errorf("pdf: session %s has none", sessionID)
					}
					return doc, nil
				},
			})
		}
		return out
	})

	srv.AddPrompt(mcp.Prompt{
		Name:        "smoke-test-page",
		Description: "Drive a real WebKit page: load it, assert on the DOM, capture a screenshot, close.",
		Arguments: []mcp.PromptArgument{
			{Name: "url", Description: "URL or local file path to test", Required: true},
			{Name: "expectation", Description: "What must be true once the page settles"},
		},
		Build: func(args map[string]string) (string, error) {
			url, err := mcp.RequireArg(args, "url")
			if err != nil {
				return "", err
			}
			want := args["expectation"]
			if want == "" {
				want = "the page's main heading or a key element is present"
			}
			return fmt.Sprintf(`Test %s in a real browser with runvm.

1. h5_open with {"target": %q, "visible": true}
2. h5_wait_for until %q is true (use a selector if it is an element)
3. h5_eval to read the state you care about and assert on it
4. h5_screenshot with a path so the run leaves an artifact
5. h5_console to collect any page errors; report them even if the assertions pass
6. h5_close

Report: what loaded, what the assertions saw, any console errors, the screenshot path.`, url, url, want), nil
		},
	})

	srv.AddPrompt(mcp.Prompt{
		Name:        "inspect-native-lib",
		Description: "Load an Android .so in the emulator, list its exports, and call one.",
		Arguments: []mcp.PromptArgument{
			{Name: "so", Description: "Path to the .so", Required: true},
			{Name: "symbol", Description: "Function to call (default: pick a simple one)"},
		},
		Build: func(args map[string]string) (string, error) {
			so, err := mcp.RequireArg(args, "so")
			if err != nil {
				return "", err
			}
			symbol := args["symbol"]
			if symbol == "" {
				symbol = "(pick a small function from the export list)"
			}
			return fmt.Sprintf(`Inspect the native library %s with runvm.

1. android_open with {"so": %q}
2. android_symbols to see what is callable; say how many exports there are
3. android_call %s with plausible arguments; pass {"buffer": n} for out-parameters
4. If a result is surprising, android_hook the symbol to a constant and call it again to
   confirm the call path rather than the implementation
5. android_close

Report: engine, export count, the call you made, the value, and anything that looked wrong.`, so, so, symbol), nil
		},
	})

	srv.AddPrompt(mcp.Prompt{
		Name:        "webbrain-automate",
		Description: "Plan and run a multi-step browser automation task with the webbrain tool (perceive, act, verify).",
		Arguments: []mcp.PromptArgument{
			{Name: "task", Description: "What to accomplish on the page, in plain language", Required: true},
			{Name: "url", Description: "Target URL to open first (optional: use the current h5 session)"},
			{Name: "max_steps", Description: "Act/verify loop cap before reporting; pass a string like \"6\" (default \"6\")"},
		},
		Build: func(args map[string]string) (string, error) {
			task, err := mcp.RequireArg(args, "task")
			if err != nil {
				return "", err
			}
			url := args["url"]
			steps := args["max_steps"]
			if steps == "" {
				steps = "6"
			}
			open := ""
			if url != "" {
				open = fmt.Sprintf("1. Open the page: h5_open with {\"target\": %q, \"webbrain\": true, \"visible\": false}. Keep the returned session id for every later call.\n", url)
			} else {
				open = "1. If no h5 session exists yet, h5_open with {\"webbrain\": true} first; otherwise reuse the open session.\n"
			}
			return fmt.Sprintf(`Complete this browser automation task with runvm's webbrain tool.

Task: %s

%s2. Perceive: webbrain snapshot (or tree with filter "interactive") to learn the page's
   structure and get stable ref_ids. If the page is still loading, h5_wait_for a selector
   first. Re-read when the page changes.
3. Plan in your head: list the concrete actions needed, in order, each addressed by a
   ref_id from the last read (fall back to text/coordinates only when no ref_id fits).
4. Act one step at a time: webbrain click / type / press / scroll / check. After an action
   that changes the page, re-snapshot or re-read the tree before the next action.
5. Verify: check the outcome (page info, a fresh tree read, or h5_eval) before declaring
   success. If a ref_id is stale or missing, re-read the tree and retry with a fresh id.
6. Loop steps 3-5 up to %s times, then report.
7. Close: h5_close the session when done.

Report: what you did at each step (action + ref_id + result), what you verified, and
whether the task is complete. Keep every intermediate read small (max_chars) so the
conversation stays light.`, task, open, steps), nil
		},
	})
}

// reserved reports whether another session would exceed the configured cap.
func (s *Set) reserved() error {
	if s.Limits.MaxSessions <= 0 {
		return nil
	}
	if n := s.Android.Len() + s.IOS.Len() + s.H5.Len(); s.Limits.MaxSessions > 0 && n >= s.Limits.MaxSessions {
		return fmt.Errorf("session limit reached (%d); close one with android_close/ios_close/h5_close, "+
			"or raise RUNVM_MAX_SESSIONS", s.Limits.MaxSessions)
	}
	return nil
}

// StartReaper closes sessions left idle for longer than the configured
// timeout. A model that forgets to close a page or an emulator should cost a
// timer, not a leak, in a server that stays up for days.
func (s *Set) StartReaper(ctx context.Context) {
	if s.Limits.IdleTimeout <= 0 {
		return
	}
	interval := s.Limits.IdleTimeout / 2
	if interval < time.Second {
		interval = time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.reap(now)
			}
		}
	}()
}

// reap closes sessions idle past the cutoff and reports how many it closed.
func (s *Set) reap(now time.Time) int {
	if s.Limits.IdleTimeout <= 0 {
		return 0
	}
	cutoff := now.Add(-s.Limits.IdleTimeout)
	closed := 0
	for _, id := range s.Android.IDs() {
		sess, ok := s.Android.Get(id)
		if !ok || !sess.LastUsed().Before(cutoff) {
			continue
		}
		if _, gone := s.Android.Delete(id); gone {
			_ = sess.Close()
			closed++
		}
	}
	for _, id := range s.IOS.IDs() {
		sess, ok := s.IOS.Get(id)
		if !ok || !sess.LastUsed().Before(cutoff) {
			continue
		}
		if _, gone := s.IOS.Delete(id); gone {
			_ = sess.Close()
			closed++
		}
	}
	for _, id := range s.H5.IDs() {
		sess, ok := s.H5.Get(id)
		if !ok || !sess.LastUsed().Before(cutoff) {
			continue
		}
		if _, gone := s.H5.Delete(id); gone {
			_ = sess.Close()
			closed++
		}
	}
	return closed
}

// CloseAll releases every live session; call it on shutdown.
func (s *Set) CloseAll() {
	for _, a := range s.Android.CloseAll() {
		_ = a.Close()
	}
	for _, i := range s.IOS.CloseAll() {
		_ = i.Close()
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
		"ios": map[string]any{
			"sessions": s.IOS.Len(),
			"scope":    "macho arm64 dylibs; no JNI, no bionic, no libSystem",
		},
		"h5": map[string]any{
			"sessions": s.H5.Len(),
		},
		"limits": map[string]any{
			"max_sessions": s.Limits.MaxSessions,
			"idle_timeout": s.Limits.IdleTimeout.String(),
		},
		"sessions": map[string]any{
			"android": s.androidDetails(),
			"ios":     s.IOS.IDs(),
			"h5":      s.H5.IDs(),
		},
	}, nil
}

// upload implements the runvm_upload tool: copy a local file into the writable
// resource store.
func (s *Set) upload(_ context.Context, args json.RawMessage) (any, error) {
	if s.Resources == nil {
		return nil, fmt.Errorf("resource store is not configured; set RUNVM_RESOURCES_DIR and restart")
	}
	var p struct {
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("runvm_upload: %v", err)
	}
	if p.Path == "" {
		return nil, fmt.Errorf("runvm_upload: missing required argument %q", "path")
	}
	m, err := s.Resources.Upload(p.Path, p.Target)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"uri":    m.URI,
		"bytes":  m.Size,
		"sha256": m.Sha256,
		"name":   m.Name,
	}, nil
}

// write implements the runvm_write tool: store text or base64 binary content
// under a writable resource URI.
func (s *Set) write(_ context.Context, args json.RawMessage) (any, error) {
	if s.Resources == nil {
		return nil, fmt.Errorf("resource store is not configured; set RUNVM_RESOURCES_DIR and restart")
	}
	var p struct {
		Target  string  `json:"target"`
		Content *string `json:"content"`
		Base64  *string `json:"base64"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("runvm_write: %v", err)
	}
	if p.Target == "" {
		return nil, fmt.Errorf("runvm_write: missing required argument %q", "target")
	}
	if p.Content != nil && p.Base64 != nil {
		return nil, fmt.Errorf("runvm_write: content and base64 are mutually exclusive")
	}
	var data []byte
	if p.Content != nil {
		data = []byte(*p.Content)
	} else if p.Base64 != nil {
		dec, err := base64.StdEncoding.DecodeString(*p.Base64)
		if err != nil {
			return nil, fmt.Errorf("runvm_write: bad base64: %v", err)
		}
		data = dec
	} else {
		return nil, fmt.Errorf("runvm_write: provide exactly one of %q or %q", "content", "base64")
	}
	m, err := s.Resources.Write(p.Target, data)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"uri":    m.URI,
		"bytes":  m.Size,
		"sha256": m.Sha256,
		"name":   m.Name,
	}, nil
}

// AndroidOpenArgs is the argument shape of android_open.
type AndroidOpenArgs struct {
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
			"session":   id,
			"pool":      sess.PoolSize(),
			"last_used": sess.LastUsed().UTC().Format(time.RFC3339),
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

func (s *Set) androidOpen(ctx context.Context, raw json.RawMessage) (any, error) {
	var a AndroidOpenArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	// The container format decides the personality, not the extension or the
	// caller's intent: a Mach-O dylib handed to android_open would otherwise
	// be fed the bionic sysroot and a JNI table it cannot use.
	if ios.IsMachO(a.SO) {
		return s.iosOpen(ctx, raw)
	}
	if a.Engine == "" {
		a.Engine = os.Getenv("RUNVM_ENGINE")
	}
	if err := s.reserved(); err != nil {
		return nil, fmt.Errorf("android_open: %w", err)
	}
	info, infoErr := android.Describe(a.SO)
	exports, exportsErr := android.Exports(a.SO)

	mcp.Notify(ctx, 0.1, "android_open: reading library")
	sess, err := android.Open(a.SO, android.OpenOptions{
		Engine:  a.Engine,
		DEX:     a.DEX,
		Pool:    a.Pool,
		MaxUses: a.MaxUses,
	})
	if err != nil {
		return nil, err
	}
	mcp.Notify(ctx, 0.9, fmt.Sprintf("android_open: %d engine(s) ready", sess.PoolSize()))
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

type iosOpenArgs struct {
	Dylib   string `json:"dylib"`
	SO      string `json:"so"` // android_open forwards its "so" here on a Mach-O input
	Engine  string `json:"engine"`
	Pool    int    `json:"pool"`
	MaxUses int64  `json:"max_uses"`
}

func (a *iosOpenArgs) path() string {
	if a.Dylib != "" {
		return a.Dylib
	}
	return a.SO
}

// iosOpen is also the iOS branch of android_open: the container format decides
// the personality, so a Mach-O input never reaches the bionic path.
func (s *Set) iosOpen(ctx context.Context, raw json.RawMessage) (any, error) {
	var a iosOpenArgs
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.path() == "" {
		return nil, errors.New("dylib: required")
	}
	if !ios.IsMachO(a.path()) {
		return nil, fmt.Errorf("ios_open: %s is not a Mach-O dylib (ELF .so files go to android_open)", a.path())
	}
	if err := s.reserved(); err != nil {
		return nil, err
	}

	mcp.Notify(ctx, 50, "booting the Darwin personality: no bionic sysroot, no JNI")
	sess, err := ios.Open(a.path(), ios.OpenOptions{Engine: a.Engine, Pool: a.Pool, MaxUses: a.MaxUses})
	if err != nil {
		// golem refuses lazy/weak bind streams outright. The build flag that
		// avoids them is not obvious from the error alone, so say it here.
		if strings.Contains(err.Error(), "lazy/weak bind") {
			return nil, fmt.Errorf("%w (build the dylib with -Wl,-bind_at_load)", err)
		}
		return nil, err
	}
	sessID := s.IOS.Add(sess)
	// A parse failure must not be indistinguishable from "this dylib has none".
	// The session booted either way, so the errors are reported as fields and
	// the rest of the result is still usable.
	exports, exportsErr := ios.Exports(a.path())
	imports, importsErr := ios.Imports(a.path())
	libs, libsErr := ios.Libraries(a.path())
	mcp.Notify(ctx, 90, "ready: call ios_call")

	// The shape is the iOS one whatever door was used; android_open's caller
	// only needs the session id to know which tool family to use next.
	out := map[string]any{
		"session":         sessID,
		"arch":            "arm64",
		"kind":            "macho-dylib",
		"assets_present":  false,
		"symbols":         len(exports),
		"imports":         imports,
		"jni_on_load":     nil,
		"init_array":      nil,
		"system_libs":     libs,
		"engine":          sess.Engine(),
		"pool_size":       sess.PoolSize(),
		"engines_created": sess.EnginesCreated(),
	}
	if len(exports) > 50 {
		out["exports"] = exports[:50]
		out["exports_truncated"] = true
	} else {
		out["exports"] = exports
		out["exports_truncated"] = false
	}
	if a.SO != "" {
		// android_open reached here: say which door it took, because the caller
		// asked for an Android library and got an iOS session.
		out["routed_to"] = "ios"
		out["note"] = "Mach-O input: opened with ios_open semantics (no JNI, no bionic); use ios_* tools"
	}
	addErrors(out, map[string]error{
		"exports_error":     exportsErr,
		"imports_error":     importsErr,
		"system_libs_error": libsErr,
	})
	// A failed read must not render as null: the lists stay empty and the
	// matching *_error field says which one could not be read.
	out["exports"] = emptyIfNil(exports)
	out["imports"] = emptyIfNil(imports)
	out["system_libs"] = emptyIfNil(libs)
	return out, nil
}

// addErrors puts the non-nil ones into out. Fields are omitted rather than set
// to null so a reader can tell "read, and there were none" from "not read".
func addErrors(out map[string]any, errs map[string]error) {
	for field, err := range errs {
		if err != nil {
			out[field] = err.Error()
		}
	}
}

// emptyIfNil renders an unread list as [] rather than null, so the field keeps
// its type whether or not the read succeeded.
func emptyIfNil(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}

func (s *Set) iosSymbols(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Filter  string `json:"filter"`
		Limit   int    `json:"limit"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 200
	}
	sess, err := s.iosSession(a.Session)
	if err != nil {
		return nil, err
	}
	path := sess.Path()
	all, err := ios.Exports(path)
	if err != nil {
		return nil, err
	}
	names := ios.SymbolFilter(all, a.Filter)
	shown := names
	truncated := false
	if len(shown) > a.Limit {
		shown = shown[:a.Limit]
		truncated = true
	}
	imports, impErr := ios.Imports(path)
	out := map[string]any{
		"session":   a.Session,
		"exports":   shown,
		"matched":   len(names),
		"total":     len(all),
		"truncated": truncated,
		"imports":   emptyIfNil(imports),
	}
	addErrors(out, map[string]error{"imports_error": impErr})
	return out, nil
}

func (s *Set) iosImports(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.iosSession(a.Session)
	if err != nil {
		return nil, err
	}
	imports, err := ios.Imports(sess.Path())
	if err != nil {
		return nil, err
	}
	libs, libsErr := ios.Libraries(sess.Path())
	out := map[string]any{
		"session":     a.Session,
		"imports":     imports,
		"count":       len(imports),
		"system_libs": libs,
		"note": "none of these have an implementation: calling one directly errors, and a call " +
			"the dylib makes to one internally gets golem's stub value 0",
	}
	addErrors(out, map[string]error{"system_libs_error": libsErr})
	return out, nil
}

func (s *Set) iosCall(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Symbol  string `json:"symbol"`
		Args    []any  `json:"args"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.iosSession(a.Session)
	if err != nil {
		return nil, err
	}
	args, err := ios.ParseArgs(a.Args)
	if err != nil {
		return nil, fmt.Errorf("ios_call: %w", err)
	}
	res, err := sess.Call(ctx, a.Symbol, args)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"symbol": a.Symbol,
		"value":  res.Value,
		"signed": int64(res.Value),
		"i32":    int32(uint32(res.Value)),
		"hex":    ios.Hex(res.Value),
	}
	if res.Buffers != nil {
		out["buffers"] = res.Buffers
	}
	return out, nil
}

func (s *Set) iosClose(_ context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.iosSession(a.Session)
	if err != nil {
		return nil, err
	}
	if _, gone := s.IOS.Delete(a.Session); gone {
		_ = sess.Close()
	}
	return map[string]any{"closed": a.Session, "was": sess.Path()}, nil
}

// iosSession resolves an ios session id.
func (s *Set) iosSession(id string) (*ios.Session, error) {
	sess, ok := s.IOS.Get(id)
	if !ok {
		return nil, fmt.Errorf("ios session %q not found (open one with ios_open, or check android_open routed a Mach-O file here)", id)
	}
	return sess, nil
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

func (s *Set) h5Open(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Target    string            `json:"target"`
		Env       map[string]string `json:"env"`
		Ephemeral *bool             `json:"ephemeral"`
		Visible   *bool             `json:"visible"`
		Webbrain  *bool             `json:"webbrain"`
		Width     int               `json:"width"`
		Height    int               `json:"height"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if err := s.reserved(); err != nil {
		return nil, fmt.Errorf("h5_open: %w", err)
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
		OnProgress: func(p float64, msg string) {
			mcp.Notify(ctx, p, "h5_open: "+msg)
		},
	})
	if err != nil {
		return nil, err
	}
	id := s.H5.Add(sess)
	if a.Webbrain != nil && *a.Webbrain {
		mcp.Notify(ctx, 0.8, "h5_open: injecting webbrain page scripts")
		if err := h5.InjectWebbrain(ctx, sess); err != nil {
			_ = sess.Close()
			s.H5.Delete(id)
			return nil, fmt.Errorf("h5_open: webbrain: %w", err)
		}
		mcp.Notify(ctx, 0.95, "h5_open: webbrain ready")
	}
	return map[string]any{"session": id, "target": sess.Target(), "webbrain": a.Webbrain != nil && *a.Webbrain}, nil
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

// pollEnvelope evaluates the predicate inside try/catch and returns a stable
// JSON string, so a throw mid-poll is reported instead of surfacing as a
// transport error.
const pollEnvelope = `(function () {
  try {
    var v = (%s);
    return JSON.stringify({ok: !!v, v: v === undefined ? null : v});
  } catch (e) {
    return JSON.stringify({ok: false, error: String(e)});
  }
})()`

// unwrapJSONString undoes webframe's formatting: a string result comes back
// JSON-quoted (e.g. "\"{\\\"ok\\\":true}\"").
func unwrapJSONString(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if unq, err := strconv.Unquote(s); err == nil {
			return unq
		}
	}
	return s
}

func (s *Set) h5WaitFor(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session    string `json:"session"`
		Expression string `json:"expression"`
		Selector   string `json:"selector"`
		TimeoutMS  int    `json:"timeout_ms"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	sess, err := s.h5Session(a.Session)
	if err != nil {
		return nil, err
	}
	expr := a.Expression
	if expr == "" && a.Selector != "" {
		sel, _ := json.Marshal(a.Selector)
		expr = "document.querySelector(" + string(sel) + ")"
	}
	if expr == "" {
		return nil, fmt.Errorf("h5_wait_for: give an expression or a selector")
	}

	timeout := time.Duration(a.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	script := fmt.Sprintf(pollEnvelope, expr)

	last := "undefined"
	start := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, evalErr := sess.Eval(ctx, script, 10*time.Second)
		if evalErr != nil {
			return nil, fmt.Errorf("h5_wait_for: %w", evalErr)
		}
		var envelope struct {
			OK    bool            `json:"ok"`
			V     json.RawMessage `json:"v"`
			Error string          `json:"error"`
		}
		if err := json.Unmarshal([]byte(unwrapJSONString(raw)), &envelope); err != nil {
			return nil, fmt.Errorf("h5_wait_for: unreadable poll result %q", raw)
		}
		if envelope.OK {
			return map[string]any{
				"matched":   true,
				"value":     json.RawMessage(envelope.V),
				"waited_ms": time.Since(start).Milliseconds(),
			}, nil
		}
		if envelope.Error != "" {
			last = envelope.Error
		} else if len(envelope.V) > 0 {
			last = string(envelope.V)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("h5_wait_for: timed out after %s waiting for %s (last value: %s)",
				timeout, expr, last)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *Set) h5PDF(_ context.Context, raw json.RawMessage) (any, error) {
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
	doc, vector, err := sess.PDF()
	if err != nil {
		return nil, err
	}
	path := a.Path
	if path == "" {
		path = fmt.Sprintf("%s.pdf", slug(sess.Target()))
	}
	if err := os.WriteFile(path, doc, 0o644); err != nil {
		return nil, fmt.Errorf("h5_pdf: write %q: %w", path, err)
	}
	note := "raster PDF: the page was captured and embedded, not printed (no print API on this platform yet)"
	if vector {
		note = "vector PDF: printed by the WebView; text is selectable and the whole page is included"
	}
	return map[string]any{
		"pdf":    path,
		"bytes":  len(doc),
		"vector": vector,
		"note":   note,
	}, nil
}

// slug turns a URL or path into something safe to use as a file name.
func slug(target string) string {
	if i := strings.LastIndexAny(target, "/\\"); i >= 0 && i+1 < len(target) {
		target = target[i+1:]
	}
	target = strings.TrimSuffix(target, ".html")
	var b strings.Builder
	for _, r := range target {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "page"
	}
	return out
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
	sess.SetLastScreenshot(png)
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

// webbrainTool drives a page through WebBrain's content scripts. One tool,
// many actions: perception (page/tree/interactive/snapshot) and control
// (click/type/press/scroll/check) over stable ref_ids from the tree.
func (s *Set) webbrainTool(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Session   string `json:"session"`
		Action    string `json:"action"`
		RefID     string `json:"ref_id"`
		Text      string `json:"text"`
		Clear     *bool  `json:"clear"`
		Filter    string `json:"filter"`
		MaxDepth  *int   `json:"max_depth"`
		MaxChars  *int   `json:"max_chars"`
		X         *int   `json:"x"`
		Y         *int   `json:"y"`
		Selector  string `json:"selector"`
		TextMatch string `json:"text_match"`
		Key       string `json:"key"`
		Repeat    *int   `json:"repeat"`
		Checked   *bool  `json:"checked"`
		TimeoutMS *int   `json:"timeout_ms"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	if a.Action == "" {
		return nil, fmt.Errorf("webbrain: action is required")
	}
	sess, err := s.h5Session(a.Session)
	if err != nil {
		return nil, err
	}
	if a.TimeoutMS == nil || *a.TimeoutMS <= 0 {
		def := 60000
		a.TimeoutMS = &def
	}

	contentAction := ""
	params := map[string]any{}
	switch a.Action {
	case "page":
		contentAction = "get_page_info"
	case "tree":
		contentAction = "get_accessibility_tree"
		if a.Filter != "" {
			params["filter"] = a.Filter
		}
		if a.MaxDepth != nil {
			params["maxDepth"] = *a.MaxDepth
		}
		if a.MaxChars != nil {
			params["maxChars"] = *a.MaxChars
		}
		if a.RefID != "" {
			params["ref_id"] = a.RefID
		}
	case "interactive":
		contentAction = "get_interactive_elements"
	case "snapshot":
		// page + compact tree in one call: the model's usual first look
		page, err := h5.WebbrainAct(ctx, sess, "get_page_info", nil, time.Duration(*a.TimeoutMS)*time.Millisecond)
		if err != nil {
			return nil, err
		}
		treeParams := map[string]any{"filter": "interactive", "maxDepth": 12}
		if a.MaxChars != nil {
			treeParams["maxChars"] = *a.MaxChars
		}
		tree, err := h5.WebbrainAct(ctx, sess, "get_accessibility_tree", treeParams, time.Duration(*a.TimeoutMS)*time.Millisecond)
		if err != nil {
			return nil, err
		}
		return map[string]any{"page": page, "tree": tree}, nil
	case "click":
		if a.RefID != "" {
			contentAction = "click_ax"
			params["ref_id"] = a.RefID
		} else {
			contentAction = "click"
			if a.Selector != "" {
				params["selector"] = a.Selector
			}
			if a.Text != "" {
				params["text"] = a.Text
			}
			if a.TextMatch != "" {
				params["textMatch"] = a.TextMatch
			}
			if a.X != nil && a.Y != nil {
				params["x"] = *a.X
				params["y"] = *a.Y
			}
		}
	case "type":
		contentAction = "type"
		if a.RefID != "" {
			params["ref_id"] = a.RefID
		}
		params["text"] = a.Text
		if a.Clear != nil {
			params["clear"] = *a.Clear
		}
	case "press":
		contentAction = "press_keys"
		if a.Key == "" {
			return nil, fmt.Errorf("webbrain: press requires key (Escape, Tab, Enter, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, ;)")
		}
		params["key"] = a.Key
		if a.Repeat != nil {
			params["repeat"] = *a.Repeat
		}
	case "scroll":
		contentAction = "scroll"
		if a.RefID != "" {
			params["ref_id"] = a.RefID
		}
		if a.X != nil {
			params["x"] = *a.X
		}
		if a.Y != nil {
			params["y"] = *a.Y
		}
	case "check":
		contentAction = "set_checked"
		if a.RefID == "" {
			return nil, fmt.Errorf("webbrain: check requires ref_id")
		}
		params["ref_id"] = a.RefID
		if a.Checked != nil {
			params["checked"] = *a.Checked
		}
	default:
		return nil, fmt.Errorf("webbrain: unknown action %q (page|tree|interactive|snapshot|click|type|press|scroll|check)", a.Action)
	}
	result, err := h5.WebbrainAct(ctx, sess, contentAction, params, time.Duration(*a.TimeoutMS)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return result, nil
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
