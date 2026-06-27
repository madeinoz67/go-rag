//go:build integration

package embed

import (
	"context"
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
