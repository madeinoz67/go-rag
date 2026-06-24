// Package near implements near-duplicate detection for go-rag chunks (audit
// H20 / spec 026). It is pure stdlib (Constitution III) and owns the fingerprint
// algorithm; the pipeline/engine orchestrate it.
//
// SimHash (Charikar) produces a 64-bit locality-sensitive fingerprint: similar
// texts hash to fingerprints at small Hamming distance, so near-duplicates are
// found by a popcount comparison (HammingNear). Features are lowercased
// whitespace-delimited words — the chunker's token notion — making the
// fingerprint a bag-of-words (order-insensitive). Deterministic for a fixed input.
package near

import (
	"crypto/sha256"
	"encoding/binary"
	"math/bits"
	"strings"
)

// SimHash computes the 64-bit SimHash fingerprint of text. Empty/whitespace-only
// text yields 0 (callers skip chunks below a minimum length, so this is an edge).
func SimHash(text string) uint64 {
	tokens := strings.Fields(strings.ToLower(text))
	var v [64]int
	for _, tok := range tokens {
		h := hash64(tok)
		for i := 0; i < 64; i++ {
			if (h>>i)&1 == 1 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}
	var fp uint64
	for i := 0; i < 64; i++ {
		if v[i] > 0 {
			fp |= uint64(1) << i
		}
	}
	return fp
}

// hash64 returns a stable 64-bit digest of s (SHA-256, first 8 bytes). Used to
// hash each feature token; deterministic and collision-resistant enough for
// fingerprinting.
func hash64(s string) uint64 {
	sum := sha256.Sum256([]byte(s))
	return binary.BigEndian.Uint64(sum[:8])
}

// HammingNear reports whether two SimHash fingerprints are within k bits of each
// other (Hamming distance ≤ k) — the near-duplicate test. k is the configured
// threshold (research R9, default 3).
func HammingNear(a, b uint64, k int) bool {
	return bits.OnesCount64(a^b) <= k
}
