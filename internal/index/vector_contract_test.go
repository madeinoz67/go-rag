package index

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// vector_contract_test.go (H27/spec 027, SC-004) pins the three VectorIndex
// invariants against the reference *Vector implementation. This suite is the bar
// any future approximate-nearest-neighbour backend must pass (FR-009): a backend
// that cannot honour these is wrapped or rejected, never silently accepted.
//
// See specs/027-vector-index-interface/contracts/vector-index.md.

// TestVectorContract_DimensionalitySkip (FR-002 / Invariant 1): a stored vector
// whose length differs from the query vector's length is EXCLUDED from Query
// results — never scored over min(len(a), len(b)) dimensions. This is the H03
// anti-silent-corruption guard, promoted from implementation detail to contract.
func TestVectorContract_DimensionalitySkip(t *testing.T) {
	v := NewVector()
	v.Add("dim3a", []float32{1, 0, 0})
	v.Add("dim3b", []float32{0, 1, 0})
	v.Add("dim4", []float32{1, 0, 0, 0}) // different dimensionality — must be skipped

	got := v.Query([]float32{1, 0, 0}, 10)
	ids := contractHitIDs(got)
	if len(got) != 2 {
		t.Fatalf("dimensionality-skip: got %d hits %v, want exactly the 2 dim-3 vectors (dim4 excluded)", len(got), ids)
	}
	for _, id := range ids {
		if id == "dim4" {
			t.Errorf("dimensionality-skip: dim4 (len 4) must NOT be scored against a len-3 query; got %v", ids)
		}
	}
	if len(got) == 0 || got[0].ChunkID != "dim3a" {
		t.Errorf("dim3a (exact match) must rank first, got %v", ids)
	}
}

// TestVectorContract_Determinism (FR-003 / Invariant 2): an identical
// (corpus, query, k) yields identical results in identical order across calls,
// and equal-score results resolve by ascending chunk-ID (the stable tie-break).
func TestVectorContract_Determinism(t *testing.T) {
	v := NewVector()
	v.Add("zebra", []float32{0, 1}) // equidistant from the query → tie with alpha
	v.Add("alpha", []float32{0, 1})
	v.Add("mid", []float32{1, 0}) // exact match for the query → rank 1

	first := v.Query([]float32{1, 0}, 10)
	for i := 0; i < 25; i++ {
		again := v.Query([]float32{1, 0}, 10)
		if !contractSameHits(first, again) {
			t.Fatalf("determinism: query result changed across repeated calls\nfirst=%v\nagain =%v", first, again)
		}
	}
	// mid ranks first (exact match); alpha before zebra (tie → ascending chunkID).
	if len(first) < 3 || first[0].ChunkID != "mid" {
		t.Fatalf("mid must rank first (exact match), got %v", contractHitIDs(first))
	}
	if first[1].ChunkID != "alpha" || first[2].ChunkID != "zebra" {
		t.Errorf("equal-score tie must break by ascending chunkID (alpha<zebra); got %v", contractHitIDs(first))
	}
}

// TestVectorContract_Concurrency (FR-004 / Invariant 3): the store is safe under
// concurrent Add/Delete + Query. Run under `go test -race` — the reference impl
// guards all access with sync.Mutex, so the detector must stay silent. (Final
// membership is non-deterministic; the invariant is no-race + no-panic.)
func TestVectorContract_Concurrency(t *testing.T) {
	v := NewVector()
	var wg sync.WaitGroup
	var queries atomic.Uint64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				id := contractID(n, j)
				v.Add(id, []float32{float32(n) * 0.1, float32(j) * 0.01})
				if v.Query([]float32{0.5, 0.5}, 10) != nil {
					queries.Add(1) // concurrent queries must complete, not race
				}
				if j%2 == 0 {
					v.Delete(id)
				}
			}
		}(i)
	}
	wg.Wait()
	if queries.Load() == 0 {
		t.Fatal("concurrency: expected concurrent Add/Delete + Query to complete; the race detector is the primary assertion")
	}
	_ = v.Query([]float32{1, 0}, 5) // one final query must not race/panic
}

// --- contract-test helpers (prefixed to avoid collisions in the test package) ---

func contractHitIDs(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ChunkID
	}
	return out
}

func contractSameHits(a, b []Hit) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ChunkID != b[i].ChunkID || a[i].Score != b[i].Score {
			return false
		}
	}
	return true
}

func contractID(n, j int) string { return fmt.Sprintf("g%d-%d", n, j) }
