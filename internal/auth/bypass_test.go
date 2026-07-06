package auth

import (
	"testing"
)

func TestBypass_LoopbackEmptyStores_Allowed(t *testing.T) {
	s := newTestStore(t)
	// Fresh store, no credentials — loopback bypass must apply.
	r := mustNewRequest(t, "GET", "/v1/query")
	r.RemoteAddr = "127.0.0.1:1234"
	p, err := s.Validate(r)
	if err != nil {
		t.Fatalf("loopback+empty: want bypass, got err=%v", err)
	}
	if p.Source != SourceBypass || p.Mode != ModeAdmin {
		t.Fatalf("bypass principal = %+v", p)
	}

	// ::1 is also loopback.
	r6 := mustNewRequest(t, "GET", "/v1/query")
	r6.RemoteAddr = "[::1]:1234"
	if _, err := s.Validate(r6); err != nil {
		t.Fatalf("::1 loopback+empty: %v", err)
	}
}

func TestBypass_NonLoopback_FailClosed(t *testing.T) {
	s := newTestStore(t)
	r := mustNewRequest(t, "GET", "/v1/query")
	r.RemoteAddr = "10.0.0.5:1234" // LAN, non-loopback
	if _, err := s.Validate(r); err != ErrNoCredential {
		t.Fatalf("non-loopback: want ErrNoCredential, got %v", err)
	}
}

func TestBypass_LoopbackNonEmptyStores_Disabled(t *testing.T) {
	s := newTestStore(t)
	// Mint one credential — the operator signalled intent to enforce auth.
	if _, _, err := CreateAPIKey(s, "first", ModeRead, nil); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	r := mustNewRequest(t, "GET", "/v1/query")
	r.RemoteAddr = "127.0.0.1:1234"
	if _, err := s.Validate(r); err != ErrNoCredential {
		t.Fatalf("loopback+non-empty: want ErrNoCredential (bypass off), got %v", err)
	}

	// Admin presence also disables bypass.
	s2 := newTestStore(t)
	if _, err := CreateAdmin(s2, DefaultAdminUsername, "pw"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	r2 := mustNewRequest(t, "GET", "/v1/query")
	r2.RemoteAddr = "127.0.0.1:1234"
	if _, err := s2.Validate(r2); err != ErrNoCredential {
		t.Fatalf("loopback+admin-present: want ErrNoCredential, got %v", err)
	}
}

func TestBypass_PresentTokenWinsOverBypass(t *testing.T) {
	s := newTestStore(t)
	display, _, err := CreateAPIKey(s, "ci", ModeWrite, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	// Even on loopback with a present token, the token path is used (not bypass).
	r := mustNewRequest(t, "GET", "/v1/query")
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer "+display)
	p, err := s.Validate(r)
	if err != nil {
		t.Fatalf("token+loopback: %v", err)
	}
	if p.Source != SourceAPIKey {
		t.Fatalf("present token did not win: source=%q", p.Source)
	}
}

func TestValidateTokenOrBypass_TokenPath(t *testing.T) {
	s := newTestStore(t)
	display, _, err := CreateAPIKey(s, "ci", ModeAdmin, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	p, err := s.ValidateTokenOrBypass(display, false)
	if err != nil {
		t.Fatalf("ValidateTokenOrBypass(token): %v", err)
	}
	if p.Mode != ModeAdmin {
		t.Fatalf("mode=%q", p.Mode)
	}
	// Empty token, non-loopback, empty stores → fail-closed.
	if _, err := s.ValidateTokenOrBypass("", false); err != ErrNoCredential {
		t.Fatalf("empty+non-loopback: want ErrNoCredential, got %v", err)
	}
}

// TestValidate_MissingRemoteAddr fails closed when RemoteAddr is unset (a
// misconfigured proxy / test harness) rather than guessing loopback.
func TestValidate_MissingRemoteAddr_FailClosed(t *testing.T) {
	s := newTestStore(t)
	r := mustNewRequest(t, "GET", "/v1/query")
	r.RemoteAddr = ""
	if _, err := s.Validate(r); err != ErrNoCredential {
		t.Fatalf("empty RemoteAddr: want ErrNoCredential, got %v", err)
	}
}
