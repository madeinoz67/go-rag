package engine_test

// delete_test.go (spec 050 / T004): pins Engine.DeleteDoc — the new cross-
// transport delete operation's engine wrapper. Proves (a) delete by ID removes
// the doc + its chunks, (b) a subsequent keyword query returns no hit (the live
// FTS/Vector index is cleared in place — no phantoms), (c) unknown ID →
// ErrNotFound, (d) empty ID → ErrInvalid, and (e) the source file on disk is
// unchanged (index-only, FR-011). Mirrors sharedNearDupEngine's ingest-then-wrap
// pattern; reuses the package's fakeEmbed.

import (
	"context"
	"errors"
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

// deleteFixtureEngine ingests one distinctive document via a standalone pipeline
// (fakeEmbed — no Ollama), drains, then returns an Engine over the same DB plus
// the on-disk source path and the resolved doc ID. The engine's lazy pipeline
// re-seeds its FTS/Vector from storage on first use, so the ingested chunks are
// queryable through it.
func deleteFixtureEngine(t *testing.T) (*engine.Engine, string, string) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	defer p.Close() // drain async embed → chunks land before we return
	src := filepath.Join(dir, "deletable.txt")
	content := []byte("tariff deficit solar deadline charge window peak overnight shoulder offpeak grid import export battery inverter\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if _, err := p.Ingest(context.Background(), defaultWS(db), src, "*"); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	eng := engine.NewWithDB(cfg, db)
	t.Cleanup(eng.Close)
	// Resolve the doc ID via the engine's list (Ingest's Result carries counts,
	// not IDs).
	res, err := eng.ListDocuments(engine.ListDocumentsRequest{})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(res.Documents) != 1 {
		t.Fatalf("setup: want 1 doc, got %d", len(res.Documents))
	}
	return eng, src, res.Documents[0].ID
}

// TestDeleteDoc_RemovesDocAndChunks: delete by ID removes the document and every
// chunk from the index; a keyword query that matched before returns no hit after
// (the live FTS/Vector is cleared in place — no phantom hits, H01/spec 011).
func TestDeleteDoc_RemovesDocAndChunks(t *testing.T) {
	eng, _, docID := deleteFixtureEngine(t)

	// Pre-condition: the distinctive term matches.
	before, err := eng.Query(context.Background(), engine.QueryRequest{Query: "tariff deficit", Mode: "keyword", K: 5, NoCache: true})
	if err != nil {
		t.Fatalf("query before: %v", err)
	}
	if len(before.Hits) == 0 {
		t.Fatal("setup: expected a keyword hit before delete")
	}

	chunksBefore, err := eng.ListChunks(docID, engine.ListChunksRequest{})
	if err != nil {
		t.Fatalf("ListChunks before: %v", err)
	}
	if len(chunksBefore.Chunks) == 0 {
		t.Fatal("setup: doc has no chunks")
	}

	if err := eng.DeleteDoc(context.Background(), docID); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}

	// Doc is gone from the list.
	docs, err := eng.ListDocuments(engine.ListDocumentsRequest{})
	if err != nil {
		t.Fatalf("ListDocuments after: %v", err)
	}
	for _, d := range docs.Documents {
		if d.ID == docID {
			t.Errorf("doc %s still listed after delete", docID)
		}
	}

	// Chunks are gone (empty page — a tolerant empty result, not an error).
	chunksAfter, err := eng.ListChunks(docID, engine.ListChunksRequest{})
	if err != nil {
		t.Fatalf("ListChunks after: %v", err)
	}
	if len(chunksAfter.Chunks) != 0 {
		t.Errorf("chunks after delete: got %d, want 0", len(chunksAfter.Chunks))
	}

	// Keyword query returns no hit — the live FTS index was cleared.
	after, err := eng.Query(context.Background(), engine.QueryRequest{Query: "tariff deficit", Mode: "keyword", K: 5, NoCache: true})
	if err != nil {
		t.Fatalf("query after: %v", err)
	}
	for _, h := range after.Hits {
		if h.DocumentID == docID {
			t.Errorf("phantom hit: doc %s still in keyword results after delete", docID)
		}
	}
}

// TestDeleteDoc_UnknownID: an unknown doc ID is a real error (ErrNotFound), not
// a silent no-op — the operator-facing surface needs a 404. (Pipeline.DeleteDoc
// is a silent no-op on missing records for the watcher's idempotent re-scans;
// the engine wrapper adds the existence check so transports can map a 404.)
func TestDeleteDoc_UnknownID(t *testing.T) {
	eng, _, _ := deleteFixtureEngine(t)
	err := eng.DeleteDoc(context.Background(), "definitely-not-a-real-doc-id")
	if !errors.Is(err, engine.ErrNotFound) {
		t.Errorf("unknown ID: err=%v, want ErrNotFound", err)
	}
}

// TestDeleteDoc_EmptyID: an empty/whitespace doc ID is ErrInvalid (400 on the
// transports), validated before any storage read.
func TestDeleteDoc_EmptyID(t *testing.T) {
	eng, _, _ := deleteFixtureEngine(t)
	for _, bad := range []string{"", "   ", "\t"} {
		if err := eng.DeleteDoc(context.Background(), bad); !errors.Is(err, engine.ErrInvalid) {
			t.Errorf("DeleteDoc(%q): err=%v, want ErrInvalid", bad, err)
		}
	}
}

// TestDeleteDoc_SourceFileUntouched: removal is index-only (FR-011) — the source
// file on disk is neither modified nor deleted.
func TestDeleteDoc_SourceFileUntouched(t *testing.T) {
	eng, src, docID := deleteFixtureEngine(t)
	want, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read src before: %v", err)
	}
	if err := eng.DeleteDoc(context.Background(), docID); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("source file vanished after delete (FR-011 violation): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("source file mutated by delete: got %q, want %q", got, want)
	}
}
