package reader

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// buildPDF constructs a minimal valid single-page PDF whose content stream draws
// `text`, with a correct xref table (offsets computed programmatically).
func buildPDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET", text)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}

	var pdf bytes.Buffer
	fmt.Fprintf(&pdf, "%%PDF-1.4\n%%\xc3\xa4\xc3\xa4\n") // header + binary comment
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n", len(objs)+1)
	fmt.Fprintf(&pdf, "0000000000 65535 f \r\n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", len(objs)+1, xref)
	return pdf.Bytes()
}

func TestPDFReader_ExtractsText(t *testing.T) {
	pdfBytes := buildPDF("Hello PDF World")
	r := &PDFReader{}
	content, md, err := r.Read(context.Background(), pdfBytes, "x.pdf")
	if err != nil {
		t.Fatalf("pdf read: %v", err)
	}
	if !bytes.Contains([]byte(content), []byte("Hello PDF World")) {
		t.Errorf("expected extracted text to contain the drawn string; got %q (metadata: %v)", content, md)
	}
}
