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

// apikeyPrefix is the human-readable prefix on every API-key credential. The
// validation hot path dispatches on this (spec 045 US2).
const apikeyPrefix = "gorag_"

// ErrUnknownAPIKey is returned when no API key matches the presented secret.
// Callers wrap this into a 401 + audit.AuthFailEvent; the reason carries no
// credential material.
var ErrUnknownAPIKey = errors.New("unknown or disabled api key")

// APIKey is a long-lived, labelled, scoped, hashed credential for programmatic
// clients (spec 045 US1). The raw secret is shown exactly once at create time;
// only StorageHash (SHA-256(secret)[:16]) is persisted.
type APIKey struct {
	ID          string     `json:"id"`           // gorag_<base64url(sha256(secret)[:8])> — display + revoke handle
	Label       string     `json:"label"`        // operator-supplied free text
	Mode        string     `json:"mode"`         // ModeRead | ModeWrite | ModeAdmin
	CreatedAt   time.Time  `json:"created_at"`   // UTC
	ExpiresAt   *time.Time `json:"expires_at"`   // nil = never expires
	Enabled     bool       `json:"enabled"`      // set false on revoke
	LastSeen    time.Time  `json:"last_seen"`    // bumped on each successful validation
	StorageHash []byte     `json:"storage_hash"` // sha256(secret)[:16] — the Pebble key (kept for self-containment)
}

// secretBytesFromBearer extracts the 32-byte secret from a presented bearer
// string. It accepts both the canonical printed form `gorag_<id8>.<secret>`
// and a bare `gorag_<secret>` (doc.go format). The id8 segment is display-only
// and never participates in validation, so anything before the final '.' is
// discarded.
func secretBytesFromBearer(bearer string) ([]byte, error) {
	if len(bearer) < len(apikeyPrefix)+1 || bearer[:len(apikeyPrefix)] != apikeyPrefix {
		return nil, fmt.Errorf("missing %q prefix", apikeyPrefix)
	}
	rest := bearer[len(apikeyPrefix):]
	// Drop the display id if present: gorag_<id8>.<secret> → <secret>.
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] == '.' {
			rest = rest[i+1:]
			break
		}
	}
	b, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return nil, fmt.Errorf("bad base64url secret: %w", err)
	}
	return b, nil
}

// CreateAPIKey mints a new labelled, scoped, hashed credential. The raw secret
// is returned exactly once as the full display string `gorag_<id8>.<secret>`;
// the caller MUST surface it to the operator immediately and never log it.
// expiresAt may be nil for a non-expiring key.
func CreateAPIKey(s *Store, label, mode string, expiresAt *time.Time) (display string, _ APIKey, _ error) {
	if !validMode(mode) {
		return "", APIKey{}, fmt.Errorf("invalid mode %q (want read|write|admin)", mode)
	}
	secret, err := randomBytes(32)
	if err != nil {
		return "", APIKey{}, err
	}
	h := sha256.Sum256(secret)
	storageHash := h[:16]
	id := apikeyPrefix + base64.RawURLEncoding.EncodeToString(h[:8])

	key := APIKey{
		ID:          id,
		Label:       label,
		Mode:        mode,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   expiresAt,
		Enabled:     true,
		StorageHash: storageHash,
	}
	val, err := json.Marshal(key)
	if err != nil {
		return "", APIKey{}, err
	}
	if err := s.db.SetWithPrefix(storage.PrefixAuthAPIKey, storageHash, val); err != nil {
		return "", APIKey{}, err
	}
	display = id + "." + base64.RawURLEncoding.EncodeToString(secret)
	return display, key, nil
}

// ValidateAPIKey resolves a presented bearer to its stored APIKey. A Get hit on
// SHA-256(secret)[:16] IS the match — there is no secret-string comparison.
// Returns ErrUnknownAPIKey when the key is absent, disabled, or expired.
func ValidateAPIKey(s *Store, bearer string) (APIKey, error) {
	secret, err := secretBytesFromBearer(bearer)
	if err != nil {
		return APIKey{}, ErrUnknownAPIKey
	}
	storageHash := hash16(secret)
	val, ok, err := s.db.GetWithPrefix(storage.PrefixAuthAPIKey, storageHash)
	if err != nil || !ok {
		return APIKey{}, ErrUnknownAPIKey
	}
	var key APIKey
	if err := json.Unmarshal(val, &key); err != nil {
		return APIKey{}, ErrUnknownAPIKey
	}
	if !key.Enabled {
		return APIKey{}, ErrUnknownAPIKey
	}
	if key.ExpiresAt != nil && time.Now().UTC().After(*key.ExpiresAt) {
		return APIKey{}, ErrUnknownAPIKey
	}
	// Best-effort last-seen bump: never fail a valid login on a write error.
	key.LastSeen = time.Now().UTC()
	if b, mErr := json.Marshal(key); mErr == nil {
		_ = s.db.SetWithPrefix(storage.PrefixAuthAPIKey, storageHash, b)
	}
	return key, nil
}

// ListAPIKeys returns every stored APIKey (never the raw secret — it is not
// persisted). Order is Pebble scan order (by storage hash).
func ListAPIKeys(s *Store) ([]APIKey, error) {
	var keys []APIKey
	err := s.db.PrefixScanByte(storage.PrefixAuthAPIKey, func(_, val []byte) bool {
		var k APIKey
		if json.Unmarshal(val, &k) == nil {
			keys = append(keys, k)
		}
		return true
	})
	return keys, err
}

// RevokeAPIKey disables the key whose ID (gorag_<id8>) matches. Since IDs are
// display-only and not separately indexed, this scans the APIKey prefix — O(n)
// over a small n (single-operator vault). Returns ErrUnknownAPIKey if no key
// bears the ID. Revoking an already-disabled key is a no-op success.
//
// The write-back goes through the stored APIKey's own StorageHash, NOT the
// scan key: PrefixScan returns keys with the prefix byte attached, while
// SetWithPrefix prepends the prefix again — using the scan key would
// double-prefix and silently miss the real record.
func RevokeAPIKey(s *Store, id string) error {
	var storageHash []byte
	var ak APIKey
	found := false
	if err := s.db.PrefixScanByte(storage.PrefixAuthAPIKey, func(_, v []byte) bool {
		var candidate APIKey
		if json.Unmarshal(v, &candidate) == nil && candidate.ID == id {
			ak = candidate
			storageHash = append([]byte(nil), candidate.StorageHash...) // own the bytes
			found = true
			return false // stop scan
		}
		return true
	}); err != nil {
		return err
	}
	if !found {
		return ErrUnknownAPIKey
	}
	ak.Enabled = false
	out, err := json.Marshal(ak)
	if err != nil {
		return err
	}
	return s.db.SetWithPrefix(storage.PrefixAuthAPIKey, storageHash, out)
}

// validMode reports whether m is a recognized scope constant.
func validMode(m string) bool {
	return m == ModeRead || m == ModeWrite || m == ModeAdmin
}

// ValidateAPIKeyRaw matches a presented secret WITHOUT prefix-stripping or
// base64url decoding — the legacy mcp.token compat path (spec 045 US4). A
// pre-upgrade bearer carries no gorag_ prefix; its raw bytes are hashed and
// looked up directly. Returns ErrUnknownAPIKey when absent, disabled, or expired.
func ValidateAPIKeyRaw(s *Store, rawSecret string) (APIKey, error) {
	if rawSecret == "" {
		return APIKey{}, ErrUnknownAPIKey
	}
	val, ok, err := s.db.GetWithPrefix(storage.PrefixAuthAPIKey, hash16([]byte(rawSecret)))
	if err != nil || !ok {
		return APIKey{}, ErrUnknownAPIKey
	}
	var key APIKey
	if err := json.Unmarshal(val, &key); err != nil {
		return APIKey{}, ErrUnknownAPIKey
	}
	if !key.Enabled {
		return APIKey{}, ErrUnknownAPIKey
	}
	if key.ExpiresAt != nil && time.Now().UTC().After(*key.ExpiresAt) {
		return APIKey{}, ErrUnknownAPIKey
	}
	return key, nil
}
