package reader

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
)

// buildPDFPages builds a minimal valid N-page PDF (one content string per page)
// with a correct xref table, plus an optional Info dictionary. Used by bookmark
// tests where headings live on different pages (spec 031 US2 PDF).
func buildPDFPages(pages []string, info map[string]string) []byte {
	n := len(pages)
	fontObj := 3 + n
	var kids []string
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n),
	}
	for i := 0; i < n; i++ {
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontObj, 4+n+i))
	}
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i := 0; i < n; i++ {
		stream := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET", pages[i])
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	infoRef := ""
	if len(info) > 0 {
		var b strings.Builder
		b.WriteString("<<")
		for _, k := range []string{"Title", "Author", "Subject", "Keywords"} {
			if v, ok := info[k]; ok {
				b.WriteString(" /" + k + " (" + pdfEscapeString(v) + ")")
			}
		}
		b.WriteString(" >>")
		objs = append(objs, b.String())
		infoRef = fmt.Sprintf(" /Info %d 0 R", 4+2*n)
	}
	var pdf bytes.Buffer
	fmt.Fprintf(&pdf, "%%PDF-1.4\n")
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

// TestPDFReader_BookmarkHeadings (spec 031 US2 PDF, SC-002): a PDF bookmark
// outline becomes positional heading spans. Bookmarks are the high-precision
// primary signal (the author's declared structure); nested bookmarks deepen the
// level. The span Offset is the byte offset of the bookmark's page-start in the
// extracted content.
func TestPDFReader_BookmarkHeadings(t *testing.T) {
	pages := []string{"intro body alpha text", "methods body beta text", "results body gamma text"}
	base := buildPDFPages(pages, nil)
	var out bytes.Buffer
	bms := []pdfcpu.Bookmark{
		{Title: "Introduction", PageFrom: 1},
		{Title: "Methods", PageFrom: 2},
		{Title: "Results", PageFrom: 3, Kids: []pdfcpu.Bookmark{
			{Title: "Discussion", PageFrom: 3},
		}},
	}
	if err := api.AddBookmarks(bytes.NewReader(base), &out, bms, true, nil); err != nil {
		t.Fatalf("AddBookmarks: %v", err)
	}
	r := &PDFReader{}
	content, md, err := r.Read(context.Background(), out.Bytes(), "book.pdf")
	if err != nil {
		t.Fatalf("pdf read: %v", err)
	}
	spans, _ := md["heading_spans"].([]HeadingSpan)
	if len(spans) != 4 {
		t.Fatalf("heading spans: got %d, want 4 (%+v)", len(spans), spans)
	}
	byTitle := map[string]HeadingSpan{}
	for _, sp := range spans {
		byTitle[sp.Text] = sp
	}
	for _, want := range []string{"Introduction", "Methods", "Results", "Discussion"} {
		if _, ok := byTitle[want]; !ok {
			t.Errorf("missing heading %q in %+v", want, spans)
		}
	}
	if byTitle["Discussion"].Level != 2 {
		t.Errorf("Discussion Level: got %d, want 2", byTitle["Discussion"].Level)
	}
	if byTitle["Introduction"].Level != 1 {
		t.Errorf("Introduction Level: got %d, want 1", byTitle["Introduction"].Level)
	}
	if byTitle["Introduction"].Offset != 0 {
		t.Errorf("Introduction offset: got %d, want 0", byTitle["Introduction"].Offset)
	}
	if byTitle["Methods"].Offset <= byTitle["Introduction"].Offset {
		t.Errorf("Methods offset %d must exceed Introduction %d", byTitle["Methods"].Offset, byTitle["Introduction"].Offset)
	}
	if byTitle["Results"].Offset <= byTitle["Methods"].Offset {
		t.Errorf("Results offset %d must exceed Methods %d", byTitle["Results"].Offset, byTitle["Methods"].Offset)
	}
	for _, p := range pages {
		if !bytes.Contains([]byte(content), []byte(p)) {
			t.Errorf("content missing page text %q", p)
		}
	}
}

// TestPDFReader_NoBookmarks (spec 031 US2 PDF, FR-007): a PDF with no outline
// produces no heading spans — graceful (font-size fallback is a separate path).
func TestPDFReader_NoBookmarks(t *testing.T) {
	pdf := buildPDFPages([]string{"plain body no outline"}, nil)
	r := &PDFReader{}
	_, md, err := r.Read(context.Background(), pdf, "x.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := md["heading_spans"]; ok {
		t.Errorf("did not expect heading_spans for an outline-less PDF; got %v", v)
	}
}

// TestPDFReader_FontSizeHeadings (spec 031 US2 font-size fallback, SC-002): an
// OUTLINE-LESS PDF (no bookmarks) with a clear font-size gap promotes the largest
// font to a heading span. The page text is the parser's flat, so the span offset
// indexes the content directly (no offset hazard). The body (smaller font) is NOT
// promoted. Single-font pages yield no spans (no gap).
func TestPDFReader_FontSizeHeadings(t *testing.T) {
	stream := "BT /F1 24 Tf 1 0 0 1 72 700 Tm (Big Section Title) Tj /F1 10 Tf 1 0 0 1 72 670 Tm (Body text under the heading goes here) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	pdf := assemblePDF(objs, "")
	r := &PDFReader{}
	content, md, err := r.Read(context.Background(), pdf, "fontsize.pdf")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	spans, _ := md["heading_spans"].([]HeadingSpan)
	foundTitle := false
	for _, sp := range spans {
		if sp.Text == "Big Section Title" {
			foundTitle = true
			if sp.Level != 1 {
				t.Errorf("heading level: got %d, want 1", sp.Level)
			}
			if sp.Offset < 0 || sp.Offset >= len(content) {
				t.Errorf("heading offset %d out of bounds (content len %d)", sp.Offset, len(content))
			}
		}
		if strings.Contains(sp.Text, "Body text") {
			t.Errorf("body text must not be promoted to a heading; got span %+v", sp)
		}
	}
	if !foundTitle {
		t.Errorf("expected a 'Big Section Title' heading span; got %+v", spans)
	}
}
