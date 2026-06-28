package cli

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRunHealth_OK proves the probe returns nil for an HTTP 200 daemon. The
// httptest server answers 200 on every path, so runHealth's GET to
// /mcp/health (built by daemon.HealthURL) succeeds. This is the path the
// Docker HEALTHCHECK depends on (spec 033, FR-006).
func TestRunHealth_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	if err := runHealth(srv.Listener.Addr().String()); err != nil {
		t.Fatalf("200: expected nil, got %v", err)
	}
}

// TestRunHealth_Non200: a non-200 daemon response MUST surface as an error
// (the HEALTHCHECK exits non-zero so Docker/compose marks the container unhealthy).
func TestRunHealth_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := runHealth(srv.Listener.Addr().String()); err == nil {
		t.Fatal("500: expected error, got nil")
	}
}

// TestRunHealth_Refused: a daemon that is not listening (connect refused) MUST
// surface as an error — the cold-start / crashed-daemon case the healthcheck
// exists to detect. Uses a port that was bound then released so it is closed.
func TestRunHealth_Refused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	if err := runHealth(addr); err == nil {
		t.Fatal("refused: expected error, got nil")
	}
}
