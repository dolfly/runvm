---
title: macOS snapshot 调研
layout: page
---

# macOS snapshot: can glaze grow `Snapshot() ([]byte, error)` with purego only?

Research note for the `runvm` screenshot/print fork. Answers whether
`github.com/progrium/glaze@v0.0.0-20261029191355-bc7899dbb235` can gain a
`Snapshot()` on its macOS backend that returns PNG bytes, using only `purego`
(no cgo), and how much of it is already proven by code shipping in that module.

**Verdict up front: likely-works.** The exact mechanism needed — a Go closure
handed to WebKit as an Objective-C block, invoked asynchronously from the
completion handler while glaze's `Run()` pumps — is *already in that file* and
*already exercised on `macos-latest` by glaze's own tests* (`Find`, see §3).
What is left is a selector chain of object/BOOL returns (`NSImage` →
`NSBitmapImageRep` → `NSData`) that needs no new ABI, plus a plumbing problem
one layer above glaze (§7).

---

## 1. How glaze talks to Objective-C

glaze is a hand-written AppKit/WebKit binding over `github.com/ebitengine/purego`
and `purego/objc`. The runtime libraries are loaded once in `ensureInit`
(Cocoa and WebKit are `dlopen`ed globally so WebKit's Objective-C classes
register into the process):

- `webview_darwin.go:151` `ensureInit()` → `webview_darwin.go:152-157`
  `purego.Dlopen("/System/Library/Frameworks/Cocoa.framework/Cocoa", …)` and
  `.../WebKit.framework/WebKit`, `RTLD_GLOBAL|RTLD_LAZY`.
- `webview_darwin.go:163-171` resolves `_dispatch_main_q` via
  `purego.Dlsym(RTLD_DEFAULT, …)` and binds `dispatch_async_f` with
  `purego.RegisterLibFunc`.

There **is** a small helper layer, at the top of `webview_darwin.go`:

| helper | line | what it does |
|---|---|---|
| `selCache sync.Map` | `webview_darwin.go:81` | caches selectors because `objc.RegisterName` takes the global objc lock |
| `sel(name) objc.SEL` | `webview_darwin.go:83` | `objc.RegisterName` + cache |
| `class(name) objc.ID` | `webview_darwin.go:93` | `objc.GetClass`, **panics** when the class is missing |
| `nsstr(s) objc.ID` | `webview_darwin.go:101` | `[NSString stringWithUTF8String:]` |
| `cstr(id) string` | `webview_darwin.go:106` | `unsafe.Slice` over `-UTF8String`'s `char *` |
| `autorelease(f func())` | `webview_darwin.go:122` | wraps `f` in an `NSAutoreleasePool` and drains it |
| `responds(id, sel)` | `webview_darwin.go:1124` | `-respondsToSelector:` for availability checks |

Everything goes through `objc.ID.Send` / `objc.Send[T]`
(`purego/objc/objc_runtime_darwin.go:141` and `:161`), which are wrappers over
`objc_msgSend`. glaze itself never calls `objc_msgSend` directly; the one raw
`SyscallN` is to reuse WebKit's own IMP for `willOpenMenu:withEvent:`:

- `webview_darwin.go:398-405`: `purego.Dlopen("/usr/lib/libobjc.A.dylib")`,
  `purego.RegisterLibFunc(&methodImplementation, lib, "class_getMethodImplementation")`,
  then `purego.SyscallN(wkWillOpenMenu, self, _cmd, menu, event)` at
  `webview_darwin.go:409-410`.

Struct returns go through `objc.Send[T]`, e.g.
`objc.Send[cgRect](w.window, sel("frame"))` at `webview_darwin.go:1362`; on
amd64 `Send[T]` automatically picks `objc_msgSend_stret` when `T` is a struct
larger than 16 bytes (`purego/objc/objc_runtime_darwin.go:164-172`), on arm64 it
is always `objc_msgSend`. `cgRect` is defined at `webview_darwin.go:74`.

## 2. Does glaze already construct Objective-C classes or blocks?

**Classes: yes, by the dozen.** `registerClasses()` (`webview_darwin.go:185`)
uses `objc.RegisterClass`, which is `objc_allocateClassPair` +
`class_addMethod` + `objc_registerClassPair`
(`purego/objc/objc_runtime_darwin.go:277`; declared at `:41-52`, bound with
`purego.RegisterLibFunc` at `:107-110`). Registered classes include:

- `GlazeAppDelegate` (`webview_darwin.go:187`)
- `GlazeScriptMessageHandler`, conforming to `WKScriptMessageHandler`
  (`webview_darwin.go:220`)
- `GlazePaintHandler` (`webview_darwin.go:236`), `GlazePaintCover` (`:254`)
- `GlazeWindowDelegate` (`:264`)
- `GlazeUIDelegate` (`:280`), conforming to `WKUIDelegate`, carrying
  `webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:`
  (`:285`), the media-capture permission method (`:290`), the three script-dialog
  methods (`:301`, `:308`, `:315`) and `createWebViewWithConfiguration:…` (`:329`)
- `GlazeURLSchemeHandler`, conforming to `WKURLSchemeHandler`
  (`webview_darwin.go:349-356`, methods `startURLSchemeTask` / `stopURLSchemeTask`
  at `:430` and `:475`)
- `GlazeFirstMouseWebView` / `GlazeWebView`, WKWebView subclasses (`:370`, `:379`)

So the callback direction **ObjC → Go** is implemented the other way round from
blocks: glaze registers a class whose methods are Go closures, conforms it to a
protocol, and hands the object to WebKit as a delegate. The Go closure receives
`(self, _cmd, …)` and recovers the `*webview` from a global registry
(`registerInstance` / `lookupEngine`, `webview_darwin.go:536-551`) rather than
from an ivar.

**Blocks: yes — two different things are already done with them.**

1. *Calling blocks that ObjC handed us.* `invokeNativeBlock`
   (`webview_darwin.go:508-519`) drives a foreign block through `NSInvocation`
   with a hand-written type string (`"v@?@"`, `"v@?q"`, `"v@?B"`), because
   `objc.Block.Invoke` only knows blocks made by `objc.NewBlock`. Used at
   `:297`, `:304`, `:311`, `:323`, `:502`, `:1997`, `:2108`, `:2119`.
2. *Passing our own Go block to ObjC.* `Find` does exactly what a snapshot needs
   — see §3.

## 3. If blocks are already used: the pattern

`webview_darwin.go:1425-1443` is the template, and it is a *WebKit completion
handler*, i.e. the same shape as `takeSnapshotWithConfiguration:`:

```go
func (w *webview) Find(text string, backwards bool, done func(bool)) {
	performOnMain(func() {
		…
		autorelease(func() {
			cfg := class("WKFindConfiguration").Send(sel("new"))
			cfg.Send(sel("autorelease"))
			cfg.Send(sel("setBackwards:"), backwards)
			// WebKit copies the block; this reference is released below.
			block := objc.NewBlock(func(_ objc.Block, result objc.ID) {
				callFind(done, objc.Send[bool](result, sel("matchFound")))
			})
			w.webView.Send(sel("findString:withConfiguration:completionHandler:"), nsstr(text), cfg, block)
			block.Release()
		})
	})
}
```

Four rules are encoded there, and they all transfer to a snapshot block:

1. **Build it on the UI thread**, inside `autorelease` (the block's Go closure
   may touch AppKit, and the returned autoreleased objects — `WKFindConfiguration`
   here — must be drained by a pool you own).
2. **The Go closure's first parameter must be `objc.Block`** — `purego`'s
   `blockCache.encode` panics otherwise
   (`purego/objc/objc_block_darwin.go:130-132`); the remaining parameters are
   the block's real arguments, and the return type must match `@encode`.
3. **`block.Release()` right after the send**, because the callee copies it.
4. **Do not block waiting for the completion** — `done` is a callback, not a
   channel receive; see §5.

The test file proves the same path end-to-end offline:
`webview_darwin_test.go:107-124` (`openPanelCompletionScenario`) makes a
`objc.NewBlock`, passes it to `invokeOpenPanelCompletion`, and asserts the Go
closure runs. And `find_test.go:17-56` runs the *real* WebKit completion handler
under `w.Run()` on the macOS CI runner.

### What `objc.NewBlock` actually builds

This is `purego` v0.11.1 (`glaze/go.mod` pins `github.com/ebitengine/purego
v0.11.1`), `purego/objc/objc_block_darwin.go`:

- `blockLayout` (`:51-58`) — the literal: `isa Class`, `flags uint32`,
  4 bytes padding, `invoke uintptr`, `descriptor *blockDescriptor` = 32 bytes.
- `blockDescriptor` (`:37-44`) — `reserved`, `size`, `reserved` (the copy
  helper slot, left nil), `dispose`, `signature *uint8` = 40 bytes.
- flags (`:24-30`): `blockHasCopyDispose = 1<<25` **and**
  `blockHasSignature = 1<<30` → `0x42000000`. (The host task's guess of
  `0x40000000` is only the signature bit; `purego` also sets copy/dispose.)
- isa is `__NSMallocBlock__` (`:22`, `:195`), *not* `_NSConcreteStackBlock`.
- `invoke` is one `purego.NewCallback` **per Go function type**, cached in
  `blockCache.layouts` (`:169-184`), so repeated snapshots do not burn callback
  slots (`purego/syscall_unix.go:44` caps them at 2000).
- `descriptor.size = unsafe.Sizeof(blockLayout{})` (`:192`), `dispose` is a
  `purego.NewCallback(cache.Functions.Delete)` (`:201`) so the Go closure is
  dropped from the map when the refcount hits zero.
- `NewBlock` (`:239-246`) lays the literal out **in Go memory**, then calls
  `_Block_copy` (`Block.Copy`, `:213-215`, bound at
  `objc_runtime_darwin.go:125`). Because the flags lack `BLOCK_NEEDS_FREE`
  (`1<<24`) and `BLOCK_IS_GLOBAL` (`1<<28`), libclosure treats it as a stack
  block: `malloc` + `memmove` + refcount 1, into a real heap block. So **no
  manual `_NSConcreteStackBlock`, no manual `Block_copy`, no manual struct
  literal is needed** — `objc.NewBlock` is already the block-literal ABI, done.

Lifetime: the returned `objc.Block` is a +1 reference; `Release()` drops it. The
callee (WebKit) `Block_copy`s it if it stores it, which is the documented
convention for completion handlers and is what glaze's `Find` relies on in
production code. The captured Go state lives in `blockFunctionCache`, not in the
block's memory, so it stays valid across the copy and is freed by the dispose
helper.

## 4. The full call sequence for PNG bytes

Objects/selectors needed (all macOS 10.12+, no availability guard needed for the
snapshot itself; guard `afterScreenUpdates` which is 10.15+):

| step | class | selector | returns |
|---|---|---|---|
| 1 | `WKSnapshotConfiguration` | `+new` then `-autorelease` (glaze pattern, `webview_darwin.go:1432-1433`) | config |
| 2 | `WKSnapshotConfiguration` | `-setSnapshotWidth:` with `[NSNumber numberWithDouble:]` | — |
| 3 | `WKSnapshotConfiguration` | `-setAfterScreenUpdates:` (10.15+, `responds` guard) | — |
| 4 | `WKWebView` (`w.webView`, set at `webview_darwin.go:1009`) | `-takeSnapshotWithConfiguration:completionHandler:` | — (async) |
| 5 | block | `(NSImage *image, NSError *error)` | — |
| 6 | `NSImage` | `-TIFFRepresentation` | `NSData *` |
| 7 | `NSBitmapImageRep` | `+imageRepWithData:` | `NSBitmapImageRep *` |
| 8 | `NSBitmapImageRep` | `-representationUsingType:properties:` with `NSBitmapImageFileTypePNG = 4`, `nil` properties | `NSData *` |
| 9 | `NSData` | `-length` (NSUInteger) and `-bytes` (`const void *`) | bytes → Go |

Notes and traps:

- **Configuration may be nil, but do not rely on it.** Apple's header does not
  mark `snapshotConfiguration` nullable; pass a real
  `[[WKSnapshotConfiguration alloc] init]` autoreleased. Cost is one message.
- **Sizing.** With `snapshotWidth` unset the image is the view's size in
  *points*, so a 2× Retina capture is not what you get. Setting
  `snapshotWidth = @(viewWidthInPoints)` gives a deterministic pixel width and a
  proportional height. This matters for `runvm`: Linux captures the whole X
  root at the Xvfb size, Windows captures the client rect; macOS should be told
  explicitly what width to produce rather than inheriting whatever the window is.
- **`rect`** (`-setRect:`, a `CGRect`) is optional. If you set it you are
  passing a 32-byte struct by value through `objc_msgSend` — glaze already does
  this (`webview_darwin.go:1008` passes `cgRect` to `initWithFrame:configuration:`),
  so it works, but there is no reason to use it for a full-page capture because
  the default is the whole view.
- **No `objc_msgSend_stret` case.** Everything returned here is `id`, `BOOL` or
  `NSUInteger` — register returns on both arm64 and amd64. `NSSize`/`CGRect` are
  only needed if you ask for `-bounds`/`-frame`; `objc.Send[cgRect]` already
  handles the amd64 stret path if you do (`objc_runtime_darwin.go:161-173`).
- **Reading `NSData` into Go** is the same `unsafe.Slice` trick as `cstr`
  (`webview_darwin.go:106-118`), but with an explicit length. Copy immediately:
  the data is owned by an autoreleased object.
- **Alternatives, and why not.** `-printOperationWithPrintInfo:` is synchronous
  but gives `NSPrintOperation` → PDF, not PNG, and printing a WKWebView runs the
  print panel machinery; `-createPDFWithConfiguration:completionHandler:`
  (macOS 11+) is the same block problem but yields PDF, which `runvm` would then
  have to rasterize. Neither is simpler than the ~40 lines above, and the PNG
  route feeds `internal/h5/pdf.go`'s existing raster path unchanged.

## 5. Threading

- `w.webView` must be messaged on the UI thread; glaze's convention is
  `performOnMain` (`webview_darwin.go:594-606`), which runs inline when already
  on the main thread and otherwise `dispatch_async_f`es to `_dispatch_main_q`
  and waits.
- WebKit delivers `takeSnapshot`'s completion handler on the **main thread**.
  glaze's `Run()` calls `[NSApp run]` (`webview_darwin.go:1186-1219`), which
  drains the main dispatch queue as part of the run loop, so the block fires
  while `Run()` is pumping. **No `dispatch_async` to main is needed**, and
  `Find` is the in-tree proof: `find_test.go` chains four searches from inside
  the completion handler, under `w.Run()`, on macOS CI.
- **The one real deadlock trap** is a *blocking* `Snapshot()` called from the UI
  thread: the thread that must run the completion handler would be parked on a
  channel. Two ways out, and glaze already contains both patterns:
  - expose the callback form (`func([]byte, error)`), like `Find`;
  - or, for a blocking `Snapshot()` on the UI thread, pump a nested loop exactly
    like `pumpUntilClosed` (`webview_darwin.go:1221-1240`) until the result
    arrives. Caveat, documented in that same function: a pump that is itself
    nested inside a main-queue callout will not drain the main queue again until
    the callout returns, so if WebKit hops the completion through the main
    queue the pump will never see it.
- From a background goroutine (the `runvm` host's command loop is one —
  `internal/h5/host.go:112-133` reads stdin on a goroutine while the main
  goroutine sits in `f.Run()`), the blocking shape is safe: `performOnMain`
  returns after the send, then the caller waits on a channel while the main loop
  keeps pumping.

## 6. Risks / unknowns

Cannot be verified without a real Mac:

1. **Does `takeSnapshot` return a non-blank `NSImage` for a window glaze never
   put on screen?** `Options.Hidden` (`webview_darwin.go:799`) builds the
   window but keeps it out of the way; `runvm` never asks for a hidden page
   capture (`Session.Screenshot` requires `visible`,
   `internal/h5/session.go:309-311`), so this is mostly a non-issue — but it
   means the macOS path inherits the same "page must be visible" precondition,
   just not the Xvfb-style "must own the display" one.
2. **Snapshot timing vs. paint.** `runvm` already does two
   `requestAnimationFrame`s plus a 120 ms beat before capturing
   (`internal/h5/session.go:320-324`). `afterScreenUpdates:YES` is the
   WebKit-native equivalent; whether it is enough (or too slow) on CI is
   empirical.
3. **Whether WebKit copies the completion block for *this* API.** `Find` proves
   it for `findString:withConfiguration:completionHandler:`; the block-copy
   convention is uniform across WebKit's completion-handler APIs, but a missing
   `Block_copy` in one API would show up as a use-after-free on the macOS
   runner, not locally.
4. **Intel Macs.** `macos-latest` is arm64; glaze's darwin amd64 code path
   (including the `objc_msgSend_stret` branch) is never exercised by CI. The
   snapshot itself uses no struct returns, so this is a low risk, but it is
   untested.
5. **Image scale/orientation.** WebKit's snapshot may be sized in points;
   whether the resulting PNG matches what Linux/Windows produce for the same
   viewport is a comparison nobody can do from Linux.
6. **Sandbox/TCC.** A plain `NSImage` → PNG conversion needs no screen-recording
   permission (unlike `CGWindowListCreateImage`/`screencapture`), which is
   precisely the argument for this route over a window-server capture. Worth
   confirming under a sandboxed `.app` if `runvm` ever ships one.

What a `macos-latest` CI test would actually prove:

- the block literal round-trip (Go closure ← WebKit completion) for this
  selector, including retain/release, i.e. the only genuinely novel ABI;
- that the completion is delivered while `Run()` pumps;
- that the `NSImage` → `NSBitmapImageRep` → PNG chain produces a decodable PNG
  with non-zero dimensions;
- that the bytes actually contain ink (a pixel check), which is the only real
  answer to unknown #1 for a visible window.

It would **not** prove anything about Intel, about a sandboxed app, or about a
hidden/off-screen window.

## 7. Plumbing (one layer above glaze) — do not skip this

Even a perfect `glaze.Snapshot()` does not make `runvm`'s `h5_screenshot` work on
macOS, because of how the current capture is wired:

- `Session.Screenshot` (`internal/h5/session.go:308-325`) runs in the **MCP
  server process**, not in the page host. It calls `captureSupported` + `capturePNG`
  (`internal/h5/screen_linux.go:30`) which shells out to `xwd` against the
  display the *child* draws on, or `PrintWindow` against the child's HWND
  (`internal/h5/capture_windows.go:22`). Both are cross-process OS captures.
- On macOS the pixels only exist inside the child's `WKWebView`. There is no
  cross-process equivalent that would not need screen-recording permission, so
  the capture must move **into the host process**: a new `hostCmd` op
  (e.g. `"snapshot"`, `internal/h5/protocol.go:12-16`) whose reply carries the
  PNG base64 in `hostMsg.Result` (`:20-31`), handled in `host.go`'s command
  loop, and `Session.Screenshot` grows a "ask the host" branch for darwin.
- Between glaze and `runvm` sits `webframe`, whose `Frame` keeps the webview
  **unexported** (`webframe.go:91-93`, `w glaze.WebView`). So a
  `glaze.Snapshot()` is unreachable from `runvm` until `webframe` exposes it:
  `func (f *Frame) Snapshot() ([]byte, error)` (or a pass-through returning the
  `glaze.WebView`). That is a third module to fork/bump, and `runvm`'s `go.mod`
  already carries the `replace` for glaze
  (`runvm/go.mod:19`), so the fork story exists but now spans two forks.
- **Do not add `Snapshot()` to `glaze.WebView`** (`webview_common.go:37`). The
  Linux and Windows backends implement that interface (`webview_linux.go`,
  `webview_windows.go`), so a new method there breaks their build and glaze's
  own CI matrix. Use a side interface and a type assertion:

  ```go
  // Snapshotter is implemented by backends that can capture the page as PNG.
  type Snapshotter interface{ Snapshot() ([]byte, error) }
  ```

  `webframe` can then do `if s, ok := f.w.(glaze.Snapshotter); ok { … }` without
  any change to the existing interface.

---

## Concrete Go sketch (not compiled)

For `webview_darwin.go` in the fork. Uses only helpers that already exist in
that file: `sel`, `class`, `autorelease`, `performOnMain`, `responds`, and
`objc.NewBlock`.

```go
// nsBitmapImageFileTypePNG is NSBitmapImageFileTypePNG from AppKit.
const nsBitmapImageFileTypePNG = 4

// Snapshotter is implemented by backends that can capture the page as PNG.
// It is deliberately NOT part of WebView: the Linux and Windows backends do not
// implement it, and adding it to that interface would break their builds.
type Snapshotter interface{ Snapshot() ([]byte, error) }

// Snapshot captures the page as PNG.
//
// Call it from a goroutine other than the UI thread: WebKit delivers the
// completion handler on the main queue, so a Snapshot that parked the UI thread
// would wait for a callback that thread alone can run. The internal
// snapshotAsync form exists for UI-thread callers (see Find).
func (w *webview) Snapshot() ([]byte, error) {
	type result struct {
		png []byte
		err error
	}
	ch := make(chan result, 1)
	w.snapshotAsync(func(png []byte, err error) { ch <- result{png, err} })
	r := <-ch
	return r.png, r.err
}

// snapshotAsync starts a WKWebView snapshot and reports the PNG on the UI
// thread (the thread WebKit calls the completion handler on).
func (w *webview) snapshotAsync(done func([]byte, error)) {
	performOnMain(func() {
		if w.webView == 0 {
			done(nil, errors.New("webview: snapshot: no web view"))
			return
		}
		autorelease(func() {
			// A real configuration, not nil: the header does not mark the
			// parameter nullable. new + autorelease matches WKFindConfiguration
			// in Find.
			cfg := class("WKSnapshotConfiguration").Send(sel("new"))
			cfg.Send(sel("autorelease"))

			// Pin the output width instead of inheriting the view's point size,
			// so a Retina window and a 1x window produce comparable PNGs.
			if b := objc.Send[cgRect](w.webView, sel("bounds")); b.Size.Width > 0 {
				width := class("NSNumber").Send(sel("numberWithDouble:"), b.Size.Width)
				cfg.Send(sel("setSnapshotWidth:"), width)
			}
			// macOS 10.15+: make WebKit flush the current frame first.
			if responds(cfg, "setAfterScreenUpdates:") {
				cfg.Send(sel("setAfterScreenUpdates:"), true)
			}

			// WebKit copies the block; our reference is released below. The
			// closure runs on the main thread, inside the run loop that Run()
			// owns, so it may touch AppKit directly.
			block := objc.NewBlock(func(_ objc.Block, image, nserr objc.ID) {
				if image == 0 {
					done(nil, fmt.Errorf("webview: snapshot: %s", nsErrorText(nserr)))
					return
				}
				png, err := pngFromNSImage(image)
				done(png, err)
			})
			w.webView.Send(sel("takeSnapshotWithConfiguration:completionHandler:"), cfg, block)
			block.Release()
		})
	})
}

// pngFromNSImage converts a WKWebView snapshot (an NSImage) to PNG bytes via
// TIFF, the only lossless bridge NSImage offers to NSBitmapImageRep.
func pngFromNSImage(image objc.ID) ([]byte, error) {
	tiff := image.Send(sel("TIFFRepresentation"))
	if tiff == 0 {
		return nil, errors.New("webview: snapshot: NSImage has no TIFF representation")
	}
	rep := class("NSBitmapImageRep").Send(sel("imageRepWithData:"), tiff)
	if rep == 0 {
		return nil, errors.New("webview: snapshot: NSBitmapImageRep could not decode the image")
	}
	// Second argument is NSDictionary<NSBitmapImageRepPropertyKey,id> * and may
	// be nil: no compression hints, just the PNG default.
	data := rep.Send(sel("representationUsingType:properties:"), nsBitmapImageFileTypePNG, objc.ID(0))
	if data == 0 {
		return nil, errors.New("webview: snapshot: PNG encoding failed")
	}
	// NSUInteger fits objc.ID on both 64-bit architectures.
	n := int(data.Send(sel("length")))
	if n <= 0 {
		return nil, errors.New("webview: snapshot: empty PNG data")
	}
	ptr := data.Send(sel("bytes")) // const void *
	out := make([]byte, n)
	copy(out, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n)) // #nosec G103 -- copying NSData's buffer before the pool drains
	return out, nil
}

// nsErrorText renders an NSError for a message, without failing on nil.
func nsErrorText(err objc.ID) string {
	if err == 0 {
		return "no image and no error"
	}
	desc := err.Send(sel("localizedDescription"))
	if desc == 0 {
		return "no image"
	}
	return cstr(desc.Send(sel("UTF8String")))
}
```

If the UI thread must be able to call it, replace the blocking `Snapshot` with
the callback form (mirroring `Find`) or wait with a nested pump like
`pumpUntilClosed` (`webview_darwin.go:1221-1240`), remembering the caveat in §5.

## How to verify on CI (ordered)

1. **Compile-only check, locally, no Mac:**
   `GOOS=darwin GOARCH=arm64 go build ./... && GOOS=darwin GOARCH=amd64 go build ./...`
   plus `GOOS=darwin go vet ./...`. Catches every typo in the selector chain
   except the ones that only the ObjC runtime can catch.
2. **Add a scenario, not a test.** In the fork, add
   `snapshotScenario() string` to `webview_darwin_test.go` and register it in
   `TestMain` (`webview_darwin_test.go:38-92`) next to `resFind`, so it runs on
   the main goroutine after `runtime.LockOSThread()`. Model it on
   `findScenario` (`find_test.go:17-56`): create a `NewWithOptions` with a
   `SchemeHandlers` page, `OnNavigation` → call `Snapshot()` **from a new
   goroutine** (or use `snapshotAsync`), then `w.Terminate()`.
3. **Assert three things, not one:** `bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n"))`;
   `image/png.Decode` succeeds and `Bounds()` is non-zero; and the decoded image
   has ink (sum of a sampled grid of RGB values > 0). A blank-but-valid PNG is
   the failure mode this whole note is worried about.
4. **Cap the scenario** with a `time.AfterFunc(15*time.Second, …)` watchdog that
   calls `Terminate`, exactly as `findScenario` does (`find_test.go:49`), so a
   never-delivered completion fails the run instead of hanging the job.
5. **Push a branch.** glaze's `macos` job (`.github/workflows/ci.yml:60-71`)
   already runs `go test ./...` on `macos-latest` with no `-short`, which means
   every `TestMain` scenario runs. One green run answers unknowns 1–3.
6. **Read the log, not just the exit code:** confirm the new scenario's result
   string appears (a skip looks like a pass in some reporters — `requireGUI`
   exists for that reason, `webview_darwin_test.go:94-98`).
7. **Only then plumb:** `webframe.Frame.Snapshot()`, then the `"snapshot"` host
   op, then `Session.Screenshot`'s darwin branch; finish with
   `go run ./cmd/runvm doctor` and `go run ./cmd/runvm selftest` on
   `macos-latest`, which runvm's `platforms` job already executes
   (`.github/workflows/ci.yml:104-170`) — `selftest` today exits 0 with a
   warning when the platform reports "not supported", so a real PNG is a visible
   behavior change, not a silent one.
8. **Optional but cheap:** run the same scenario under
   `go test -run TestSnapshot -count=1` in a loop of ~20 on the runner to shake
   out the retain/release race in the block lifetime (§6.3).

## Verdict

**Likely-works.**

- The only structurally new thing — passing a Go block to WebKit and having it
  called back — is already implemented at `webview_darwin.go:1436-1440` and is
  already exercised against a real WKWebView completion handler on
  `macos-latest` (`find_test.go`). Nothing about `takeSnapshot`'s ABI differs
  from `findString:withConfiguration:completionHandler:` except an extra object
  argument in the block, which `purego`'s `encode` handles by construction
  (`objc_block_darwin.go:111-145`).
- The `NSImage` → `TIFFRepresentation` → `NSBitmapImageRep` →
  `representationUsingType:properties:` chain is plain `id`-returning message
  sends; no struct returns, no `_stret`, no new classes, no cgo.
- The residual risk is behavioral, not structural: whether the pixels are real
  for a window glaze only sort-of shows, and whether the completion is delivered
  promptly under `Run()`. Both are one CI run away from being answered.
- The genuinely hard part is **not** in glaze: `runvm` currently captures from
  the MCP server process with OS-level tools, and on macOS the pixels are only
  reachable inside the page-host process, behind an unexported `webframe` field
  (§7). Budget for two fork bumps (glaze, webframe) plus a host-protocol op, not
  one function.
- **Needs-a-Mac-to-develop** is *not* the right label: it can be written and
  compile-checked from Linux, and verified by pushing a branch that the existing
  `macos-latest` job already tests. It is, however, needs-a-Mac-to-*debug* if
  the first run comes back blank.
- **First thing to try:** add `snapshotScenario()` to
  `webview_darwin_test.go` together with the ~60-line `snapshotAsync` +
  `pngFromNSImage` above, and push it. Do not touch `webframe`, the host
  protocol, or `runvm` until that run prints a PNG header and a non-zero ink
  sum.
