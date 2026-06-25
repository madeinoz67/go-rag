package index

import (
	"context"
	"reflect"
	"testing"
)

// fakeVectorIndex is a minimal VectorIndex used only to prove the seam
// (H27/spec 027, SC-001): Retrieval depends on the VectorIndex contract, not on
// the concrete *Vector. It mirrors a real *Vector's (id→vec) contents and
// semantics (linear cosine scan, identical sort) so the two are interchangeable —
// identical results then prove Retrieval is wired to the contract, not the type.
type fakeVectorIndex struct {
	store map[string][]float32
}

func (f *fakeVectorIndex) Add(id string, vec []float32) {
	if f.store == nil {
		f.store = map[string][]float32{}
	}
	f.store[id] = vec
}
func (f *fakeVectorIndex) Delete(id string) { delete(f.store, id) }
func (f *fakeVectorIndex) Query(vec []float32, k int) []Hit {
	type scored struct {
		id string
		s  float64
	}
	var all []scored
	for id, cv := range f.store {
		if len(cv) != len(vec) {
			continue // Invariant 1 honoured by the fake, same as the reference.
		}
		all = append(all, scored{id, cosine(vec, cv)})
	}
	// Sort desc by score, then asc by chunkID — matches the reference tie-break
	// (Invariant 2) so identical contents yield identical order.
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].s > all[i].s || (all[j].s == all[i].s && all[j].id < all[i].id) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if k > 0 && k < len(all) {
		all = all[:k]
	}
	out := make([]Hit, len(all))
	for i, a := range all {
		out[i] = Hit{ChunkID: a.id, Score: a.s}
	}
	return out
}

// TestRetrieval_VectorIndexSeam (H27/spec 027, SC-001) proves Retrieval depends
// on the VectorIndex contract, not on the concrete *Vector: a Retrieval wired to
// a real *Vector and one wired to a fakeVectorIndex holding the SAME vectors
// return byte-identical hybrid and semantic results for the same query. If this
// ever fails, the seam is broken — Retrieval has re-coupled to the concrete type.
func TestRetrieval_VectorIndexSeam(t *testing.T) {
	seed := func(v VectorIndex) {
		v.Add("c1", []float32{1.0, 0.0})
		v.Add("c2", []float32{0.8, 0.2})
		v.Add("c3", []float32{0.1, 0.9})
	}
	fts := newTestFTS(t)
	fts.Index("c1", map[string]string{"body": "alpha keyword"})
	fts.Index("c2", map[string]string{"body": "alpha note"})

	realVec := NewVector()
	fakeVec := &fakeVectorIndex{}
	seed(realVec)
	seed(fakeVec)

	q := staticEmbed([]float32{1.0, 0.0})
	realR := NewRetrieval(fts, realVec, q)
	fakeR := NewRetrieval(fts, fakeVec, q)

	for _, mode := range []Mode{ModeSemantic, ModeHybrid} {
		want, err := realR.Search(context.Background(), "alpha", 5, mode, nil)
		if err != nil {
			t.Fatalf("real search (mode %d): %v", mode, err)
		}
		got, err := fakeR.Search(context.Background(), "alpha", 5, mode, nil)
		if err != nil {
			t.Fatalf("fake search (mode %d): %v", mode, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("mode %d: Retrieval results differ between *Vector and fakeVectorIndex — the seam is broken\nwant=%v\ngot =%v", mode, want, got)
		}
	}
}

// TestNewRetrieval_AcceptsVectorIndex (H27/spec 027, US1) is a compile-time
// guarantee that the constructor depends on the contract: it asserts at test
// time that *Vector satisfies VectorIndex (the reference implementation), so a
// future accidental tightening of the interface is caught here.
func TestNewRetrieval_AcceptsVectorIndex(t *testing.T) {
	var _ VectorIndex = NewVector() // compiles iff *Vector satisfies the contract
	r := NewRetrieval(newTestFTS(t), NewVector(), staticEmbed([]float32{1.0, 0.0}))
	if r == nil {
		t.Fatal("NewRetrieval returned nil")
	}
}
