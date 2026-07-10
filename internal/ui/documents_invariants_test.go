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

// TestDocuments_ReadOnlyRoutes — T022: every /api/documents* route is registered
// as GET only. The Go 1.22+ pattern ServeMux returns 405 Method Not Allowed when
// a request's path matches a registered pattern but its method does not — so a
// 405 on POST/PUT/DELETE/PATCH for each documents route proves the view is
// strictly read-only (no mutation handler is wired). GET must NOT be 405, which
// proves the GET handler is registered. The no-Node-artifacts half of the
// read-only + no-Node invariant is covered by TestNoNodeArtifacts (run command
// includes it via the TestNoNode prefix). [FR-009, N6]
func TestDocuments_ReadOnlyRoutes(t *testing.T) {
	eng := newTestEngine(t)
	putUIDoc(t, eng, "d1", "embedded", nil, 1)
	srvURL, tok := authedDocServer(t, eng)

	// expectedRoutes is the exhaustive set of /api/documents* routes from
	// Server.Handler — the authoritative registration. Each concrete path
	// matches exactly one registered GET pattern (search is a literal, more
	// specific than {id}, so it wins for that path).
	expectedRoutes := []string{
		"/api/documents",
		"/api/documents/search",               // literal, beats {id}
		"/api/documents/d1",                   // {id}
		"/api/documents/d1/chunks",            // {id}/chunks
		"/api/documents/d1/chunks/c1/context", // {id}/chunks/{chunkID}/context
	}

	writeMethods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, path := range expectedRoutes {
		// GET must be registered: any non-405 status proves the GET handler
		// ran (200/400/404 all count — the resource need not exist for the
		// route to be registered).
		getResp := bearerGet(t, srvURL+path, tok)
		getResp.Body.Close()
		if getResp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("GET %s: got 405, want GET registered (non-405)", path)
		}

		// Every write method must be rejected with 405 — the view is read-only.
		for _, m := range writeMethods {
			req, err := http.NewRequest(m, srvURL+path, nil)
			if err != nil {
				t.Fatalf("new %s %s req: %v", m, path, err)
			}
			req.Header.Set("Authorization", "Bearer "+tok)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", m, path, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: got %d, want 405 (read-only)", m, path, resp.StatusCode)
			}
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
