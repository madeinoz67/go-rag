package engine

// cache_result_test.go (internal package `engine`) proves the H06/spec 016
// result cache behaves correctly at the Engine.Query level: a repeated query is
// a transparent hit (FR-008), every result-shaping input is part of the key, the
// cache is bounded/NoCache/RerankFailed/disabled correct.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newResultCacheEngine builds a cache-enabled engine over a temp DB with the
// deterministic embedder, configuring the result-cache capacity. results<=0
// disables the cache (mirrors the kill-switch).
func newResultCacheEngine(t *testing.T, results int) *Engine {
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
	cfg.QueryCacheResults = results
	cfg.QueryCacheEmbeddings = 64
	if results <= 0 {
		cfg.QueryCacheEnabled = false
	}
	e := NewWithEmbedder(cfg, db, cacheFakeEmb{})
	t.Cleanup(e.Close)
	return e
}

// TestResultCache_RepeatIsHit asserts a second identical query is a transparent
// hit: the cache serves it (Hits increments) and the result equals the cold one.
func TestResultCache_RepeatIsHit(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	addDoc(t, e, "alpha retrieval document about searching and ranking")
	waitForEpochStable(t, e)

	req := QueryRequest{Query: "alpha", Mode: "keyword", K: 5}
	cold, err := e.Query(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.resultCache.Stats().Misses; got != 1 {
		t.Fatalf("first query misses = %d, want 1", got)
	}

	cached, err := e.Query(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.resultCache.Stats().Hits; got != 1 {
		t.Fatalf("second query hits = %d, want 1 (transparent hit)", got)
	}
	if !hitsEqual(cold.Hits, cached.Hits) {
		t.Fatalf("cached result != cold result (transparency): cold=%v cached=%v", cold.Hits, cached.Hits)
	}
	if cold.RerankFailed != cached.RerankFailed {
		t.Fatalf("RerankFailed differs cold=%v cached=%v", cold.RerankFailed, cached.RerankFailed)
	}
}

// TestResultCache_KeyComponentsMiss asserts that changing ANY single result-
// shaping input is a miss (different key), so parameters can never collide.
func TestResultCache_KeyComponentsMiss(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	addDoc(t, e, "alpha retrieval document about searching and ranking")

	base := QueryRequest{Query: "alpha", Mode: "keyword", K: 5}
	if _, err := e.Query(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	hitsBefore := e.resultCache.Stats().Hits

	variants := []QueryRequest{
		{Query: "alpha", Mode: "keyword", K: 6},                                      // different k
		{Query: "alpha", Mode: "semantic", K: 5},                                     // different mode
		{Query: "alpha", Mode: "keyword", K: 5, Threshold: 0.5},                      // different threshold
		{Query: "alpha", Mode: "keyword", K: 5, RRFK: 30},                            // different rrf_k
		{Query: "alpha", Mode: "keyword", K: 5, ContextWindow: 1},                    // different context window
		{Query: "alpha", Mode: "keyword", K: 5, Filter: &index.Filter{Type: "text"}}, // different filter
		{Query: "beta", Mode: "keyword", K: 5},                                       // different query
	}
	for i, v := range variants {
		if _, err := e.Query(context.Background(), v); err != nil {
			t.Fatalf("variant %d: %v", i, err)
		}
		if e.resultCache.Stats().Hits != hitsBefore {
			t.Fatalf("variant %d was a cache hit; want miss (different key): %+v", i, v)
		}
	}
}

// TestResultCache_Eviction asserts the result cache evicts least-recently-used
// at capacity (a bounded cache, not unbounded).
func TestResultCache_Eviction(t *testing.T) {
	e := newResultCacheEngine(t, 1) // capacity 1
	addDoc(t, e, "alpha beta gamma delta epsilon zeta")
	waitForEpochStable(t, e)

	// Query A (fills the single slot), then query B (evicts A), then query A
	// again — must be a miss (A was evicted), proving bounded LRU, not unbounded.
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Query(context.Background(), QueryRequest{Query: "beta", Mode: "keyword", K: 5}); err != nil {
		t.Fatal(err)
	}
	hitsBefore := e.resultCache.Stats().Hits
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5}); err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Stats().Hits != hitsBefore {
		t.Fatalf("evicted query 'alpha' was served from cache; want miss (capacity=1 LRU eviction)")
	}
	if e.resultCache.Stats().Size > 1 {
		t.Fatalf("size %d exceeds capacity 1", e.resultCache.Stats().Size)
	}
}

// TestResultCache_NoCacheBypass asserts NoCache bypasses serving but still
// stores (D5): a NoCache query is recomputed fresh (not served from cache), and
// the next NORMAL query of the same key hits.
func TestResultCache_NoCacheBypass(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	addDoc(t, e, "alpha retrieval document about searching")
	waitForEpochStable(t, e) // ensure no lingering async bump invalidates the key

	// First query is NoCache: must NOT be served (cache is empty anyway), and
	// must still STORE the result (D5).
	hitsBefore := e.resultCache.Stats().Hits
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5, NoCache: true}); err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Stats().Hits != hitsBefore {
		t.Fatalf("NoCache query was served from cache; want bypass")
	}

	// Next NORMAL query of the same key must HIT — NoCache stored the result.
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5}); err != nil {
		t.Fatal(err)
	}
	if got, want := e.resultCache.Stats().Hits, hitsBefore+1; got != want {
		t.Fatalf("hits after normal query following NoCache = %d, want %d (NoCache must still store, D5)", got, want)
	}
}

// TestResultCache_RerankFailedNotCached asserts a degraded (RerankFailed) result
// is never stored, so a retry isn't frozen at the failure (FR-009).
func TestResultCache_RerankFailedNotCached(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	// Point the reranker at a closed port so rerank fails deterministically.
	// The injected embedder is unaffected (it never touches OllamaURL).
	e.cfg.RerankModel = "dead-reranker"
	e.cfg.OllamaURL = "http://127.0.0.1:1"
	addDoc(t, e, "alpha retrieval document about searching and ranking")

	req := QueryRequest{Query: "alpha", Mode: "keyword", K: 5}
	res1, err := e.Query(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res1.RerankFailed {
		t.Fatalf("precondition: rerank did not fail (RerankFailed=false); cannot assert non-caching")
	}

	// Second identical query must NOT be served from cache.
	hitsBefore := e.resultCache.Stats().Hits
	if _, err := e.Query(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Stats().Hits != hitsBefore {
		t.Fatalf("RerankFailed result was served from cache; want always-miss (FR-009)")
	}
}

// TestResultCache_Disabled asserts the kill-switch: with caching off, every
// query is a miss and nothing is stored, and results are unaffected.
func TestResultCache_Disabled(t *testing.T) {
	e := newResultCacheEngine(t, 0) // disabled
	if e.resultCache.Enabled() {
		t.Fatalf("disabled cache reports Enabled=true")
	}
	addDoc(t, e, "alpha retrieval document about searching")

	req := QueryRequest{Query: "alpha", Mode: "keyword", K: 5}
	r1, err := e.Query(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := e.Query(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Stats().Hits != 0 {
		t.Fatalf("disabled cache recorded a hit; want 0")
	}
	if e.resultCache.Stats().Size != 0 {
		t.Fatalf("disabled cache stored an entry (size=%d); want 0", e.resultCache.Stats().Size)
	}
	if !hitsEqual(r1.Hits, r2.Hits) {
		t.Fatalf("results differ across disabled-cache queries: r1=%v r2=%v", r1.Hits, r2.Hits)
	}
}
