package engine

import (
	"context"
	"strings"
	"testing"
)

// vault_create_test.go (spec 051) pins the three Phase-1 engine fixes: (1)
// ListVaults reads the in-db registry (a vault created with no on-disk directory
// still lists); (2) CreateVault registers an empty vault + validates; (3) the
// default vault cannot be deleted.

func TestCreateVault_RegistersAndLists(t *testing.T) {
	e := newCacheEngine(t)
	ctx := context.Background()

	// A brand-new vault registers in the in-db store + lists with 0 documents.
	if err := e.CreateVault(ctx, "archive"); err != nil {
		t.Fatalf("CreateVault(archive): %v", err)
	}
	listed, err := e.ListVaults("")
	if err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	if !vaultNamed(listed, "archive") {
		t.Fatalf("post-create: want 'archive' listed, got %v", names(listed))
	}
	if c := docCount(listed, "archive"); c != 0 {
		t.Errorf("new vault document count: got %d, want 0", c)
	}

	// Duplicate refused.
	if err := e.CreateVault(ctx, "archive"); err == nil {
		t.Error("duplicate CreateVault should fail")
	}
	// Invalid names refused (vaultpkg.ValidateName: lowercase alnum + hyphens, 1–64).
	for _, bad := range []string{"", "Bad Name", "UPPER", "under_score", strings.Repeat("a", 65)} {
		if err := e.CreateVault(ctx, bad); err == nil {
			t.Errorf("CreateVault(%q): want error, got nil", bad)
		}
	}
}

func TestListVaults_InDbRegistryNotDirs(t *testing.T) {
	// The fix: ListVaults reads the in-db registry, so a vault registered by a
	// raw WriteVaultName (no on-disk directory, no document) is listed — the
	// stale dir-based ListVaults would have missed it.
	e := newCacheEngine(t)
	ws := e.db.ResolveVaultPrefix("synthetic")
	if err := e.db.WriteVaultName(ws, "synthetic"); err != nil {
		t.Fatalf("WriteVaultName: %v", err)
	}
	listed, _ := e.ListVaults("")
	if !vaultNamed(listed, "synthetic") {
		t.Errorf("in-db vault 'synthetic' should be listed, got %v", names(listed))
	}
}

func TestDeleteVault_DefaultRefused(t *testing.T) {
	e := newCacheEngine(t)
	// The guard fires before any data work, so it holds whether or not the
	// default vault is currently registered.
	if err := e.DeleteVault(context.Background(), "default"); err == nil {
		t.Error("DeleteVault(default) should be refused")
	}
}

func vaultNamed(entries []VaultEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

func docCount(entries []VaultEntry, name string) int {
	for _, e := range entries {
		if e.Name == name {
			return e.Documents
		}
	}
	return -1
}

func names(entries []VaultEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}
