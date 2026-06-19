package reader

import (
	"context"
	"fmt"
)

// PDFReader extracts text from PDF documents. The real extraction uses pdfcpu and
// is wired in the next increment with a binary fixture for validation
// (PRD risk R1 — pdfcpu text extraction is unreliable; integration needs a real
// sample.pdf). Until then Read returns a clear "not yet integrated" error so the
// registry stays complete and the pipeline can detect unsupported-vs-pending.
type PDFReader struct{}

func (r *PDFReader) Name() string                  { return "PDF" }
func (r *PDFReader) SupportedExtensions() []string { return []string{".pdf"} }
func (r *PDFReader) SupportedMimeTypes() []string  { return []string{"application/pdf"} }

func (r *PDFReader) Read(_ context.Context, _ []byte, _ string) (string, map[string]any, error) {
	return "", map[string]any{"format": "pdf"}, fmt.Errorf("pdf reader: pdfcpu integration pending (task T021)")
}
