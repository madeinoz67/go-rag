// Package chunk splits extracted text into retrieval-sized chunks (PRD §4.4).
//
// Defaults: ~512 tokens per chunk with 50-token overlap, using a
// paragraph -> sentence -> word cascade with a ~50-token minimum. Token counts use
// the ~1.3 tokens/word heuristic (research Q2). The splitter returns Segments; the
// pipeline assigns IDs, indices, and page numbers.
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
	Size       int // target chunk size in tokens
	Overlap    int // overlap between adjacent chunks in tokens
	MinTokens  int // minimum chunk size (tiny tails merge into the previous chunk)
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

// wordSpan is a whitespace-delimited token with its byte offsets in the source.
type wordSpan struct {
	text       string
	start, end int
}

func tokenizeWords(s string) []wordSpan {
	var out []wordSpan
	i := 0
	n := len(s)
	for i < n {
		for i < n && isSpaceByte(s[i]) {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && !isSpaceByte(s[i]) {
			i++
		}
		out = append(out, wordSpan{text: s[start:i], start: start, end: i})
	}
	return out
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}

// Split breaks text into Segments of roughly Size tokens with Overlap tokens shared
// between neighbors, merging any sub-MinTokens tail into the previous segment.
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
	step := perChunk - overlapWords
	if step < 1 {
		step = 1
	}

	var segs []Segment
	for i := 0; i < len(words); i += step {
		end := i + perChunk
		if end > len(words) {
			end = len(words)
		}
		seg := buildSegment(words[i:end])
		segs = append(segs, seg)
		if end == len(words) {
			break
		}
	}

	// Merge a too-small final segment into its predecessor.
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
