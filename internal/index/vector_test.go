package index

import (
	"path/filepath"
	"testing"
)

func TestVector_QueryNearestFirst(t *testing.T) {
	v := NewVector()
	v.Add("c1", []float32{1.0, 0.0, 0.0})
	v.Add("c2", []float32{0.0, 1.0, 0.0})
	v.Add("c3", []float32{0.9, 0.1, 0.0})

	hits := v.Query([]float32{0.95, 0.05, 0.0}, 3)
	if len(hits) == 0 || hits[0].ChunkID != "c1" {
		t.Fatalf("nearest to query must be c1, got %v", hits)
	}
	// c1 (1,0,0) is more similar to (0.95,0.05,0) than c3 (0.9,0.1,0)? both close;
	// assert c1 OR c3 is first and c2 (orthogonal) is last.
	if hits[len(hits)-1].ChunkID != "c2" {
		t.Fatalf("orthogonal c2 must rank last, got %v", hits)
	}
}

func TestVector_Delete(t *testing.T) {
	v := NewVector()
	v.Add("c1", []float32{1.0, 0.0})
	v.Delete("c1")
	if hits := v.Query([]float32{1.0, 0.0}, 5); len(hits) != 0 {
		t.Fatalf("deleted vector should not match, got %v", hits)
	}
}

func TestVector_PersistenceSurvivesReload(t *testing.T) {
	v := NewVector()
	v.Add("keep", []float32{1.0, 0.0, 0.5})
	v.Add("gone", []float32{0.0, 1.0, 0.0})

	path := filepath.Join(t.TempDir(), "vectors.json")
	if err := v.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	v2 := NewVector()
	if err := v2.Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	hits := v2.Query([]float32{1.0, 0.0, 0.5}, 2)
	if len(hits) == 0 || hits[0].ChunkID != "keep" {
		t.Fatalf("reloaded store must still rank 'keep' first, got %v", hits)
	}
}

// TestVector_QuerySkipsMismatchedLength (audit H03): a stored vector whose length
// differs from the query must be SKIPPED, not garbage-scored via cosine's silent
// min(len) truncation.
func TestVector_QuerySkipsMismatchedLength(t *testing.T) {
	v := NewVector()
	v.Add("match", []float32{1.0, 0.0, 0.0, 0.0}) // dim 4 — scoreable
	v.Add("wrongdim", []float32{0.9, 0.1, 0.0})   // dim 3 — must be skipped, not scored

	hits := v.Query([]float32{1.0, 0.0, 0.0, 0.0}, 5)
	if len(hits) != 1 || hits[0].ChunkID != "match" {
		t.Fatalf("mismatched-length vector must be skipped, got %v", hits)
	}
}

// TestVector_QueryAllMismatchedReturnsNone: when every stored vector mismatches
// the query dimensionality, no hits are returned (no panic, no garbage).
func TestVector_QueryAllMismatchedReturnsNone(t *testing.T) {
	v := NewVector()
	v.Add("a", []float32{1.0, 0.0, 0.0})                                   // dim 3
	v.Add("b", []float32{0.0, 1.0, 0.0})                                   // dim 3
	if hits := v.Query([]float32{1.0, 0.0, 0.0, 0.0}, 5); len(hits) != 0 { // query dim 4
		t.Fatalf("all-mismatched corpus must yield no hits, got %v", hits)
	}
}
