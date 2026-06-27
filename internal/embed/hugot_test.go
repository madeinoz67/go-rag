//go:build integration

package embed

import (
	"context"
	"math"
	"testing"

	"github.com/madeinoz67/go-rag/internal/embed/modelbundle"
)

// TestHugotEmbedder_ProducesPinnedDim loads the bundled model (fetching ~33MB on
// first run) and asserts it produces modelbundle.EmbeddingDim vectors. Build-tagged
// `integration` so it is excluded from the normal (offline, fast) test suite; run
// with: go test -tags integration ./internal/embed/
func TestHugotEmbedder_ProducesPinnedDim(t *testing.T) {
	ctx := context.Background()
	if _, err := modelbundle.EnsureModel(ctx); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	e := NewHugot()
	vecs, err := e.Embed(ctx, []string{"hello world", "retrieval augmented generation"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("want 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != modelbundle.EmbeddingDim {
		t.Fatalf("dim = %d, want %d", len(vecs[0]), modelbundle.EmbeddingDim)
	}
	if e.Dimensions() != modelbundle.EmbeddingDim {
		t.Fatalf("Dimensions() = %d, want %d", e.Dimensions(), modelbundle.EmbeddingDim)
	}
	if e.Model() != modelbundle.ModelID {
		t.Fatalf("Model() = %q, want %q", e.Model(), modelbundle.ModelID)
	}
	// determinism: identical input → identical vector
	a, _ := e.Embed(ctx, []string{"go-rag local embeddings"})
	if len(a) != 1 || len(a[0]) != modelbundle.EmbeddingDim {
		t.Fatalf("determinism probe: unexpected shape %v", a)
	}
	for _, v := range a[0] {
		if v != 0 {
			return // non-zero vector produced
		}
	}
	t.Fatal("embedding vector is all zeros")
}

// TestHugotEmbedder_SemanticSanity is a quality guard beyond dimensionality: a
// retrieval-flavoured query must embed closer (cosine) to a relevant doc than to an
// unrelated one. Catches tokenizer/pooling regressions that produce the right dim but
// garbage semantics. Build-tagged `integration` (needs the model).
func TestHugotEmbedder_SemanticSanity(t *testing.T) {
	ctx := context.Background()
	if _, err := modelbundle.EnsureModel(ctx); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	e := NewHugot()
	docs, err := e.Embed(ctx, []string{
		"a local retrieval augmented generation database for documents",
		"pasta recipes with tomato sauce, basil, and olive oil",
	})
	if err != nil || len(docs) != 2 {
		t.Fatalf("embed two docs: err=%v len=%d", err, len(docs))
	}
	q, err := e.Embed(ctx, []string{"how does the retrieval database work"})
	if err != nil || len(q) != 1 {
		t.Fatalf("embed query: err=%v len=%d", err, len(q))
	}
	simRel := cosine(q[0], docs[0])
	simIrr := cosine(q[0], docs[1])
	if simRel <= simIrr {
		t.Fatalf("relevant doc must be more similar to the query: rel=%.4f irr=%.4f", simRel, simIrr)
	}
}

// cosine returns the cosine similarity of two equal-length float32 vectors.
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
