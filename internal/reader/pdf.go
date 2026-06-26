package reader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
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
	rs := bytes.NewReader(data)
	pageText := map[int]string{} // pdfcpu page number -> extracted text
	if err := api.ExtractContent(rs, nil, func(rd io.Reader, page int) error {
		var b bytes.Buffer
		if _, e := io.Copy(&b, rd); e != nil {
			return e
		}
		raw := b.String()
		legacy := extractShowText(raw)
		frags, flat, amb := parsePositionedText(raw)
		// Table detection (spec 031 T017): parse the content stream for positioned
		// text and splice any detected grid tables into the parser's doc-ordered
		// flat text. Non-table or ambiguous pages keep the identity-stable legacy
		// extractShowText output (never worse than today).
		if !amb && len(frags) >= 4 {
			if rendered := renderPageWithTables(flat, frags); rendered != flat {
				pageText[page] = rendered
				return nil
			}
		}
		pageText[page] = legacy
		return nil
	}, nil); err != nil {
		return "", nil, fmt.Errorf("pdf extract: %w", err)
	}
	text, pageOffsets := joinPageText(pageText)
	md := map[string]any{
		"format":     "pdf",
		"page_count": len(pageOffsets),
	}
	populatePDFMetadata(data, md)
	if spans := pdfHeadingSpans(data, pageOffsets); len(spans) > 0 {
		md["heading_spans"] = spans // spec 031 US2 — non-identity sidecar (pipeline strips before identity)
	}
	return text, md, nil
}

// joinPageText concatenates per-page extracted text in page order and returns the
// joined text plus a map of page number -> byte offset where that page's text
// begins (spec 031 US2 — bookmarks map a heading to its page's offset).
func joinPageText(pageText map[int]string) (string, map[int]int) {
	pages := make([]int, 0, len(pageText))
	for p := range pageText {
		pages = append(pages, p)
	}
	sort.Ints(pages)
	var content strings.Builder
	pageOffsets := make(map[int]int, len(pages))
	for _, p := range pages {
		pageOffsets[p] = content.Len()
		content.WriteString(pageText[p])
		content.WriteByte('\n')
	}
	return strings.TrimRight(content.String(), "\n"), pageOffsets
}

// pdfHeadingSpans (spec 031 US2 PDF, research R3/T002) reads the PDF bookmark
// outline via pdfcpu and maps it to positional heading spans. A bookmark's
// PageFrom (1-based) is translated to the byte offset of that page's start in the
// extracted content. Nested bookmarks deepen the level. PDFs without an outline
// (api.Bookmarks returns ErrNoOutlines) produce no spans — the font-size fallback
// is a separate path.
func pdfHeadingSpans(data []byte, pageOffsets map[int]int) []HeadingSpan {
	bms, err := api.Bookmarks(bytes.NewReader(data), nil)
	if err != nil || len(bms) == 0 {
		return nil
	}
	var spans []HeadingSpan
	var walk func(kids []pdfcpu.Bookmark, level int)
	walk = func(kids []pdfcpu.Bookmark, level int) {
		for _, bm := range kids {
			if title := strings.TrimSpace(bm.Title); title != "" {
				if off, ok := pageOffsets[bm.PageFrom]; ok {
					spans = append(spans, HeadingSpan{Level: level, Text: title, Offset: off})
				}
			}
			walk(bm.Kids, level+1)
		}
	}
	walk(bms, 1)
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].Offset < spans[j].Offset })
	return spans
}

// populatePDFMetadata (spec 031 US1) reads the PDF Info dictionary via pdfcpu and
// merges document properties into md. Title/Author/Subject become first-class
// metadata fields; the Keywords field is split into metadata["tags"] so it flows
// into the existing --tags filter (spec 014 bridge, tagsFromMetadata). Absent or
// unreadable properties are silently omitted — a metadata-less PDF is not an error
// (FR-001/FR-007/SC-005). The enriched metadata enters the document identity hash
// via GenerateID (Constitution II — metadata is stable document content); the
// pipeline passes md straight through to Document.Metadata.
func populatePDFMetadata(data []byte, md map[string]any) {
	// api.PDFInfo (not api.Properties) is the right accessor: pdfcpu reserves
	// "Properties" for custom document-info keys and excludes the standard Info
	// dict fields; PDFInfo returns Title/Author/Subject/Keywords directly (spec
	// 031 research.md, T002 spike). Unreadable -> properties gracefully absent.
	info, err := api.PDFInfo(bytes.NewReader(data), "", nil, false, nil)
	if err != nil || info == nil {
		return
	}
	if v := strings.TrimSpace(info.Title); v != "" {
		md["title"] = v
	}
	if v := strings.TrimSpace(info.Author); v != "" {
		md["author"] = v
	}
	if v := strings.TrimSpace(info.Subject); v != "" {
		md["subject"] = v
	}
	if len(info.Keywords) > 0 {
		md["tags"] = info.Keywords
	}
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

// keep io referenced for future streaming.
var _ = io.EOF
