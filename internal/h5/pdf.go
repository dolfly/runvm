package h5

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
)

// encodePDF wraps one raster image in a single-page PDF.
//
// This is deliberately a raster PDF, not a vector one: the cross-platform
// WebView APIs expose no print-to-PDF, so the honest options are a picture of
// the page or nothing. For "put the page in a slide deck" that is enough, and
// docs/ROADMAP.md keeps the native-print fork as the path to a real one.
func encodePDF(img image.Image, quality int) ([]byte, error) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("pdf: encode jpeg: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	var out bytes.Buffer
	offsets := make([]int, 0, 6)
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	out.WriteString("%PDF-1.4\n")
	// Binary comment: tells tools the file is not text.
	out.Write([]byte{'%', 0xe2, 0xe3, 0xcf, 0xd3, '\n'})

	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] "+
		"/Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>", w, h))

	// The image is the JPEG stream verbatim: DCTDecode means "this is already
	// a JPEG", so nothing is re-encoded and the file stays small.
	offsets = append(offsets, out.Len())
	fmt.Fprintf(&out, "4 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d "+
		"/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", w, h, jpg.Len())
	out.Write(jpg.Bytes())
	out.WriteString("\nendstream\nendobj\n")

	content := fmt.Sprintf("q\n%d 0 0 %d 0 0 cm\n/Im0 Do\nQ\n", w, h)
	offsets = append(offsets, out.Len())
	fmt.Fprintf(&out, "5 0 obj\n<< /Length %d >>\nstream\n%s endstream\nendobj\n", len(content), content)

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", len(offsets)+1)
	out.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes(), nil
}
