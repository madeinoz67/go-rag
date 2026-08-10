package ui

// memory_graph_test.go (spec 060 US3 / T022) — the disabled-bridge degraded path
// + the backfill-action conflict path. The happy path (bridge present, engrams
// returned) is covered by the bridge package's own Browse/ReadEngram tests via the
// FakeClient; here the engine's bridge is nil (the default), exercising the
// graceful-degrade contract every US3 route must hold.

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMemoryGraph_DisabledBridgeIsDegraded(t *testing.T) {
	eng := newTestEngine(t) // bridge nil — BridgeEnabled is false by default
	srv := newUITest(t, eng)

	t.Run("status", func(t *testing.T) {
		resp := bearerGet(t, srv.URL+"/api/memory-graph/status", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var got map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["enabled"] != false || got["degraded"] != true {
			t.Errorf("status payload = %v, want enabled=false degraded=true", got)
		}
	})

	t.Run("browse", func(t *testing.T) {
		resp := bearerGet(t, srv.URL+"/api/memory-graph/browse", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("browse: got %d, want 200", resp.StatusCode)
		}
		var got memoryGraphBrowseResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !got.Degraded {
			t.Errorf("browse Degraded = false, want true (bridge disabled)")
		}
	})

	t.Run("engram 404", func(t *testing.T) {
		resp := bearerGet(t, srv.URL+"/api/memory-graph/engrams/anything", "")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("engram: got %d, want 404 (bridge disabled)", resp.StatusCode)
		}
	})

	t.Run("backfill action 409", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/memory-graph/backfill/pause", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("backfill pause: got %d, want 409 (bridge disabled)", resp.StatusCode)
		}
	})

	t.Run("unknown backfill action 404", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/memory-graph/backfill/bogus", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("backfill bogus: got %d, want 404", resp.StatusCode)
		}
	})
}

// TestMemoryGraph_RoutesRegistered confirms the routes exist (no longer 404 at the
// mux level) — a smoke that the Handler() wired them. The disabled-bridge payload
// checks above cover behavior; this guards against a registration regression.
func TestMemoryGraph_RoutesRegistered(t *testing.T) {
	srv := newUITest(t, newTestEngine(t))
	for _, path := range []string{
		"/api/memory-graph/status",
		"/api/memory-graph/browse",
		"/api/memory-graph/engrams/x",
	} {
		resp := bearerGet(t, srv.URL+path, "")
		// 200 (degraded) is the expected non-404 outcome for these GETs.
		if resp.StatusCode == http.StatusNotFound && path != "/api/memory-graph/engrams/x" {
			t.Errorf("%s: got 404 (route not registered?)", path)
		}
		resp.Body.Close()
	}
}
