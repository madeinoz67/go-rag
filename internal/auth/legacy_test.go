package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTokenFile(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp.token")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

func TestImportLegacyToken_FromFile(t *testing.T) {
	s := newTestStore(t)
	path := writeTokenFile(t, "legacy-bearer-value-12345\n")

	imported, err := ImportLegacyTokenFromFile(s, path, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !imported {
		t.Fatal("imported=false, want true")
	}
	// The plaintext file is scrubbed after import (spec 045 red-team G): the
	// value now lives hashed in the store; cleartext must not linger on disk.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plaintext mcp.token still present after import (err=%v)", err)
	}
	// The raw value authenticates with NO gorag_ prefix (legacy client shape).
	p, err := s.ValidateToken("legacy-bearer-value-12345")
	if err != nil {
		t.Fatalf("ValidateToken(raw legacy): %v", err)
	}
	if p.Source != SourceAPIKey || p.Mode != ModeAdmin {
		t.Fatalf("principal = %+v", p)
	}

	// It shows up in the key list with the legacy label.
	list, err := ListAPIKeys(s)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(list) != 1 || list[0].Label != "legacy-mcp" || list[0].Mode != ModeAdmin {
		t.Fatalf("list = %+v", list)
	}
}

func TestImportLegacyToken_Idempotent(t *testing.T) {
	s := newTestStore(t)
	path := writeTokenFile(t, "the-value")

	if _, err := ImportLegacyTokenFromFile(s, path, ""); err != nil {
		t.Fatalf("import 1: %v", err)
	}
	// Second run must not create a duplicate or rotate.
	if _, err := ImportLegacyTokenFromFile(s, path, ""); err != nil {
		t.Fatalf("import 2: %v", err)
	}
	list, _ := ListAPIKeys(s)
	if len(list) != 1 {
		t.Fatalf("after 2 imports, len(list) = %d, want 1", len(list))
	}
	// Still authenticates.
	if _, err := s.ValidateToken("the-value"); err != nil {
		t.Fatalf("post-rerun validate: %v", err)
	}
}

func TestImportLegacyToken_SkipsWhenKeysExist(t *testing.T) {
	s := newTestStore(t)
	// Operator has already minted a key — legacy import must not run.
	if _, _, err := CreateAPIKey(s, "modern", ModeAdmin, nil); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	path := writeTokenFile(t, "legacy-value")

	imported, err := ImportLegacyTokenFromFile(s, path, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported {
		t.Fatal("imported=true despite non-empty store")
	}
	// And the legacy value does NOT authenticate (it was never imported).
	if _, err := s.ValidateToken("legacy-value"); err != ErrUnknownCredential {
		t.Fatalf("legacy value in non-empty store: want ErrUnknownCredential, got %v", err)
	}
}

func TestImportLegacyToken_AbsentFile_NoOp(t *testing.T) {
	s := newTestStore(t)
	imported, err := ImportLegacyTokenFromFile(s, filepath.Join(t.TempDir(), "nope"), "")
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if imported {
		t.Fatal("missing file: imported=true")
	}
}

func TestImportLegacyToken_EmptyFile_NoOp(t *testing.T) {
	s := newTestStore(t)
	path := writeTokenFile(t, "  \n")
	imported, err := ImportLegacyTokenFromFile(s, path, "")
	if err != nil {
		t.Fatalf("empty file: %v", err)
	}
	if imported {
		t.Fatal("empty file: imported=true")
	}
}

func TestImportLegacyToken_LegacyRevokeWorks(t *testing.T) {
	s := newTestStore(t)
	path := writeTokenFile(t, "legacy-value")
	if _, err := ImportLegacyTokenFromFile(s, path, ""); err != nil {
		t.Fatalf("Import: %v", err)
	}
	list, _ := ListAPIKeys(s)
	if len(list) != 1 {
		t.Fatalf("len = %d", len(list))
	}
	if err := RevokeAPIKey(s, list[0].ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := s.ValidateToken("legacy-value"); err != ErrUnknownCredential {
		t.Fatalf("revoked legacy still authenticates: %v", err)
	}
}

// TestImportLegacyToken_StampedExpiry (spec 045 red-team MED): the imported
// legacy key must carry a finite ExpiresAt (≈90 days) so a leaked pre-upgrade
// mcp.token sunsets automatically.
func TestImportLegacyToken_StampedExpiry(t *testing.T) {
	s := newTestStore(t)
	path := writeTokenFile(t, "legacy-value")
	if _, err := ImportLegacyTokenFromFile(s, path, ""); err != nil {
		t.Fatalf("Import: %v", err)
	}
	list, _ := ListAPIKeys(s)
	if len(list) != 1 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].ExpiresAt == nil {
		t.Fatal("imported legacy key has no expiry (would authenticate forever)")
	}
	if list[0].ExpiresAt.Before(time.Now().UTC().Add(24 * time.Hour)) {
		t.Fatalf("legacy expiry too soon: %v", list[0].ExpiresAt)
	}
	if list[0].ExpiresAt.After(time.Now().UTC().Add(180 * 24 * time.Hour)) {
		t.Fatalf("legacy expiry too far out (>180d): %v", list[0].ExpiresAt)
	}
}
