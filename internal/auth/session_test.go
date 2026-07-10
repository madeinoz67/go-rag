package auth

import (
	"strings"
	"testing"
	"time"
)

func TestMintSession_ReturnsOpaqueTokenOnce(t *testing.T) {
	s := newTestStore(t)
	tok, sess, err := MintSession(s, DefaultAdminUsername, "127.0.0.1", DefaultSessionTTL)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	if !strings.HasPrefix(tok, "gorags_") {
		t.Fatalf("token %q missing gorags_ prefix", tok)
	}
	if sess.User != DefaultAdminUsername {
		t.Fatalf("session user = %q", sess.User)
	}
	if len(sess.TokenHash) != 16 {
		t.Fatalf("TokenHash len = %d, want 16", len(sess.TokenHash))
	}
	if !sess.ExpiresAt.After(sess.CreatedAt) {
		t.Fatal("ExpiresAt not after CreatedAt")
	}
}

func TestMintValidate_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	tok, _, err := MintSession(s, "admin", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	sess, err := ValidateSession(s, tok)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if sess.User != "admin" {
		t.Fatalf("user = %q", sess.User)
	}
	// LastSeen bumped past CreatedAt on the validation call.
	if !sess.LastSeen.After(sess.CreatedAt) && !sess.LastSeen.Equal(sess.CreatedAt) {
		t.Fatalf("LastSeen not bumped: %v vs %v", sess.LastSeen, sess.CreatedAt)
	}
}

func TestValidateSession_RejectsGarbageAndUnknown(t *testing.T) {
	s := newTestStore(t)
	cases := map[string]string{
		"empty":        "",
		"wrong prefix": "gorag_abcd",
		"apikey form":  "gorag_something",
		"bad base64":   "gorags_!!!!",
		"unknown": "gorags_AAAAAAAABBBBBBBBCCCCCCCCDDDDDDDD" +
			"EEEEEEEEFFFFFFFFGGGGGGGGHHHHHHHH",
	}
	for name, tok := range cases {
		if _, err := ValidateSession(s, tok); err != ErrUnknownSession && name != "apikey form" && name != "wrong prefix" {
			// "wrong prefix"/"apikey form" surface a parse error, not ErrUnknownSession.
			t.Errorf("%s: want ErrUnknownSession, got %v", name, err)
		}
	}
}

func TestSessionExpiry(t *testing.T) {
	s := newTestStore(t)
	tok, _, err := MintSession(s, "admin", "127.0.0.1", -1*time.Hour) // already expired
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	if _, err := ValidateSession(s, tok); err != ErrUnknownSession {
		t.Fatalf("expired session validated: err=%v", err)
	}
}

func TestRevokeSession_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	tok, sess, err := MintSession(s, "admin", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	// Revoke by hash (the admin path).
	if err := RevokeSessionByHash(s, sess.TokenHash); err != nil {
		t.Fatalf("RevokeSessionByHash: %v", err)
	}
	if _, err := ValidateSession(s, tok); err != ErrUnknownSession {
		t.Fatalf("revoked-by-hash session validated: err=%v", err)
	}
	// Re-revoke is a no-op success.
	if err := RevokeSessionByHash(s, sess.TokenHash); err != nil {
		t.Fatalf("RevokeSessionByHash(idempotent): %v", err)
	}
}

func TestRevokeSession_ByBearer(t *testing.T) {
	s := newTestStore(t)
	tok, _, err := MintSession(s, "admin", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	if err := RevokeSession(s, tok); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := ValidateSession(s, tok); err != ErrUnknownSession {
		t.Fatalf("revoked session validated: err=%v", err)
	}
	// Revoking a malformed bearer is a no-op success.
	if err := RevokeSession(s, "garbage"); err != nil {
		t.Fatalf("RevokeSession(garbage): %v", err)
	}
}

func TestListSessions(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := MintSession(s, "admin", "1.1.1.1", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, _, err := MintSession(s, "admin", "2.2.2.2", time.Hour); err != nil {
		t.Fatal(err)
	}
	list, err := ListSessions(s)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
}

// TestValidateSession_NoResurrectUnderConcurrentRevoke (spec 045 red-team
// HIGH): a ValidateSession LastSeen write must not resurrect a session a
// concurrent RevokeSession just deleted. The fix is a per-Store mutex + re-Get
// under the lock. This stress test hammers both paths; after the revoke
// goroutine is done + we sync, the session MUST be gone (validate returns
// ErrUnknownSession). Run with -race to also catch the underlying data race.
func TestValidateSession_NoResurrectUnderConcurrentRevoke(t *testing.T) {
	s := newTestStore(t)
	tok, _, err := MintSession(s, "admin", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("MintSession: %v", err)
	}
	done := make(chan struct{})
	quit := make(chan struct{})
	// Validator hammer.
	go func() {
		defer close(quit)
		for {
			select {
			case <-done:
				return
			default:
				_, _ = ValidateSession(s, tok) // result varies; ignore
			}
		}
	}()
	// Revoke repeatedly, then we'll confirm it sticks.
	for i := 0; i < 50; i++ {
		_ = RevokeSession(s, tok)
	}
	close(done)
	<-quit // join hammer so its final Validate* cannot race t.Cleanup's db.Close()
	// After revoke settled, the session must NOT validate.
	if _, err := ValidateSession(s, tok); err != ErrUnknownSession {
		t.Fatalf("session resurrected after revoke: err=%v", err)
	}
}

// TestValidateAPIKey_NoResurrectUnderConcurrentRevoke mirrors the session race
// for API keys (same read-modify-write-LastSeen pattern).
func TestValidateAPIKey_NoResurrectUnderConcurrentRevoke(t *testing.T) {
	s := newTestStore(t)
	display, _, err := CreateAPIKey(s, "ci", ModeAdmin, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	id := id8of(display)
	done := make(chan struct{})
	quit := make(chan struct{})
	go func() {
		defer close(quit)
		for {
			select {
			case <-done:
				return
			default:
				_, _ = ValidateAPIKey(s, display)
			}
		}
	}()
	for i := 0; i < 50; i++ {
		_ = RevokeAPIKey(s, id)
	}
	close(done)
	<-quit // join hammer so its final Validate* cannot race t.Cleanup's db.Close()
	// Revoked key must not authenticate.
	if _, err := ValidateAPIKey(s, display); err != ErrUnknownAPIKey {
		t.Fatalf("api key resurrected after revoke: err=%v", err)
	}
}

// id8of extracts "gorag_<id8>" from a full display string.
func id8of(display string) string {
	for i := 0; i < len(display); i++ {
		if display[i] == '.' {
			return display[:i]
		}
	}
	return display
}
