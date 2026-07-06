package auth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// newTestStore opens a Pebble store in a temp dir for auth tests. It does NOT
// run the migration step (auth prefixes are data-only — a fresh Pebble instance
// accepts writes to any byte prefix), keeping the test focused on auth logic.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db)
}

func TestCreateAPIKey_DisplayAndStorageHash(t *testing.T) {
	s := newTestStore(t)
	display, key, err := CreateAPIKey(s, "ci", ModeRead, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	// Display form: gorag_<id8>.<secret>
	if !strings.HasPrefix(display, "gorag_") {
		t.Fatalf("display %q missing gorag_ prefix", display)
	}
	parts := strings.SplitN(display, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("display %q must be <id>.<secret>", display)
	}
	if parts[0] != key.ID {
		t.Fatalf("display id %q != key.ID %q", parts[0], key.ID)
	}
	if len(key.StorageHash) != 16 {
		t.Fatalf("StorageHash len = %d, want 16", len(key.StorageHash))
	}
	if !key.Enabled || key.Mode != ModeRead || key.Label != "ci" {
		t.Fatalf("unexpected key: %+v", key)
	}
}

func TestCreateValidate_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	display, _, err := CreateAPIKey(s, "ci", ModeWrite, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	got, err := ValidateAPIKey(s, display)
	if err != nil {
		t.Fatalf("ValidateAPIKey(full): %v", err)
	}
	if !got.Enabled || got.Mode != ModeWrite {
		t.Fatalf("validated key wrong: %+v", got)
	}

	// Bare form (gorag_<secret> — no id8 prefix) also validates.
	bare := "gorag_" + strings.SplitN(display, ".", 2)[1]
	if _, err := ValidateAPIKey(s, bare); err != nil {
		t.Fatalf("ValidateAPIKey(bare): %v", err)
	}
}

func TestValidate_SecretNeverPersisted(t *testing.T) {
	s := newTestStore(t)
	display, _, err := CreateAPIKey(s, "ci", ModeAdmin, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	secret := strings.SplitN(display, ".", 2)[1]

	// Every stored value under the APIKey prefix must NOT contain the secret.
	encountered := 0
	err = s.db.PrefixScanByte(storage.PrefixAuthAPIKey, func(_, val []byte) bool {
		encountered++
		if strings.Contains(string(val), secret) {
			t.Fatalf("raw secret leaked into stored value: %s", val)
		}
		return true
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if encountered != 1 {
		t.Fatalf("expected 1 stored key, got %d", encountered)
	}
}

func TestValidate_RejectsGarbageAndUnknown(t *testing.T) {
	s := newTestStore(t)
	cases := map[string]string{
		"empty":        "",
		"wrong prefix": "deadbeef.abcdef",
		"bad base64":   "gorag_!!!!",
		"unknown secret": "gorag_AAAAAAAABBBBBBBBCCCCCCCCDDDDDDDD" +
			"EEEEEEEEFFFFFFFFGGGGGGGGHHHHHHHH", // valid b64u, not in store
	}
	for name, tok := range cases {
		if _, err := ValidateAPIKey(s, tok); err != ErrUnknownAPIKey {
			t.Errorf("%s: want ErrUnknownAPIKey, got %v", name, err)
		}
	}
}

func TestExpiryEnforced(t *testing.T) {
	s := newTestStore(t)
	past := time.Now().UTC().Add(-time.Hour)
	display, _, err := CreateAPIKey(s, "expired", ModeRead, &past)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if _, err := ValidateAPIKey(s, display); err != ErrUnknownAPIKey {
		t.Fatalf("expired key validated: err=%v", err)
	}

	future := time.Now().UTC().Add(time.Hour)
	display2, _, err := CreateAPIKey(s, "valid", ModeRead, &future)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if _, err := ValidateAPIKey(s, display2); err != nil {
		t.Fatalf("unexpired key rejected: %v", err)
	}
}

func TestListAndRevoke(t *testing.T) {
	s := newTestStore(t)
	d1, k1, err := CreateAPIKey(s, "one", ModeRead, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey 1: %v", err)
	}
	if _, _, err := CreateAPIKey(s, "two", ModeAdmin, nil); err != nil {
		t.Fatalf("CreateAPIKey 2: %v", err)
	}

	list, err := ListAPIKeys(s)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}

	// Revoke by display id (gorag_<id8>).
	if err := RevokeAPIKey(s, k1.ID); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	// The revoked key no longer authenticates.
	if _, err := ValidateAPIKey(s, d1); err != ErrUnknownAPIKey {
		t.Fatalf("revoked key validated: err=%v", err)
	}
	// Revoking again is a no-op success (idempotent).
	if err := RevokeAPIKey(s, k1.ID); err != nil {
		t.Fatalf("RevokeAPIKey(idempotent): %v", err)
	}
	// Unknown id errors.
	if err := RevokeAPIKey(s, "gorag_unknown"); err != ErrUnknownAPIKey {
		t.Fatalf("RevokeAPIKey(unknown): want ErrUnknownAPIKey, got %v", err)
	}
}

func TestCreate_RejectsBadMode(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := CreateAPIKey(s, "x", "superuser", nil); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
