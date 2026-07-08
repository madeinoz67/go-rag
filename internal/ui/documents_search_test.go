package ui

import (
	"net/http"
	"testing"
)

// documents_search_test.go (package ui) proves spec 047 US3: the content-search
// handler validates its input (400 on empty/missing q + bad limit) and is
// bearer-guarded (401). The handler validates q/limit BEFORE calling engine.Query,
// so these are testable without a live embedder/index; the matching behavior is
// engine.Query's job (tested in internal/engine), and the handler projects hits
// to distinct parent documents. [FR-007, R2]

func TestDocuments_SearchContract(t *testing.T) {
	eng := newTestEngine(t)
	srvURL, tok := authedDocServer(t, eng)

	// 400 on empty q (?q=).
	bad := bearerGet(t, srvURL+"/api/documents/search?q=", tok)
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("empty q: got %d want 400", bad.StatusCode)
	}

	// 400 on missing q (no param at all).
	none := bearerGet(t, srvURL+"/api/documents/search", tok)
	none.Body.Close()
	if none.StatusCode != http.StatusBadRequest {
		t.Errorf("missing q: got %d want 400", none.StatusCode)
	}

	// 400 on out-of-range limit (0 and 999).
	for _, q := range []string{
		"/api/documents/search?q=x&limit=0",
		"/api/documents/search?q=x&limit=999",
	} {
		resp := bearerGet(t, srvURL+q, tok)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("bad limit %s: got %d want 400", q, resp.StatusCode)
		}
	}

	// 400 on non-integer limit.
	nan := bearerGet(t, srvURL+"/api/documents/search?q=x&limit=abc", tok)
	nan.Body.Close()
	if nan.StatusCode != http.StatusBadRequest {
		t.Errorf("non-integer limit: got %d want 400", nan.StatusCode)
	}

	// 401 without a bearer (guard).
	noTok := bearerGet(t, srvURL+"/api/documents/search?q=x", "")
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer: got %d want 401", noTok.StatusCode)
	}

	// No Set-Cookie on a search response (Bearer-only, CSRF-free).
	resp := bearerGet(t, srvURL+"/api/documents/search?q=x", tok)
	resp.Body.Close()
	if c := resp.Header.Get("Set-Cookie"); c != "" {
		t.Errorf("Set-Cookie on search: got %q want none", c)
	}
}
