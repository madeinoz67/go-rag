package reader

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// buildChartImagePDF renders a simple 3-bar "declining" chart to JPEG and embeds
// it as a PDF image XObject (/DCTDecode), so extractPDFImages has a real image to
// extract (spec 031 US4 — the reader side that was only fake-tested until now).
func buildChartImagePDF(t *testing.T) []byte {
	t.Helper()
	const w, h = 200, 150
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	bars := []struct {
		x, top, wid int
		c           color.RGBA
	}{
		{30, 30, 40, color.RGBA{220, 60, 60, 255}},
		{90, 60, 40, color.RGBA{60, 180, 80, 255}},
		{150, 90, 40, color.RGBA{60, 90, 220, 255}},
	}
	for _, b := range bars {
		for x := b.x; x < b.x+b.wid; x++ {
			for y := b.top; y < h-20; y++ {
				img.Set(x, y, b.c)
			}
		}
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	jpegBytes := jb.Bytes()
	content := "q 200 0 0 150 50 400 cm /Im1 Do Q"
	var pdf bytes.Buffer
	fmt.Fprintf(&pdf, "%%PDF-1.4\n")
	offsets := make([]int, 6)
	writeObj := func(n int, body string) {
		offsets[n] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>")
	writeObj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	// obj5: image XObject with a binary JPEG stream.
	offsets[5] = pdf.Len()
	fmt.Fprintf(&pdf, "5 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", w, h, len(jpegBytes))
	pdf.Write(jpegBytes)
	pdf.WriteString("\nendstream\nendobj\n")
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 6\n0000000000 65535 f \r\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", xref)
	return pdf.Bytes()
}

// TestPDFReader_ExtractsEmbeddedImage (spec 031 US4): a PDF with a real JPEG image
// XObject yields an ImageRef with non-empty bytes (the captioner's input). Also
// confirms the bytes are a decodable JPEG (SOI 0xFFD8) — if pdfcpu returned raw
// decoded pixels instead, the vision model could not decode them and SC-004 would
// fail, so this guards the captioner's input format.
func TestPDFReader_ExtractsEmbeddedImage(t *testing.T) {
	pdf := buildChartImagePDF(t)
	r := &PDFReader{}
	_, md, err := r.Read(context.Background(), pdf, "chart.pdf")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	imgs, _ := md["images"].([]ImageRef)
	if len(imgs) == 0 {
		t.Fatalf("expected >=1 extracted image; md keys: format=%v page_count=%v (image extraction failed on a real image XObject)", md["format"], md["page_count"])
	}
	b := imgs[0].Bytes
	if len(b) < 4 {
		t.Fatalf("extracted image bytes too short: %d", len(b))
	}
	// JPEG SOI marker — confirms the bytes are an encoded JPEG the vision model can decode.
	if b[0] != 0xFF || b[1] != 0xD8 {
		t.Errorf("extracted image bytes are not a JPEG (expected SOI 0xFFD8); first bytes: % x; FileType=%q — captioner would send undecodable bytes to the vision model", b[:4], imgs[0].FileType)
	}
	t.Logf("extracted image: %d bytes, FileType=%q, dims=%dx%d — valid JPEG for captioning", len(b), imgs[0].FileType, imgs[0].Width, imgs[0].Height)
}
