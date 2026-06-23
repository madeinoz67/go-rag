package engine

// drift_test.go (internal package `engine`) proves the H11/spec 017 drift
// verdict + readiness behavior at the engine level: hard drift (model/dim/
// convention) is detected and makes readiness NOT READY while liveness stays OK
// (clarification posture A); a matching baseline is clean and ready.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newDriftEngine builds an engine over a temp DB with the deterministic fake
// embedder. ollamaURL controls the version-fetch path: "" = offline (version
// skipped, FR-010), an httptest URL = a controllable live version, a dead URL
// = "unknown" (unreachable, FR-006).
func newDriftEngine(t *testing.T, ollamaURL string) *Engine {
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
	cfg.OllamaURL = ollamaURL
	e := NewWithEmbedder(cfg, db, cacheFakeEmb{})
	t.Cleanup(e.Close)
	return e
}

// TestDrift_HardDrift_ModelMismatch: baseline model ≠ configured → hard drift,
// readiness NOT READY, liveness OK (posture A).
func TestDrift_HardDrift_ModelMismatch(t *testing.T) {
	e := newDriftEngine(t, "") // offline: focus on the hard comparison
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
	e := newDriftEngine(t, "")
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
	e := newDriftEngine(t, "")
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
	e := newDriftEngine(t, "")
	v := e.RefreshDriftVerdict(context.Background())
	if v.Verdict != VerdictNA {
		t.Fatalf("verdict=%q, want n/a (no baseline)", v.Verdict)
	}
	h := e.Health(context.Background())
	if !h.Ready {
		t.Errorf("Ready=false with no baseline; want true (nothing to compare)")
	}
}

// --- US2: Ollama-version pinning (soft drift) ---

// versionServer is an httptest Ollama stand-in that serves only /api/version.
func versionServer(t *testing.T, version string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestDrift_VersionWarning: baseline version ≠ live → version-warning (soft),
// Ready stays true (warn, serve).
func TestDrift_VersionWarning(t *testing.T) {
	e := newDriftEngine(t, versionServer(t, "0.5.0"))
	if err := SaveBaseline(e.db, &CorpusBaseline{Model: "fake", Dim: 2, Convention: "", OllamaVersion: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	v := e.RefreshDriftVerdict(context.Background())
	if v.Verdict != VerdictVersionWarning {
		t.Fatalf("verdict=%q, want version-warning", v.Verdict)
	}
	if v.Hard {
		t.Errorf("Hard=true on version-warning; want false (soft)")
	}
	if v.LiveVersion != "0.5.0" || v.BaselineVersion != "0.1.0" {
		t.Errorf("versions baseline=%q live=%q", v.BaselineVersion, v.LiveVersion)
	}
	h := e.Health(context.Background())
	if !h.Ready || !h.OK {
		t.Errorf("Health Ready=%v OK=%v, want both true (soft drift serves)", h.Ready, h.OK)
	}
}

// TestDrift_HardWinsOverVersion: model mismatch AND version differ → hard-drift
// wins (Ready false), not version-warning.
func TestDrift_HardWinsOverVersion(t *testing.T) {
	e := newDriftEngine(t, versionServer(t, "0.5.0"))
	if err := SaveBaseline(e.db, &CorpusBaseline{Model: "nomic-embed-text", Dim: 2, Convention: "", OllamaVersion: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	e.cfg.EmbeddingModel = "mxbai-embed-large" // model mismatch too
	v := e.RefreshDriftVerdict(context.Background())
	if v.Verdict != VerdictHardDrift || !v.Hard {
		t.Fatalf("verdict=%q hard=%v, want hard-drift/true (hard wins over version)", v.Verdict, v.Hard)
	}
	if e.Health(context.Background()).Ready {
		t.Errorf("Ready=true; want false (hard drift)")
	}
}

// TestDrift_OllamaUnreachable: dead OllamaURL → live "unknown"; boot safe
// (RefreshDriftVerdict returns no error); model/convention still computed; the
// verdict is "unknown" (version couldn't be verified) but NOT hard → Ready true.
func TestDrift_OllamaUnreachable(t *testing.T) {
	e := newDriftEngine(t, "http://127.0.0.1:1") // unreachable
	if err := SaveBaseline(e.db, &CorpusBaseline{Model: "fake", Dim: 2, Convention: "", OllamaVersion: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	v := e.RefreshDriftVerdict(context.Background()) // must not hang/error
	if v.LiveVersion != "unknown" {
		t.Fatalf("LiveVersion=%q, want unknown (unreachable)", v.LiveVersion)
	}
	if v.Verdict != VerdictUnknown {
		t.Fatalf("verdict=%q, want unknown (Ollama unreachable, version unverifiable)", v.Verdict)
	}
	if v.Hard {
		t.Errorf("Hard=true on unreachable; want false (model/convention match)")
	}
	if !e.Health(context.Background()).Ready {
		t.Errorf("Ready=false on unreachable; want true (not hard)")
	}
}

// TestDrift_OfflineEmbedderSkipsVersion: OllamaURL="" (offline/injected embedder)
// → live "" → version comparison skipped (FR-010); a matching profile is clean.
func TestDrift_OfflineEmbedderSkipsVersion(t *testing.T) {
	e := newDriftEngine(t, "") // offline
	if err := SaveBaseline(e.db, &CorpusBaseline{Model: "fake", Dim: 2, Convention: "", OllamaVersion: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	v := e.RefreshDriftVerdict(context.Background())
	if v.LiveVersion != "" {
		t.Fatalf("LiveVersion=%q, want \"\" (offline)", v.LiveVersion)
	}
	if v.Verdict != VerdictClean {
		t.Fatalf("verdict=%q, want clean (offline skips version; profile matches)", v.Verdict)
	}
}
