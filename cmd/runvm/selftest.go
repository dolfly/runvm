package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/dolfly/runvm/internal/h5"
)

// selftest is the platform check CI can run anywhere: open a page that paints
// a known colour, capture it, and confirm the capture is not blank. It is what
// turns "the code compiles on Windows" into "the screenshot path works there".
//
// Exit codes: 0 pass, 1 fail, 2 unsupported on this platform (so a platform job
// can skip rather than fail on a capability we have not built yet).
func selftest() {
	dir, err := os.MkdirTemp("", "runvm-selftest-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "selftest: temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	page := filepath.Join(dir, "page.html")
	const html = `<!doctype html><body style="margin:0;background:#4a90d9">
<div style="width:200px;height:120px;background:#e8b04b"></div></body>`
	if err := os.WriteFile(page, []byte(html), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "selftest: write page: %v\n", err)
		os.Exit(1)
	}

	// The iOS path first: it needs no window, so it is the one check that
	// runs the same way on every platform with a CPU engine.
	switch code := selftestIOS(); code {
	case 0:
	case 2:
		fmt.Fprintln(os.Stderr, "selftest: skipped, iOS probe needs a CPU engine (libunicorn)")
	default:
		os.Exit(code)
	}

	sess, err := h5.Open(page, h5.OpenOptions{Visible: true, Width: 640, Height: 400, Ephemeral: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "selftest: open page: %v\n", err)
		os.Exit(1)
	}
	defer sess.Close()

	png, err := sess.Screenshot()
	if err != nil {
		// Two kinds of "cannot do this here": the platform has no capture
		// path yet, and the window we need is not enumerable on this runner.
		// Both are a skip with a reason, not a silent pass — the message says
		// which, and CI turns it into a warning.
		if strings.Contains(err.Error(), "not supported") ||
			strings.Contains(err.Error(), "no window in this process") {
			fmt.Fprintf(os.Stderr, "selftest: skipped, screenshot not supported on this platform: %v\n", err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "selftest: screenshot: %v\n", err)
		os.Exit(1)
	}

	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		fmt.Fprintf(os.Stderr, "selftest: decode: %v\n", err)
		os.Exit(1)
	}
	colors := distinctColors(img)
	if len(colors) < 2 {
		fmt.Fprintf(os.Stderr, "selftest: captured %dx%d but only %d colour(s): the window did not paint\n",
			img.Bounds().Dx(), img.Bounds().Dy(), len(colors))
		os.Exit(1)
	}
	fmt.Printf("selftest: ok — %dx%d, %d distinct colours, %d bytes PNG\n",
		img.Bounds().Dx(), img.Bounds().Dy(), len(colors), len(png))

	// The PDF path is a separate capability: raster everywhere, vector on the
	// backends that can print. Saying which one this machine produced is the
	// difference between a check and a claim.
	doc, vector, err := sess.PDF()
	if err != nil {
		if strings.Contains(err.Error(), "not supported") {
			fmt.Fprintf(os.Stderr, "selftest: skipped, pdf not supported on this platform: %v\n", err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "selftest: pdf: %v\n", err)
		os.Exit(1)
	}
	if !bytes.HasPrefix(doc, []byte("%PDF")) {
		fmt.Fprintf(os.Stderr, "selftest: pdf does not start with %%PDF\n")
		os.Exit(1)
	}
	kind := "raster"
	if vector {
		kind = "vector"
	}
	fmt.Printf("selftest: ok — %s PDF, %d bytes\n", kind, len(doc))
}

// distinctColors samples the image and reports how many different colours it
// found, which is enough to tell a painted page from a blank one.
func distinctColors(img image.Image) map[uint32]struct{} {
	seen := map[uint32]struct{}{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bb, a := img.At(x, y).RGBA()
			seen[uint32(r>>8)<<24|uint32(g>>8)<<16|uint32(bb>>8)<<8|uint32(a>>8)] = struct{}{}
			if len(seen) > 64 {
				return seen
			}
		}
	}
	return seen
}
