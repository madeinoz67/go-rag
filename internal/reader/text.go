package reader

import (
	"context"
	"strings"
	"unicode"
)

// TextReader extracts plain text from .txt/.log/.csv files.
type TextReader struct{}

func (r *TextReader) Name() string                  { return "Text" }
func (r *TextReader) SupportedExtensions() []string { return []string{".txt", ".log", ".csv"} }
func (r *TextReader) SupportedMimeTypes() []string  { return []string{"text/plain"} }

func (r *TextReader) Read(_ context.Context, data []byte, _ string) (string, map[string]any, error) {
	s := string(data)
	md := map[string]any{
		"encoding":   "utf-8",
		"line_count": countLines(s),
		"char_count": len(s),
	}
	if spans := detectTextHeadings(s); len(spans) > 0 {
		md["heading_spans"] = spans // spec 031 US2 — non-identity sidecar (pipeline strips before identity)
	}
	return s, md, nil
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// detectTextHeadings (spec 031 US2, research R4) best-effort detects heading
// lines in plain text and returns positional spans whose Offset is the byte index
// of the heading line's start in s (the unchanged content the reader returns).
// Patterns, in priority order per line:
//   - Setext underline: a following line of "=" (>=3) marks the line H1; "-" H2.
//   - ALL CAPS line: 3..80 chars, has an uppercase letter, no lowercase, no
//     terminal sentence punctuation -> H1.
//   - Short ":"-terminated line (<60 chars, has a letter) -> H2.
// Lower confidence than Markdown/DOCX; text without patterns yields no spans.
func detectTextHeadings(s string) []HeadingSpan {
	lines := strings.Split(s, "\n")
	offsets := make([]int, len(lines))
	off := 0
	for i, ln := range lines {
		offsets[i] = off
		off += len(ln) + 1 // +1 for the "\n" removed by Split
	}
	var spans []HeadingSpan
	for i, ln := range lines {
		t := strings.TrimRight(ln, "\r")
		h := strings.TrimSpace(t)
		if h == "" {
			continue
		}
		// Setext: the NEXT line being a run of '='/'-' makes this line a heading.
		if i+1 < len(lines) {
			under := strings.TrimRight(lines[i+1], "\r")
			if isUnderline(under, '=') {
				spans = append(spans, HeadingSpan{Level: 1, Text: h, Offset: offsets[i]})
				continue
			}
			if isUnderline(under, '-') {
				spans = append(spans, HeadingSpan{Level: 2, Text: h, Offset: offsets[i]})
				continue
			}
		}
		if isAllCapsHeading(h) {
			spans = append(spans, HeadingSpan{Level: 1, Text: h, Offset: offsets[i]})
			continue
		}
		if isColonHeading(h) {
			spans = append(spans, HeadingSpan{Level: 2, Text: h, Offset: offsets[i]})
		}
	}
	return spans
}

func isUnderline(s string, c rune) bool {
	if len(s) < 3 {
		return false
	}
	for _, r := range s {
		if r != c {
			return false
		}
	}
	return true
}

func isAllCapsHeading(s string) bool {
	n := len(s)
	if n < 3 || n > 80 {
		return false
	}
	hasUpper, hasLower := false, false
	for _, r := range s {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	if !hasUpper || hasLower {
		return false
	}
	switch s[n-1] {
	case '.', ',', ';', ':', '!', '?':
		return false
	}
	return true
}

func isColonHeading(s string) bool {
	n := len(s)
	if n < 2 || n > 60 || !strings.HasSuffix(s, ":") {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
