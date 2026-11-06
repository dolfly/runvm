# Windows capture: why `EnumWindows` finds "no window in this process"

Research note for the Windows CI failure of `runvm selftest`. Nothing here changes
source; it is an audit of the code that produced:

```
selftest: no window in this process (pid 6428, session 2, 0 process windows, 76 top-level windows system-wide)
```

**Bottom line up front:** the windowing layer is fine. `EnumWindows` is not
returning nothing — it returns 76 windows. The filter rejects every one of them
because it compares against the **parent** process's PID, while the HWND belongs
to the **child** process `runvm web-host`, which runvm itself spawned. It is a
cross-process bug in `internal/h5/capture_windows.go`, not a glaze/WebView2
window-visibility bug.

## Path shorthand used below

| Prefix | Real path |
| --- | --- |
| `glaze/` | `/root/.dsh/im/.gopath/pkg/mod/github.com/progrium/glaze@v0.0.0-20261029191355-bc7899dbb235/` |
| `webframe/` | `/root/.dsh/im/.gopath/pkg/mod/tractor.dev/webframe@v0.0.0-20261030030822-25c24cb61aad/` |
| `runvm/` | repo root, `/root/.dsh/im/runvm/` |

---

## 0. What actually happens (call chain)

```
runvm selftest                          ← PARENT process (the one that prints the error)
│  cmd/runvm/selftest.go:36             h5.Open(page, {Visible:true, Width:640, Height:400})
│
├─ internal/h5/session.go:101           exec.Command(exe, "web-host", target)   ← NEW PROCESS
│  internal/h5/session.go:106-114       RUNVM_VISIBLE=1, RUNVM_VIEWPORT=640x400
│
└─ runvm web-host (child, other PID)    ← CHILD process: every window lives here
   │  cmd/runvm/main.go:43              runtime.LockOSThread()
   │  internal/h5/host.go:21            h5.RunHost
   │  internal/h5/host.go:59-62         webframe.New(Window: true, Width/Height)
   │
   ├─ webframe/webframe.go:127-147      glaze.NewWithOptions{Hidden: !opts.Window}   (Hidden=false)
   ├─ glaze/webview_windows.go:502-509  RegisterClassExW("glaze_webview")
   ├─ glaze/webview_windows.go:511-515  CreateWindowExW(exStyle=0, style=WS_OVERLAPPEDWINDOW,
   │                                       parent=0, 640x480, lpCreateParams=engineID)  ← TOP-LEVEL HWND
   ├─ glaze/webview2_windows.go:430     CreateCoreWebView2Controller(hwnd = that window)
   ├─ glaze/webview2_windows.go:820-825 PutIsVisible(true); !hidden → ShowWindow(SW_SHOW)
   ├─ internal/h5/host.go:110           emit {"event":"display", text:$DISPLAY, own:...}
   ├─ internal/h5/host.go:134-135       f.Navigate(target); f.Run()   ← GetMessage loop
   │
   └─ ...later, parent's readLoop sees {"event":"ready"} → h5.Open returns (session.go:154-157)

runvm selftest (parent, again)
│  cmd/runvm/selftest.go:43             sess.Screenshot()
├─ internal/h5/session.go:308-325       Screenshot → capturePNG(display="")   (session.go:324)
└─ internal/h5/capture_windows.go:89    ownWindow()
   ├─ :90   pid := windows.GetCurrentProcessId()      ← PARENT pid (6428)
   ├─ :97-114 EnumWindows(cb)
   ├─ :99   GetWindowThreadProcessId(hwnd, &winPID)   ← child pid for the only interesting window
   ├─ :100  if winPID != pid { return 1 }             ← filters it out, every time
   └─ :126-139 no candidates → the error message above
```

The parent (`runvm` itself) never creates a window. Every Win32 window that runvm
owns belongs to the child. So the PID comparison at
`runvm/internal/h5/capture_windows.go:100` can never match.

Corroboration from the same code and docs:

- `runvm/internal/h5/protocol.go:3-8` and `runvm/ARCHITECTURE.md:15` state the
  design: one page = one `web-host` child process.
- The child is started with a plain `exec.Command` and no `SysProcAttr`
  (`runvm/internal/h5/session.go:101-137`; the only `SysProcAttr` in the package
  is on the Linux Xvfb helper, `internal/h5/display_linux.go:50`), so it inherits
  the parent's window station and desktop — its window is therefore one of the 76
  that the parent's `EnumWindows` reported (an inference, but forced by the next
  point).
- `h5.Open` succeeded before the capture ran, which means
  `glaze.NewWithOptions` returned nil error, which means `CreateWindowExW` returned
  a non-zero HWND (`glaze/webview_windows.go:516-519`) and the WebView2 controller
  was created. The window exists; the parent just is not its owner.
- The comment at `runvm/internal/h5/capture_windows.go:20` — "The window is ours
  (the host created it)" — is the tell: in this code "the host" means the *child*,
  but the function runs in the parent.

A secondary consequence: the error text's advice ("open the page with
`visible: true`", `capture_windows.go:136-138`) is actively misleading here. The
page *was* visible; visibility never mattered (see §2 and §3).

---

## 1. How is the window created?

All of it is in `glaze/webview_windows.go`, only when `Options.Window == nil`
(i.e. glaze owns the window). `webframe` always leaves `glaze.Options.Window`
unset (`webframe/webframe.go:127-147`), so this is the path runvm takes.

**Class registration** — one class name for every instance in the process:

```go
// glaze/webview_windows.go:455
var classNamePtr = utf16("glaze_webview")

// glaze/webview_windows.go:501-509
if w.ownsWindow {
    wc := wndClassExW{
        lpfnWndProc:   wndProcCB,
        hInstance:     w.hinst,
        lpszClassName: classNamePtr,
    }
    wc.hIcon, wc.hIconSm = exeIcons()
    wc.cbSize = uint32(unsafe.Sizeof(wc))
    registerClassExW(&wc) // idempotent across instances (same class name)
```

- `wndProcCB` is a `purego.NewCallback(wndProc)` trampoline created once
  (`glaze/webview_windows.go:205`, the proc itself at `:265-335`).
- `hInstance` is `getModuleHandleW(0)` (`:178`, `:499`).
- No background brush, cursor, or menu is registered (`wndClassExW` blank fields,
  `:96-109`).

**Window creation** — top-level, overlapped, never `WS_VISIBLE`:

```go
// glaze/webview_windows.go:511-515
w.window = createWindowExW(
    0, classNamePtr, utf16(""), wsOverlappedWindow,
    cwUseDefault, cwUseDefault, 640, 480,
    0, 0, w.hinst, w.id, // lpCreateParams = engine id (integer)
)
```

| Property | Value | Where |
| --- | --- | --- |
| `dwExStyle` | `0` | `:512` |
| class | `L"glaze_webview"` | `:455`, `:512` |
| window title | `L""` (set later by `SetTitle`, `:576`) | `:512` |
| `dwStyle` | `wsOverlappedWindow = 0x00CF0000` = `WS_OVERLAPPED\|WS_CAPTION\|WS_SYSMENU\|WS_THICKFRAME\|WS_MINIMIZEBOX\|WS_MAXIMIZEBOX` — **no `WS_VISIBLE` (0x10000000)** | `:35`, `:512` |
| x, y | `CW_USEDEFAULT` (`^int32(0x7fffffff)`) | `:33`, `:513` |
| width, height | `640, 480` (window rect, later overridden by `SetSize`) | `:513` |
| `hWndParent` | `0` → `HWND_DESKTOP` ⇒ **top-level window** | `:514` |
| `hMenu` | `0` | `:514` |
| `hInstance` | `GetModuleHandleW(0)` | `:514` |
| `lpCreateParams` | `w.id` (integer engine id, not a Go pointer) | `:514`, design note `:9-17` |

The engine id is stashed at `WM_NCCREATE` in `GWLP_USERDATA`
(`glaze/webview_windows.go:267-273`) and every later message recovers it
(`:275`). `WM_DESTROY` clears it and the `window` field
(`:321-331`).

**Which thread:** the goroutine that calls `NewWithOptions`, pinned with
`runtime.LockOSThread()` in a `sync.Once` (`glaze/webview_windows.go:481-484`):

```go
uiThreadOnce.Do(func() {
    runtime.LockOSThread()
    uiThreadID = getCurrentThreadID()
})
```

In runvm that is the child's main goroutine: `cmd/runvm/main.go:43` →
`h5.RunHost` → `webframe.New` → `glaze.NewWithOptions`. The WebView2 controller
is created on that same thread immediately afterwards
(`glaze/webview2_windows.go:430`, inside `embed`, `:764-831`), and the message
loop that services the window is `GetMessageW` in `embed` (`:784-795`) and then
`Run` (`glaze/webview_windows.go:542-548`).

**Size:** `webframe` immediately calls `SetSize` when both dimensions are
non-zero (`webframe/webframe.go:156-158`), which for runvm is the visible path
(`RUNVM_VIEWPORT=640x400` → `internal/h5/host.go:63-64` → `webframe.go:71`).
`SetSize` rewrites the style bits and grows the rect by the non-client area via
`AdjustWindowRectEx`, then `SetWindowPos`
(`glaze/webview_windows.go:589-609`).

---

## 2. Is `Hidden` honoured, and how?

**Yes, and in a way that has nothing to do with enumerability.**

`webframe` maps its public `Options.Window bool` to glaze's `Hidden`
(`webframe/webframe.go:70`, `:129`):

```go
Hidden: !opts.Window,
```

runvm sets `Window: visible` (`internal/h5/host.go:62`), and `visible` comes from
`RUNVM_VISIBLE=1` (`internal/h5/host.go:22`, set by `internal/h5/session.go:106-107`
when `h5.OpenOptions.Visible`). So `Visible: true` ⇒ `Window: true` ⇒
`Hidden: false`.

The three mechanisms, in order:

1. **Born hidden.** `wsOverlappedWindow` does not include `WS_VISIBLE`
   (`glaze/webview_windows.go:35`, `:512`). `CreateWindowExW` therefore creates
   the window **without `WS_VISIBLE`**, which is the canonical Win32 "create it
   hidden" idiom. There is no `SW_HIDE` anywhere in the backend; hiding is
   simply "never show".
2. **Shown only when not hidden.** After the controller is ready, `embed` does:

   ```go
   // glaze/webview2_windows.go:816-825
   asController(w.controller).PutIsVisible(true)
   if w.hidden {
       return nil
   }
   showWindow(w.window, swShow)
   updateWindow(w.window)
   ```

   `SetSize` repeats the same guard for later resizes
   (`glaze/webview_windows.go:605-608`).
3. **`PutIsVisible(true)` even when hidden** (`:820`). The comment
   (`:816-819`) explains why: WebView2 decides throttling from the controller's
   `IsVisible` and its bounds, not from whether the HWND was ever shown, so a
   hidden page still runs at full speed, animation frames included.

The corresponding public contract is documented at `glaze/scheme.go:200-217`:
*"…the window is never shown — not at creation, while loading, on SetSize, or in
Run — and the app is not activated for it. Focus and Raise do nothing."* And
`Hidden` cannot be combined with an externally supplied window:
`ErrHiddenWindow` / `checkHidden` at `glaze/scheme.go:251-265`.

**What makes it a "window" at all:** registering a class and calling
`CreateWindowExW` with `hWndParent = 0` inserts a window object into the
desktop's top-level window list, as a child of the desktop. `WS_VISIBLE` is only
a style bit that controls painting, hit-testing, and `IsWindowVisible` — it does
**not** control existence or enumeration. That is precisely why
`EnumWindows` reports invisible windows and why `capture_windows.go:111` has to
ask `IsWindowVisible` separately.

So for this bug, hidden vs. visible was never load-bearing. A `Hidden: true`
glaze window is just as enumerable as a shown one (it would even have a
non-empty client rect, because `CreateWindowExW` sized it 640×480).

---

## 3. Why does `EnumWindows` return nothing?

### 3.1 What is actually true in the failing run

It does not return nothing. The message says 76 top-level windows, and the
diagnostic code counts them in the same function
(`internal/h5/capture_windows.go:126-139`). The zero is `len(cands)`, i.e. the
number of windows that passed the PID test at `:100` **and** had a non-empty
client rect at `:104-110`.

`pid` there is `windows.GetCurrentProcessId()` (`:90`) — the MCP/selftest
process. The only window that matters was created by `CreateWindowExW` in the
`runvm web-host` child (`internal/h5/session.go:101`). Its
`GetWindowThreadProcessId` is the child's PID, so it is skipped along with the
other 75.

Why each of the other candidate explanations does **not** apply:

| Hypothesis | Verdict | Evidence |
| --- | --- | --- |
| Message-only window (`HWND_MESSAGE` parent) | **No.** Parent is `0` (`HWND_DESKTOP`); the string `HWND_MESSAGE` appears nowhere in glaze. A message-only window would indeed be invisible to `EnumWindows`, but glaze never creates one. | `glaze/webview_windows.go:514`; repo-wide grep for `HWND_MESSAGE` → 0 hits |
| Created on a thread with no message loop yet | **No, and irrelevant.** The window is created on the `LockOSThread`ed UI thread (`:481-484`), and `embed` pumps `GetMessageW` before returning (`glaze/webview2_windows.go:784-795`). Even without a loop, a window exists in the desktop list as soon as `CreateWindowExW` returns; `EnumWindows` does not need anyone to pump. | `:481-484`, `webview2_windows.go:784-795` |
| Window destroyed and recreated | **No.** It lives from `NewWithOptions` until `Destroy` (`webview_windows.go:611-644`, called from `webframe/webframe.go:333-336`) or a page-initiated `WM_CLOSE` (`:309-320`), which runvm only triggers on `window.close()`. It is not recreated on navigation. `WM_DESTROY` zeroes `w.window` (`:329`). | `glaze/webview_windows.go:611-644` |
| HWND lives in a different process | **Yes — this is the bug.** The HWND belongs to `runvm web-host`, not to the process that calls `ownWindow`. | `runvm/internal/h5/session.go:101`; `capture_windows.go:90`, `:100` |
| WebView2's own processes own the window | **No.** glaze passes *its own* HWND to `CreateCoreWebView2Controller` (`glaze/webview2_windows.go:430`), so `msedgewebview2.exe` only ever owns **child** HWNDs inside it (the `Chrome_WidgetWin_*` / render-host child windows). `EnumWindows` walks top-level windows only, so those would not appear even if the PID matched. | `glaze/webview2_windows.go:227-230`, `:430` |

### 3.2 The part that is a real, but secondary, hazard

`EnumWindows` enumerates the top-level windows of **the calling thread's
desktop**. Parent and child share a desktop here (plain `exec.Command`, no
`SysProcAttr`), and the log's "76 top-level windows system-wide" proves the
desktop exists and is populated. If runvm were ever launched in a service
session and the child placed on a different window station/desktop, the child's
window would genuinely be absent from the parent's enumeration — but that is not
what the CI log shows, and fixing the PID is still the first step.

### 3.3 Cross-check that closes the loop

`runvm doctor` opens `about:blank` through the same `h5.Open` path
(`internal/doctor/doctor.go:75-88`) and succeeds, including a round-trip
`Eval`. That path never calls `capturePNG`. So "WebView2 is alive but this
process has no window" is exactly the expected signature of capture running in
the wrong process.

---

## 4. Where is the HWND stored, and how could it be exposed?

**Storage.** `webview.window uintptr` — `glaze/webview_windows.go:405` (field
block `:402-406`, next to `ownsWindow`). Written at `:511` (owned) or `:522`
(embedded into a host HWND), cleared at `:329` (`WM_DESTROY`), `:631` and
`:636` (`Destroy`).

**It is already exported — but not through webframe.**

```go
// glaze/webview_windows.go:571-574
func (w *webview) Window() unsafe.Pointer {
    p := w.window
    return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}
```

That method is part of the cross-platform interface,
`glaze/webview_common.go:54-57`:

```go
// Window returns a native window handle pointer. ... when using Win32 backend
// the pointer is HWND pointer.
Window() unsafe.Pointer
```

The gap is one layer up. `webframe.Frame` stores the webview privately
(`webframe/webframe.go:92`, `w glaze.WebView`) and exposes no accessor — the
complete method set is `navigated/receive/take/failEvals/Navigate/Reload/Eval/
EvalContext/evalExpr/Run/Terminate`. So runvm cannot reach the HWND at all.

**Smallest change to expose it (in webframe):**

```go
// webframe/webframe.go — new method, ~3 lines plus the unsafe import
// Window returns the native window handle of the page: HWND on Windows.
func (f *Frame) Window() unsafe.Pointer { return f.w.Window() }
```

Cost: it turns webframe into something runvm must fork or `replace`
(`runvm/go.mod:19` replaces glaze already, but not webframe), so it is *not*
free.

**Smallest change that needs no dependency change.** The child can obtain its
own HWND with the exact code already in the repo — in the child,
`GetCurrentProcessId()` *is* the right PID — and report it as a `uint64` in the
existing `display` event (`internal/h5/host.go:110`, consumed at
`internal/h5/session.go:183-186`). And even without any of that, the parent
already knows the child PID: `s.cmd.Process.Pid` (`internal/h5/session.go:31`,
`:101`, `:135`). No new API is strictly required for the fix.

---

## 5. Alternative capture paths, ranked

| Rank | Option | Effort | Reuses | Main risk |
| --- | --- | --- | --- | --- |
| 1 | **(a) HWND + `PrintWindow`**, from the correct process | ~8–25 lines, no fork | `capture_windows.go:143-222` | cross-process `PrintWindow` needs a pumping target and a composited desktop |
| 2 | **(d) Move the whole capture into the child**, ship PNG (or base64) over the JSONL pipe | ~15–25 lines + 1 protocol op | same GDI code, moved | base64 over the pipe; parent keeps the "last screenshot" bookkeeping |
| 3 | **(c) `ICoreWebView2::CapturePreview`** exposed from glaze | fork + new COM handler + new host op (~150–250 lines) | — | dependency fork; async COM; but gives pixel-accurate, desktop-free capture and unlocks macOS later |
| 4 | **(d) CDP `Page.captureScreenshot`** via `CallDevToolsProtocolMethod` | same fork cost as (c), slightly less COM | vtable slot already declared | needs a completed-handler + JSON/base64 parsing; still in-child |
| 5 | **(b) Create our own host window, hand it to glaze** | webframe change + new API | glaze embed path exists | does not solve the cross-process problem at all |
| — | **(d) `Windows.Graphics.Capture` / screen `BitBlt`** | large | — | needs D3D11 + frame pool + the HWND anyway; fails without a composited desktop; `BitBlt` grabs the desktop, which the project explicitly refuses to do (`README.md:230`) |

### (a) HWND + `PrintWindow` — recommended

Two variants, same machinery:

- **a1, child reports its HWND.** The `web-host` child calls the existing
  `ownWindow()` logic with its own PID (which is correct in-process), and emits
  the handle in the `display` event (`internal/h5/host.go:110`); the parent stores
  it and uses it directly. Most precise: no desktop-wide enumeration in the
  parent, a clear error when there is no window, and no PID-reuse ambiguity.
- **a2, parent enumerates by the child PID.** `ownWindow(pid uint32)` instead of
  `GetCurrentProcessId()`, and pass `s.cmd.Process.Pid`. Smallest possible diff.

Why this is the right first move:

- The capture code already exists and is already correct in every respect except
  the PID: `printWindow` (`capture_windows.go:143-222`) does
  `GetClientRect → CreateCompatibleDC/Bitmap → PrintWindow(PW_RENDERFULLCONTENT)
  → GetDIBits → image.RGBA`, and the reason for `PW_RENDERFULLCONTENT` is
  documented at `:16-21` and used at `:175-178` — exactly what DirectComposition
  / WebView2 content needs.
- `session.Screenshot` already waits two animation frames plus 120 ms before
  capturing (`internal/h5/session.go:318-323`), which is the right warm-up.
- No fork, no new dependency surface, and the platform contract
  (`capturePNG(display string)`, `captureSupported(own, display)`) barely moves.

Risks and mitigations:

- **Cross-process `PrintWindow`** sends `WM_PRINT`/`WM_PRINTCLIENT` to the target
  window's thread. The child is inside `GetMessageW` (`glaze/webview_windows.go:542-548`)
  and its `wndProc` forwards everything it does not handle to `DefWindowProcW`
  (`:332-333`), so this is serviced normally. A hung page host would block the
  call; the screenshot path already has a bounded retry loop
  (`capture_windows.go:29-35`) but no timeout — worth adding if this becomes the
  shipped path.
- **Black frames.** `PW_RENDERFULLCONTENT` goes through DWM composition. If the
  window is minimized, fully occluded, or the session has no composited desktop,
  the result can be uniformly black. `selftest`'s colour-count check
  (`cmd/runvm/selftest.go:63-68`) is the right detector; do not remove it.
- **The window must have been shown at least once.** A `Hidden: true` glaze window
  is never composited, so `PW_RENDERFULLCONTENT` is expected to return a blank
  frame; `Visible: true` stays a prerequisite for pixels (though never for
  enumeration).
- **DPI.** `procGetDpiForWindow` is declared (`capture_windows.go:80`) but never
  used; the code deliberately renders at 96-DPI-equivalent (`:153`). On a
  high-DPI runner the captured bitmap may be smaller than the physical window.
  Pre-existing, unrelated to this bug.

### (b) Create our own host window and hand it to glaze — not worth it

glaze fully supports embedding: `Options.Window unsafe.Pointer`
(`glaze/scheme.go:36-41`), `NewWindow` (`glaze/webview_windows.go:466-468`), and
the embed path subclasses the host window instead of owning it
(`:521-530`, `subclassProc` at `:357-397`).

But:

1. `webframe` never forwards it — its `glaze.Options` literal
   (`webframe/webframe.go:127-147`) sets `Debug`, `Hidden`, `DisableThrottling`,
   `Ephemeral`, and the callbacks, and nothing for `Window`. Its own
   `Options.Window` is a **bool**, name-colliding with glaze's
   `Window unsafe.Pointer`; a host-window API would need a new field and a
   webframe fork.
2. `checkHidden` rejects `Hidden` + `Window` (`glaze/scheme.go:251-265`), and
   webframe unconditionally sets `Hidden: !opts.Window`, so the two options
   cannot currently coexist.
3. Most importantly, it does not address the bug: the window would still be
   created in the child process. The parent would still need the HWND across the
   process boundary, which is exactly what (a) already provides.

It only becomes interesting if runvm ever wants to control styles, decorations,
or a parent-child window hierarchy from its own code.

### (c) `CapturePreview` (glaze fork) — the strategic fix, not the minimal one

Reachability: `w.controller` is `ICoreWebView2Controller*`
(`glaze/webview_windows.go:409`, set at `glaze/webview2_windows.go:445`), and the
webview pointer is `w.webview2` (`glaze/webview2_windows.go:446`). Both are
private fields of an unexported type; nothing on glaze's public interface
exposes them. The vtable slot for `CapturePreview` is already declared — but only
so the offsets of later methods are right; it is never invoked:

```go
// glaze/webview2_windows.go:155-157
ExecuteScript                       uintptr
CapturePreview                      uintptr
Reload                              uintptr
```

(never referenced outside the struct — a repo-wide grep finds only that line).

To use it you would need: a new exported glaze method (e.g.
`Snapshot(w io.Writer) error`) that calls
`CapturePreview(handler, COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG)`, a new
COM completion-handler kind in the existing handler machinery
(`webview2_windows.go:339`, `:411-468`, `newHandler` at `:560`), an `IStream`
(`CreateStreamOnHGlobal`), and a new `{"op":"screenshot"}` in the host protocol
plus parent plumbing. Cost is real (a fork of glaze, or an upstream PR), but the
payoff is bigger than this bug: it captures the **page**, not the window, does not
depend on the window being on screen (so it is the only option that would work on
a truly headless runner), returns PNG bytes directly, and the same shape of API
(`webkit_web_view_get_snapshot`, `takeSnapshotWithConfiguration:`) would fix
macOS too. `docs/ROADMAP.md:5-21` already names this as option A.

One correction to the brief: `CapturePreview` is on the **base**
`ICoreWebView2` interface ([Microsoft docs](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2?view=webview2-1.0.2210.55)),
not on `ICoreWebView2_16`. The `_16` interface is the printing family —
`Print`, `PrintToPdfStream`, `ShowPrintUI`
([reference](https://raw.githubusercontent.com/MicrosoftDocs/webview2-win32-reference/live/1-0-2045-28/icorewebview2_16.md)).
`PrintToPdfStream` is arguably an even better fit for `h5_pdf`
(`internal/h5/pdf.go` currently renders PDF by embedding a PNG).

### (d) Other paths

- **Capture inside the child (PrintWindow there), send PNG over the pipe.** Keeps
  the HWND in the process that owns it — no cross-process `PrintWindow` at all —
  and reuses the same GDI code, moved into the child. Needs one new protocol op
  and a PNG/base64 payload; the parent keeps `shotPNG`/`shotAt` bookkeeping
  (`internal/h5/session.go:327-344`) unchanged. Good fallback if cross-process
  `PrintWindow` turns out to be flaky on runners.
- **CDP `Page.captureScreenshot` via `CallDevToolsProtocolMethod`.** The slot is
  already in the vtable (`glaze/webview2_windows.go:165`), and the note in
  `docs/ROADMAP.md:13` that "there is no CDP endpoint" is only true for the
  *socket/pipe* transport — this API calls the protocol in-process without any
  debugging port. Still in-child, still needs a fork to expose, and the result
  arrives as JSON with base64 data through a completed-handler. Comparable cost
  to (c) with less upside.
- **`Windows.Graphics.Capture`.** Needs a `GraphicsCaptureItem`, a D3D11 device
  and a frame pool, and still starts from the HWND. Heavy, COM/WinRT glue,
  generally requires an interactive composited session, and adds failure modes
  on exactly the runners where this bug appeared. Not recommended.
- **`BitBlt` from the screen DC.** Rejected by design: the project's stated
  guarantee is "the capture is that page, not the desktop" (`README.md:230`,
  `internal/h5/screen_linux.go:15-19`).

---

## 6. Recommended minimal fix

**Give the Windows capture code the child's identity.** No dependency changes,
no fork, no new window.

### Option A (smallest): pass the child PID — ~8 lines, 4 files

1. `internal/h5/capture_windows.go`
   - `func ownWindow() (uintptr, error)` → `func ownWindow(pid uint32) (uintptr, error)`
     (`:89`), delete `pid := uint32(windows.GetCurrentProcessId())` (`:90`), and
     reword the diagnostic at `:126-139` to name the PID it looked for.
   - `func capturePNG(string)` → `func capturePNG(_ string, pid uint32)` (`:22`),
     forwarding to `ownWindow(pid)` (`:30`).
2. `internal/h5/screen_linux.go:30` and `internal/h5/screen_other.go:9` — add the
   unused second parameter so the shared signature still compiles off Windows.
3. `internal/h5/session.go:324` — `return capturePNG(display, uint32(s.cmd.Process.Pid))`
   (`s.cmd.Process` is non-nil: the process was started at `:135`).

Risk: still a desktop-wide enumeration from the parent; the error message has to
stay honest about that.

### Option B (recommended): the child reports its HWND — ~20 lines, 5–6 files

Same `PrintWindow` path, but no enumeration in the parent at all.

1. `internal/h5/protocol.go` — add a handle field to `hostMsg` (`:20-31`):
   `HWND uint64` with json tag `hwnd,omitempty`.
2. `internal/h5/capture_windows.go` — keep `ownWindow(pid uint32)` and add, for
   the child side:

   ```go
   // hostWindow returns this process's page window: the child is the owner, so
   // its own PID is the right filter.
   func hostWindow() uintptr {
       h, _ := ownWindow(uint32(os.Getpid()))
       return h
   }
   ```

3. `internal/h5/host.go:110` — include it in the existing event (emitted after
   `webframe.New` returned, i.e. after `CreateWindowExW`):
   `emit(hostMsg{Event: "display", Text: os.Getenv("DISPLAY"), Own: own, HWND: uint64(hostWindow())})`.
4. `internal/h5/session.go` — store `hwnd` in `readLoop`'s `case "display"`
   (`:183-186`) next to `s.display`/`s.own`, and pass it to
   `capturePNG(display, s.hwnd)` (`:324`).
5. `internal/h5/capture_windows.go` — `func capturePNG(_ string, hwnd uintptr)`:
   use `hwnd` if non-zero, else fall back to `ownWindow(uint32(windows.GetCurrentProcessId()))`
   so the old behaviour is preserved for any path that has no host handle; and
   keep `captureSupported` (`:52`) unchanged.
6. Non-Windows builds need `hostWindow() uintptr { return 0 }` — put it beside the
   existing stubs in `internal/h5/screen_linux.go` / `screen_other.go` (or a new
   `//go:build !windows` file).

### What not to do for this bug

Do not touch `Visible`/`Hidden` plumbing in `internal/h5/host.go:62` or
`webframe/webframe.go:129`. The page really was visible; changing that would
hide the real defect and make the selftest silently pass on a blank window
(if it ever got that far).

### How to verify

- `runvm selftest` on `windows-latest` should print
  `selftest: ok — 640x400, N distinct colours, …` (`cmd/runvm/selftest.go:69-70`).
- If it still fails, the error must now say *which* PID it searched and how many
  candidate windows that PID had — the current message cannot distinguish "wrong
  process" from "no window".
- CI already treats a "not supported on this platform" failure as a warning
  (`.github/workflows/ci.yml:151-163`), but the `no window in this process`
  text is *not* in that allow-list, which is why the job failed loudly instead of
  skipping.

---

## 7. Residual risks, summarized

| Risk | Applies to | Severity | Mitigation |
| --- | --- | --- | --- |
| `PrintWindow` across processes blocks on the target's message loop | (a) | medium | the child pumps `GetMessage`; add a timeout around the retry loop |
| Blank/black capture on non-composited sessions or minimized windows | (a), (d) | medium | keep `selftest`'s distinct-colour assertion; never minimize; prefer (c) long-term |
| PID-reuse / multiple windows in one child | (a2) | low | use the child-reported HWND (a1/B) |
| Parent/child on different desktops (service session) | (a2) | low today | enumeration would see nothing; (c) removes the dependency entirely |
| Forking glaze/webframe raises maintenance cost | (c), (d) | high | keep it behind a pinned `replace` (as `go.mod:19` already does for glaze) and land the selftest first |
| DPI scaling changes captured dimensions | (a), (d) | low | pre-existing; use `GetDpiForWindow` (already bound, `capture_windows.go:80`) if it matters |

---

## 8. Evidence index (one line per claim)

- Window class/name: `glaze/webview_windows.go:455`, `:501-509`.
- `CreateWindowExW` arguments: `glaze/webview_windows.go:511-515`; styles `:32-37`.
- Top-level parent: `glaze/webview_windows.go:514` (`0` at the `parent` position).
- UI thread pinning: `glaze/webview_windows.go:481-484`; loop `:542-548`;
  WebView2 pump `webview2_windows.go:784-795`.
- Visibility: `glaze/webview2_windows.go:820-825`; `webview_windows.go:605-608`;
  contract `scheme.go:200-217`; `Hidden`+`Window` conflict `scheme.go:251-265`.
- HWND storage/export: `glaze/webview_windows.go:405`, `:511`, `:522`, `:571-574`;
  interface `webview_common.go:54-57`.
- webframe's private webview and forwarding: `webframe/webframe.go:92`, `:127-147`,
  `:156-158`.
- Child process spawn: `runvm/internal/h5/session.go:101-137`;
  architecture `runvm/ARCHITECTURE.md:15`, `runvm/internal/h5/protocol.go:3-8`.
- Wrong-PID filter: `runvm/internal/h5/capture_windows.go:90`, `:97-114`, `:126-139`.
- Capture entry point: `runvm/internal/h5/session.go:308-325`,
  `runvm/cmd/runvm/selftest.go:36`, `:43`.
- `PrintWindow` implementation: `runvm/internal/h5/capture_windows.go:143-222`,
  `PW_RENDERFULLCONTENT` at `:175-178`.
- `CapturePreview` slot never called: `glaze/webview2_windows.go:156`;
  controller/pointer fields `glaze/webview_windows.go:409-410`.

No source files were modified; this note and its directory are the only additions.
