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
// discarded. The decoded secret MUST be exactly 32 bytes — the only length
// CreateAPIKey ever mints — so malformed-shape bearers are rejected at parse
// before any hash/DB work (spec 045 red-team finding, LOW).
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
	if len(b) != 32 {
		return nil, fmt.Errorf("secret must be 32 bytes, got %d", len(b))
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
//
// All failure paths execute equal work (a dummy unmarshal + enabled/expiry
// checks on the miss path) so an attacker cannot timing-distinguish "no such
// key" from "disabled" or "expired" (spec 045 red-team finding, LOW). The
// LastSeen bump is serialized against RevokeAPIKey and re-checked under the
// lock so a concurrent revoke cannot be clobbered (HIGH — would resurrect the
// revoked key on the next validate).
// ValidateAPIKey resolves a presented bearer to its stored APIKey. A Get hit on
// SHA-256(secret)[:16] IS the match — there is no secret-string comparison.
// Returns ErrUnknownAPIKey when the key is absent, disabled, or expired.
//
// Pure read: no LastSeen write-back, no lock. The validate path MUST stay
// write-free so revoke (a Pebble Delete) cannot race a validate write-back and
// resurrect the credential — a per-Store mutex would not even fix this, because
// each transport builds its own Store (the cross-instance race is uncloseable
// with a lock). LastSeen is set once at creation; listing shows issued-at.
// All failure paths execute equal work (padAPIKeyFailure on the miss path) so
// an attacker cannot timing-distinguish "no such key" from "disabled/expired".
func ValidateAPIKey(s *Store, bearer string) (APIKey, error) {
	secret, err := secretBytesFromBearer(bearer)
	if err != nil {
		padAPIKeyFailure()
		return APIKey{}, ErrUnknownAPIKey
	}
	storageHash := hash16(secret)
	val, ok, err := s.db.GetWithPrefix(storage.PrefixAuthAPIKey, storageHash)
	if err != nil || !ok {
		padAPIKeyFailure()
		return APIKey{}, ErrUnknownAPIKey
	}
	var key APIKey
	if err := json.Unmarshal(val, &key); err != nil {
		return APIKey{}, ErrUnknownAPIKey
	}
	if !key.Enabled || (key.ExpiresAt != nil && time.Now().UTC().After(*key.ExpiresAt)) {
		return APIKey{}, ErrUnknownAPIKey
	}
	return key, nil
}

// padAPIKeyFailure does the same CPU work a Get-hit-then-disabled/expired path
// does (unmarshal + enabled/expiry checks) so the Get-miss path is not timing-
// distinguishable. No functional effect.
func padAPIKeyFailure() {
	var k APIKey
	_ = json.Unmarshal(decoyAPIKeyJSON, &k)
	_ = k.Enabled
	if k.ExpiresAt != nil {
		_ = time.Now().UTC().After(*k.ExpiresAt)
	}
}

// decoyAPIKeyJSON is a valid APIKey JSON used only to pad the miss path.
var decoyAPIKeyJSON = []byte(`{"id":"","label":"","mode":"read","created_at":"2026-01-01T00:00:00Z","enabled":true,"expires_at":"2026-01-02T00:00:00Z","storage_hash":""}`)

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

// DeleteAPIKey permanently removes the key whose ID (gorag_<id8>) matches — unlike
// RevokeAPIKey (which disables it and keeps the record for audit). The record is
// gone and there is no re-enable path. Returns ErrUnknownAPIKey if no key has the
// id. O(n) scan (IDs are display-only, not separately indexed) — same shape as
// RevokeAPIKey.
func DeleteAPIKey(s *Store, id string) error {
	var storageHash []byte
	found := false
	if err := s.db.PrefixScanByte(storage.PrefixAuthAPIKey, func(_, v []byte) bool {
		var candidate APIKey
		if json.Unmarshal(v, &candidate) == nil && candidate.ID == id {
			storageHash = append([]byte(nil), candidate.StorageHash...)
			found = true
			return false
		}
		return true
	}); err != nil {
		return err
	}
	if !found {
		return ErrUnknownAPIKey
	}
	return s.db.DeleteWithPrefix(storage.PrefixAuthAPIKey, storageHash)
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
		padAPIKeyFailure()
		return APIKey{}, ErrUnknownAPIKey
	}
	val, ok, err := s.db.GetWithPrefix(storage.PrefixAuthAPIKey, hash16([]byte(rawSecret)))
	if err != nil || !ok {
		padAPIKeyFailure()
		return APIKey{}, ErrUnknownAPIKey
	}
	var key APIKey
	if err := json.Unmarshal(val, &key); err != nil {
		return APIKey{}, ErrUnknownAPIKey
	}
	if !key.Enabled || (key.ExpiresAt != nil && time.Now().UTC().After(*key.ExpiresAt)) {
		return APIKey{}, ErrUnknownAPIKey
	}
	return key, nil
}
