package reader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"

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
	// spec 031 US2: decide heading source up front. Outlined PDFs use bookmark
	// spans (page-granularity; identity-stable legacy text). Outline-less PDFs use
	// the parser's flat text as the page text + font-size heading detection — the
	// flat IS the page text, so font-size frag offsets index it directly (no offset
	// hazard). Bookmarks win when present (the high-precision signal).
	bms, _ := api.Bookmarks(bytes.NewReader(data), nil)
	hasBookmarks := len(bms) > 0

	pageText := map[int]string{}            // pdfcpu page number -> extracted text
	pageFrags := map[int][]positionedText{} // outline-less non-table pages (for font-size)
	if err := api.ExtractContent(rs, nil, func(rd io.Reader, page int) error {
		var b bytes.Buffer
		if _, e := io.Copy(&b, rd); e != nil {
			return e
		}
		raw := b.String()
		frags, flat, amb := parsePositionedText(raw)
		// Table detection (spec 031 T017) on the parser's flat text.
		if !amb && len(frags) >= 4 {
			if rendered := renderPageWithTables(flat, frags); rendered != flat {
				pageText[page] = rendered // table detected (flat-based, spliced); no font-size spans
				return nil
			}
		}
		// No table on this page.
		if hasBookmarks {
			pageText[page] = extractShowText(raw) // outlined: identity-stable legacy
		} else {
			pageText[page] = flat // outline-less: flat so font-size offsets align
			if !amb {
				pageFrags[page] = frags
			}
		}
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
	var spans []HeadingSpan
	if hasBookmarks {
		spans = bookmarkHeadingSpans(bms, pageOffsets)
	} else {
		spans = fontSizeHeadingSpans(pageFrags, pageOffsets)
	}
	if len(spans) > 0 {
		md["heading_spans"] = spans // spec 031 US2 — non-identity sidecar (pipeline strips before identity)
	}
	if imgs := extractPDFImages(data); len(imgs) > 0 {
		md["images"] = imgs // spec 031 US4 — transient image bytes; pipeline strips before identity (Constitution II)
	}
	return text, md, nil
}

// extractPDFImages (spec 031 US4) reads embedded PDF images via pdfcpu into
// transient ImageRefs (bytes + page position) for the pipeline to caption
// post-ACK. Bounded: a 2 MiB per-image cap and a 32-image per-doc cap protect
// the in-memory job queue from image-heavy PDFs. Returns nil on any error or
// when no images are present (graceful — images are optional). The reader makes
// NO model calls; bytes never persist (the pipeline discards them after captioning).
func extractPDFImages(data []byte) []ImageRef {
	rawImgs, err := api.ExtractImagesRaw(bytes.NewReader(data), nil, nil)
	if err != nil {
		return nil
	}
	const maxImageBytes = 2 * 1024 * 1024
	const maxImages = 32
	var refs []ImageRef
	for _, m := range rawImgs {
		for _, img := range m {
			if img.Reader == nil {
				continue // CRITICAL: a nil-interface reader panics inside io.ReadAll
			}
			b, err := io.ReadAll(img.Reader)
			if err != nil || len(b) == 0 || len(b) > maxImageBytes {
				continue
			}
			refs = append(refs, ImageRef{
				PageNr:   img.PageNr,
				Bytes:    b,
				Width:    img.Width,
				Height:   img.Height,
				FileType: img.FileType,
			})
			if len(refs) >= maxImages {
				return refs
			}
		}
	}
	return refs
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
func bookmarkHeadingSpans(bms []pdfcpu.Bookmark, pageOffsets map[int]int) []HeadingSpan {
	if len(bms) == 0 {
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

// fontSizeHeadingSpans (spec 031 US2 — font-size fallback for OUTLINE-LESS PDFs)
// promotes the largest font sizes on each page to heading spans. It runs ONLY on
// pages whose text is the parser's flat (no detected table), so each frag's
// ByteStart indexes the page text directly — sidestepping the offset hazard that
// table splicing would introduce (table pages get the spliced render + no
// font-size spans). Conservative: requires a clear size gap (max > 1.15x median);
// degrades to no spans on ambiguity (FR-007).
func fontSizeHeadingSpans(pageFrags map[int][]positionedText, pageOffsets map[int]int) []HeadingSpan {
	var spans []HeadingSpan
	for page, frags := range pageFrags {
		var sizes []float64
		for _, f := range frags {
			if f.FontSize > 0 {
				sizes = append(sizes, f.FontSize)
			}
		}
		if len(sizes) < 2 {
			continue
		}
		sort.Float64s(sizes)
		median := sizes[(len(sizes)-1)/2] // lower median = the body size (so a few large headings stand out)
		maxSize := sizes[len(sizes)-1]
		if maxSize <= median*1.15 {
			continue // no clear size gap -> not heading structure
		}
		threshold := median + (maxSize-median)*0.6
		pageOff := pageOffsets[page]
		for _, f := range frags {
			if f.FontSize >= threshold && isHeadingText(f.Text) {
				spans = append(spans, HeadingSpan{Level: 1, Text: strings.TrimSpace(f.Text), Offset: pageOff + f.ByteStart})
			}
		}
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].Offset < spans[j].Offset })
	return spans
}

// isHeadingText reports whether a string looks like a heading: 2..80 chars, no
// terminal sentence punctuation, and contains a letter.
func isHeadingText(s string) bool {
	s = strings.TrimSpace(s)
	n := len(s)
	if n < 2 || n > 80 {
		return false
	}
	switch s[n-1] {
	case '.', ',', ';', ':', '!', '?':
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
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
