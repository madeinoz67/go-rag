package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble"

	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// TestV4E2E_MergeOnRun throws a synthetic legacy layout at RunMigrations with
// SetLegacyRoot wired, the way engine.Open would, and asserts the v4 step
// merges the legacy vault into the unified store, archives it, and lands at v4.
func TestV4E2E_MergeOnRun(t *testing.T) {
	dir := t.TempDir()
	legacyRoot := filepath.Join(dir, "vaults")
	unifiedPath := filepath.Join(dir, "store", "data")

	// Legacy vault "default" with one document key (kind|payload).
	mkLegacy(t, filepath.Join(legacyRoot, "default", "data"), [][2][]byte{
		{append([]byte{storage.PrefixDocument}, []byte("doc1")...), []byte(`{"id":"doc1"}`)},
	})

	// Stand up the unified store the way storage.Open would, then arm the legacy
	// root and run the full migration set (v1..v4) — v4MultiVault must merge.
	if err := os.MkdirAll(filepath.Dir(unifiedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := pebble.Open(unifiedPath, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		t.Fatalf("open unified: %v", err)
	}
	defer db.Close()

	SetLegacyRoot(legacyRoot)
	defer SetLegacyRoot("") // reset for other tests in this package

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// Landed at v4.
	if got, _ := readVersion(db); got != ExpectedVersion {
		t.Fatalf("version = %d, want %d", got, ExpectedVersion)
	}

	// Legacy doc merged under the widened shape, value verbatim.
	ws := keys.VaultPrefix("default")
	if v, ok := mustGet(t, db, keys.DocumentKey(ws, "doc1")); !ok || !bytes.Equal(v, []byte(`{"id":"doc1"}`)) {
		t.Fatalf("merged default/doc1: %q (ok=%v)", v, ok)
	}
	// Registry written.
	if name, ok := mustGet(t, db, keys.VaultMetaKey(ws)); !ok || string(name) != "default" {
		t.Fatalf("vault meta: %q (ok=%v)", name, ok)
	}
	// Legacy dir archived.
	if _, err := os.Stat(filepath.Join(legacyRoot, "default.prev")); err != nil {
		t.Fatalf("legacy default not archived: %v", err)
	}
}

// countVaultScoped counts keys in db whose first byte is `kind` and whose next
// 8 bytes equal `ws` — i.e. the widened vault-scoped keys for one (kind, vault)
// after the v4 merge. Overflow-safe: seeks to the prefix and stops at the first
// non-matching key (no byte-increment upper bound that could wrap on 0xFF).
func countVaultScoped(t *testing.T, db *pebble.DB, kind byte, ws [8]byte) int {
	t.Helper()
	prefix := append([]byte{kind}, ws[:]...)
	iter, err := db.NewIter(nil)
	if err != nil {
		t.Fatalf("iter: %v", err)
	}
	defer iter.Close()
	n := 0
	for iter.SeekGE(prefix); iter.Valid() && bytes.HasPrefix(iter.Key(), prefix); iter.Next() {
		n++
	}
	return n
}

// TestV4E2E_TwoVaults_3to4 (T022) is the two-vault E2E migration validation.
// The sibling TestV4E2E_MergeOnRun covers a single legacy vault; the package's
// TestMergeLegacyVaults covers two vaults but through the standalone
// MergeLegacyVaults path (not RunMigrations) and does not pin the schema
// version transition. This test fills the gap:
//
//   - create TWO legacy per-vault DBs with a known vault-scoped key count each,
//   - pre-seed the unified store at schema version 3 (the last pre-merge
//     version), so RunMigrations applies ONLY the v4 step (the precise 3→4
//     transition the task names),
//   - run RunMigrations the way engine.Open does (SetLegacyRoot wired),
//   - assert the store lands at ExpectedVersion (4),
//   - assert BOTH vaults' data appears in the unified store under the widened
//     shape, values verbatim, with correct per-vault key counts,
//   - assert cross-vault isolation (alpha's ws range holds no beta key), and
//   - assert both legacy directories are archived to .prev.
func TestV4E2E_TwoVaults_3to4(t *testing.T) {
	dir := t.TempDir()
	legacyRoot := filepath.Join(dir, "vaults")
	unifiedPath := filepath.Join(dir, "store", "data")

	// Legacy vault "alpha": 3 documents + 2 chunks = 5 vault-scoped keys.
	mkLegacy(t, filepath.Join(legacyRoot, "alpha", "data"), [][2][]byte{
		{append([]byte{storage.PrefixDocument}, []byte("a-doc-1")...), []byte(`{"id":"a-doc-1"}`)},
		{append([]byte{storage.PrefixDocument}, []byte("a-doc-2")...), []byte(`{"id":"a-doc-2"}`)},
		{append([]byte{storage.PrefixDocument}, []byte("a-doc-3")...), []byte(`{"id":"a-doc-3"}`)},
		{append([]byte{storage.PrefixChunk}, []byte("a-chk-1")...), []byte(`{"id":"a-chk-1"}`)},
		{append([]byte{storage.PrefixChunk}, []byte("a-chk-2")...), []byte(`{"id":"a-chk-2"}`)},
	})
	// Legacy vault "beta": 2 documents = 2 vault-scoped keys (no chunks).
	mkLegacy(t, filepath.Join(legacyRoot, "beta", "data"), [][2][]byte{
		{append([]byte{storage.PrefixDocument}, []byte("b-doc-1")...), []byte(`{"id":"b-doc-1"}`)},
		{append([]byte{storage.PrefixDocument}, []byte("b-doc-2")...), []byte(`{"id":"b-doc-2"}`)},
	})

	// Stand up the unified store the way storage.Open would.
	if err := os.MkdirAll(filepath.Dir(unifiedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := pebble.Open(unifiedPath, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		t.Fatalf("open unified: %v", err)
	}
	defer db.Close()

	// Pre-seed at schema version 3 so RunMigrations applies ONLY the v4 step —
	// the precise 3→4 transition. (writeVersion is package-local; this file is
	// package migrate.) This simulates an existing v3 store upgrading, vs. the
	// fresh-bootstrap path TestV4E2E_MergeOnRun exercises.
	if err := writeVersion(db, 3); err != nil {
		t.Fatalf("seed v3: %v", err)
	}
	if got, _ := readVersion(db); got != 3 {
		t.Fatalf("seed check: version = %d, want 3 before run", got)
	}

	SetLegacyRoot(legacyRoot)
	defer SetLegacyRoot("") // reset for other tests in this package

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// Landed at ExpectedVersion (4) — the 3→4 transition fired exactly once.
	if got, _ := readVersion(db); got != ExpectedVersion {
		t.Fatalf("version = %d, want %d (ExpectedVersion)", got, ExpectedVersion)
	}

	wsA := keys.VaultPrefix("alpha")
	wsB := keys.VaultPrefix("beta")

	// --- Correct counts per vault (the "correct counts" acceptance check). ---
	// alpha: 3 docs + 2 chunks.
	if got := countVaultScoped(t, db, storage.PrefixDocument, wsA); got != 3 {
		t.Errorf("alpha document count = %d, want 3", got)
	}
	if got := countVaultScoped(t, db, storage.PrefixChunk, wsA); got != 2 {
		t.Errorf("alpha chunk count = %d, want 2", got)
	}
	// beta: 2 docs, 0 chunks.
	if got := countVaultScoped(t, db, storage.PrefixDocument, wsB); got != 2 {
		t.Errorf("beta document count = %d, want 2", got)
	}
	if got := countVaultScoped(t, db, storage.PrefixChunk, wsB); got != 0 {
		t.Errorf("beta chunk count = %d, want 0", got)
	}

	// --- Data integrity: representative keys present under the widened shape,
	// values verbatim (no decoding — the migration is shape-agnostic). ---
	if v, ok := mustGet(t, db, keys.DocumentKey(wsA, "a-doc-1")); !ok || !bytes.Equal(v, []byte(`{"id":"a-doc-1"}`)) {
		t.Errorf("alpha a-doc-1: %q (ok=%v)", v, ok)
	}
	if v, ok := mustGet(t, db, keys.ChunkKey(wsA, "a-chk-2")); !ok || !bytes.Equal(v, []byte(`{"id":"a-chk-2"}`)) {
		t.Errorf("alpha a-chk-2: %q (ok=%v)", v, ok)
	}
	if v, ok := mustGet(t, db, keys.DocumentKey(wsB, "b-doc-2")); !ok || !bytes.Equal(v, []byte(`{"id":"b-doc-2"}`)) {
		t.Errorf("beta b-doc-2: %q (ok=%v)", v, ok)
	}

	// --- Registry written for both vaults. ---
	if name, ok := mustGet(t, db, keys.VaultMetaKey(wsA)); !ok || string(name) != "alpha" {
		t.Errorf("alpha vault meta: %q (ok=%v)", name, ok)
	}
	if name, ok := mustGet(t, db, keys.VaultMetaKey(wsB)); !ok || string(name) != "beta" {
		t.Errorf("beta vault meta: %q (ok=%v)", name, ok)
	}

	// --- Cross-vault isolation: beta's doc must NOT appear under alpha's ws. ---
	if _, ok := mustGet(t, db, keys.DocumentKey(wsA, "b-doc-2")); ok {
		t.Error("beta b-doc-2 leaked into alpha ws range (vault isolation broken)")
	}
	if _, ok := mustGet(t, db, keys.DocumentKey(wsB, "a-doc-1")); ok {
		t.Error("alpha a-doc-1 leaked into beta ws range (vault isolation broken)")
	}

	// --- Both legacy directories archived to .prev. ---
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(legacyRoot, name+".prev")); err != nil {
			t.Errorf("legacy %s not archived: %v", name, err)
		}
	}
}
