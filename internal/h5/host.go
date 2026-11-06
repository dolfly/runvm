package h5

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"tractor.dev/webframe"
)

// RunHost is the child-process body: load target in a WebKit view, publish
// console output and exceptions as JSON lines on stdout, and evaluate
// JavaScript on request. It returns the process exit code.
//
// It must be called from the main goroutine, after runtime.LockOSThread:
// webframe binds the webview to the OS thread that created it.
func RunHost(target string, env map[string]string, ephemeral bool) int {
	visible := os.Getenv("RUNVM_VISIBLE") == "1"
	width, height := viewportSize()
	// A visible page always gets its own display, so a capture can never grab
	// somebody's desktop by accident.
	own, err := ensureDisplay(visible, width, height)
	if err != nil {
		emitFatal(err)
		return 1
	}
	target = resolveTarget(target)

	var (
		wmu sync.Mutex
		enc = json.NewEncoder(os.Stdout)
	)
	emit := func(v any) {
		wmu.Lock()
		defer wmu.Unlock()
		_ = enc.Encode(v)
	}
	reply := func(id int, result string, err error) {
		ok := err == nil
		msg := hostMsg{ID: id, OK: &ok, Result: result}
		if err != nil {
			msg.Error = err.Error()
		}
		emit(msg)
	}

	ready := make(chan struct{})
	var readyOnce sync.Once
	done := make(chan struct{})
	var doneOnce sync.Once
	finish := func() { doneOnce.Do(func() { close(done) }) }
	exitCode := 0

	var f *webframe.Frame
	frame, err := webframe.New(webframe.Options{
		Env:       env,
		Ephemeral: ephemeral,
		Window:    visible,
		Width:     width,
		Height:    height,
		Console: func(level, text string) {
			emit(hostMsg{Event: "console", Level: level, Text: text})
		},
		Exception: func(text string) {
			emit(hostMsg{Event: "exception", Level: "error", Text: text})
		},
		Ready: func() {
			// The parent's h5_open blocks until this arrives: hooks are
			// installed and the page is evaluable from here on.
			readyOnce.Do(func() {
				emit(hostMsg{Event: "ready"})
				close(ready)
			})
		},
		Loaded: func() {
			emit(hostMsg{Event: "loaded"})
		},
		LoadFailed: func(url string, err error) {
			// Nothing will run in a page that never loaded, so failing loudly
			// beats hanging until the caller's timeout.
			emit(hostMsg{Event: "exception", Level: "load", Text: fmt.Sprintf("failed to load %s: %v", url, err)})
			finish()
		},
		Exit: func(code int) {
			exitCode = code
			emit(hostMsg{Event: "exit", Code: code})
			finish()
			if f != nil {
				f.Terminate()
			}
		},
		Closed: func() {
			finish()
			if f != nil {
				f.Terminate()
			}
		},
	})
	if err != nil {
		emit(hostMsg{Event: "exception", Level: "fatal", Text: err.Error()})
		return 1
	}
	f = frame
	// Tell the parent where this page lives, so h5_screenshot knows whether a
	// capture is possible at all.
	emit(hostMsg{Event: "display", Text: os.Getenv("DISPLAY"), Own: own})

	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for sc.Scan() {
			var cmd hostCmd
			if err := json.Unmarshal(sc.Bytes(), &cmd); err != nil {
				continue
			}
			switch cmd.Op {
			case "eval":
				<-ready // webframe requires Ready before Eval
				id, js := cmd.ID, cmd.JS
				f.Eval(js, func(res string, err error) { reply(id, res, err) })
			case "snapshot":
				// Must not run on the UI thread: the capture completes on the
				// main queue, which this process is pumping.
				id := cmd.ID
				go func() {
					png, err := f.Snapshot()
					if err != nil {
						ok := false
						msg := hostMsg{ID: id, OK: &ok, Error: err.Error()}
						if errors.Is(err, webframe.ErrSnapshotUnsupported) {
							msg.Error = "unsupported: " + err.Error()
						}
						emit(msg)
						return
					}
					ok := true
					emit(hostMsg{ID: id, OK: &ok, PNG: png})
				}()
			case "close":
				f.Terminate()
				return
			}
		}
		// stdin closed: the parent went away, so should we.
		f.Terminate()
	}()

	f.Navigate(target)
	f.Run()

	// An exit() from the page closes done; a Terminate from close/stdin does
	// not, so only wait when the page asked for a specific code.
	select {
	case <-done:
		return exitCode
	default:
		return 0
	}
}

// resolveTarget turns an existing local path into a file:// URL. WebKit has no
// notion of a relative filesystem path: passing one through navigates to a
// URL that 404s, which looks like a broken page rather than a wrong argument.
func resolveTarget(target string) string {
	if u, err := url.Parse(target); err == nil && u.Scheme != "" && len(u.Scheme) > 1 {
		return target
	}
	if _, err := os.Stat(target); err != nil {
		return target
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return target
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

// emitFatal reports a failure that happens before a frame exists.
func emitFatal(err error) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(hostMsg{Event: "exception", Level: "fatal", Text: err.Error()})
}

// viewportSize reads RUNVM_VIEWPORT ("1280x800"); zero means the page decides.
func viewportSize() (int, int) {
	var w, h int
	if _, err := fmt.Sscanf(os.Getenv("RUNVM_VIEWPORT"), "%dx%d", &w, &h); err != nil {
		return 0, 0
	}
	return w, h
}
