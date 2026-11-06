//go:build linux

package h5

import (
	"encoding/binary"
	"image/color"
	"testing"
)

// buildXWD assembles a tiny true-colour dump so the decoder can be tested
// without an X server: 2x2 pixels, 32 bpp, LSB-first, one colour per pixel.
func buildXWD(pixels []uint32) []byte {
	buf := make([]byte, xwdHeaderBytes+len(pixels)*4)
	put := func(off int, v uint32) { binary.BigEndian.PutUint32(buf[off:], v) }
	put(0, xwdHeaderBytes) // header_size
	put(4, 7)              // file_version
	put(8, 2)              // pixmap_format (ZPixmap)
	put(12, 24)            // pixmap_depth
	put(16, 2)             // width
	put(20, 2)             // height
	put(24, 0)             // xoffset
	put(28, 0)             // byte_order: LSBFirst
	put(32, 32)            // bitmap_unit
	put(36, 0)             // bitmap_bit_order
	put(40, 32)            // bitmap_pad
	put(44, 32)            // bits_per_pixel
	put(48, 8)             // bytes_per_line
	put(52, 4)             // visual_class: TrueColor
	put(56, 0x00ff0000)    // red_mask
	put(60, 0x0000ff00)    // green_mask
	put(64, 0x000000ff)    // blue_mask
	put(68, 8)             // bits_per_rgb
	put(72, 256)           // colormap_entries
	put(76, 0)             // ncolors
	for i, px := range pixels {
		binary.LittleEndian.PutUint32(buf[xwdHeaderBytes+i*4:], px)
	}
	return buf
}

func TestDecodeXWD(t *testing.T) {
	// The masks below are R=0x00ff0000, G=0x0000ff00, B=0x000000ff.
	img, err := decodeXWD(buildXWD([]uint32{
		0x00ff0000, // red
		0x0000ff00, // green
		0x000000ff, // blue
		0x00ffffff, // white
	}))
	if err != nil {
		t.Fatalf("decodeXWD: %v", err)
	}
	if got := img.Bounds().Size(); got.X != 2 || got.Y != 2 {
		t.Fatalf("size = %v", got)
	}
	want := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}}
	for i, w := range want {
		x, y := i%2, i/2
		r, g, b, a := img.At(x, y).RGBA()
		got := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
		if got != w {
			t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, w)
		}
	}
}

func TestDecodeXWDRejects(t *testing.T) {
	if _, err := decodeXWD([]byte{1, 2, 3}); err == nil {
		t.Error("short buffer should be refused")
	}

	pseudo := buildXWD([]uint32{0, 0, 0, 0})
	binary.BigEndian.PutUint32(pseudo[52:], 3) // PseudoColor
	if _, err := decodeXWD(pseudo); err == nil {
		t.Error("indexed visuals should be refused rather than decoded wrong")
	}

	truncated := buildXWD([]uint32{0, 0, 0, 0})[:xwdHeaderBytes+4]
	if _, err := decodeXWD(truncated); err == nil {
		t.Error("truncated pixel data should be refused")
	}
}
