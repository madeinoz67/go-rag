package auth

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestValidateToken_DispatchesAPIKey(t *testing.T) {
	s := newTestStore(t)
	display, _, err := CreateAPIKey(s, "ci", ModeWrite, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	p, err := s.ValidateToken(display)
	if err != nil {
		t.Fatalf("ValidateToken(apikey): %v", err)
	}
	if p.Source != SourceAPIKey || p.Mode != ModeWrite {
		t.Fatalf("principal = %+v", p)
	}
	if !strings.HasPrefix(p.Subject, "gorag_") {
		t.Fatalf("subject %q", p.Subject)
	}
}

func TestValidateToken_DispatchesSession(t *testing.T) {
	s := newTestStore(t)
	tok, _, err := MintSession(s, "admin", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	p, err := s.ValidateToken(tok)
	if err != nil {
		t.Fatalf("ValidateToken(session): %v", err)
	}
	if p.Source != SourceSession || p.Mode != ModeAdmin || p.Subject != "admin" {
		t.Fatalf("principal = %+v", p)
	}
}

// TestValidateToken_SessionBeforeAPIKeyPrefix is the regression guard for the
// gorags_/gorag_ prefix-collision: a session token MUST route to the session
// validator, never be misparsed as an API key. If the dispatcher tested
// apikeyPrefix first, this session token would fail API-key validation.
func TestValidateToken_SessionBeforeAPIKeyPrefix(t *testing.T) {
	s := newTestStore(t)
	tok, _, err := MintSession(s, "admin", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	// "gorags_..." starts with "gorag_" — confirm dispatch still lands on session.
	p, err := s.ValidateToken(tok)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if p.Source != SourceSession {
		t.Fatalf("misrouted to %q", p.Source)
	}
}

func TestValidateToken_RejectsFailures(t *testing.T) {
	s := newTestStore(t)
	// Create then disable an API key; ensure it no longer validates.
	display, _, err := CreateAPIKey(s, "ci", ModeRead, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	id := strings.SplitN(display, ".", 2)[0] // gorag_<id8>
	if err := RevokeAPIKey(s, id); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}

	cases := map[string]string{
		"empty":           "",
		"garbage":         "not-a-token",
		"unknown prefix":  "deadbeef_xyz",
		"disabled apikey": display,
	}
	for name, tok := range cases {
		_, err := s.ValidateToken(tok)
		switch name {
		case "empty":
			if err != ErrNoCredential {
				t.Errorf("%s: want ErrNoCredential, got %v", name, err)
			}
		default:
			if err != ErrUnknownCredential {
				t.Errorf("%s: want ErrUnknownCredential, got %v", name, err)
			}
		}
	}
}

func TestValidateToken_LengthCap(t *testing.T) {
	s := newTestStore(t)
	huge := "gorag_" + strings.Repeat("A", maxBearerLen)
	if _, err := s.ValidateToken(huge); err != ErrUnknownCredential {
		t.Fatalf("over-length bearer: want ErrUnknownCredential, got %v", err)
	}
}

func TestValidateToken_ExpiredSessionRejected(t *testing.T) {
	s := newTestStore(t)
	tok, _, err := MintSession(s, "admin", "127.0.0.1", -1*time.Hour)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	if _, err := s.ValidateToken(tok); err != ErrUnknownCredential {
		t.Fatalf("expired session: want ErrUnknownCredential, got %v", err)
	}
}

func TestValidate_ReadsBearerHeader(t *testing.T) {
	s := newTestStore(t)
	display, _, err := CreateAPIKey(s, "ci", ModeAdmin, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	r := mustNewRequest(t, "GET", "/api/status")
	r.Header.Set("Authorization", "Bearer "+display)
	p, err := s.Validate(r)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Source != SourceAPIKey || p.Mode != ModeAdmin {
		t.Fatalf("principal = %+v", p)
	}

	// No header → ErrNoCredential.
	r2 := mustNewRequest(t, "GET", "/api/status")
	if _, err := s.Validate(r2); err != ErrNoCredential {
		t.Fatalf("no header: want ErrNoCredential, got %v", err)
	}

	// Wrong scheme → ErrNoCredential.
	r3 := mustNewRequest(t, "GET", "/api/status")
	r3.Header.Set("Authorization", "Basic "+display)
	if _, err := s.Validate(r3); err != ErrNoCredential {
		t.Fatalf("wrong scheme: want ErrNoCredential, got %v", err)
	}
}

func mustNewRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return r
}
