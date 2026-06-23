package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	if len(env.Result.Tools) != 17 {
		t.Fatalf("want 17 tools, got %d", len(env.Result.Tools))
	}
}

func TestHTTPBearerAuth(t *testing.T) {
	ts := httptest.NewServer(New(t.TempDir()).HTTPHandler("secret"))
	defer ts.Close()

	// No token -> 401.
	resp, err := http.Post(ts.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without token: want 401, got %d", resp.StatusCode)
	}

	// Wrong token -> 401.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer wrong")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: want 401, got %d", resp2.StatusCode)
	}

	// Correct token -> 200.
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("correct token: want 200, got %d", resp3.StatusCode)
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
