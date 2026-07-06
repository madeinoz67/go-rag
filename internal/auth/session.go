package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// sessionPrefix is the human-readable prefix on every session token. Note it is
// goragS_ — the 's' distinguishes it from the API-key gorag_ prefix. The
// dispatcher in auth.Validate MUST test gorags_ before gorag_ (longest-first),
// otherwise every session token would be misrouted to the API-key validator.
const sessionPrefix = "gorags_"

// DefaultSessionTTL is the default session lifetime (spec 045 US3).
const DefaultSessionTTL = 12 * time.Hour

// ErrUnknownSession is returned when no session matches the presented token
// (absent, expired, or revoked). Callers collapse it into 401 +
// audit.AuthFailEvent; the reason carries no token material.
var ErrUnknownSession = errors.New("unknown or expired session")

// Session is a short-lived, opaque, store-tracked token minted by admin login
// (spec 045 US3). The opaque token `gorags_<base64url(32)>` is returned to the
// client exactly once at login; only TokenHash (SHA-256(token)[:16]) is
// persisted. Pebble key: PrefixAuthSession || TokenHash.
type Session struct {
	TokenHash []byte    `json:"token_hash"` // sha256(token)[:16] — the Pebble key
	User      string    `json:"user"`       // the admin username
	ExpiresAt time.Time `json:"expires_at"` // now + TTL
	LastSeen  time.Time `json:"last_seen"`  // bumped on each validated request
	CreatedIP string    `json:"created_ip"` // connecting peer (audit trail)
	CreatedAt time.Time `json:"created_at"`
}

// MintSession creates a new session for user, returning the opaque token string
// exactly once. The caller MUST hand it to the client and never persist it.
// ttl == 0 falls back to DefaultSessionTTL; a negative ttl yields an already-
// expired session (used by tests; harmless in production).
func MintSession(s *Store, user, ip string, ttl time.Duration) (token string, _ Session, _ error) {
	if user == "" {
		return "", Session{}, errors.New("session user must not be empty")
	}
	if ttl == 0 {
		ttl = DefaultSessionTTL
	}
	secret, err := randomBytes(32)
	if err != nil {
		return "", Session{}, err
	}
	token = sessionPrefix + base64.RawURLEncoding.EncodeToString(secret)
	h := sha256.Sum256(secret)
	tokenHash := h[:16]

	now := time.Now().UTC()
	sess := Session{
		TokenHash: tokenHash,
		User:      user,
		ExpiresAt: now.Add(ttl),
		LastSeen:  now,
		CreatedIP: ip,
		CreatedAt: now,
	}
	val, err := json.Marshal(sess)
	if err != nil {
		return "", Session{}, err
	}
	if err := s.db.SetWithPrefix(storage.PrefixAuthSession, tokenHash, val); err != nil {
		return "", Session{}, err
	}
	return token, sess, nil
}

// sessionSecretBytes extracts the 32-byte secret from a presented session token
// string (`gorags_<base64url(32)>`).
func sessionSecretBytes(bearer string) ([]byte, error) {
	if len(bearer) < len(sessionPrefix)+1 || bearer[:len(sessionPrefix)] != sessionPrefix {
		return nil, fmt.Errorf("missing %q prefix", sessionPrefix)
	}
	b, err := base64.RawURLEncoding.DecodeString(bearer[len(sessionPrefix):])
	if err != nil {
		return nil, fmt.Errorf("bad base64url token: %w", err)
	}
	return b, nil
}

// ValidateSession resolves a presented bearer to its stored Session. A Get hit
// on SHA-256(token)[:16] IS the match — no token-string comparison. Expired
// sessions are eagerly deleted (collapsing expired into absent so the two are
// not timing-distinguishable, spec 045 red-team finding LOW). The LastSeen
// bump is serialized against RevokeSession/RevokeSessionByHash with a re-Get
// under the lock so a concurrent revoke cannot be clobbered (HIGH — would
// resurrect the revoked session on the next validate).
func ValidateSession(s *Store, bearer string) (Session, error) {
	secret, err := sessionSecretBytes(bearer)
	if err != nil {
		return Session{}, ErrUnknownSession
	}
	tokenHash := hash16(secret)
	val, ok, err := s.db.GetWithPrefix(storage.PrefixAuthSession, tokenHash)
	if err != nil || !ok {
		return Session{}, ErrUnknownSession
	}
	var sess Session
	if err := json.Unmarshal(val, &sess); err != nil {
		return Session{}, ErrUnknownSession
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		// Eager expiry: collapse expired into absent (reaped now, not later).
		_ = s.db.DeleteWithPrefix(storage.PrefixAuthSession, tokenHash)
		return Session{}, ErrUnknownSession
	}

	// LastSeen bump — serialized against revoke; re-Get under the lock so a
	// RevokeSession/RevokeSessionByHash that landed between the read above and
	// here is observed (bail without writing back, which would resurrect it).
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	val2, ok2, err2 := s.db.GetWithPrefix(storage.PrefixAuthSession, tokenHash)
	if err2 != nil || !ok2 {
		return Session{}, ErrUnknownSession
	}
	var fresh Session
	if json.Unmarshal(val2, &fresh) != nil || time.Now().UTC().After(fresh.ExpiresAt) {
		_ = s.db.DeleteWithPrefix(storage.PrefixAuthSession, tokenHash)
		return Session{}, ErrUnknownSession
	}
	fresh.LastSeen = now
	if b, mErr := json.Marshal(fresh); mErr == nil {
		_ = s.db.SetWithPrefix(storage.PrefixAuthSession, tokenHash, b)
	}
	return fresh, nil
}

// RevokeSessionByHash deletes the session whose TokenHash equals hash (the
// admin session-revoke path takes a hash, not the raw token). A missing session
// is a no-op success (idempotent). Serialized against ValidateSession's LastSeen
// write so the two cannot cross (a validate write-back could otherwise resurrect
// a just-deleted session).
func RevokeSessionByHash(s *Store, hash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.DeleteWithPrefix(storage.PrefixAuthSession, hash)
}

// RevokeSession deletes the session backing a presented bearer (the logout
// path). A missing/already-expired session is a no-op success.
func RevokeSession(s *Store, bearer string) error {
	secret, err := sessionSecretBytes(bearer)
	if err != nil {
		return nil // nothing to revoke
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.DeleteWithPrefix(storage.PrefixAuthSession, hash16(secret))
}

// ListSessions returns every stored session (never the raw token). Order is
// Pebble scan order (by token hash).
func ListSessions(s *Store) ([]Session, error) {
	var out []Session
	err := s.db.PrefixScanByte(storage.PrefixAuthSession, func(_, val []byte) bool {
		var sess Session
		if json.Unmarshal(val, &sess) == nil {
			out = append(out, sess)
		}
		return true
	})
	return out, err
}
