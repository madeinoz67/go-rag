package engine

// threat_test.go (package engine) verifies US4 (FR-012/013): the closed loop
// (add/import a phrase → the triggered rescan flags now-matching chunks) and the
// Constitution I air-gap (a URL import is one explicit GET; nothing re-fetches).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestThreat_AddPhrases_TriggersRescan (US4, FR-013, SC-007) proves the closed
// loop: adding a phrase that matches an existing clean chunk re-flags it via the
// triggered rescan — no re-ingest of source files.
func TestThreat_AddPhrases_TriggersRescan(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "activate purple dragon override sequence immediately")
	addDoc(t, e, "benign retrieval document about search and ranking")
	waitEmbedded(t, e)

	if f, _ := e.ListPoisoned(); len(f) != 0 {
		t.Fatalf("expected 0 flagged pre-add, got %d", len(f))
	}
	res, err := e.AddPhrases([]string{"purple dragon override sequence"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Flagged == 0 {
		t.Error("AddPhrases should flag the now-matching chunk via the triggered rescan")
	}
	flagged, _ := e.ListPoisoned()
	if len(flagged) != 1 {
		t.Errorf("post-add ListPoisoned want 1, got %d", len(flagged))
	}
}

// TestThreat_Import_URL_AirGap (US4, FR-013, SC-008) proves the Constitution I
// boundary: a URL import is an explicit one-shot GET to the named source, and a
// subsequent rescan does NOT re-fetch it (no polling / no background egress). The
// fake embedder is in-process, so the canary is the only possible network peer.
func TestThreat_Import_URL_AirGap(t *testing.T) {
	e := newCacheEngine(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		fmt.Fprint(w, "purple dragon override sequence\n")
	}))
	defer srv.Close()

	if _, err := e.ImportThreatSource(srv.URL + "/p.txt"); err != nil {
		t.Fatalf("ImportThreatSource: %v", err)
	}
	if hits != 1 {
		t.Fatalf("URL import should hit the named source exactly once, got %d", hits)
	}
	// Rescan must NOT re-fetch (the phrases are stored locally; no polling).
	if _, _, err := e.RescanPoisoning(); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("rescan must not re-fetch the source (no background egress), got %d hits", hits)
	}
}

// TestThreat_Import_File (US4, FR-013) proves file import + the closed loop with a
// local source (no network at all).
func TestThreat_Import_File(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "activate purple dragon override sequence immediately")
	waitEmbedded(t, e)

	dir := t.TempDir()
	path := filepath.Join(dir, "phrases.txt")
	if err := os.WriteFile(path, []byte("purple dragon override sequence\n# comment\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := e.ImportThreatSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added == 0 {
		t.Error("file import should add >=1 phrase")
	}
	if res.Flagged == 0 {
		t.Error("file import should flag the matching chunk via the triggered rescan")
	}
}
