package auth

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"time"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// legacy.go implements the spec 045 US4 zero-break upgrade path: an existing
// deployment's static mcp.token becomes a `legacy-mcp` admin API key on the
// first post-upgrade open, so scripts carrying the old bearer keep working.
//
// The legacy token is a bare string (no gorag_ prefix) that pre-upgrade clients
// send as `Authorization: Bearer <mcp.token>`. It is imported verbatim:
// StorageHash = SHA-256(value)[:16], and ValidateToken's legacy fallback (see
// auth.go) hash-looks-up a no-prefix bearer against the APIKey store — so the
// unchanged old value authenticates through the new validator.
//
// Import is one-shot: it runs only when the APIKey store is empty (the operator
// has not yet minted their first key). Once any key exists, the legacy import
// is skipped — the operator has moved past it.

// ImportLegacyTokenFromFile reads the legacy bearer token at tokenFilePath and,
// when non-empty AND the APIKey store is empty, imports it as a `legacy-mcp`
// admin API key. Returns (imported=true) when it created the key, (false) when
// it skipped (file absent/empty, or the store already holds keys). A missing
// file is not an error.
func ImportLegacyTokenFromFile(s *Store, tokenFilePath, label string) (bool, error) {
	value, err := os.ReadFile(tokenFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil // no legacy token — nothing to do
		}
		return false, err
	}
	tok := trimSpaceASCII(string(value))
	if tok == "" {
		return false, nil
	}
	return importLegacyTokenValue(s, tok, label)
}

// importLegacyTokenValue stores tok as a legacy admin API key when the APIKey
// store is empty. Idempotent: a re-run on a store that already holds the legacy
// hash is a no-op success (it does not re-create or rotate).
func importLegacyTokenValue(s *Store, tok, label string) (bool, error) {
	if label == "" {
		label = "legacy-mcp"
	}
	// Skip when the operator has already minted keys — they've moved past legacy.
	has, err := anyKey(s, storage.PrefixAuthAPIKey)
	if err != nil {
		return false, err
	}
	if has {
		// If the legacy hash is already present (re-run), report not-imported.
		if _, ok, err := s.db.GetWithPrefix(storage.PrefixAuthAPIKey, hash16([]byte(tok))); err == nil && ok {
			return false, nil
		}
		return false, nil
	}
	hash := hash16([]byte(tok))
	// Spec 045 red-team (MED): stamp a finite expiry so a leaked pre-upgrade
	// mcp.token sunsets automatically instead of authenticating as admin
	// forever. 90 days gives scripts time to migrate to a gorag_ key.
	expiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	key := APIKey{
		ID:          apikeyPrefix + id8([]byte(tok)),
		Label:       label,
		Mode:        ModeAdmin,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   &expiry,
		Enabled:     true,
		StorageHash: hash,
	}
	val, err := json.Marshal(key)
	if err != nil {
		return false, err
	}
	if err := s.db.SetWithPrefix(storage.PrefixAuthAPIKey, hash, val); err != nil {
		return false, err
	}
	log.Printf("auth: imported legacy bearer token as api key %q (%s); mint a gorag_ key and decommission it when convenient", key.ID, label)
	return true, nil
}

// trimSpaceASCII trims leading/trailing ASCII whitespace (the legacy token file
// may carry a trailing newline). Avoids pulling in strings for a hot-enough
// path and keeps the trim locale-independent.
func trimSpaceASCII(s string) string {
	start, end := 0, len(s)
	for start < end && isASCIISpace(s[start]) {
		start++
	}
	for end > start && isASCIISpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}
