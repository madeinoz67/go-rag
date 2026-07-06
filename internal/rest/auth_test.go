package rest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
)

// newAuthServer builds a REST server with a bootstrapped admin, returning the
// test server and the admin password. Reuses the newEngineWithCorpus harness so
// the engine has a real backing DB (and thus eng.DB() is non-nil → store wired).
func newAuthServer(t *testing.T, adminPassword string) (*httptest.Server, *Server) {
	t.Helper()
	eng := newEngineWithCorpus(t, "auth surface needs a backing corpus to open a store")
	srv := New(eng, "")
	if srv.store == nil {
		t.Fatalf("auth store not wired (eng.DB() == nil)")
	}
	if _, err := auth.CreateAdmin(srv.store, auth.DefaultAdminUsername, adminPassword); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

// assertNoSetCookie fails if the response carries any Set-Cookie header — the
// hard contract invariant of the /api/auth/* surface (CSRF-free, Bearer-only).
func assertNoSetCookie(t *testing.T, resp *http.Response) {
	t.Helper()
	if c := resp.Header.Get("Set-Cookie"); c != "" {
		t.Fatalf("Set-Cookie emitted on %s: %q — the auth surface is Bearer-only", resp.Request.URL.Path, c)
	}
	if _, ok := resp.Header["Set-Cookie"]; ok {
		t.Fatalf("Set-Cookie[*] emitted on %s — the auth surface is Bearer-only", resp.Request.URL.Path)
	}
}

func login(t *testing.T, ts *httptest.Server, username, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(loginRequest{Username: username, Password: password})
	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	return resp
}

func TestAuth_Login_RoundTrip_NoSetCookie(t *testing.T) {
	ts, srv := newAuthServer(t, "correct-horse")
	resp := login(t, ts, "", "correct-horse")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	assertNoSetCookie(t, resp)

	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(out.Token, "gorags_") {
		t.Fatalf("token %q missing gorags_ prefix", out.Token)
	}
	if out.ExpiresAt == "" {
		t.Fatal("expires_at empty")
	}

	// The minted token authorizes an admin route (GET /api/auth/session).
	req, _ := http.NewRequest("GET", ts.URL+"/api/auth/session", nil)
	req.Header.Set("Authorization", "Bearer "+out.Token)
	sr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/auth/session: %v", err)
	}
	sr.Body.Close()
	if sr.StatusCode != http.StatusOK {
		t.Fatalf("session list status = %d, want 200", sr.StatusCode)
	}
	assertNoSetCookie(t, sr)
	_ = srv
}

func TestAuth_Login_BadPassword_401_NoSetCookie(t *testing.T) {
	ts, _ := newAuthServer(t, "correct-horse")
	resp := login(t, ts, "", "wrong-password")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad-password status = %d, want 401", resp.StatusCode)
	}
	assertNoSetCookie(t, resp)
}

func TestAuth_Login_UnknownUser_401_SameAsBadPassword(t *testing.T) {
	// No username enumeration: an unknown user must look identical to a wrong
	// password (both 401, same body shape).
	ts, _ := newAuthServer(t, "correct-horse")
	resp := login(t, ts, "no-such-user", "whatever")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown-user status = %d, want 401", resp.StatusCode)
	}
}

func TestAuth_Logout_InvalidatesSession(t *testing.T) {
	ts, _ := newAuthServer(t, "correct-horse")
	resp := login(t, ts, "", "correct-horse")
	var out loginResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	// Logout with the token.
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+out.Token)
	lo, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	lo.Body.Close()
	if lo.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", lo.StatusCode)
	}
	assertNoSetCookie(t, lo)

	// The revoked token no longer authorizes the admin route.
	req2, _ := http.NewRequest("GET", ts.URL+"/api/auth/session", nil)
	req2.Header.Set("Authorization", "Bearer "+out.Token)
	sr, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("post-logout session list: %v", err)
	}
	sr.Body.Close()
	if sr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-logout status = %d, want 401", sr.StatusCode)
	}
}

func TestAuth_SessionList_AdminOnly(t *testing.T) {
	ts, srv := newAuthServer(t, "correct-horse")
	// A read-mode API key is valid but NOT admin → 403 on session list.
	display, _, err := auth.CreateAPIKey(srv.store, "reader", auth.ModeRead, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/auth/session", nil)
	req.Header.Set("Authorization", "Bearer "+display)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET session (read key): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-key session list status = %d, want 403", resp.StatusCode)
	}

	// An admin API key reaches it.
	adisp, _, err := auth.CreateAPIKey(srv.store, "root", auth.ModeAdmin, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey admin: %v", err)
	}
	req2, _ := http.NewRequest("GET", ts.URL+"/api/auth/session", nil)
	req2.Header.Set("Authorization", "Bearer "+adisp)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET session (admin key): %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("admin-key session list status = %d, want 200", resp2.StatusCode)
	}
	// The list must never leak a raw token.
	raw, _ := io.ReadAll(resp2.Body)
	if strings.Contains(string(raw), "gorags_") {
		t.Fatalf("session list leaked a raw token: %s", raw)
	}
}

func TestAuth_NoBearer_OnSessionRoutes_401(t *testing.T) {
	ts, _ := newAuthServer(t, "correct-horse")
	resp, err := http.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatalf("GET session (no bearer): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-bearer status = %d, want 401", resp.StatusCode)
	}
}
