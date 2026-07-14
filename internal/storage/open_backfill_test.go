package storage

import (
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// open_backfill_test.go (spec 051) verifies that Open backfills the vault
// registry for data written before the spec 052 name registry existed — the
// fix for "0 vaults" on a daemon whose default corpus predates the registry.

func TestOpen_BackfillsLegacyDefaultData(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	// Write a document under the "default" ws with NO registry entry — mimics a
	// pre-spec-052 corpus (data present, name registry absent).
	db1, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ws := keys.VaultPrefix("default")
	if err := db1.Set(keys.DocumentKey(ws, "legacy-doc"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if db1.VaultNameExists("default") {
		t.Fatal("default must not be registered before reopen")
	}
	if err := db1.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen — Open runs BackfillVaultNames, which registers the data-bearing
	// "default" vault under its recovered name (not a placeholder hex).
	db2, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if !db2.VaultNameExists("default") {
		t.Fatal("after reopen, legacy default data should be backfilled + registered")
	}
	names, err := db2.ListVaultNames()
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, n := range names {
		if n == "default" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("ListVaultNames after backfill: want 'default', got %v", names)
	}
}
