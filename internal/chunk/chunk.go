// Package chunk splits extracted text into retrieval-sized chunks (PRD §4.4),
// using a boundary-aware paragraph -> sentence -> word cascade (audit H10/spec 013):
// a chunk is filled toward the Size-token budget, snapped to end at a sentence
// boundary when one falls in the back half of the window, and hard-flushed at a
// paragraph boundary so chunks do not span paragraphs that fit. A single sentence
// larger than the budget falls back to a word-window cut (the cascade's word
// level) — so a chunk never cuts a sentence mid-way unless that sentence itself is
// over-long.
//
// Defaults: ~512 tokens per chunk with 50-token overlap (word-granularity, shared
// between neighbors) and a ~50-token minimum (tiny tails merge into the previous
// chunk). Token counts use the ~1.3 tokens/word heuristic. The splitter returns
// Segments; the pipeline assigns IDs, indices, and page numbers.
package chunk

import (
	"math"
	"strings"
)

// Segment is one chunk's text plus its position in the source document.
type Segment struct {
	Text         string
	StartCharIdx int
	EndCharIdx   int
	TokenCount   int
}

// Splitter splits text into overlapping token-bounded segments.
type Splitter struct {
	Size      int // target chunk size in tokens
	Overlap   int // overlap between adjacent chunks in tokens
	MinTokens int // minimum chunk size (tiny tails merge into the previous chunk)
}

// NewSplitter returns a splitter with the given size and overlap, defaulting to the
// PRD defaults (512 / 50) when non-positive.
func NewSplitter(size, overlap int) *Splitter {
	if size <= 0 {
		size = 512
	}
	if overlap < 0 {
		overlap = 0
	}
	return &Splitter{Size: size, Overlap: overlap, MinTokens: 50}
}

// EstimateTokens approximates token count as ~1.3 tokens per whitespace-delimited
// word (research Q2).
func EstimateTokens(s string) int {
	words := len(strings.Fields(s))
	if words == 0 {
		return 0
	}
	return int(math.Ceil(float64(words) * 1.3))
}

// wordsForTokens converts a token budget into an approximate word count.
func wordsForTokens(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	return int(math.Floor(float64(tokens) / 1.3))
}

// wordSpan is a whitespace-delimited token with its byte offsets in the source and
// boundary flags (audit H10/spec 013): sentEnd marks the last word of a sentence
// (ends in a terminator); paraEnd marks the last word before a paragraph break.
type wordSpan struct {
	text       string
	start, end int
	sentEnd    bool
	paraEnd    bool
}

func tokenizeWords(s string) []wordSpan {
	var out []wordSpan
	i := 0
	n := len(s)
	for i < n {
		// Capture the inter-word whitespace (to detect paragraph breaks) and skip it.
		wsStart := i
		for i < n && isSpaceByte(s[i]) {
			i++
		}
		ws := s[wsStart:i]
		if i >= n {
			break
		}
		start := i
		for i < n && !isSpaceByte(s[i]) {
			i++
		}
		word := s[start:i]
		w := wordSpan{text: word, start: start, end: i, sentEnd: endsSentence(word)}
		// A paragraph break before this word ends the previous word's paragraph.
		if len(out) > 0 && hasParagraphBreak(ws) {
			out[len(out)-1].paraEnd = true
			out[len(out)-1].sentEnd = true
		}
		out = append(out, w)
	}
	// End of text is always a valid chunk boundary.
	if len(out) > 0 {
		out[len(out)-1].sentEnd = true
	}
	return out
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}

// Split breaks text into Segments of roughly Size tokens using the boundary-aware
// cascade (audit H10/spec 013): a chunk is filled toward the Size budget, snapped
// to end at a sentence boundary when one falls in the back half of the window, and
// hard-flushed at a paragraph boundary so chunks do not span paragraphs that fit.
// A single sentence larger than the budget falls back to a word-window cut (the
// cascade's word level). Overlap tokens are shared between neighbors (word
// granularity); a sub-MinTokens final tail merges into its predecessor.
func (s *Splitter) Split(text string) []Segment {
	words := tokenizeWords(text)
	if len(words) == 0 {
		return nil
	}
	perChunk := wordsForTokens(s.Size)
	if perChunk < 1 {
		perChunk = 1
	}
	overlapWords := wordsForTokens(s.Overlap)
	if overlapWords >= perChunk {
		overlapWords = perChunk / 4
	}

	var segs []Segment
	i := 0
	for i < len(words) {
		end := i + perChunk
		if end > len(words) {
			end = len(words)
		}

		// Greedy-fill (H10 option B): if the remaining text fits one chunk, take it
		// all whole. Splitting a small document at internal paragraph boundaries
		// over-fragments it and hurts retrieval (fewer terms per chunk for BM25); a
		// document (or trailing section) that fits the size budget is one coherent
		// chunk. Only break at boundaries when the remaining content exceeds the
		// budget — the audit's "respect boundaries when you must cut," not "force
		// every paragraph into its own chunk."
		if len(words)-i <= perChunk {
			segs = append(segs, buildSegment(words[i:end]))
			break
		}

		// Paragraph flush: flush at a paragraph boundary so chunks don't span
		// paragraphs — but only once the chunk has reached a meaningful size, so a
		// tiny leading fragment (e.g. a section heading) merges into the following
		// paragraph instead of becoming a standalone low-content chunk that pollutes
		// BM25 (short chunks get disproportionate term-frequency weight). FR-003
		// honors paragraphs that carry real content; a heading attaches to its section.
		minFlushWords := wordsForTokens(s.MinTokens)
		flushed := false
		for k := i; k < end; k++ {
			if words[k].paraEnd && (k-i+1) >= minFlushWords {
				end = k + 1
				flushed = true
				break
			}
		}
		// Sentence snap: otherwise end at the last sentence boundary in the back
		// half of the window, so chunks end at sentence edges when one is available
		// (FR-002). If none is found (a long sentence), keep the word cut — the
		// sentence continues into the next chunk via overlap (word fallback, FR-004).
		if !flushed {
			half := i + perChunk/2
			if half <= i {
				half = i + 1
			}
			for k := end - 1; k >= half && k > i; k-- {
				if words[k].sentEnd {
					end = k + 1
					break
				}
			}
		}

		segs = append(segs, buildSegment(words[i:end]))
		if end >= len(words) {
			break
		}
		// Advance (FR-007 overlap). Share the last overlapWords with the next chunk
		// — but NOT backward across a paragraph boundary the chunk was flushed at
		// (paragraphs are respected, FR-003, and stepping back would re-enter the
		// paragraph). When the overlap budget >= chunk size, advance to end with no
		// overlap (avoid re-slicing the same span).
		var next int
		if flushed {
			next = end
		} else {
			next = end - overlapWords
			if next <= i {
				next = end
			}
		}
		if next <= i {
			next = i + 1
		}
		i = next
	}

	// Merge a too-small final segment into its predecessor (unchanged).
	if len(segs) >= 2 && segs[len(segs)-1].TokenCount < s.MinTokens {
		last := segs[len(segs)-1]
		prev := &segs[len(segs)-2]
		prev.Text = strings.TrimSpace(prev.Text + " " + last.Text)
		prev.EndCharIdx = last.EndCharIdx
		prev.TokenCount = EstimateTokens(prev.Text)
		segs = segs[:len(segs)-1]
	}
	return segs
}

func buildSegment(words []wordSpan) Segment {
	var b strings.Builder
	for i, w := range words {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(w.text)
	}
	text := b.String()
	return Segment{
		Text:         text,
		StartCharIdx: words[0].start,
		EndCharIdx:   words[len(words)-1].end,
		TokenCount:   EstimateTokens(text),
	}
}
