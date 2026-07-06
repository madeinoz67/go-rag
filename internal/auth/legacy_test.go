package auth

import (
	"os"
	"path/filepath"
	"testing"
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
