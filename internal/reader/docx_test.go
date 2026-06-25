package reader

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

// docxPara is one Word paragraph for the heading fixture: Style is the pStyle
// val (e.g. "Heading1", "Title") or "" for a plain body paragraph.
type docxPara struct {
	Style string
	Text  string
}

// mustBuildDocxParas builds a minimal .docx whose document.xml has the given
// paragraphs (each optionally tagged with a pStyle), used to exercise heading
// extraction (spec 031 US2).
func mustBuildDocxParas(t *testing.T, paras []docxPara) []byte {
	t.Helper()
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		body.WriteString("<w:p>")
		if p.Style != "" {
			body.WriteString(`<w:pPr><w:pStyle w:val="` + p.Style + `"/></w:pPr>`)
		}
		body.WriteString(`<w:r><w:t xml:space="preserve">` + p.Text + `</w:t></w:r></w:p>`)
	}
	body.WriteString(`</w:body></w:document>`)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(body.String())); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestDocxReader_HeadingSpans (spec 031 US2, SC-002, research R3): Word heading
// styles (Heading1..N + Title) become positional spans. DOCX is the richest
// heading signal — pStyle is explicit in the XML, not inferred.
func TestDocxReader_HeadingSpans(t *testing.T) {
	docx := mustBuildDocxParas(t, []docxPara{
		{"Heading1", "Introduction"},
		{"", "Body text one."},
		{"Heading2", "Background"},
		{"", "Body text two."},
		{"Title", "Doc Title Heading"},
	})
	r := &DocxReader{}
	content, md, err := r.Read(context.Background(), docx, "x.docx")
	if err != nil {
		t.Fatalf("docx read: %v", err)
	}
	spans, _ := md["heading_spans"].([]HeadingSpan)
	want := []HeadingSpan{
		{Level: 1, Text: "Introduction"},
		{Level: 2, Text: "Background"},
		{Level: 1, Text: "Doc Title Heading"},
	}
	if len(spans) != len(want) {
		t.Fatalf("heading spans: got %d, want %d (%+v)", len(spans), len(want), spans)
	}
	for i, w := range want {
		if spans[i].Level != w.Level || spans[i].Text != w.Text {
			t.Errorf("span[%d]: got %+v, want %+v", i, spans[i], w)
		}
	}
	// Body text + heading text all present in the extracted content.
	for _, want := range []string{"Introduction", "Background", "Doc Title Heading", "Body text one.", "Body text two."} {
		if !bytes.Contains([]byte(content), []byte(want)) {
			t.Errorf("content missing %q; got %q", want, content)
		}
	}
	// Offsets ascending, in-bounds, and each heading text lands at its offset.
	for i, sp := range spans {
		if sp.Offset < 0 || sp.Offset >= len(content) {
			t.Errorf("span[%d] offset %d out of bounds (len %d)", i, sp.Offset, len(content))
		}
		if i > 0 && spans[i-1].Offset >= sp.Offset {
			t.Errorf("span offsets not ascending: %d then %d", spans[i-1].Offset, sp.Offset)
		}
		if !strings.HasPrefix(content[sp.Offset:], sp.Text) {
			t.Errorf("span[%d] offset %d does not point at %q (got %q)", i, sp.Offset, sp.Text, content[sp.Offset:])
		}
	}
}

// TestDocxReader_NoHeadings (spec 031 US2, FR-007): a body-only docx (no heading
// styles) produces no spans — graceful, content still extracts.
func TestDocxReader_NoHeadings(t *testing.T) {
	docx := mustBuildDocxParas(t, []docxPara{
		{"", "Just a paragraph."},
		{"", "Another paragraph."},
	})
	r := &DocxReader{}
	_, md, err := r.Read(context.Background(), docx, "x.docx")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := md["heading_spans"]; ok {
		t.Errorf("did not expect heading_spans for a body-only docx; got %v", v)
	}
}
