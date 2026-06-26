package reader

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// buildPDFTable builds a single-page PDF whose content stream is a Tm-positioned
// table (columns align precisely). Used to exercise end-to-end table extraction
// through PDFReader.Read (spec 031 US3 T016/SC-003).
func buildPDFTable(cells [][]string) []byte {
	var stream strings.Builder
	stream.WriteString("BT /F1 10 Tf ")
	for r, row := range cells {
		y := 700 - r*20
		for c, cell := range row {
			x := 72 + c*100
			fmt.Fprintf(&stream, "1 0 0 1 %d %d Tm (%s) Tj ", x, y, cell)
		}
	}
	stream.WriteString("ET")
	s := stream.String()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s),
	}
	return assemblePDF(objs, "")
}

// assemblePDF wraps raw PDF objects (1-based) with a correct xref + trailer. Shared
// by table fixtures; infoRef is "/Info N 0 R" or "".
func assemblePDF(objs []string, infoRef string) []byte {
	var pdf bytes.Buffer
	fmt.Fprintf(&pdf, "%%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF", len(objs)+1, infoRef, xref)
	return pdf.Bytes()
}

// TestPDFReader_TableExtracted (spec 031 US3, SC-003): a table PDF is read with
// the grid rendered as a Markdown table in the content (searchable, pipes intact).
func TestPDFReader_TableExtracted(t *testing.T) {
	pdf := buildPDFTable([][]string{
		{"Name", "Age", "City"},
		{"Alice", "30", "Perth"},
		{"Bob", "25", "Sydney"},
	})
	r := &PDFReader{}
	content, _, err := r.Read(context.Background(), pdf, "table.pdf")
	if err != nil {
		t.Fatalf("pdf read: %v", err)
	}
	for _, want := range []string{
		"| Name | Age | City |",
		"|---|---|---|",
		"| Alice | 30 | Perth |",
		"| Bob | 25 | Sydney |",
	} {
		if !bytes.Contains([]byte(content), []byte(want)) {
			t.Errorf("content missing %q; got:\n%s", want, content)
		}
	}
}

// TestPDFReader_ProseNoTable (spec 031 US3, FR-007): a prose PDF yields no table
// pipes — content is the plain extracted text (never worse than today).
func TestPDFReader_ProseNoTable(t *testing.T) {
	stream := "BT /F1 10 Tf 72 700 Td (A single line of body prose with no tabular structure at all) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	pdf := assemblePDF(objs, "")
	r := &PDFReader{}
	content, _, err := r.Read(context.Background(), pdf, "prose.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "|---|") {
		t.Errorf("prose must not contain a table; got:\n%s", content)
	}
	if !strings.Contains(content, "body prose") {
		t.Errorf("prose text should still extract; got:\n%s", content)
	}
}
