package engine_test

// neardup_e2e_test.go (H20/spec 026, US1/T018): end-to-end proof that --dedup
// collapses near-duplicate hits to one representative.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// sharedNearDupEngine ingests two near-identical docs (a word-reordering — same
// SimHash, different bytes) via a standalone pipeline, drains the async worker so
// near-dup clustering lands, then returns an Engine over the DB.
func sharedNearDupEngine(t *testing.T) *engine.Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	defer p.Close() // drain → async clustering lands before we return
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	words := "the go-rag server performs keyword retrieval over local documents stored on disk with a buffer cache"
	write("v1.txt", words)
	write("v2.txt", "cache buffer a with disk on stored documents local over retrieval keyword performs server go-rag the")
	if _, err := p.Ingest(context.Background(), filepath.Join(dir, "v1.txt"), "*"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Ingest(context.Background(), filepath.Join(dir, "v2.txt"), "*"); err != nil {
		t.Fatal(err)
	}
	return engine.NewWithDB(cfg, db)
}

// TestNearDup_Collapse_E2E (US1, SC-001): query a shared phrase; without dedup
// both near-dup chunks appear; with --dedup the pair collapses to one.
func TestNearDup_Collapse_E2E(t *testing.T) {
	eng := sharedNearDupEngine(t)
	q := "keyword retrieval"

	without, err := eng.Query(context.Background(), engine.QueryRequest{Query: q, Mode: "keyword", K: 5, NoCache: true})
	if err != nil {
		t.Fatalf("query without dedup: %v", err)
	}
	if len(without.Hits) < 2 {
		t.Fatalf("want >=2 hits without dedup, got %d", len(without.Hits))
	}

	withDedup, err := eng.Query(context.Background(), engine.QueryRequest{Query: q, Mode: "keyword", K: 5, Dedup: true, NoCache: true})
	if err != nil {
		t.Fatalf("query with dedup: %v", err)
	}
	if len(withDedup.Hits) >= len(without.Hits) {
		t.Errorf("dedup should reduce hits: without=%d with=%d", len(without.Hits), len(withDedup.Hits))
	}
	if len(withDedup.Hits) == 0 {
		t.Error("dedup should keep at least one representative")
	}
}
