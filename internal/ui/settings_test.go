package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/engine"
)

// settingsServer stands up a fresh engine + admin + UI server and logs in,
// returning the server, a valid bearer session token, and the engine (for
// parity checks). Mirrors the TestPlaceholder_Routes / observability scaffolding.
func settingsServer(t *testing.T) (*httptest.Server, string, *engine.Engine) {
	t.Helper()
	eng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)
	tok, _ := login(t, srv.URL, auth.DefaultAdminUsername, "s3cret", http.StatusOK)
	return srv, tok, eng
}

func getSettings(t *testing.T, srv *httptest.Server, tok string) settingsDTO {
	t.Helper()
	resp := bearerGet(t, srv.URL+"/api/settings", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/settings: want 200, got %d", resp.StatusCode)
	}
	var got settingsDTO
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return got
}

// TestSettings_RetrievalAndEmbeddings — US1. Authenticated GET /api/settings
// returns the effective retrieval + embedding configuration, and every field
// equals the engine's own Status/Config projection (FR-007 parity, SC-002).
func TestSettings_RetrievalAndEmbeddings(t *testing.T) {
	srv, tok, eng := settingsServer(t)
	got := getSettings(t, srv, tok)

	// Use the SAME vault the handler chose, so the comparison is exact regardless
	// of the default-vault spelling.
	info, err := eng.Status(got.Vault)
	if err != nil {
		t.Fatalf("eng.Status: %v", err)
	}
	want := toSettingsDTO(info, eng.Config(), got.Vault)
	if got.Retrieval != want.Retrieval || got.Embeddings != want.Embeddings {
		t.Errorf("retrieval/embeddings mismatch:\n got retrieval=%+v embeddings=%+v\nwant retrieval=%+v embeddings=%+v",
			got.Retrieval, got.Embeddings, want.Retrieval, want.Embeddings)
	}
	// Prefix mode resolves to a non-empty effective value (auto|on|off) even for a
	// fake/empty corpus — the "auto" → resolved-convention edge case (FR-003).
	if got.Embeddings.PrefixMode == "" {
		t.Errorf("prefix_mode empty: want auto|on|off, got %q", got.Embeddings.PrefixMode)
	}
}

// TestSettings_CacheChunkingRedaction — US2. The cache/chunking/redaction groups
// match the engine projection; chunking's policy is the fixed cascade (spec
// 013/025); a default-off redaction surfaces a non-negative pattern count.
func TestSettings_CacheChunkingRedaction(t *testing.T) {
	srv, tok, eng := settingsServer(t)
	got := getSettings(t, srv, tok)

	info, err := eng.Status(got.Vault)
	if err != nil {
		t.Fatalf("eng.Status: %v", err)
	}
	want := toSettingsDTO(info, eng.Config(), got.Vault)
	if got.Cache != want.Cache || got.Chunking != want.Chunking || got.Redaction != want.Redaction {
		t.Errorf("cache/chunking/redaction mismatch:\n got cache=%+v chunking=%+v redaction=%+v\nwant cache=%+v chunking=%+v redaction=%+v",
			got.Cache, got.Chunking, got.Redaction, want.Cache, want.Chunking, want.Redaction)
	}
	if got.Chunking.BoundaryMode != "paragraph-sentence-word" || !got.Chunking.SectionContext {
		t.Errorf("chunking fixed-policy drift: %+v", got.Chunking)
	}
	if got.Redaction.PatternCount < 0 {
		t.Errorf("pattern_count negative: %d", got.Redaction.PatternCount)
	}
}

// TestSettings_401Unguarded — US3. Without a bearer session the route 401s: an
// initialized vault (admin present) means the spec 045 loopback bypass does NOT
// fire. Mirrors TestObservabilityMetrics_401Unguarded.
func TestSettings_401Unguarded(t *testing.T) {
	srv, _, _ := settingsServer(t) // admin created ⇒ initialized ⇒ guard enforced
	resp := bearerGet(t, srv.URL+"/api/settings", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/settings without bearer: want 401, got %d", resp.StatusCode)
	}
}

// TestSettings_ReadOnly — US3 / FR-006. The endpoint mutates nothing: two reads
// return identical DTOs and document/chunk counts are unchanged.
func TestSettings_ReadOnly(t *testing.T) {
	srv, tok, eng := settingsServer(t)
	before, _ := eng.Status("default")

	first := getSettings(t, srv, tok)
	second := getSettings(t, srv, tok)
	if first != second {
		t.Errorf("read-only / deterministic violated: two reads differ\n first=%+v\nsecond=%+v", first, second)
	}
	after, _ := eng.Status("default")
	if before.Documents != after.Documents || before.Chunks != after.Chunks {
		t.Errorf("read mutated state: docs %d→%d, chunks %d→%d",
			before.Documents, after.Documents, before.Chunks, after.Chunks)
	}
}
