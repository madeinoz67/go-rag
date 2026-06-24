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
