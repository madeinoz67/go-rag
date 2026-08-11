package muninn

import (
	"context"
	"strings"
	"testing"
)

// TestLoopbackDialer_RefusesNonLoopback is the FR-002 security boundary: the
// bridge MUST NOT connect to a non-loopback MuninnDB, even if config validation
// was bypassed or a loopback hostname was rebound to a public IP. The refusal
// happens before any TCP dial, so these cases need no listener.
func TestLoopbackDialer_RefusesNonLoopback(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		addr string
		want string // substring expected in the refusal error
	}{
		{"public IPv4", "8.8.8.8:8477", "non-loopback"},
		{"public IPv6", "[2606:4700::1]:8477", "non-loopback"},
		{"bare port", ":8477", "bare port"},
		{"malformed", "no-port", "bad endpoint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loopbackDialer(false)(ctx, tc.addr)
			if err == nil {
				t.Fatalf("dialer accepted %q (want refusal)", tc.addr)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("dialer %q: error %q does not contain %q", tc.addr, err.Error(), tc.want)
			}
		})
	}
}

// TestLoopbackDialer_AllowsLoopback confirms a loopback address passes the gate
// (it then fails to connect because nothing listens on :1 — the point is that
// the failure is a dial error, NOT a refusal).
func TestLoopbackDialer_AllowsLoopback(t *testing.T) {
	_, err := loopbackDialer(false)(context.Background(), "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected a dial error on :1 (nothing listens), got nil")
	}
	// Must NOT be a refusal — the loopback gate passed; this is a connection error.
	if strings.Contains(err.Error(), "non-loopback") || strings.Contains(err.Error(), "bare port") {
		t.Fatalf("loopback 127.0.0.1 was refused: %v", err)
	}
}

// TestDial_RejectsEmptyToken confirms the auth precondition: no target vault key,
// no client. The key comes from GORAG_BRIDGE_TOKEN; an empty token is a config
// error the operator must fix before the bridge can egress.
func TestDial_RejectsEmptyToken(t *testing.T) {
	_, err := Dial(context.Background(), "127.0.0.1:8477", "", false)
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("Dial with empty token: want token error, got %v", err)
	}
}

// TestLoopbackDialer_AllowExternalBypassesGate confirms that allowExternal=true
// (Docker/multi-container opt-in) skips the loopback refusal — a non-loopback
// endpoint dials directly instead of being rejected. Uses TEST-NET-1 (RFC 5737,
// guaranteed unreachable) so the dial fails with a connection error, not a refusal.
func TestLoopbackDialer_AllowExternalBypassesGate(t *testing.T) {
	_, err := loopbackDialer(true)(context.Background(), "192.0.2.1:8477")
	if err == nil {
		t.Fatal("expected a dial error (192.0.2.1 is TEST-NET-1, nothing listens)")
	}
	if strings.Contains(err.Error(), "non-loopback") || strings.Contains(err.Error(), "bare port") {
		t.Fatalf("allowExternal=true should bypass the loopback gate, but got refusal: %v", err)
	}
}
