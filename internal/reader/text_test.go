package reader

import (
	"context"
	"testing"
)

// TestTextReader_HeadingSpans (spec 031 US2, SC-002, research R4): best-effort
// heading detection in plain text emits positional spans. Covers all three
// patterns — ALL CAPS, Setext (====) underline, and short ":"-terminated lines.
// Content is returned UNCHANGED (offsets index into the raw text the chunker
// sees); spans are non-identity sidecars stripped by the pipeline.
func TestTextReader_HeadingSpans(t *testing.T) {
	src := "INTRODUCTION\nbody one\nMETHODS\n========\nResults Section:\nbody two\n"
	r := &TextReader{}
	content, md, err := r.Read(context.Background(), []byte(src), "x.txt")
	if err != nil {
		t.Fatal(err)
	}
	if content != src {
		t.Errorf("text content must be unchanged; got %q", content)
	}
	spans, _ := md["heading_spans"].([]HeadingSpan)
	want := []HeadingSpan{
		{Level: 1, Text: "INTRODUCTION"},
		{Level: 1, Text: "METHODS"},
		{Level: 2, Text: "Results Section:"},
	}
	if len(spans) != len(want) {
		t.Fatalf("heading spans: got %d, want %d (%+v)", len(spans), len(want), spans)
	}
	for i, w := range want {
		if spans[i].Level != w.Level || spans[i].Text != w.Text {
			t.Errorf("span[%d]: got %+v, want %+v", i, spans[i], w)
		}
	}
	// Offsets ascending, in-bounds, indexing into the returned content.
	for i, sp := range spans {
		if sp.Offset < 0 || sp.Offset >= len(content) {
			t.Errorf("span[%d] offset %d out of bounds (content len %d)", i, sp.Offset, len(content))
		}
		if i > 0 && spans[i-1].Offset >= sp.Offset {
			t.Errorf("span offsets not ascending: %d then %d", spans[i-1].Offset, sp.Offset)
		}
	}
	// Spot check: first heading sits at offset 0.
	if spans[0].Offset != 0 {
		t.Errorf("first heading offset: got %d, want 0", spans[0].Offset)
	}
}

// TestTextReader_NoHeadings (spec 031 US2, FR-007): prose / data without heading
// patterns produces no spans — graceful, no error, content unchanged.
func TestTextReader_NoHeadings(t *testing.T) {
	src := []byte("just some prose with no structure at all\nit flows on\n")
	r := &TextReader{}
	content, md, err := r.Read(context.Background(), src, "x.txt")
	if err != nil {
		t.Fatal(err)
	}
	if content != string(src) {
		t.Errorf("content changed: %q", content)
	}
	if v, ok := md["heading_spans"]; ok {
		t.Errorf("did not expect heading_spans for heading-less text; got %v", v)
	}
}
