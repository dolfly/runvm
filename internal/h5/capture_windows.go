//go:build windows

package h5

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// capturePNG grabs the page window with PrintWindow.
//
// WebView2 renders through DirectComposition, which is why PW_RENDERFULLCONTENT
// (0x2) matters: without it PrintWindow returns an empty frame for composited
// content. The window is ours (the host created it), so no desktop capture and
// no permissions are involved.
func capturePNG(string) ([]byte, error) {
	hwnd, err := ownWindow()
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
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procEnumWindows     = user32.NewProc("EnumWindows")
	procGetWindowPID    = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procGetClientRect   = user32.NewProc("GetClientRect")
	procPrintWindow     = user32.NewProc("PrintWindow")
	procGetDC           = user32.NewProc("GetDC")
	procReleaseDC       = user32.NewProc("ReleaseDC")
	procCreateCompatDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject    = gdi32.NewProc("SelectObject")
	procGetDIBits       = gdi32.NewProc("GetDIBits")
	procDeleteObject    = gdi32.NewProc("DeleteObject")
	procDeleteDC        = gdi32.NewProc("DeleteDC")
	procGetDpiForWindow = user32.NewProc("GetDpiForWindow")
)

type rect struct{ Left, Top, Right, Bottom int32 }

// ownWindow returns the largest visible top-level window owned by this process,
// which is the one webframe created for the page.
func ownWindow() (uintptr, error) {
	pid := uint32(windows.GetCurrentProcessId())
	var best uintptr
	var bestArea int64
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var winPID uint32
		procGetWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))
		if winPID != pid {
			return 1
		}
		if ok, _, _ := procIsWindowVisible.Call(hwnd); ok == 0 {
			return 1
		}
		var r rect
		if ok, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
			return 1
		}
		area := int64(r.Right-r.Left) * int64(r.Bottom-r.Top)
		if area > bestArea {
			best, bestArea = hwnd, area
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	if best == 0 {
		return 0, errors.New("screenshot: no visible window in this process (open the page with visible: true)")
	}
	return best, nil
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
