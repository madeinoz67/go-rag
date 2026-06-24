package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newTestEngineCfg is newTestEngine with a config mutator, so H22 tests can set
// pool_size / adaptive_depth_enabled before the engine is built (the engine
// holds cfg by value, so post-construction mutation is not possible).
func newTestEngineCfg(t *testing.T, mutate func(*config.Config)) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "nomic-embed-text"
	if mutate != nil {
		mutate(&cfg)
	}
	return NewWithDB(cfg, db), dir
}

// TestEngine_Query_EffectivePool_Resolution (H22/spec 024, US1) proves the
// effective candidate pool resolves as per-query override > config ceiling >
// default (60), and that it + the effective depth/mode are echoed on the
// response. Runs on an empty corpus (keyword mode returns empty Hits but still
// populates the effective fields); NoCache forces fresh computation each call.
func TestEngine_Query_EffectivePool_Resolution(t *testing.T) {
	eng, _ := newTestEngineCfg(t, nil) // default cfg: PoolSize 60

	// PoolSize 0 → config default 60; effK defaults to 5; mode echoed.
	res, err := eng.Query(t.Context(), QueryRequest{Query: "x", Mode: "keyword", NoCache: true})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res.EffectivePool != 60 {
		t.Errorf("default pool: EffectivePool=%d want 60", res.EffectivePool)
	}
	if res.EffectiveK != 5 {
		t.Errorf("default k: EffectiveK=%d want 5", res.EffectiveK)
	}
	if res.EffectiveMode != "keyword" {
		t.Errorf("mode echo: EffectiveMode=%q want keyword", res.EffectiveMode)
	}

	// Per-query override 30 wins over config.
	res, _ = eng.Query(t.Context(), QueryRequest{Query: "x", Mode: "keyword", PoolSize: 30, NoCache: true})
	if res.EffectivePool != 30 {
		t.Errorf("override: EffectivePool=%d want 30", res.EffectivePool)
	}

	// A raised config ceiling (100) applies when no override is set.
	eng2, _ := newTestEngineCfg(t, func(c *config.Config) { c.PoolSize = 100 })
	res, _ = eng2.Query(t.Context(), QueryRequest{Query: "x", Mode: "keyword", NoCache: true})
	if res.EffectivePool != 100 {
		t.Errorf("cfg ceiling: EffectivePool=%d want 100", res.EffectivePool)
	}
	// Per-query override still beats the raised config.
	res, _ = eng2.Query(t.Context(), QueryRequest{Query: "x", Mode: "keyword", PoolSize: 20, NoCache: true})
	if res.EffectivePool != 20 {
		t.Errorf("override>cfg: EffectivePool=%d want 20", res.EffectivePool)
	}
}

// TestEngine_Query_EffectiveK_Resolution (H22/spec 024, US2) proves effective
// depth resolves as explicit > recommended > default (FR-006): the classifier
// recommends a shallow k for a short factoid only when k is unset; an explicit k
// wins; a comparative query gets no recommendation (default); and with the
// classifier disabled no classification occurs.
func TestEngine_Query_EffectiveK_Resolution(t *testing.T) {
	engC, _ := newTestEngineCfg(t, func(c *config.Config) { c.AdaptiveDepthEnabled = true })

	// Factoid, no explicit k → recommended 3.
	res, _ := engC.Query(t.Context(), QueryRequest{Query: "max batch size", Mode: "keyword", NoCache: true})
	if res.EffectiveK != 3 {
		t.Errorf("factoid recommended: EffectiveK=%d want 3", res.EffectiveK)
	}
	// Explicit k=8 beats the classifier.
	res, _ = engC.Query(t.Context(), QueryRequest{Query: "max batch size", Mode: "keyword", K: 8, NoCache: true})
	if res.EffectiveK != 8 {
		t.Errorf("explicit wins: EffectiveK=%d want 8", res.EffectiveK)
	}
	// Comparative, no explicit k → no recommendation → default 5.
	res, _ = engC.Query(t.Context(), QueryRequest{Query: "compare caching and drift approaches", Mode: "keyword", NoCache: true})
	if res.EffectiveK != 5 {
		t.Errorf("comparative default: EffectiveK=%d want 5", res.EffectiveK)
	}

	// Classifier disabled → factoid uses the default (no classification).
	eng, _ := newTestEngineCfg(t, nil)
	res, _ = eng.Query(t.Context(), QueryRequest{Query: "max batch size", Mode: "keyword", NoCache: true})
	if res.EffectiveK != 5 {
		t.Errorf("disabled factoid: EffectiveK=%d want 5", res.EffectiveK)
	}
}

// TestEngine_Query_FR011_PoolShrinksWithRecommendedK (H22/spec 024, US2/FR-011)
// proves that when the classifier recommends a shallow k, the effective candidate
// pool shrinks with it (k+slack clamped to [floor, ceiling]); with no
// recommendation the full configured ceiling is used; and a per-query override
// beats the classifier-derived pool.
func TestEngine_Query_FR011_PoolShrinksWithRecommendedK(t *testing.T) {
	eng, _ := newTestEngineCfg(t, func(c *config.Config) { c.AdaptiveDepthEnabled = true })

	// Factoid (recommended k=3) → pool = EffectivePoolFor(3, 10, 20, 60) = 20.
	res, _ := eng.Query(t.Context(), QueryRequest{Query: "max batch size", Mode: "keyword", NoCache: true})
	if res.EffectivePool != 20 {
		t.Errorf("factoid shrunk pool: EffectivePool=%d want 20", res.EffectivePool)
	}
	// Comparative (no recommendation) → full ceiling 60.
	res, _ = eng.Query(t.Context(), QueryRequest{Query: "compare caching and drift approaches", Mode: "keyword", NoCache: true})
	if res.EffectivePool != 60 {
		t.Errorf("comparative full pool: EffectivePool=%d want 60", res.EffectivePool)
	}
	// Per-query override beats classifier-derived shrinking.
	res, _ = eng.Query(t.Context(), QueryRequest{Query: "max batch size", Mode: "keyword", PoolSize: 50, NoCache: true})
	if res.EffectivePool != 50 {
		t.Errorf("override>classifier: EffectivePool=%d want 50", res.EffectivePool)
	}
}
