package engine

// cache_embed_test.go (internal package `engine`) proves the H06/spec 016
// query-embedding cache (US3): a repeated query string under the same embedding
// profile reuses its vector without an Ollama round-trip, survives result-cache
// churn and ingests, and is flushed by a profile change (Migrate).

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// countingEmb is a deterministic dim-2 embedder that counts Embed calls, so a
// test can prove the embedding cache served a vector without re-embedding.
type countingEmb struct {
	calls atomic.Int64
}

func (c *countingEmb) Embed(_ context.Context, texts []string) ([][]float32, error) {
	c.calls.Add(1)
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1.0, 0.0}
	}
	return out, nil
}
func (c *countingEmb) Dimensions() int { return 2 }
func (c *countingEmb) Model() string   { return "fake" }

var _ embed.Embedder = (*countingEmb)(nil)

// newCacheEngineEmb builds a cache-enabled engine over a temp DB using emb.
func newCacheEngineEmb(t *testing.T, emb embed.Embedder) *Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	e := NewWithEmbedder(cfg, db, emb)
	t.Cleanup(e.Close)
	return e
}

// TestEmbedCache_ReusedOnResultMiss: a result-cache miss with identical query
// text must NOT re-embed — the vector is served from the embedding cache.
func TestEmbedCache_ReusedOnResultMiss(t *testing.T) {
	emb := &countingEmb{}
	e := newCacheEngineEmb(t, emb)
	addDoc(t, e, "alpha retrieval document about searching and ranking")

	// First query (hybrid → embeds the query): one Embed call, vector cached.
	// (Reset first: addDoc embedded the document chunks via the same embedder.)
	emb.calls.Store(0)
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "hybrid", K: 5}); err != nil {
		t.Fatal(err)
	}
	if got := emb.calls.Load(); got != 1 {
		t.Fatalf("after first query, Embed calls = %d, want 1", got)
	}

	// Same query text, different k → result-cache MISS, but embedding cache HIT:
	// Embed must not be called again.
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "hybrid", K: 6}); err != nil {
		t.Fatal(err)
	}
	if got := emb.calls.Load(); got != 1 {
		t.Fatalf("after result-miss with same query text, Embed calls = %d, want 1 (embedding cache must reuse)", got)
	}
	if e.embedCache.Stats().Hits == 0 {
		t.Fatalf("embedding cache recorded no hits; want >=1")
	}
}

// TestEmbedCache_IngestDoesNotFlush: ingesting a document does not invalidate
// cached query vectors (document content does not change query embeddings).
func TestEmbedCache_IngestDoesNotFlush(t *testing.T) {
	emb := &countingEmb{}
	e := newCacheEngineEmb(t, emb)
	addDoc(t, e, "alpha retrieval document about searching")

	// First query embeds once (reset clears the ingest document-embed count).
	emb.calls.Store(0)
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "hybrid", K: 5}); err != nil {
		t.Fatal(err)
	}
	if got := emb.calls.Load(); got != 1 {
		t.Fatalf("first query Embed calls = %d, want 1", got)
	}

	// Ingest an unrelated document (bumps the result-cache epoch but must NOT
	// flush the embedding cache).
	addDoc(t, e, "beta storage document about persistence")

	// Same query → result miss (epoch advanced) but embedding cache HIT, so the
	// query embeds ZERO times.
	emb.calls.Store(0)
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "hybrid", K: 5}); err != nil {
		t.Fatal(err)
	}
	if got := emb.calls.Load(); got != 0 {
		t.Fatalf("after ingest, query Embed calls = %d, want 0 (ingest must not flush the embedding cache)", got)
	}
}

// TestEmbedCache_MigrateFlushes: Migrate (model change) flushes the embedding
// cache, so the next query re-embeds under the new profile.
func TestEmbedCache_MigrateFlushes(t *testing.T) {
	emb := &countingEmb{}
	e := newCacheEngineEmb(t, emb)
	addDoc(t, e, "migrate document content for the embedding cache")

	if _, err := e.Query(context.Background(), QueryRequest{Query: "migrate", Mode: "hybrid", K: 5}); err != nil {
		t.Fatal(err)
	}
	if e.embedCache.Len() == 0 {
		t.Fatalf("precondition: embedding cache should hold the query vector")
	}

	// Force a real migration (stored model "fake" != configured "different").
	e.cfg.EmbeddingModel = "different-model"
	if _, err := e.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if e.embedCache.Len() != 0 {
		t.Fatalf("embedding cache not flushed by Migrate (size=%d); want 0", e.embedCache.Len())
	}
}

// TestEmbedCacheKey_DiffersByProfile: the embedding-cache key includes the
// embedding profile (model + dim + convention), so vectors under different
// profiles never collide. (Unit-level proof of the profile-eviction-by-key rule.)
func TestEmbedCacheKey_DiffersByProfile(t *testing.T) {
	k1 := embedCacheKey("fake|2|nomic", "search_query: alpha")
	k2 := embedCacheKey("fake|2|nomic", "search_query: alpha")
	if k1 != k2 {
		t.Fatalf("identical profile+text produced different keys")
	}
	// Different model → different key.
	if embedCacheKey("fake|2|nomic", "search_query: alpha") == embedCacheKey("other|2|nomic", "search_query: alpha") {
		t.Fatalf("keys collide across models")
	}
	// Different dim → different key.
	if embedCacheKey("fake|2|nomic", "search_query: alpha") == embedCacheKey("fake|768|nomic", "search_query: alpha") {
		t.Fatalf("keys collide across dims")
	}
	// Different convention → different key.
	if embedCacheKey("fake|2|nomic", "search_query: alpha") == embedCacheKey("fake|2|e5", "search_query: alpha") {
		t.Fatalf("keys collide across conventions")
	}
	// Different text → different key.
	if embedCacheKey("fake|2|nomic", "search_query: alpha") == embedCacheKey("fake|2|nomic", "search_query: beta") {
		t.Fatalf("keys collide across query texts")
	}
}
