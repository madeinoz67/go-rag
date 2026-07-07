package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// Store is the Pebble-backed credential store. It wraps a *storage.DB; the
// API-key, admin-user, and session types (each in its own file) add methods on
// Store.
//
// Every credential is keyed by SHA-256(secret)[:16]; the raw secret is never
// persisted, and a Get hit IS the match (no secret-string comparison).
//
// Store is read-only on the validate hot path (ValidateAPIKey/ValidateSession
// do no write-back), so there is no mutex: a revoke (Pebble Delete) cannot race
// a validate write-back and resurrect a credential. Each transport builds its
// own Store over the shared *storage.DB — that is safe precisely because
// validate never writes.
type Store struct {
	db *storage.DB
}

// NewStore wraps an open *storage.DB for credential storage.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// randomBytes returns n cryptographically random bytes.
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// hash16 returns SHA-256(value)[:16] — the Pebble key suffix for hashed
// credentials (API keys and sessions). 128 bits is far beyond any plausible
// collision bound.
func hash16(value []byte) []byte {
	h := sha256.Sum256(value)
	return h[:16]
}

// id8 returns base64url(SHA-256(value)[:8]) — a short, stable display ID
// derived from the secret (the same hash used for the storage key, truncated).
func id8(value []byte) string {
	h := sha256.Sum256(value)
	return base64.RawURLEncoding.EncodeToString(h[:8])
}
