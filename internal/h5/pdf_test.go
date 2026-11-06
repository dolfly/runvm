package h5

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"strings"
	"testing"
)

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 60), uint8(y * 90), 120, 255})
		}
	}
	return img
}

func TestEncodePDFStructure(t *testing.T) {
	doc, err := encodePDF(testImage(), 90)
	if err != nil {
		t.Fatalf("encodePDF: %v", err)
	}
	s := string(doc)
	for _, want := range []string{"%PDF-1.4", "/Type /Catalog", "/Type /Pages", "/Type /Page ",
		"/Subtype /Image", "/Filter /DCTDecode", "/MediaBox [0 0 4 3]", "xref", "trailer"} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF is missing %q", want)
		}
	}
	if !bytes.HasSuffix(doc, []byte("%%EOF\n")) {
		t.Error("PDF must end with the EOF marker")
	}

	// The embedded stream has to be a decodable JPEG of the source size.
	start := bytes.Index(doc, []byte("stream\n")) + len("stream\n")
	end := bytes.Index(doc, []byte("\nendstream"))
	img, err := jpeg.Decode(bytes.NewReader(doc[start:end]))
	if err != nil {
		t.Fatalf("embedded jpeg: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 3 {
		t.Errorf("embedded image size = %v", b)
	}
}

func TestPDFXrefOffsetsPointAtObjects(t *testing.T) {
	doc, err := encodePDF(testImage(), 80)
	if err != nil {
		t.Fatal(err)
	}
	xrefAt := bytes.Index(doc, []byte("\nxref\n"))
	if xrefAt < 0 {
		t.Fatal("no xref table")
	}
	lines := strings.Split(string(doc[xrefAt+len("\nxref\n"):]), "\n")
	if lines[0] != "0 6" {
		t.Fatalf("xref subsection = %q, want %q", lines[0], "0 6")
	}
	// lines[1] is the mandatory free entry; object n follows at lines[n+1].
	if fields := strings.Fields(lines[1]); fields[2] != "f" {
		t.Fatalf("first xref entry should be free, got %q", lines[1])
	}
	for i := 1; i <= 5; i++ {
		offset, err := strconv.Atoi(strings.Fields(lines[i+1])[0])
		if err != nil {
			t.Fatalf("xref line %d = %q: %v", i+1, lines[i+1], err)
		}
		if offset <= 0 || offset >= len(doc) {
			t.Errorf("object %d offset %d out of range", i, offset)
			continue
		}
		head := string(doc[offset:minInt(offset+16, len(doc))])
		if !strings.HasPrefix(head, strconv.Itoa(i)+" 0 obj") {
			t.Errorf("object %d offset points at %q", i, head)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
