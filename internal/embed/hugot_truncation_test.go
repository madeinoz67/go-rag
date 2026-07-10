package embed

import (
	"math"
	"strings"
	"testing"
)

// TestSplitTextByWords_BelowLimit returns the text whole when it fits.
func TestSplitTextByWords_BelowLimit(t *testing.T) {
	got := splitTextByWords("short text", 100)
	if len(got) != 1 || got[0] != "short text" {
		t.Fatalf("expected single whole substring, got %v", got)
	}
}

// TestSplitTextByWords_SplitsAtWordBoundaries produces substrings each ≤ maxChars,
// broken at whitespace (never mid-word).
func TestSplitTextByWords_SplitsAtWordBoundaries(t *testing.T) {
	// 50 chars, maxChars 20 → expect 3 subs, each ≤ 20, no sub starts/ends mid-word
	// (whitespace trimmed at the cut).
	text := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu"
	subs := splitTextByWords(text, 20)
	if len(subs) < 2 {
		t.Fatalf("expected ≥2 subs, got %d (%v)", len(subs), subs)
	}
	for i, s := range subs {
		if len(s) > 20 {
			t.Errorf("sub %d length %d > maxChars 20: %q", i, len(s), s)
		}
		// no leading/trailing whitespace (we TrimLeft at each cut)
		if s != strings.TrimSpace(s) {
			t.Errorf("sub %d has surrounding whitespace: %q", i, s)
		}
	}
	// rejoined content covers the original words (whitespace-normalized)
	joined := strings.Join(subs, " ")
	if strings.Contains(joined, "alpha") == false || strings.Contains(joined, "lambda") == false {
		t.Errorf("rejoined content lost words: %q", joined)
	}
}

// TestSplitTextByWords_NoWhitespaceHardSplits preserves the bound when a run has no
// whitespace within the window (hard-split at maxChars).
func TestSplitTextByWords_NoWhitespaceHardSplits(t *testing.T) {
	long := strings.Repeat("x", 55) // no whitespace at all
	subs := splitTextByWords(long, 20)
	if len(subs) < 3 {
		t.Fatalf("expected ≥3 hard-split subs, got %d", len(subs))
	}
	for i, s := range subs {
		if len(s) > 20 {
			t.Errorf("sub %d length %d exceeds maxChars 20", i, len(s))
		}
	}
}

// TestSplitTextByWords_MaxCharsZeroClamped guards against a zero/negative budget
// (would otherwise produce an infinite loop / empty subs).
func TestSplitTextByWords_MaxCharsZeroClamped(t *testing.T) {
	subs := splitTextByWords("abc def", 0)
	if len(subs) == 0 {
		t.Fatal("expected non-empty split even with maxChars=0")
	}
}

// TestMeanPool_AveragesUnitVectors returns the elementwise mean; each input is unit
// length so the pooled magnitude is < 1 (the caller re-normalizes).
func TestMeanPool_AveragesUnitVectors(t *testing.T) {
	vecs := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
	got := meanPool(vecs)
	want := []float32{1.0 / 3, 1.0 / 3, 1.0 / 3}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-6 {
			t.Errorf("dim %d: got %f, want %f", i, got[i], want[i])
		}
	}
}

// TestMeanPool_EmptyReturnsNil guards the divide-by-zero path.
func TestMeanPool_EmptyReturnsNil(t *testing.T) {
	if got := meanPool(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

// TestNormalizeL2_ProducesUnitLength: output magnitude is 1.
func TestNormalizeL2_ProducesUnitLength(t *testing.T) {
	v := []float32{3, 0, 4} // magnitude 5
	out := normalizeL2(v)
	var mag float64
	for _, x := range out {
		mag += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(mag)-1.0) > 1e-6 {
		t.Errorf("normalized magnitude = %f, want 1", math.Sqrt(mag))
	}
}

// TestNormalizeL2_ZeroVectorUnchanged avoids div-by-zero.
func TestNormalizeL2_ZeroVectorUnchanged(t *testing.T) {
	v := []float32{0, 0, 0}
	out := normalizeL2(v)
	for _, x := range out {
		if x != 0 {
			t.Errorf("zero vector should stay zero, got %v", out)
		}
	}
}

// TestIsOverLength_CharProxyFallback covers the tokenizer-unreachable path: a
// zero-value HugotEmbedder (pipe == nil → tokenCount returns -1) falls back to the
// ~3-chars/token byte proxy. This runs offline (no model needed).
func TestIsOverLength_CharProxyFallback(t *testing.T) {
	e := NewHugot() // pipe is nil → tokenCount returns -1 → char-proxy path
	// 512 * 3 = 1536 chars is the threshold; a longer text is over-length.
	under := strings.Repeat("a", 1536)
	over := strings.Repeat("a", 1537)
	if e.isOverLength(under, 512) {
		t.Error("text at exactly maxSeq*3 chars should NOT be flagged over-length")
	}
	if !e.isOverLength(over, 512) {
		t.Error("text longer than maxSeq*3 chars should be flagged over-length (char-proxy)")
	}
	// tokenCount on a pipe-less embedder returns -1 (the unreachable signal).
	if n := e.tokenCount("anything"); n != -1 {
		t.Errorf("tokenCount with nil pipe = %d, want -1", n)
	}
}
