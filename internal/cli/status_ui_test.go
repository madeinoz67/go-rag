package cli

import (
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/daemon"
)

// status_ui_test.go pins the spec 046 addition: `go-rag status` (daemon running)
// shows the UI transport address alongside REST/gRPC, and omits every optional
// transport that is disabled. The daemon-running print path needs a live daemon
// to exercise end-to-end, so the address formatting is covered via the
// formatBoundAddrs helper (the same function newStatusCmd calls).

func TestFormatBoundAddrs_IncludesAllEnabledTransports(t *testing.T) {
	got := formatBoundAddrs(daemon.Addrs{
		MCPAddr:  "127.0.0.1:7878",
		RESTAddr: "127.0.0.1:7879",
		GRPCAddr: "127.0.0.1:7880",
		UIAddr:   "127.0.0.1:7881",
	})
	for _, want := range []string{
		"REST 127.0.0.1:7879",
		"gRPC 127.0.0.1:7880",
		"UI 127.0.0.1:7881", // spec 046 — the line that used to be missing
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatBoundAddrs missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatBoundAddrs_OmitsDisabledTransports(t *testing.T) {
	// Only MCP set (always-on); every optional transport disabled.
	got := formatBoundAddrs(daemon.Addrs{MCPAddr: "127.0.0.1:7878"})
	for _, absent := range []string{"REST", "gRPC", "UI"} {
		if strings.Contains(got, absent) {
			t.Errorf("disabled %s should be absent; got:\n%s", absent, got)
		}
	}
}

func TestFormatBoundAddrs_UIOnly(t *testing.T) {
	// REST/gRPC disabled, UI enabled (the spec 046 console-only config).
	got := formatBoundAddrs(daemon.Addrs{MCPAddr: "127.0.0.1:7878", UIAddr: "127.0.0.1:7881"})
	if !strings.Contains(got, "UI 127.0.0.1:7881") {
		t.Errorf("UI line missing; got:\n%s", got)
	}
	if strings.Contains(got, "REST") || strings.Contains(got, "gRPC") {
		t.Errorf("disabled REST/gRPC should be absent; got:\n%s", got)
	}
}
