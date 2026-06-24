package near

import (
	"math/bits"
	"testing"
)

func TestSimHash_Deterministic(t *testing.T) {
	a := SimHash("the quick brown fox")
	b := SimHash("the quick brown fox")
	if a != b {
		t.Errorf("SimHash not deterministic: %x != %x", a, b)
	}
}

func TestSimHash_OrderInsensitive(t *testing.T) {
	// Bag-of-words: word order must not change the fingerprint.
	if SimHash("alpha beta gamma") != SimHash("gamma beta alpha") {
		t.Error("SimHash should be order-insensitive (bag of words)")
	}
}

func TestSimHash_Identical_ZeroDistance(t *testing.T) {
	x := SimHash("some representative passage about retrieval and ranking")
	if !HammingNear(x, x, 0) {
		t.Error("identical text must be at Hamming distance 0")
	}
}

func TestSimHash_Locality(t *testing.T) {
	// A passage and a small-edit revision should be much closer than two
	// topically-distinct passages — the locality property that makes near-dup
	// detection work. Assert the relation, not an exact threshold (k is tuned at
	// the integration / eval level — SC-004).
	orig := "the go-rag server performs keyword retrieval over local documents stored on disk"
	rev := "the go-rag server performs keyword retrieval over local documents kept on disk"
	distinct := "quantum entanglement lets two particles share state across any distance instantly"
	dEdit := bits.OnesCount64(SimHash(orig) ^ SimHash(rev))
	dDistinct := bits.OnesCount64(SimHash(orig) ^ SimHash(distinct))
	if dEdit >= dDistinct {
		t.Errorf("small edit should be nearer than distinct: edit=%d distinct=%d", dEdit, dDistinct)
	}
	// A whitespace-only change is a perfect near-dup (distance 0): Fields collapses
	// repeated whitespace, so the token bag is identical.
	if bits.OnesCount64(SimHash("a b c")^SimHash("a  b  c")) != 0 {
		t.Error("whitespace-only change should be distance 0")
	}
}

func TestHammingNear_Boundary(t *testing.T) {
	// k is inclusive: distance exactly k is "near"; k+1 is not.
	const a uint64 = 0
	cases := []struct {
		b    uint64
		k    int
		want bool
	}{
		{0b0000, 0, true},  // identical
		{0b0001, 0, false}, // distance 1 > 0
		{0b0001, 1, true},  // distance 1 <= 1
		{0b0011, 1, false}, // distance 2 > 1
		{0b0011, 2, true},
		{^a, 64, true},  // all 64 bits differ, k=64
		{^a, 63, false}, // 64 > 63
	}
	for _, c := range cases {
		if got := HammingNear(a, c.b, c.k); got != c.want {
			t.Errorf("HammingNear(%x,%x,k=%d)=%v want %v", a, c.b, c.k, got, c.want)
		}
	}
}
