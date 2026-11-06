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

	sess, err := h5.Open(page, h5.OpenOptions{Visible: true, Width: 640, Height: 400, Ephemeral: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "selftest: open page: %v\n", err)
		os.Exit(1)
	}
	defer sess.Close()

	png, err := sess.Screenshot()
	if err != nil {
		if strings.Contains(err.Error(), "not supported") {
			fmt.Fprintf(os.Stderr, "selftest: skipped: %v\n", err)
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
