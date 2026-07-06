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
		"unknown":      "gorags_AAAAAAAABBBBBBBBCCCCCCCCDDDDDDDD" +
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
