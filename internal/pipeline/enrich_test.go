package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// fakeEmbedPI is a hermetic embed.Embedder for the enrichment test (no Ollama).
type fakeEmbedPI struct{}

func (fakeEmbedPI) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1.0, 0.0}
	}
	return out, nil
}
func (fakeEmbedPI) Dimensions() int { return 2 }
func (fakeEmbedPI) Model() string   { return "fake" }

// fakeEnricher returns a fixed tag set + summary so the sidecar write is
// deterministic and hermetic (no model call).
type fakeEnricher struct{}

func (fakeEnricher) Model() string { return "fake-gen" }
func (fakeEnricher) Enrich(_ context.Context, _ string) ([]string, string, error) {
	return []string{"security", "backups"}, "a doc about security backups", nil
}

// TestPipeline_EnrichsDocument (spec 029, US1 / SC-001): with an enricher bound,
// an ingested document gains a non-identity Enrichment sidecar async-after-ACK
// (tags + summary + status + model). The sidecar is a separate Document field,
// so identity is unchanged; enrichment is post-ACK so ingest is non-blocking.
func TestPipeline_EnrichsDocument(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	p := New(db, chunk.NewSplitter(512, 50), fakeEmbedPI{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	p.SetEnricher(fakeEnricher{})

	dp := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(dp, []byte("Nightly incremental backups protect against ransomware and disk failure. "+
		"The retention window is thirty days of security-hardened snapshots stored locally."), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	res, err := p.Ingest(context.Background(), dp, "*")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.New == 0 {
		t.Fatal("expected 1 new document")
	}
	p.Close() // drain async embed + enrich

	// Read the document back; it must carry the Enrichment sidecar.
	var doc model.Document
	found := false
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_ []byte, val []byte) bool {
		if json.Unmarshal(val, &doc) == nil {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Fatal("no document stored")
	}
	if doc.Enrichment == nil {
		t.Fatalf("expected Enrichment sidecar, got nil; doc=%+v", doc)
	}
	if doc.Enrichment.Status != model.EnrichStatusDone {
		t.Errorf("Status = %q, want %q", doc.Enrichment.Status, model.EnrichStatusDone)
	}
	if len(doc.Enrichment.Tags) != 2 || doc.Enrichment.Tags[0] != "security" || doc.Enrichment.Tags[1] != "backups" {
		t.Errorf("Tags = %v, want [security backups]", doc.Enrichment.Tags)
	}
	if doc.Enrichment.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if doc.Enrichment.Model != "fake-gen" {
		t.Errorf("Model = %q, want fake-gen", doc.Enrichment.Model)
	}
}

// TestPipeline_NoEnricherIsNoop (spec 029, FR-006): with no enricher bound (the
// default / opt-in-off), a document ingests normally and carries no sidecar —
// byte-identical to pre-feature behaviour.
func TestPipeline_NoEnricherIsNoop(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	p := New(db, chunk.NewSplitter(512, 50), fakeEmbedPI{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	// No SetEnricher — enrichment off.
	dp := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(dp, []byte("A document with no enrichment configured."), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := p.Ingest(context.Background(), dp, "*"); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	p.Close()

	var doc model.Document
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_ []byte, val []byte) bool {
		_ = json.Unmarshal(val, &doc)
		return false
	})
	if doc.Enrichment != nil {
		t.Fatalf("with no enricher, Enrichment must be nil, got %+v", doc.Enrichment)
	}
}
