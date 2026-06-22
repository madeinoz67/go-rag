package chunk

import (
	"strings"
	"testing"
)

func TestSplit_SingleSmallChunk(t *testing.T) {
	s := NewSplitter(512, 50)
	segs := s.Split("a short sentence with a few words")
	if len(segs) != 1 {
		t.Fatalf("short input -> 1 segment, got %d", len(segs))
	}
	if segs[0].TokenCount == 0 {
		t.Error("token count must be > 0")
	}
}

func TestSplit_CountScalesWithLength(t *testing.T) {
	s := NewSplitter(512, 50)
	small := strings.Repeat("word ", 100)
	large := strings.Repeat("word ", 5000)
	if n1, n2 := len(s.Split(small)), len(s.Split(large)); n2 <= n1 {
		t.Fatalf("larger input must yield more chunks: small=%d large=%d", n1, n2)
	}
	if len(s.Split(large)) < 2 {
		t.Fatal("large input must produce multiple chunks")
	}
}

func TestSplit_OverlapBetweenChunks(t *testing.T) {
	s := NewSplitter(20, 10) // tiny sizes force multiple chunks with overlap
	text := strings.Repeat("alpha beta gamma delta epsilon ", 40)
	segs := s.Split(text)
	if len(segs) < 2 {
		t.Skip("not enough chunks to test overlap")
	}
	// Overlapping char ranges: chunk i ends after chunk i+1 starts.
	for i := 0; i < len(segs)-1; i++ {
		if segs[i].EndCharIdx <= segs[i+1].StartCharIdx {
			t.Fatalf("chunks %d/%d do not overlap: end=%d nextStart=%d", i, i+1, segs[i].EndCharIdx, segs[i+1].StartCharIdx)
		}
	}
}

func TestSplit_NoSubMinimumTail(t *testing.T) {
	s := NewSplitter(512, 50)
	s.MinTokens = 50
	// Build text large enough to chunk, with a short trailing run.
	text := strings.Repeat("tokenword ", 2000) + "tiny tail"
	segs := s.Split(text)
	if len(segs) < 2 {
		t.Skip("not enough chunks")
	}
	last := segs[len(segs)-1]
	if last.TokenCount < s.MinTokens {
		t.Fatalf("final chunk below minimum (%d < %d); should have merged", last.TokenCount, s.MinTokens)
	}
}

func TestSplit_EmptyInput(t *testing.T) {
	s := NewSplitter(512, 50)
	if segs := s.Split(""); segs != nil {
		t.Fatalf("empty input -> nil, got %v", segs)
	}
	if segs := s.Split("   \n  "); segs != nil {
		t.Fatalf("whitespace-only -> nil, got %v", segs)
	}
}

// --- H10/spec 013: boundary-aware cascade tests ---

// TestSplit_ChunksEndAtSentenceBoundaries (FR-002, SC-001): with sentence
// terminators present, every chunk ends at a sentence boundary.
func TestSplit_ChunksEndAtSentenceBoundaries(t *testing.T) {
	s := NewSplitter(8, 2) // ~6 words/chunk
	s.MinTokens = 0        // observe the raw cascade, no tail-merge
	text := "aa bb cc. dd ee ff. gg hh ii. jj kk ll. mm nn oo. pp qq rr."
	segs := s.Split(text)
	if len(segs) < 2 {
		t.Fatalf("need >=2 chunks to test boundaries, got %d", len(segs))
	}
	for i, seg := range segs {
		if !endsSentence(seg.Text) {
			t.Errorf("chunk %d does not end at a sentence boundary: %q", i, seg.Text)
		}
	}
}

// TestSplit_ParagraphsNotSpanned (FR-003, SC-002): when a document EXCEEDS the
// size budget, it is split at the paragraph boundary — no chunk spans paragraphs.
// (A doc that fits one budget stays one chunk by design; this test forces a split
// with a size small enough that the two paragraphs together exceed it.)
func TestSplit_ParagraphsNotSpanned(t *testing.T) {
	s := NewSplitter(78, 10) // ~60 words/chunk; each paragraph ~47 words (< budget, > MinTokens)
	para1 := "first " + strings.Repeat("alpha ", 45) + "done."
	para2 := "second " + strings.Repeat("beta ", 45) + "done."
	segs := s.Split(para1 + "\n\n" + para2)
	if len(segs) != 2 {
		t.Fatalf("want 2 chunks (one per paragraph), got %d", len(segs))
	}
	if strings.Contains(segs[0].Text, "second") || strings.Contains(segs[0].Text, "beta") {
		t.Errorf("chunk 0 spans into paragraph 2: %q", segs[0].Text)
	}
	if !strings.Contains(segs[1].Text, "second") {
		t.Errorf("chunk 1 should be paragraph 2: %q", segs[1].Text)
	}
}

// TestSplit_OverlongSentence_WordFallback (FR-004): a single sentence larger than
// the budget is word-split into multiple budget-sized chunks, never one oversize.
func TestSplit_OverlongSentence_WordFallback(t *testing.T) {
	s := NewSplitter(20, 5) // ~15 words/chunk
	s.MinTokens = 0
	// One long "sentence": no terminator until the very end.
	text := strings.Repeat("word ", 100) + "end."
	segs := s.Split(text)
	if len(segs) < 2 {
		t.Fatalf("over-long sentence must word-split into >=2 chunks, got %d", len(segs))
	}
}

// TestSplit_NoTerminator_DegradesToWordWindow (edge case): text with no sentence
// terminators still chunks via the word-window fallback (no one giant chunk).
func TestSplit_NoTerminator_DegradesToWordWindow(t *testing.T) {
	s := NewSplitter(20, 5)
	segs := s.Split(strings.Repeat("word ", 100))
	if len(segs) < 2 {
		t.Fatalf("no-terminator text should still chunk (word window), got %d", len(segs))
	}
}

// TestSplit_CJKSentenceBoundaries (FR-008): CJK sentence terminators (。) are
// recognized as boundaries and CJK content is preserved.
func TestSplit_CJKSentenceBoundaries(t *testing.T) {
	// endsSentence must recognize CJK terminators directly.
	if !endsSentence("你好。") {
		t.Fatal("endsSentence must recognize the CJK terminator 。")
	}
	s := NewSplitter(4, 0) // ~3 words/chunk, no overlap -> clean split
	s.MinTokens = 0
	// Six short CJK sentences separated by spaces (so each is one tokenized word).
	text := "一。 二。 三。 四。 五。 六。"
	segs := s.Split(text)
	if len(segs) < 2 {
		t.Fatalf("CJK sentences should split into >=2 chunks, got %d", len(segs))
	}
	for i, seg := range segs {
		if !endsSentence(seg.Text) {
			t.Errorf("chunk %d does not end at a CJK boundary: %q", i, seg.Text)
		}
	}
}
