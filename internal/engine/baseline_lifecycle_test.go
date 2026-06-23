package engine

// baseline_lifecycle_test.go (internal package `engine`) proves the H11/spec 017
// corpus-baseline lifecycle: written on first embed, backfilled for a pre-H11
// corpus on first boot, refreshed after migrate.

import (
	"context"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// waitForBaseline polls until a corpus baseline exists (the first-embed write
// happens just after the embed loop in processJob, so it can lag waitEmbedded —
// the same async window the H06 epoch tests account for).
func waitForBaseline(t *testing.T, e *Engine) *CorpusBaseline {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, ok := LoadBaseline(e.db); ok {
			return b
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("corpus baseline never written within 5s")
	return nil
}

// TestBaseline_WrittenOnFirstEmbed: ingesting into an empty corpus writes a
// baseline capturing the embedding profile.
func TestBaseline_WrittenOnFirstEmbed(t *testing.T) {
	e := newDriftEngine(t, "")
	addDoc(t, e, "a document whose embedding seeds the corpus baseline")

	b := waitForBaseline(t, e)
	if b.Model != "fake" { // cacheFakeEmb.Model()
		t.Errorf("baseline model = %q, want fake", b.Model)
	}
	if b.Dim != 2 { // cacheFakeEmb.Dimensions()
		t.Errorf("baseline dim = %d, want 2", b.Dim)
	}
	if b.RecordedAt.IsZero() {
		t.Errorf("baseline RecordedAt not stamped")
	}
}

// TestBaseline_BackfillOnFirstBoot: a corpus with embeddings but no baseline
// (a pre-H11 vault) gets a baseline derived from the stored majority on the
// first drift check — no re-ingestion.
func TestBaseline_BackfillOnFirstBoot(t *testing.T) {
	e := newDriftEngine(t, "")
	addDoc(t, e, "pre-existing corpus document for backfill")
	waitForBaseline(t, e) // first-embed already wrote one; delete it to simulate pre-H11

	if err := e.db.DeleteWithPrefix(storage.PrefixCorpusMeta, []byte(corpusBaselineKey)); err != nil {
		t.Fatalf("delete baseline: %v", err)
	}
	if _, ok := LoadBaseline(e.db); ok {
		t.Fatal("baseline still present after delete")
	}

	// The next drift check backfills from CorpusProfile (the stored majority).
	e.RefreshDriftVerdict(context.Background())
	b, ok := LoadBaseline(e.db)
	if !ok {
		t.Fatal("baseline not backfilled on first drift check")
	}
	if b.Model != "fake" {
		t.Errorf("backfilled model = %q, want fake (majority)", b.Model)
	}
	if b.Dim != 2 {
		t.Errorf("backfilled dim = %d, want 2", b.Dim)
	}
}

// TestBaseline_RefreshedAfterMigrate: refreshBaselineAfterMigrate rewrites the
// baseline (RecordedAt advances) and refreshes the cached verdict.
func TestBaseline_RefreshedAfterMigrate(t *testing.T) {
	e := newDriftEngine(t, "")
	// Plant an OLD baseline (as if from a prior model), then refresh.
	old := &CorpusBaseline{Model: "old-model", Dim: 999, Convention: "x", OllamaVersion: "0.1.0"}
	if err := SaveBaseline(e.db, old); err != nil {
		t.Fatal(err)
	}

	e.refreshBaselineAfterMigrate(context.Background())
	b, ok := LoadBaseline(e.db)
	if !ok {
		t.Fatal("baseline missing after refresh")
	}
	if b.Model != "fake" { // refresh writes the current embedder's model
		t.Errorf("refreshed model = %q, want fake", b.Model)
	}
	if b.Dim != 2 { // dim comes from the current embedder (cacheFakeEmb), not preserved
		t.Errorf("refreshed dim = %d, want 2 (current embedder)", b.Dim)
	}
	if !b.RecordedAt.After(old.RecordedAt) {
		t.Errorf("RecordedAt not advanced by refresh")
	}
}
