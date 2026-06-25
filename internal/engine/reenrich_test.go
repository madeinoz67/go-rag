package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// reFirstDoc reads the (single) stored document (test helper).
func reFirstDoc(t *testing.T, db *storage.DB) model.Document {
	t.Helper()
	var d model.Document
	found := false
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_ []byte, val []byte) bool {
		if json.Unmarshal(val, &d) == nil {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Fatal("no document in DB")
	}
	return d
}

// reSetEnrichment plants an EnrichInfo sidecar on the stored document (test helper).
func reSetEnrichment(t *testing.T, db *storage.DB, docID string, info *model.EnrichInfo) {
	t.Helper()
	raw, ok, _ := db.GetWithPrefix(storage.PrefixDocument, []byte(docID))
	if !ok {
		t.Fatal("doc not found")
	}
	var d model.Document
	if json.Unmarshal(raw, &d) != nil {
		t.Fatal("unmarshal doc")
	}
	d.Enrichment = info
	dj, _ := json.Marshal(d)
	_ = db.SetWithPrefix(storage.PrefixDocument, []byte(docID), dj)
}

// TestReEnrich_DisabledIsNoop (spec 029, US3 back-fill): with enrichment off,
// ReEnrich is a no-op — zero summary, no sidecar written.
func TestReEnrich_DisabledIsNoop(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4})

	sum, err := eng.ReEnrich(context.Background())
	if err != nil {
		t.Fatalf("ReEnrich: %v", err)
	}
	if sum.New != 0 || sum.Errors != 0 {
		t.Errorf("disabled ReEnrich must be a no-op, got %+v", sum)
	}
	if d := reFirstDoc(t, db); d.Enrichment != nil {
		t.Errorf("disabled ReEnrich must not enrich, got %+v", d.Enrichment)
	}
}

// TestReEnrich_SkipsAlreadyEnriched (spec 029, US3 back-fill): a document
// already successfully enriched (Done) is skipped — its sidecar is unchanged and
// no model call is made (the only doc is Done, so ReEnrich has no work).
func TestReEnrich_SkipsAlreadyEnriched(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	cfg.EnrichmentEnabled = true
	cfg.EnrichmentModel = "fake-gen"
	doc := reFirstDoc(t, db)
	reSetEnrichment(t, db, doc.ID, &model.EnrichInfo{Tags: []string{"planted"}, Status: model.EnrichStatusDone, Model: "fake-gen"})

	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4})
	sum, err := eng.ReEnrich(context.Background())
	if err != nil {
		t.Fatalf("ReEnrich: %v", err)
	}
	if sum.New != 0 {
		t.Errorf("already-enriched doc must be skipped, got New=%d", sum.New)
	}
	after := reFirstDoc(t, db)
	if after.Enrichment == nil || after.Enrichment.Status != model.EnrichStatusDone ||
		len(after.Enrichment.Tags) != 1 || after.Enrichment.Tags[0] != "planted" {
		t.Errorf("enriched doc sidecar must be unchanged, got %+v", after.Enrichment)
	}
}
