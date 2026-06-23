package engine

// drift_test.go (internal package `engine`) proves the H11/spec 017 drift
// verdict + readiness behavior at the engine level: hard drift (model/dim/
// convention) is detected and makes readiness NOT READY while liveness stays OK
// (clarification posture A); a matching baseline is clean and ready.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newDriftEngine builds an engine over a temp DB with the deterministic fake
// embedder and OllamaURL="" (offline → version fetch skipped), so the verdict
// is a pure function of the baseline vs configured model/dim/convention.
func newDriftEngine(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	cfg.OllamaURL = "" // offline: version comparison skipped (FR-010 path)
	e := NewWithEmbedder(cfg, db, cacheFakeEmb{})
	t.Cleanup(e.Close)
	return e
}

// TestDrift_HardDrift_ModelMismatch: baseline model ≠ configured → hard drift,
// readiness NOT READY, liveness OK (posture A).
func TestDrift_HardDrift_ModelMismatch(t *testing.T) {
	e := newDriftEngine(t)
	if err := SaveBaseline(e.db, &CorpusBaseline{Model: "nomic-embed-text", Dim: 2, Convention: ""}); err != nil {
		t.Fatal(err)
	}
	e.cfg.EmbeddingModel = "mxbai-embed-large"

	v := e.RefreshDriftVerdict(context.Background())
	if v.Verdict != VerdictHardDrift || !v.Hard {
		t.Fatalf("verdict=%q hard=%v, want hard-drift/true", v.Verdict, v.Hard)
	}
	if len(v.Reasons) == 0 {
		t.Fatalf("hard drift must list reasons, got none")
	}

	h := e.Health(context.Background())
	if !h.OK {
		t.Errorf("Health.OK = false, want true (liveness stays OK on drift)")
	}
	if h.Ready {
		t.Errorf("Health.Ready = true, want false (hard drift → not ready)")
	}
	if h.DriftVerdict != VerdictHardDrift {
		t.Errorf("Health.DriftVerdict = %q, want hard-drift", h.DriftVerdict)
	}
}

// TestDrift_HardDrift_DimMismatch: a dimension change (same model name, dim
// differs) is also hard drift — the Ollama-model-update-changed-dim case.
func TestDrift_HardDrift_DimMismatch(t *testing.T) {
	e := newDriftEngine(t)
	// Baseline model matches configured, but dim differs from the live embedder
	// (cacheFakeEmb reports dim 2).
	if err := SaveBaseline(e.db, &CorpusBaseline{Model: "fake", Dim: 768, Convention: ""}); err != nil {
		t.Fatal(err)
	}
	v := e.RefreshDriftVerdict(context.Background())
	if v.Verdict != VerdictHardDrift {
		t.Fatalf("verdict=%q, want hard-drift (dim mismatch)", v.Verdict)
	}
	if e.Health(context.Background()).Ready {
		t.Errorf("Ready=true on dim mismatch; want false")
	}
}

// TestDrift_Clean: a matching baseline → clean, ready, liveness OK.
func TestDrift_Clean(t *testing.T) {
	e := newDriftEngine(t)
	if err := SaveBaseline(e.db, &CorpusBaseline{Model: "fake", Dim: 2, Convention: ""}); err != nil {
		t.Fatal(err)
	}
	v := e.RefreshDriftVerdict(context.Background())
	if v.Verdict != VerdictClean {
		t.Fatalf("verdict=%q, want clean", v.Verdict)
	}
	h := e.Health(context.Background())
	if !h.OK || !h.Ready {
		t.Fatalf("Health OK=%v Ready=%v, want both true (clean)", h.OK, h.Ready)
	}
}

// TestDrift_NoBaselineNA: no baseline (empty corpus / before backfill) → n/a,
// readiness stays ready (nothing to compare; liveness OK).
func TestDrift_NoBaselineNA(t *testing.T) {
	e := newDriftEngine(t)
	v := e.RefreshDriftVerdict(context.Background())
	if v.Verdict != VerdictNA {
		t.Fatalf("verdict=%q, want n/a (no baseline)", v.Verdict)
	}
	h := e.Health(context.Background())
	if !h.Ready {
		t.Errorf("Ready=false with no baseline; want true (nothing to compare)")
	}
}
