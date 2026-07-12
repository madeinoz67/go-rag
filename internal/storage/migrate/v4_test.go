package migrate

import (
	"bytes"
	"os"
	"testing"

	"github.com/cockroachdb/pebble"

	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// writeLegacyDB opens a fresh Pebble DB at path and writes the given (key,value)
// pairs verbatim. Used to construct synthetic pre-v2 per-vault stores.
func writeLegacyDB(t *testing.T, path string, kv [][2][]byte) *pebble.DB {
	t.Helper()
	db, err := pebble.Open(path, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		t.Fatalf("open legacy db %q: %v", path, err)
	}
	batch := db.NewBatch()
	for _, p := range kv {
		if err := batch.Set(p[0], p[1], nil); err != nil {
			batch.Close()
			t.Fatalf("legacy set: %v", err)
		}
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		batch.Close()
		t.Fatalf("legacy commit: %v", err)
	}
	batch.Close()
	return db
}

// TestRewriteLegacyVault verifies the pure key-rewrite primitive: vault-scoped
// keys gain ws after the kind byte (values verbatim); global + schema keys are
// skipped; the registry lands; re-running is a no-op (idempotent).
func TestRewriteLegacyVault(t *testing.T) {
	dir := t.TempDir()
	unifiedPath := dir + "/unified"
	unified, err := pebble.Open(unifiedPath, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		t.Fatalf("open unified: %v", err)
	}
	t.Cleanup(func() { unified.Close() })

	legacyPath := dir + "/legacy/data"
	legacyDB := writeLegacyDB(t, legacyPath, [][2][]byte{
		// Vault-scoped keys (current shape: kind | payload).
		{append([]byte{storage.PrefixDocument}, []byte("doc1")...), []byte(`{"id":"doc1"}`)},
		{append([]byte{storage.PrefixChunk}, []byte("chk1")...), []byte(`{"id":"chk1"}`)},
		{append([]byte{storage.PrefixEmbedding}, []byte("chk1")...), []byte("EMB")},
		// A global key (config) and the schema-version key — MUST be skipped.
		{append([]byte{storage.PrefixConfig}, []byte("model")...), []byte("nomic")},
		{[]byte{0xFF, 's', 'c', 'h', 'e', 'm', 'a'}, []byte{0, 0, 0, 0, 0, 0, 0, 3}},
	})
	t.Cleanup(func() { legacyDB.Close() })

	stats, err := RewriteLegacyVault(unified, legacyDB, "work")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if stats.Widened != 3 {
		t.Fatalf("widened count: got %d want 3", stats.Widened)
	}
	if stats.SkippedGlobal != 2 {
		t.Fatalf("skipped global: got %d want 2", stats.SkippedGlobal)
	}

	ws := keys.VaultPrefix("work")
	// Every vault-scoped key is present under the widened shape, value verbatim.
	wantDoc := keys.DocumentKey(ws, "doc1")
	if v, ok := mustGet(t, unified, wantDoc); !bytes.Equal(v, []byte(`{"id":"doc1"}`)) {
		t.Fatalf("widened doc value: %q (ok=%v)", v, ok)
	}
	wantChunk := keys.ChunkKey(ws, "chk1")
	if v, ok := mustGet(t, unified, wantChunk); !bytes.Equal(v, []byte(`{"id":"chk1"}`)) {
		t.Fatalf("widened chunk value: %q (ok=%v)", v, ok)
	}
	wantEmb := keys.EmbeddingKey(ws, "chk1")
	if v, ok := mustGet(t, unified, wantEmb); !bytes.Equal(v, []byte("EMB")) {
		t.Fatalf("widened emb value: %q (ok=%v)", v, ok)
	}

	// Registry keys present.
	if name, ok := mustGet(t, unified, keys.VaultMetaKey(ws)); !ok || string(name) != "work" {
		t.Fatalf("vault meta: %q (ok=%v)", name, ok)
	}
	if wsb, ok := mustGet(t, unified, keys.VaultNameIndexKey("work")); !ok || !bytes.Equal(wsb, ws[:]) {
		t.Fatalf("vault name index: %x (ok=%v) want %x", wsb, ok, ws)
	}

	// Global/config keys NOT copied to the unified store.
	if _, ok := mustGet(t, unified, append([]byte{storage.PrefixConfig}, []byte("model")...)); ok {
		t.Fatal("config key was copied (should be skipped)")
	}

	// Idempotent: second rewrite reports already-migrated and copies nothing new.
	stats2, err := RewriteLegacyVault(unified, legacyDB, "work")
	if err != nil {
		t.Fatalf("rewrite 2: %v", err)
	}
	if stats2.Widened != 0 {
		t.Fatalf("idempotent rewrite copied %d keys (want 0)", stats2.Widened)
	}
}

// TestMergeLegacyVaults covers the filesystem merge: two legacy per-vault DBs
// collapse into one unified store with correct wsPrefixes and no data loss; the
// legacy directories are archived to .prev; a fresh root returns ErrNoLegacyVaults.
func TestMergeLegacyVaults(t *testing.T) {
	dir := t.TempDir()
	legacyRoot := dir + "/vaults"
	unifiedPath := dir + "/store/data"

	// Vault "alpha": 1 doc + 1 chunk.
	mkLegacy(t, legacyRoot+"/alpha/data", [][2][]byte{
		{append([]byte{storage.PrefixDocument}, []byte("a-doc")...), []byte("A")},
		{append([]byte{storage.PrefixChunk}, []byte("a-chk")...), []byte("AC")},
	})
	// Vault "beta": 2 docs.
	mkLegacy(t, legacyRoot+"/beta/data", [][2][]byte{
		{append([]byte{storage.PrefixDocument}, []byte("b-doc-1")...), []byte("B1")},
		{append([]byte{storage.PrefixDocument}, []byte("b-doc-2")...), []byte("B2")},
	})

	report, err := MergeLegacyVaults(unifiedPath, legacyRoot)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(report.Vaults) != 2 {
		t.Fatalf("expected 2 vaults migrated, got %d", len(report.Vaults))
	}
	if len(report.Archived) != 2 {
		t.Fatalf("expected 2 archives, got %d", len(report.Archived))
	}

	// Verify data in the unified store.
	unified, err := pebble.Open(unifiedPath, &pebble.Options{Logger: migrateLogger{}, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen unified: %v", err)
	}
	defer unified.Close()

	wsA := keys.VaultPrefix("alpha")
	wsB := keys.VaultPrefix("beta")
	if v, ok := mustGet(t, unified, keys.DocumentKey(wsA, "a-doc")); !ok || !bytes.Equal(v, []byte("A")) {
		t.Fatalf("alpha doc: %q (ok=%v)", v, ok)
	}
	if v, ok := mustGet(t, unified, keys.DocumentKey(wsB, "b-doc-2")); !ok || !bytes.Equal(v, []byte("B2")) {
		t.Fatalf("beta doc 2: %q (ok=%v)", v, ok)
	}

	// Cross-vault isolation: alpha's ws range must not contain beta's doc.
	if _, ok := mustGet(t, unified, keys.DocumentKey(wsA, "b-doc-2")); ok {
		t.Fatal("beta doc leaked into alpha ws range")
	}

	// Legacy dirs archived.
	for _, a := range report.Archived {
		if _, err := os.Stat(a); err != nil {
			t.Fatalf("archive missing: %s (%v)", a, err)
		}
	}

	// Re-running is a no-op (legacy dirs are archived) → ErrNoLegacyVaults.
	if _, err := MergeLegacyVaults(unifiedPath, legacyRoot); err != ErrNoLegacyVaults {
		t.Fatalf("re-merge: got %v, want ErrNoLegacyVaults", err)
	}
}

// TestMergeLegacyVaults_FreshRoot verifies the fresh-install path: an absent
// legacy root returns ErrNoLegacyVaults without touching the unified store.
func TestMergeLegacyVaults_FreshRoot(t *testing.T) {
	dir := t.TempDir()
	_, err := MergeLegacyVaults(dir+"/store/data", dir+"/nonexistent")
	if err != ErrNoLegacyVaults {
		t.Fatalf("fresh root: got %v, want ErrNoLegacyVaults", err)
	}
}

// TestRefuseNewerVersion pins the refuse-newer guard (FR-015): a store whose
// persisted schema version exceeds ExpectedVersion is rejected, not silently
// downgraded.
func TestRefuseNewerVersion(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/db"
	db, err := pebble.Open(dbPath, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Persist a version one higher than this binary supports.
	if err := writeVersion(db, ExpectedVersion+1); err != nil {
		t.Fatalf("write version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	db2, err := pebble.Open(dbPath, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if _, err := Run(db2, ExpectedVersion, defaultMigrations); err == nil {
		t.Fatal("expected refuse-newer error, got nil")
	}
}

func mkLegacy(t *testing.T, path string, kv [][2][]byte) {
	t.Helper()
	db := writeLegacyDB(t, path, kv)
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}
}

func mustGet(t *testing.T, db *pebble.DB, key []byte) ([]byte, bool) {
	t.Helper()
	v, closer, err := db.Get(key)
	if err == pebble.ErrNotFound {
		return nil, false
	}
	if err != nil {
		t.Fatalf("get %x: %v", key, err)
	}
	defer closer.Close()
	out := make([]byte, len(v))
	copy(out, v)
	return out, true
}
