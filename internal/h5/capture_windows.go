//go:build windows

package h5

import (
	"fmt"
	"image"
	"image/png"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// capturePNG grabs the page window with PrintWindow.
//
// WebView2 renders through DirectComposition, which is why PW_RENDERFULLCONTENT
// (0x2) matters: without it PrintWindow returns an empty frame for composited
// content. The window is ours (the host created it), so no desktop capture and
// no permissions are involved.
func capturePNG(_ string, pid uint32) ([]byte, error) {
	// The window may still be mapping right after the page reports ready;
	// give it a moment instead of failing on a race we can absorb.
	var (
		hwnd uintptr
		err  error
	)
	for i := 0; i < 10; i++ {
		hwnd, err = ownWindow(pid)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	img, err := printWindow(hwnd)
	if err != nil {
		return nil, err
	}
	var buf []byte
	w := &sliceWriter{&buf}
	if err := png.Encode(w, img); err != nil {
		return nil, fmt.Errorf("screenshot: encode png: %w", err)
	}
	return buf, nil
}

// captureSupported: Windows needs no display ownership, only a window.
func captureSupported(_ bool, _ string) error { return nil }

type sliceWriter struct{ b *[]byte }

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w.b = append(*w.b, p...)
	return len(p), nil
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procEnumWindows          = user32.NewProc("EnumWindows")
	procProcessIdToSessionId = kernel32.NewProc("ProcessIdToSessionId")
	procGetWindowPID         = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procGetClientRect        = user32.NewProc("GetClientRect")
	procPrintWindow          = user32.NewProc("PrintWindow")
	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procCreateCompatDC       = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatBmp      = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procGetDIBits            = gdi32.NewProc("GetDIBits")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procDeleteDC             = gdi32.NewProc("DeleteDC")
	procGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
)

type rect struct{ Left, Top, Right, Bottom int32 }

// ownWindow returns the largest top-level window owned by pid.
//
// pid is the page host's, not ours: the window is created by glaze inside the
// `runvm web-host` child process, so enumerating our own process finds nothing
// at all. Visible windows win over hidden ones: a hidden window can still be
// captured, but only after it has rendered, and on a session without a desktop
// nothing is visible at all.
func ownWindow(pid uint32) (uintptr, error) {
	type cand struct {
		hwnd    uintptr
		visible bool
		area    int64
	}
	var cands []cand
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var winPID uint32
		procGetWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))
		if winPID != pid {
			return 1
		}
		var r rect
		if ok, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
			return 1
		}
		area := int64(r.Right-r.Left) * int64(r.Bottom-r.Top)
		if area <= 0 {
			return 1
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		cands = append(cands, cand{hwnd: hwnd, visible: vis != 0, area: area})
		return 1
	})
	procEnumWindows.Call(cb, 0)

	var best cand
	for _, c := range cands {
		switch {
		case best.hwnd == 0,
			c.visible && !best.visible,
			c.visible == best.visible && c.area > best.area:
			best = c
		}
	}
	if best.hwnd == 0 {
		// Diagnostics beat a bare failure: this is the one place where the
		// environment (session 0, no desktop) can be the whole story.
		session := uint32(0)
		procProcessIdToSessionId.Call(uintptr(pid), uintptr(unsafe.Pointer(&session)))
		total := 0
		procEnumWindows.Call(syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
			total++
			return 1
		}), 0)
		return 0, fmt.Errorf("screenshot: no window in this process "+
			"(pid %d, session %d, %d process windows, %d top-level windows system-wide; "+
			"open the page with visible: true)", pid, session, len(cands), total)
	}
	return best.hwnd, nil
}

func printWindow(hwnd uintptr) (image.Image, error) {
	var r rect
	if ok, _, err := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return nil, fmt.Errorf("screenshot: GetClientRect: %w", err)
	}
	width, height := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("screenshot: window has no client area (%dx%d)", width, height)
	}

	// Render at 96 DPI-equivalent: PrintWindow draws the window as composited.
	screenDC, _, err := procGetDC.Call(hwnd)
	if screenDC == 0 {
		return nil, fmt.Errorf("screenshot: GetDC: %w", err)
	}
	defer procReleaseDC.Call(hwnd, screenDC)

	memDC, _, err := procCreateCompatDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("screenshot: CreateCompatibleDC: %w", err)
	}
	defer procDeleteDC.Call(memDC)

	bmp, _, err := procCreateCompatBmp.Call(screenDC, uintptr(width), uintptr(height))
	if bmp == 0 {
		return nil, fmt.Errorf("screenshot: CreateCompatibleBitmap: %w", err)
	}
	defer procDeleteObject.Call(bmp)

	old, _, _ := procSelectObject.Call(memDC, bmp)
	defer procSelectObject.Call(memDC, old)

	const pwRenderFullContent = 2
	if ok, _, err := procPrintWindow.Call(hwnd, memDC, pwRenderFullContent); ok == 0 {
		return nil, fmt.Errorf("screenshot: PrintWindow: %w", err)
	}

	type bitmapInfoHeader struct {
		Size          uint32
		Width         int32
		Height        int32
		Planes        uint16
		BitCount      uint16
		Compression   uint32
		SizeImage     uint32
		XPelsPerMeter int32
		YPelsPerMeter int32
		ClrUsed       uint32
		ClrImportant  uint32
	}
	type bitmapInfo struct {
		Header bitmapInfoHeader
		Colors [1]uint32
	}
	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(width),
		Height:      -int32(height), // top-down
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
	}}
	buf := make([]byte, width*height*4)
	if ok, _, err := procGetDIBits.Call(memDC, bmp, 0, uintptr(height),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); ok == 0 {
		return nil, fmt.Errorf("screenshot: GetDIBits: %w", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			o := img.PixOffset(x, y)
			img.Pix[o+0] = buf[i+2]
			img.Pix[o+1] = buf[i+1]
			img.Pix[o+2] = buf[i+0]
			img.Pix[o+3] = 0xff
		}
	}
	return img, nil
}
