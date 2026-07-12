package ui

// documents_invariants_test.go (package ui) pins spec 047 US4 invariants:
// the Documents view is strictly read-only (T022), its document projection is
// byte-identical to the REST transport over the same engine (T023), and the
// edge states (empty corpus / un-enriched / failed-embed) render without error
// (T024). These run alongside the existing TestNoNodeArtifacts (no Node/Vite/
// Tailwind build artifacts) which the US4 run command includes via TestNoNode.
//
// Engine access is direct-under-prefix (the same hermetic setup the US1/US2/US3
// tests use): the UI reads engine.ListDocuments/ListChunks, so writing documents
// under 0x02 is sufficient — no ingest pipeline needed.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madeinoz67/go-rag/internal/rest"
)

// readBody fetches url with a Bearer token and returns the raw response body,
// failing if the status is not 200. Used by the parity test to compare the
// exact wire bytes between the UI and REST transports.
func readBody(t *testing.T, url, token string) []byte {
	t.Helper()
	resp := bearerGet(t, url, token)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", url, resp.StatusCode, b)
	}
	return b
}

// TestDocuments_RegisteredRoutes — T022 (spec 047) updated for spec 050: the
// Documents surface is read-only EXCEPT for the three spec-050 write routes
// (POST /api/documents, DELETE /api/documents/{id}, POST /api/documents/{id}/
// reingest). The Go 1.22+ pattern ServeMux returns 405 when a path matches a
// registered pattern but the method does not — so a 405 proves no handler is
// wired for that method+path, and a non-405 proves one is. This pins the exact
// registered write surface: PUT/PATCH are 405 everywhere (never registered);
// POST is registered only on the collection + reingest; DELETE only on {id}.
// GET must never be 405 on these paths. The no-Node half of the invariant is
// covered by TestNoNodeArtifacts. [spec 050 FR-005, N6]
func TestDocuments_RegisteredRoutes(t *testing.T) {
	eng := newTestEngine(t)
	putUIDoc(t, eng, "d1", "embedded", nil, 1)
	srvURL, tok := authedDocServer(t, eng)

	getPaths := []string{
		"/api/documents",
		"/api/documents/search",               // literal, beats {id}
		"/api/documents/d1",                   // {id}
		"/api/documents/d1/chunks",            // {id}/chunks
		"/api/documents/d1/chunks/c1/context", // {id}/chunks/{chunkID}/context
	}

	// GET is registered on every documents path → never 405.
	for _, path := range getPaths {
		resp := bearerGet(t, srvURL+path, tok)
		resp.Body.Close()
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("GET %s: got 405, want GET registered", path)
		}
	}

	// (method, path, want405): the exact spec-050 write surface. want405=false
	// means a handler IS registered there (the write route); the response is
	// 400/404/200 rather than 405. PUT and PATCH are 405 everywhere (never
	// registered). POST is wired only on the collection + reingest. DELETE
	// only on {id} (which also matches /api/documents/search as id="search").
	type probe struct {
		method  string
		path    string
		want405 bool
	}
	probes := []probe{
		// PUT / PATCH: 405 everywhere.
		{http.MethodPut, "/api/documents/d1", true},
		{http.MethodPatch, "/api/documents/d1", true},
		// POST collection (add) → registered (400 on empty body).
		{http.MethodPost, "/api/documents", false},
		// POST on a GET-only documents path → 405.
		{http.MethodPost, "/api/documents/d1/chunks", true},
		// POST reingest → registered (404 on a non-existent source).
		{http.MethodPost, "/api/documents/d1/reingest", false},
		// DELETE collection → 405 (POST registered, not DELETE).
		{http.MethodDelete, "/api/documents", true},
		// DELETE {id} → registered (204 on d1, 404 on unknown).
		{http.MethodDelete, "/api/documents/d1", false},
		// DELETE on a GET-only deeper path → 405.
		{http.MethodDelete, "/api/documents/d1/chunks", true},
	}
	for _, p := range probes {
		body := bytes.NewReader([]byte("{}"))
		if p.method == http.MethodDelete {
			body = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(p.method, srvURL+p.path, body)
		if err != nil {
			t.Fatalf("new %s %s: %v", p.method, p.path, err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if p.method != http.MethodDelete {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", p.method, p.path, err)
		}
		resp.Body.Close()
		if p.want405 && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got %d, want 405 (not a registered write route)", p.method, p.path, resp.StatusCode)
		}
		if !p.want405 && resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got 405, want a registered write route (spec 050)", p.method, p.path)
		}
	}
}

// TestDocuments_CrossTransportParity — T023: against one engine, the UI document
// projection from GET /api/documents is byte-identical to the REST projection
// from GET /v1/documents. Both transports read the same engine.ListDocuments
// and project through DTOs that share field order + json tags
// (ui.documentDTO mirrors rest.documentMetaDTO by construction; both wrappers
// share the documents/next_page_token tags). Both writeJSON helpers are
// identical (json.NewEncoder.Encode), so the raw bodies must match exactly.
// This pins the invariant: a future field added to one transport but not the
// other is caught here. [FR-002, FR-003]
func TestDocuments_CrossTransportParity(t *testing.T) {
	eng := newTestEngine(t)
	// Enriched + un-enriched rows exercise the full field set, including the
	// omitempty enrichment fields (tags/summary/enrichment_*).
	putUIDoc(t, eng, "rich", "embedded", []string{"security", "go"}, 1)
	putUIDoc(t, eng, "plain", "pending", nil, 2)
	srvURL, tok := authedDocServer(t, eng) // admin created → bearer valid on BOTH transports

	// REST server over the SAME engine. rest.New builds its auth store from
	// eng.DB(), so the session minted by the UI /login validates on REST too
	// (single shared credential store — spec 045 US2).
	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	t.Cleanup(restSrv.Close)

	const q = "/api/documents?page_size=10"
	uiBody := readBody(t, srvURL+q, tok)
	restBody := readBody(t, restSrv.URL+"/v1/documents?page_size=10", tok)

	if !bytes.Equal(uiBody, restBody) {
		// Decode for a field-for-field diagnostic on failure.
		var uiList, restList documentsListResponse
		json.Unmarshal(uiBody, &uiList)
		json.Unmarshal(restBody, &restList)
		t.Fatalf("UI /api/documents != REST /v1/documents projection.\n"+
			"UI  (%d rows) = %s\nREST(%d rows) = %s",
			len(uiList.Documents), uiBody, len(restList.Documents), restBody)
	}
}

// TestDocuments_EdgeStates — T024: the read-only view renders the edge states
// (empty corpus, un-enriched, failed-embed) without error. Empty corpus is
// covered by TestDocuments_EmptyCorpus and un-enriched detail by
// TestDocuments_DetailUnenriched; this test pins the remaining gap — a document
// in the failed-embed state (Status="error", the engine's failure status per
// ListDocumentsRequest.Status) renders in the list as a normal row with empty
// enrichment (not an error), and re-asserts un-enriched rows render empty in
// the LIST projection (the detail test only covers the single-doc shape).
// [FR-012]
func TestDocuments_EdgeStates(t *testing.T) {
	eng := newTestEngine(t)
	putUIDoc(t, eng, "boom", "error", nil, 1)    // failed-embed
	putUIDoc(t, eng, "plain", "pending", nil, 2) // un-enriched
	srvURL, tok := authedDocServer(t, eng)

	resp := bearerGet(t, srvURL+"/api/documents?page_size=10", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list edge states: got %d want 200", resp.StatusCode)
	}
	var list documentsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Documents) != 2 {
		t.Fatalf("len=%d want 2 (failed-embed + un-enriched)", len(list.Documents))
	}
	// Both rows must render with empty enrichment fields (not error, not null).
	for _, d := range list.Documents {
		if d.Summary != "" {
			t.Errorf("%s: summary=%q want empty", d.ID, d.Summary)
		}
		if len(d.Tags) != 0 {
			t.Errorf("%s: tags=%v want empty", d.ID, d.Tags)
		}
		if d.EnrichmentStatus != "" {
			t.Errorf("%s: enrichment_status=%q want empty", d.ID, d.EnrichmentStatus)
		}
	}
}
