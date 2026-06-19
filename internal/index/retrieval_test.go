package index

import (
	"context"
	"testing"
)

// staticEmbed returns a fixed vector regardless of input, so query embedding is
// deterministic and close to a chosen chunk's vector.
func staticEmbed(vec []float32) EmbedFunc {
	return func(_ context.Context, _ []string) ([][]float32, error) {
		return [][]float32{vec}, nil
	}
}

func TestRetrieval_Hybrid_BothListsRankAboveOneList(t *testing.T) {
	fts := NewFTS()
	vec := NewVector()

	// c1 matches FTS ("alpha") and is near the query vector.
	fts.Index("c1", map[string]string{"body": "alpha keyword document"})
	vec.Add("c1", []float32{0.99, 0.0})
	// c3 matches FTS only; its vector is orthogonal to the query.
	fts.Index("c3", map[string]string{"body": "alpha other note"})
	vec.Add("c3", []float32{0.0, 1.0})

	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	hits, err := r.Search(context.Background(), "alpha", 5, ModeHybrid, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "c1" {
		t.Fatalf("c1 (in both lists) must rank first, got %v", hits)
	}
}

func TestRetrieval_CollapseSameDocument(t *testing.T) {
	fts := NewFTS()
	vec := NewVector()
	fts.Index("c1", map[string]string{"body": "alpha beta"})
	fts.Index("c1b", map[string]string{"body": "alpha gamma"})
	fts.Index("c2", map[string]string{"body": "alpha delta"})
	vec.Add("c1", []float32{1.0, 0.0})
	vec.Add("c1b", []float32{0.9, 0.1})
	vec.Add("c2", []float32{0.1, 0.9})

	docOf := func(id string) string {
		if id == "c1" || id == "c1b" {
			return "docA"
		}
		return "docB"
	}
	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	hits, err := r.Search(context.Background(), "alpha", 5, ModeHybrid, docOf)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]bool{}
	for _, h := range hits {
		docs[docOf(h.ChunkID)] = true
	}
	if docs["docA"] && docs["docB"] && len(hits) == 2 {
		return // collapsed to one per doc
	}
	if len(hits) != 2 {
		t.Fatalf("same-document hits must collapse to top-1 per doc; got %d hits: %v", len(hits), hits)
	}
}

func TestRetrieval_ModeSelection(t *testing.T) {
	fts := NewFTS()
	vec := NewVector()
	// cFTS: only in FTS. cVEC: only in vector.
	fts.Index("cFTS", map[string]string{"body": "unique keyword term"})
	vec.Add("cVEC", []float32{1.0, 0.0})

	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))

	// Keyword mode must NOT surface the vector-only chunk.
	kw, _ := r.Search(context.Background(), "unique keyword", 5, ModeKeyword, nil)
	for _, h := range kw {
		if h.ChunkID == "cVEC" {
			t.Fatal("keyword mode must not use vector index")
		}
	}
	// Semantic mode must NOT surface the FTS-only chunk.
	sem, _ := r.Search(context.Background(), "unique keyword", 5, ModeSemantic, nil)
	for _, h := range sem {
		if h.ChunkID == "cFTS" {
			t.Fatal("semantic mode must not use FTS index")
		}
	}
}
