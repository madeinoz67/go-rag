package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
)

func getAPIKeys(t *testing.T, srv *httptest.Server, tok string) []apiKeyView {
	t.Helper()
	resp := bearerGet(t, srv.URL+"/api/settings/auth/api-keys", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET api-keys: want 200, got %d", resp.StatusCode)
	}
	var got []apiKeyView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// createAPIKey returns the decoded response + the HTTP response (for status checks).
func createAPIKey(t *testing.T, srv *httptest.Server, tok, label, mode string) (createAPIKeyResponse, *http.Response) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"label": label, "mode": mode})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/settings/auth/api-keys", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var out createAPIKeyResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out, resp
}

// TestAPIKeys_List — US1. Fresh vault ⇒ empty list; no secret field anywhere.
func TestAPIKeys_List(t *testing.T) {
	srv, tok, _ := settingsServer(t)
	got := getAPIKeys(t, srv, tok)
	if len(got) != 0 {
		t.Fatalf("fresh vault: want 0 keys, got %d", len(got))
	}
}

// TestAPIKeys_Create — US2 + FR-003. Secret returned ONCE; the list response
// contains NO "secret" field; invalid mode + missing label ⇒ 400 (no key created).
func TestAPIKeys_Create(t *testing.T) {
	srv, tok, _ := settingsServer(t)
	out, resp := createAPIKey(t, srv, tok, "ci", "read")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: want 201, got %d", resp.StatusCode)
	}
	if !strings.HasPrefix(out.Secret, "gorag_") || out.Secret == "" {
		t.Errorf("secret: want gorag_…, got %q", out.Secret)
	}
	if out.Label != "ci" || out.Mode != "read" || !out.Enabled {
		t.Errorf("view mismatch: %+v", out)
	}
	// FR-003: the list response must NOT contain the secret (structural check).
	raw := bearerGet(t, srv.URL+"/api/settings/auth/api-keys", tok)
	body, _ := io.ReadAll(raw.Body)
	raw.Body.Close()
	if strings.Contains(string(body), "secret") {
		t.Errorf("FR-003 violation — list contains 'secret': %s", body)
	}
	if len(getAPIKeys(t, srv, tok)) != 1 {
		t.Error("list: want 1 key after create")
	}
	// 400: invalid mode (no key created).
	if _, bad := createAPIKey(t, srv, tok, "x", "bogus"); bad.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid mode: want 400, got %d", bad.StatusCode)
	}
	// 400: missing/blank label.
	if _, bad := createAPIKey(t, srv, tok, "  ", "read"); bad.StatusCode != http.StatusBadRequest {
		t.Errorf("blank label: want 400, got %d", bad.StatusCode)
	}
}

// TestAPIKeys_Revoke — US3. Revoked bearer fails ValidateAPIKey immediately;
// unknown id ⇒ 404; the list shows the key disabled (audit trail).
func TestAPIKeys_Revoke(t *testing.T) {
	srv, tok, eng := settingsServer(t)
	out, _ := createAPIKey(t, srv, tok, "ci", "write")
	store := auth.NewStore(eng.DB())
	if _, err := auth.ValidateAPIKey(store, out.Secret); err != nil {
		t.Fatalf("created key must validate before revoke: %v", err)
	}
	// Revoke.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/settings/auth/api-keys/"+out.ID, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: want 204, got %d", resp.StatusCode)
	}
	// The revoked bearer now fails auth.
	if _, err := auth.ValidateAPIKey(store, out.Secret); err == nil {
		t.Error("revoked key must fail ValidateAPIKey")
	}
	// The list still shows it, disabled.
	list := getAPIKeys(t, srv, tok)
	if len(list) != 1 || list[0].Enabled {
		t.Errorf("list should show 1 disabled key, got %+v", list)
	}
	// Unknown id ⇒ 404.
	req2, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/settings/auth/api-keys/gorag_nope", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: want 404, got %d", resp2.StatusCode)
	}
}

// TestAPIKeys_401Unguarded — every route is admin-gated (no bearer ⇒ 401).
func TestAPIKeys_401Unguarded(t *testing.T) {
	srv, _, _ := settingsServer(t)
	resp := bearerGet(t, srv.URL+"/api/settings/auth/api-keys", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET no bearer: want 401, got %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/settings/auth/api-keys/gorag_x", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("DELETE no bearer: want 401, got %d", resp2.StatusCode)
	}
}
