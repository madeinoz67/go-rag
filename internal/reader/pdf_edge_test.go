package reader

import (
	"context"
	"fmt"
	"testing"
)

// TestPDFReader_CorruptedNoPanic (spec 031 US3 Polish, FR-007/edge cases): the
// reader must NEVER panic on malformed/corrupted/non-PDF input — it returns an
// error or empty content. A worker-goroutine panic would crash the ingest worker.
func TestPDFReader_CorruptedNoPanic(t *testing.T) {
	r := &PDFReader{}
	inputs := [][]byte{
		[]byte("not a pdf at all"),
		[]byte("%PDF-1.4\ntruncated without xref"),
		{0x00, 0x01, 0x02, 0x03, 0xff, 0xfe},
		nil,
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Errorf("Read panicked on input %q: %v", in, rec)
				}
			}()
			_, _, _ = r.Read(context.Background(), in, "x.pdf")
		}()
	}
}

// TestPDFReader_EmptyContent (spec 031 US3 Polish, scanned-PDF edge case): a
// valid PDF whose content stream has no text operators (image-only / scanned-like)
// ingests cleanly with empty content — no error, no panic. OCR is out of scope;
// the doc is searchable only if captioning produces a caption chunk (US4).
func TestPDFReader_EmptyContent(t *testing.T) {
	stream := "BT ET" // a text object with no show-text operators
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	pdf := assemblePDF(objs, "")
	r := &PDFReader{}
	content, md, err := r.Read(context.Background(), pdf, "empty.pdf")
	if err != nil {
		t.Fatalf("empty-content PDF should ingest cleanly, got err: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content for a text-less PDF, got %q", content)
	}
	if md["format"] != "pdf" {
		t.Errorf("expected format=pdf metadata, got %v", md["format"])
	}
}
