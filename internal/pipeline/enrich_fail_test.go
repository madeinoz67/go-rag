package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/enrich"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// failingEnricher simulates enrichment failures: permanent (bad output) or
// transient (model unreachable). The document must still ingest and query
// normally regardless — only the sidecar status differs.
type failingEnricher struct {
	transient bool
}

func (f failingEnricher) Model() string { return "fake-gen" }
func (f failingEnricher) Enrich(_ context.Context, _ string) ([]string, string, error) {
	if f.transient {
		return nil, "", errors.New("model unreachable")
	}
	return nil, "", enrich.WrapPermanent(errors.New("bad model output"))
}

// ingestWith ingests one doc under the given enricher and returns the stored
// Document (after draining the async workers).
func ingestWith(t *testing.T, e enrich.Enricher) model.Document {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	p := New(db, chunk.NewSplitter(512, 50), fakeEmbedPI{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	if e != nil {
		p.SetEnricher(e)
	}
	dp := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(dp, []byte("A document on nightly incremental backups and retention."), 0o644); err != nil {
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
	return doc
}

// TestEnrich_PermanentFailureMarksFailed (spec 029, US3 / SC-004, T015): a
// permanent enrichment failure (bad model output) marks the document failed
// (terminal) — but the document still ingests.
func TestEnrich_PermanentFailureMarksFailed(t *testing.T) {
	doc := ingestWith(t, failingEnricher{transient: false})
	if doc.Enrichment == nil {
		t.Fatal("permanent failure should record a failed sidecar, got nil")
	}
	if doc.Enrichment.Status != model.EnrichStatusFailed {
		t.Errorf("Status = %q, want %q", doc.Enrichment.Status, model.EnrichStatusFailed)
	}
}

// TestEnrich_TransientFailureLeavesNil (spec 029, US3 / SC-004, T015): a
// transient failure (model unreachable) leaves the sidecar nil for a later retry —
// the document still ingests cleanly.
func TestEnrich_TransientFailureLeavesNil(t *testing.T) {
	doc := ingestWith(t, failingEnricher{transient: true})
	if doc.Enrichment != nil {
		t.Fatalf("transient failure must leave Enrichment nil for retry, got %+v", doc.Enrichment)
	}
}
