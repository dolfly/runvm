//go:build linux

package h5

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math/bits"
	"os/exec"
)

// capturePNG grabs the whole X root window and returns it as PNG bytes.
//
// Only the private Xvfb this process starts is ever captured: a session that
// inherited a real DISPLAY refuses instead, because screenshotting a user's
// desktop because they asked for a page capture is a bug, not a feature.
func captureSupported(own bool, display string) error {
	if display == "" {
		return fmt.Errorf("screenshot: the page host reported no display")
	}
	if !own {
		return fmt.Errorf("screenshot: the page is drawing on an inherited display; run without DISPLAY (or under xvfb-run) so the capture is the page and not the desktop")
	}
	return nil
}

func capturePNG(display string) ([]byte, error) {
	xwd, err := exec.LookPath("xwd")
	if err != nil {
		return nil, fmt.Errorf("screenshot: xwd not found (apt install x11-apps)")
	}
	cmd := exec.Command(xwd, "-root", "-silent", "-display", display)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("screenshot: xwd: %v: %s", err, bytes.TrimSpace(errBuf.Bytes()))
	}

	img, err := decodeXWD(out.Bytes())
	if err != nil {
		return nil, err
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, fmt.Errorf("screenshot: encode png: %w", err)
	}
	return pngBuf.Bytes(), nil
}

// xwdHeader mirrors the 25 big-endian uint32 fields XWD puts before the pixel
// data. Offsets are positional; the format is fixed.
type xwdHeader struct {
	headerSize     uint32
	fileVersion    uint32
	pixmapFormat   uint32
	pixmapDepth    uint32
	pixmapWidth    uint32
	pixmapHeight   uint32
	xoffset        uint32
	byteOrder      uint32
	bitmapUnit     uint32
	bitmapBitOrder uint32
	bitmapPad      uint32
	bitsPerPixel   uint32
	bytesPerLine   uint32
	visualClass    uint32
	redMask        uint32
	greenMask      uint32
	blueMask       uint32
}

const xwdHeaderBytes = 100
const xwdColorBytes = 12

// decodeXWD converts an xwd dump of a TrueColor/direct-color visual into an
// image. Indexed (pseudo-color) dumps are refused rather than decoded wrong.
func decodeXWD(b []byte) (image.Image, error) {
	if len(b) < xwdHeaderBytes {
		return nil, fmt.Errorf("screenshot: xwd dump is %d bytes, too short for a header", len(b))
	}
	var h xwdHeader
	fields := []*uint32{
		&h.headerSize, &h.fileVersion, &h.pixmapFormat, &h.pixmapDepth,
		&h.pixmapWidth, &h.pixmapHeight, &h.xoffset, &h.byteOrder,
		&h.bitmapUnit, &h.bitmapBitOrder, &h.bitmapPad, &h.bitsPerPixel,
		&h.bytesPerLine, &h.visualClass, &h.redMask, &h.greenMask, &h.blueMask,
	}
	for i, f := range fields {
		*f = binary.BigEndian.Uint32(b[i*4:])
	}
	// fields 18..24 (bits_per_rgb, colormap_entries, ncolors, window_*) follow
	ncolors := binary.BigEndian.Uint32(b[76:])

	const trueColor, directColor = 4, 5
	if h.visualClass != trueColor && h.visualClass != directColor {
		return nil, fmt.Errorf("screenshot: unsupported visual class %d (only true-color dumps are decoded)", h.visualClass)
	}
	if h.bitsPerPixel != 24 && h.bitsPerPixel != 32 {
		return nil, fmt.Errorf("screenshot: unsupported depth %d bits per pixel", h.bitsPerPixel)
	}

	offset := int(h.headerSize) + int(ncolors)*xwdColorBytes
	need := offset + int(h.bytesPerLine)*int(h.pixmapHeight)
	if len(b) < need {
		return nil, fmt.Errorf("screenshot: truncated dump (%d of %d bytes)", len(b), need)
	}

	width, height := int(h.pixmapWidth), int(h.pixmapHeight)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	step := int(h.bitsPerPixel / 8)

	for y := 0; y < height; y++ {
		row := b[offset+y*int(h.bytesPerLine):]
		for x := 0; x < width; x++ {
			var px uint32
			switch {
			case step == 4 && h.byteOrder == 0: // LSBFirst
				px = binary.LittleEndian.Uint32(row[x*step:])
			case step == 4:
				px = binary.BigEndian.Uint32(row[x*step:])
			default: // 24bpp: three bytes
				if h.byteOrder == 0 {
					px = uint32(row[x*step])<<16 | uint32(row[x*step+1])<<8 | uint32(row[x*step+2])
				} else {
					px = uint32(row[x*step]) | uint32(row[x*step+1])<<8 | uint32(row[x*step+2])<<16
				}
			}
			i := img.PixOffset(x, y)
			img.Pix[i+0] = maskChannel(px, h.redMask)
			img.Pix[i+1] = maskChannel(px, h.greenMask)
			img.Pix[i+2] = maskChannel(px, h.blueMask)
			img.Pix[i+3] = 0xff
		}
	}
	return img, nil
}

// maskChannel pulls one colour channel out of a packed pixel and scales it to
// 0..255, whatever width the visual uses.
func maskChannel(px, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := uint(bits.TrailingZeros32(mask))
	val := (px & mask) >> shift
	max := mask >> shift
	return uint8(uint64(val) * 255 / uint64(max))
}
