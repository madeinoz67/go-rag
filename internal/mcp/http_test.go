package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newAuthMCPServer stands up an MCP HTTP server backed by a real Pebble store
// (daemon mode), so spec 045 auth.Validate runs on /mcp. Returns the test
// server and the store (the caller mints credentials into it).
func newAuthMCPServer(t *testing.T) (*httptest.Server, *auth.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := auth.NewStore(db)
	ts := httptest.NewServer(NewWithDB(dbPath, db, config.Default()).HTTPHandler(""))
	t.Cleanup(ts.Close)
	return ts, store
}

func TestHTTPHealth(t *testing.T) {
	ts := httptest.NewServer(New(t.TempDir()).HTTPHandler(""))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/mcp/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: want 200, got %d", resp.StatusCode)
	}
}

func TestHTTPToolsList(t *testing.T) {
	ts := httptest.NewServer(New(t.TempDir()).HTTPHandler(""))
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list: want 200, got %d", resp.StatusCode)
	}
	var env struct {
		Result struct {
			Tools []any `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if len(env.Result.Tools) != 23 { // spec 039 added go_rag_list_documents
		t.Fatalf("want 23 tools, got %d", len(env.Result.Tools))
	}
}

func TestHTTPBearerAuth(t *testing.T) {
	ts, store := newAuthMCPServer(t)
	// Minting a credential disables the loopback bypass, so /mcp now requires a
	// valid bearer (spec 045 US2/US5).
	display, _, err := auth.CreateAPIKey(store, "test", auth.ModeAdmin, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	// No bearer -> 401.
	resp, err := http.Post(ts.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without bearer: want 401, got %d", resp.StatusCode)
	}

	// Wrong bearer -> 401.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer gorag_nope.nope")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong bearer: want 401, got %d", resp2.StatusCode)
	}

	// Valid key -> 200.
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+display)
	resp3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("valid key: want 200, got %d", resp3.StatusCode)
	}
	body, _ := io.ReadAll(resp3.Body)
	if !strings.Contains(string(body), "tools") {
		t.Fatalf("expected tools in response: %s", body)
	}
}

func TestHTTPNotificationAccepted(t *testing.T) {
	ts := httptest.NewServer(New(t.TempDir()).HTTPHandler(""))
	defer ts.Close()
	// notifications/initialized has no id -> handler returns nil -> 202.
	resp, err := http.Post(ts.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification: want 202, got %d", resp.StatusCode)
	}
}
