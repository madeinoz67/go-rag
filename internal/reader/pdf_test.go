package reader

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// buildPDF constructs a minimal valid single-page PDF whose content stream draws
// `text`, with a correct xref table (offsets computed programmatically). If info is
// non-empty, an Info dictionary object is appended and referenced from the trailer
// (spec 031 US1 — PDF document properties).
func buildPDF(text string, info map[string]string) []byte {
	stream := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET", text)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	infoRef := ""
	if len(info) > 0 {
		var b strings.Builder
		b.WriteString("<<")
		for _, k := range []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer", "CreationDate", "ModDate"} {
			if v, ok := info[k]; ok {
				b.WriteString(" /" + k + " (" + pdfEscapeString(v) + ")")
			}
		}
		b.WriteString(" >>")
		objs = append(objs, b.String())
		infoRef = " /Info 6 0 R"
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
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF", len(objs)+1, infoRef, xref)
	return pdf.Bytes()
}

// pdfEscapeString escapes the PDF literal-string metacharacters \ ( ).
// pdfEscapeString escapes the PDF literal-string metacharacters: backslash and parens.
func pdfEscapeString(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0x5C || r == '(' || r == ')' {
			b.WriteRune(0x5C)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestPDFReader_ExtractsText(t *testing.T) {
	pdfBytes := buildPDF("Hello PDF World", nil)
	r := &PDFReader{}
	content, md, err := r.Read(context.Background(), pdfBytes, "x.pdf")
	if err != nil {
		t.Fatalf("pdf read: %v", err)
	}
	if !bytes.Contains([]byte(content), []byte("Hello PDF World")) {
		t.Errorf("expected extracted text to contain the drawn string; got %q (metadata: %v)", content, md)
	}
}

// TestPDFReader_Metadata (spec 031 US1, SC-001): document properties in the Info
// dictionary populate Document.Metadata. Keywords flow into metadata["tags"] so
// the existing --tags filter (spec 014, tagsFromMetadata) matches them.
func TestPDFReader_Metadata(t *testing.T) {
	pdfBytes := buildPDF("body text here", map[string]string{
		"Title":    "Quarterly Report",
		"Author":   "Jane Doe",
		"Subject":  "Finance Q4",
		"Keywords": "revenue, q4, finance",
	})
	r := &PDFReader{}
	_, md, err := r.Read(context.Background(), pdfBytes, "report.pdf")
	if err != nil {
		t.Fatalf("pdf read: %v", err)
	}
	if got := md["title"]; got != "Quarterly Report" {
		t.Errorf("metadata[title]: got %v, want %q", got, "Quarterly Report")
	}
	if got := md["author"]; got != "Jane Doe" {
		t.Errorf("metadata[author]: got %v, want %q", got, "Jane Doe")
	}
	if got := md["subject"]; got != "Finance Q4" {
		t.Errorf("metadata[subject]: got %v, want %q", got, "Finance Q4")
	}
	tags, _ := md["tags"].([]string)
	if len(tags) != 3 {
		t.Fatalf("metadata[tags]: expected 3 tags, got %v", tags)
	}
	wantTags := map[string]bool{"revenue": true, "q4": true, "finance": true}
	for _, tg := range tags {
		if !wantTags[tg] {
			t.Errorf("metadata[tags]: unexpected tag %q in %v", tg, tags)
		}
	}
}

// TestPDFReader_NoMetadata (spec 031 US1, FR-007/SC-005): a PDF with no Info
// dictionary ingests cleanly — properties are gracefully absent, text still
// extracts. Not an error.
func TestPDFReader_NoMetadata(t *testing.T) {
	pdfBytes := buildPDF("plain body", nil)
	r := &PDFReader{}
	content, md, err := r.Read(context.Background(), pdfBytes, "plain.pdf")
	if err != nil {
		t.Fatalf("pdf read: %v", err)
	}
	for _, k := range []string{"title", "author", "subject", "tags"} {
		if _, ok := md[k]; ok {
			t.Errorf("did not expect metadata[%q] for a metadata-less PDF; got %v", k, md[k])
		}
	}
	if !bytes.Contains([]byte(content), []byte("plain body")) {
		t.Errorf("text should still extract; got %q", content)
	}
}
