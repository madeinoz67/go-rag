package chunk

import (
	"strings"
	"unicode/utf8"
)

// sentences.go provides the boundary-awareness primitives for chunking (audit
// H10 / spec 013): detecting sentence and paragraph terminators so Split can snap
// chunk boundaries to structural edges instead of cutting mid-sentence.
//
// Detection is rule-based and pure Go (no NLP): ASCII ".!?" + CJK "。！？" are
// sentence terminators; a paragraph boundary is a blank line (two+ newlines) in
// the whitespace between words. The markdown reader's stripMarkdown preserves
// blank lines, so paragraph structure reaches the splitter intact. A single linear
// scan (O(text)) keeps this safe on the sync ingest path.

// sentenceEnd reports whether r is a sentence terminator (ASCII or CJK).
func sentenceEnd(r rune) bool {
	switch r {
	case '.', '!', '?', '。', '！', '？':
		return true
	}
	return false
}

// endsSentence reports whether word ends a sentence — its final rune is a
// sentence terminator. Abbreviations like "Dr." are not special-cased: they count
// as a sentence end here, which only means a chunk MAY end there (safe — slightly
// more chunks for abbreviation-heavy text, dependency-free).
func endsSentence(word string) bool {
	if word == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(word)
	return sentenceEnd(r)
}

// hasParagraphBreak reports whether the inter-word whitespace contains a blank
// line (two or more newlines) — a paragraph boundary. Single newlines (wrapped
// lines / list items) are NOT a paragraph break.
func hasParagraphBreak(ws string) bool {
	return strings.Count(ws, "\n") >= 2
}
