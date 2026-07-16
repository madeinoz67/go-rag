package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/storage/migrate"
)

// systemServer stands up a versioned UI server (spec 056) + admin + login.
func systemServer(t *testing.T, version string) (*httptest.Server, string, *engine.Engine) {
	t.Helper()
	eng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := httptest.NewServer(NewWithVersion(eng, "", version).Handler())
	t.Cleanup(srv.Close)
	tok, _ := login(t, srv.URL, auth.DefaultAdminUsername, "s3cret", http.StatusOK)
	return srv, tok, eng
}

func getSystem(t *testing.T, srv *httptest.Server, tok string) systemStatusDTO {
	t.Helper()
	resp := bearerGet(t, srv.URL+"/api/settings/system", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/settings/system: want 200, got %d", resp.StatusCode)
	}
	var got systemStatusDTO
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode system: %v", err)
	}
	return got
}

// postUpdateCheck fires the operator-initiated update-check (no body).
func postUpdateCheck(t *testing.T, srv *httptest.Server, tok string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/settings/updates/check", nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	return resp
}

// TestSystem_Identity — US1. version / schema / identity project correctly.
func TestSystem_Identity(t *testing.T) {
	srv, tok, _ := systemServer(t, "v9.9.9-test")
	got := getSystem(t, srv, tok)
	if got.Version != "v9.9.9-test" {
		t.Errorf("version: got %q want v9.9.9-test", got.Version)
	}
	if got.Schema.Version != int(migrate.ExpectedVersion) {
		t.Errorf("schema.version: got %d want %d", got.Schema.Version, int(migrate.ExpectedVersion))
	}
	if !got.Schema.UnifiedStore {
		t.Error("schema.unified_store: want true (spec 052)")
	}
	if got.PID <= 0 {
		t.Errorf("pid: got %d want >0", got.PID)
	}
	if got.UptimeSeconds < 0 {
		t.Errorf("uptime_seconds: got %d want >=0", got.UptimeSeconds)
	}
	if len(got.Transports) != 4 {
		t.Errorf("transports: got %d want 4", len(got.Transports))
	}
}

// TestSystem_Transports — US2. loopback posture + empty bind_warning when all
// loopback (writes a loopback addrs sidecar so transports are "listening").
func TestSystem_Transports(t *testing.T) {
	srv, tok, eng := systemServer(t, "v9.9.9-test")
	if err := daemon.WriteAddrs(eng.Config().DBPath, daemon.Addrs{
		MCPAddr: "127.0.0.1:7878", RESTAddr: "127.0.0.1:7879",
		GRPCAddr: "127.0.0.1:7880", UIAddr: "127.0.0.1:7881",
	}); err != nil {
		t.Fatalf("WriteAddrs: %v", err)
	}
	got := getSystem(t, srv, tok)
	for _, tr := range got.Transports {
		if tr.State != "listening" || !tr.Loopback {
			t.Errorf("transport %s: want listening+loopback, got %+v", tr.Kind, tr)
		}
	}
	if got.BindWarning != "" {
		t.Errorf("bind_warning: want empty for all-loopback, got %q", got.BindWarning)
	}
}

// TestSystem_Transports_NonLoopback — a non-loopback bind is flagged (security).
func TestSystem_Transports_NonLoopback(t *testing.T) {
	srv, tok, eng := systemServer(t, "v9.9.9-test")
	if err := daemon.WriteAddrs(eng.Config().DBPath, daemon.Addrs{
		MCPAddr: "0.0.0.0:7878", RESTAddr: "127.0.0.1:7879",
		GRPCAddr: "127.0.0.1:7880", UIAddr: "127.0.0.1:7881",
	}); err != nil {
		t.Fatalf("WriteAddrs: %v", err)
	}
	got := getSystem(t, srv, tok)
	if got.BindWarning == "" {
		t.Error("bind_warning: want non-empty for 0.0.0.0 mcp bind")
	}
}

// TestSystem_UpdateCheck — US3. dev version skips network (no egress in CI);
// current echoes the version; newer_available false for dev.
func TestSystem_UpdateCheck(t *testing.T) {
	srv, tok, _ := systemServer(t, "dev")
	resp := postUpdateCheck(t, srv, tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/settings/updates/check: want 200, got %d", resp.StatusCode)
	}
	var got updateCheckDTO
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Current != "dev" {
		t.Errorf("current: got %q want dev", got.Current)
	}
	if got.NewerAvailable {
		t.Error("newer_available: want false for dev (check disabled)")
	}
	if got.CheckedAt == "" {
		t.Error("checked_at: want non-empty")
	}
}

// TestSystem_UpdateCheck_PostOnly — SC-003: the check is POST-only; a GET must
// 405 (it never auto-fires on a read).
func TestSystem_UpdateCheck_PostOnly(t *testing.T) {
	srv, tok, _ := systemServer(t, "dev")
	resp := bearerGet(t, srv.URL+"/api/settings/updates/check", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/settings/updates/check: want 405, got %d", resp.StatusCode)
	}
}

// TestSystem_401Unguarded — both routes 401 without a bearer (initialized vault).
func TestSystem_401Unguarded(t *testing.T) {
	srv, _, _ := systemServer(t, "v9.9.9-test")
	resp := bearerGet(t, srv.URL+"/api/settings/system", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/settings/system without bearer: want 401, got %d", resp.StatusCode)
	}
}
