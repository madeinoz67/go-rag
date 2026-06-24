package reader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// PDFReader extracts text from PDF documents using pdfcpu's content extraction.
// PDF text extraction is inherently unreliable for some documents (PRD risk R1);
// this reader pulls text from the Tj/TJ show-text operators in the content stream
// and reports page_count where available.
type PDFReader struct{}

func (r *PDFReader) Name() string                  { return "PDF" }
func (r *PDFReader) SupportedExtensions() []string { return []string{".pdf"} }
func (r *PDFReader) SupportedMimeTypes() []string  { return []string{"application/pdf"} }

func (r *PDFReader) Read(_ context.Context, data []byte, _ string) (string, map[string]any, error) {
	var buf bytes.Buffer
	err := api.ExtractContent(bytes.NewReader(data), nil, func(rs io.Reader, _ int) error {
		_, e := io.Copy(&buf, rs)
		return e
	}, nil)
	if err != nil {
		return "", nil, fmt.Errorf("pdf extract: %w", err)
	}
	text := extractShowText(buf.String())
	md := map[string]any{
		"format":     "pdf",
		"page_count": countPages(buf.String()),
	}
	return text, md, nil
}

var (
	reShow    = regexp.MustCompile(`\(([^)]*)\)\s*Tj`)
	reShowArr = regexp.MustCompile(`\[(.*?)\]\s*TJ`)
	reStr     = regexp.MustCompile(`\(([^)]*)\)`)
)

// extractShowText pulls literal strings out of Tj and TJ show operators.
func extractShowText(content string) string {
	var b strings.Builder
	for _, m := range reShow.FindAllStringSubmatch(content, -1) {
		b.WriteString(m[1])
		b.WriteByte(' ')
	}
	for _, m := range reShowArr.FindAllStringSubmatch(content, -1) {
		for _, s := range reStr.FindAllStringSubmatch(m[1], -1) {
			b.WriteString(s[1])
			b.WriteByte(' ')
		}
	}
	return strings.TrimSpace(b.String())
}

// countPages counts "BT" begin-text markers as a rough page/segment indicator.
func countPages(content string) int {
	return strings.Count(content, "BT")
}

// keep io referenced for future streaming.
var _ = io.EOF
