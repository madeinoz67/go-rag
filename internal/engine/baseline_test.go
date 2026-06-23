package engine

// baseline_test.go + version_test.go (internal package `engine`) cover the H11
// foundational primitives: corpus-baseline persistence and the Ollama version
// fetch, in isolation.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/storage"
)

func openTempDB(t *testing.T) *storage.DB {
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
	return db
}

func TestCorpusBaseline_SaveLoadRoundTrip(t *testing.T) {
	db := openTempDB(t)

	if _, ok := LoadBaseline(db); ok {
		t.Fatalf("LoadBaseline on empty DB returned a baseline; want none")
	}

	want := &CorpusBaseline{
		Model:         "nomic-embed-text",
		Dim:           768,
		Convention:    "nomic",
		OllamaVersion: "0.1.0",
	}
	if err := SaveBaseline(db, want); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	got, ok := LoadBaseline(db)
	if !ok {
		t.Fatalf("LoadBaseline after save returned !ok")
	}
	if got.Model != want.Model || got.Dim != want.Dim ||
		got.Convention != want.Convention || got.OllamaVersion != want.OllamaVersion {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, want)
	}
	if got.RecordedAt.IsZero() {
		t.Fatalf("RecordedAt not stamped by SaveBaseline")
	}
	// Overwrite refreshes RecordedAt.
	prev := got.RecordedAt
	time.Sleep(10 * time.Millisecond)
	_ = SaveBaseline(db, &CorpusBaseline{Model: "mxbai-embed-large", Dim: 1024})
	got2, _ := LoadBaseline(db)
	if got2.Model != "mxbai-embed-large" || got2.Dim != 1024 {
		t.Fatalf("overwrite did not replace: %+v", got2)
	}
	if !got2.RecordedAt.After(prev) {
		t.Fatalf("overwrite did not advance RecordedAt")
	}
}

func TestCorpusBaseline_JSONShape(t *testing.T) {
	// Pin the persisted field names so a later, incompatible change is caught.
	b := CorpusBaseline{Model: "m", Dim: 1, Convention: "c", OllamaVersion: "v",
		RecordedAt: time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)}
	data, _ := json.Marshal(b)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"model", "dim", "convention", "ollama_version", "recorded_at"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing JSON field %q in baseline record", k)
		}
	}
}

func TestOllamaVersion_KnownVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.5.1"})
	}))
	defer srv.Close()
	if got := ollamaVersion(context.Background(), srv.URL); got != "0.5.1" {
		t.Fatalf("ollamaVersion = %q, want 0.5.1", got)
	}
}

func TestOllamaVersion_EmptyOffline(t *testing.T) {
	if got := ollamaVersion(context.Background(), ""); got != "" {
		t.Fatalf("ollamaVersion(empty) = %q, want \"\" (offline/injected)", got)
	}
}

func TestOllamaVersion_Unreachable(t *testing.T) {
	got := ollamaVersion(context.Background(), "http://127.0.0.1:1")
	if got != "unknown" {
		t.Fatalf("ollamaVersion(unreachable) = %q, want unknown", got)
	}
}

func TestOllamaVersion_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if got := ollamaVersion(context.Background(), srv.URL); got != "unknown" {
		t.Fatalf("ollamaVersion(404) = %q, want unknown", got)
	}
}
