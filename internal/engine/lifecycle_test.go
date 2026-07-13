package engine_test

// lifecycle_test.go (spec 052 / US3 / T020): pins the three vault-lifecycle
// operations on the unified store — RenameVault (metadata-only, data stays),
// ClearVault (range-tombstone, vault stays registered), DeleteVault (clear +
// drop registry). Mirrors quickstart §3: rename → query (results present) →
// clear → query (empty).
//
// Fixture pattern follows delete_test.go / parity_test.go: a standalone pipeline
// with fakeEmbed ingests one distinctive document under a named vault (registered
// via WriteVaultName so the 0x1A/0x1B registry keys exist), drains, then the
// Engine over the same DB queries through the Pebble-backed FTS re-seeded by
// LoadIndex. Keyword queries never touch the embedder.
//
// Registry assertions use db.ListVaultNames / VaultNameExists (the Pebble 0x1A/0x1B
// layer the lifecycle methods operate on), NOT eng.ListVaults (which reads the
// filesystem vaultpkg registry — a transport/CLI concern outside T019's scope).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// lifecycleEngine opens a fresh unified DB and returns an Engine over it, the
// underlying DB (for direct registry writes/asserts), and the temp dir.
func lifecycleEngine(t *testing.T) (*engine.Engine, *storage.DB, string) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eng := engine.NewWithDB(cfg, db)
	t.Cleanup(eng.Close)
	return eng, db, dir
}

// ingestInto registers the vault name, ingests one distinctive document into it
// via a standalone pipeline (fakeEmbed — no Ollama), and drains. The Pebble-
// backed FTS persists postings so the engine's lazy LoadIndex re-seeds them.
func ingestInto(t *testing.T, db *storage.DB, vault, dir, body string) {
	t.Helper()
	ws := db.ResolveVaultPrefix(vault)
	if err := db.WriteVaultName(ws, vault); err != nil {
		t.Fatalf("write vault name %q: %v", vault, err)
	}
	p := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	src := filepath.Join(dir, vault+".txt")
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if _, err := p.Ingest(context.Background(), ws, src, "*"); err != nil {
		t.Fatalf("ingest %q: %v", vault, err)
	}
	p.Close()
}

// keywordHits runs a fresh (uncached) keyword lookup so lifecycle mutations are
// always observed, never a stale cache hit. Returns the hit count.
func keywordHits(t *testing.T, eng *engine.Engine, vault, term string) int {
	t.Helper()
	res, err := eng.Query(context.Background(), vault, engine.QueryRequest{Query: term, Mode: "keyword", K: 10, NoCache: true})
	if err != nil {
		t.Fatalf("query %q in %q: %v", term, vault, err)
	}
	return len(res.Hits)
}

// vaultRegistered reports whether name is in the Pebble registry (0x1A/0x1B).
func vaultRegistered(t *testing.T, db *storage.DB, name string) bool {
	t.Helper()
	return db.VaultNameExists(name)
}

// TestRenameVault_MetadataOnly_DataPresent: rename is metadata-only — the
// wsPrefix is frozen, so no data key moves. A query under the new name returns
// the same hits, the document survives, and the registry is repointed (old name
// gone, new name present). Mirrors the storage-layer TestRenameVault_MetadataOnly
// contract at the engine surface (quickstart §3).
func TestRenameVault_MetadataOnly_DataPresent(t *testing.T) {
	eng, db, dir := lifecycleEngine(t)
	ingestInto(t, db, "work", dir, "solar tariff deficit battery inverter charge window peak grid import export\n")

	if got := keywordHits(t, eng, "work", "tariff"); got == 0 {
		t.Fatal("setup: expected a keyword hit in \"work\" before rename")
	}

	if err := eng.RenameVault(context.Background(), "work", "projects"); err != nil {
		t.Fatalf("RenameVault: %v", err)
	}

	// Data is present under the new name (ws unchanged → same postings).
	if got := keywordHits(t, eng, "projects", "tariff"); got == 0 {
		t.Error("after rename: expected hits under \"projects\", got none (data should not move)")
	}
	// The document survives the rename (ListDocuments reads the Document prefix,
	// which carries ws, not the name).
	docs, err := eng.ListDocuments("projects", engine.ListDocumentsRequest{})
	if err != nil {
		t.Fatalf("ListDocuments after rename: %v", err)
	}
	if len(docs.Documents) != 1 {
		t.Errorf("after rename: want 1 doc under \"projects\", got %d", len(docs.Documents))
	}
	// Registry repointed: old name gone, new name present.
	if vaultRegistered(t, db, "work") {
		t.Error("after rename: \"work\" should be gone from the registry")
	}
	if !vaultRegistered(t, db, "projects") {
		t.Error("after rename: \"projects\" should be registered")
	}
}

// TestClearVault_TombstonesData_VaultStaysRegistered: clear range-tombstones
// every vault-scoped kind for the ws. Queries return empty and the in-memory
// index is evicted (re-seeded empty on next access), but the vault is still
// registered and immediately re-writable (registry keys preserved).
func TestClearVault_TombstonesData_VaultStaysRegistered(t *testing.T) {
	eng, db, dir := lifecycleEngine(t)
	ingestInto(t, db, "work", dir, "solar tariff deficit battery inverter charge window peak grid import export\n")

	if got := keywordHits(t, eng, "work", "tariff"); got == 0 {
		t.Fatal("setup: expected a keyword hit before clear")
	}
	if !vaultRegistered(t, db, "work") {
		t.Fatal("setup: \"work\" should be registered before clear")
	}

	if err := eng.ClearVault(context.Background(), "work"); err != nil {
		t.Fatalf("ClearVault: %v", err)
	}

	// Queries return nothing — FTS postings tombstoned, in-memory index evicted.
	if got := keywordHits(t, eng, "work", "tariff"); got != 0 {
		t.Errorf("after clear: want 0 hits, got %d", got)
	}
	// Document prefix (0x02) tombstoned too.
	docs, err := eng.ListDocuments("work", engine.ListDocumentsRequest{})
	if err != nil {
		t.Fatalf("ListDocuments after clear: %v", err)
	}
	if len(docs.Documents) != 0 {
		t.Errorf("after clear: want 0 docs, got %d", len(docs.Documents))
	}
	// The vault is STILL registered — clear keeps the 0x1A/0x1B keys.
	if !vaultRegistered(t, db, "work") {
		t.Error("after clear: \"work\" should still be registered (registry preserved)")
	}
}

// TestClearVault_OneVaultDoesNotTouchAnother: clearing one vault must not touch
// another vault's data. Two vaults share the SAME unified DB; clearing alpha
// leaves beta's docs and keyword hits intact (cross-vault isolation via the ws
// partition — the load-bearing safety property of the range-tombstone scope).
func TestClearVault_OneVaultDoesNotTouchAnother(t *testing.T) {
	eng, db, dir := lifecycleEngine(t)
	ingestInto(t, db, "alpha", dir, "solar tariff deficit battery inverter charge window peak grid\n")
	ingestInto(t, db, "beta", dir, "overnight shoulder offpeak grid import export meter\n")

	if got := keywordHits(t, eng, "alpha", "tariff"); got == 0 {
		t.Fatal("setup: expected a hit in \"alpha\" before clear")
	}
	if got := keywordHits(t, eng, "beta", "offpeak"); got == 0 {
		t.Fatal("setup: expected a hit in \"beta\" before clear")
	}

	if err := eng.ClearVault(context.Background(), "alpha"); err != nil {
		t.Fatalf("ClearVault alpha: %v", err)
	}

	// alpha is empty.
	if got := keywordHits(t, eng, "alpha", "tariff"); got != 0 {
		t.Errorf("after clear alpha: want 0 hits, got %d", got)
	}
	// beta is untouched — its ws partition was never in alpha's tombstone ranges.
	if got := keywordHits(t, eng, "beta", "offpeak"); got == 0 {
		t.Errorf("after clear alpha: \"beta\" hit lost — cross-vault isolation broken (want >=1, got %d)", got)
	}
	if !vaultRegistered(t, db, "beta") {
		t.Error("after clear alpha: \"beta\" should still be registered")
	}
}

// TestDeleteVault_GoneFromRegistryAndQueries: delete = clear + registry drop.
// The vault disappears from the Pebble registry (0x1B point-deleted) and queries
// return empty (data tombstoned).
func TestDeleteVault_GoneFromRegistryAndQueries(t *testing.T) {
	eng, db, dir := lifecycleEngine(t)
	ingestInto(t, db, "disposable", dir, "solar tariff deficit battery inverter charge window peak grid\n")

	if got := keywordHits(t, eng, "disposable", "tariff"); got == 0 {
		t.Fatal("setup: expected a keyword hit before delete")
	}
	if !vaultRegistered(t, db, "disposable") {
		t.Fatal("setup: \"disposable\" should be registered before delete")
	}

	if err := eng.DeleteVault(context.Background(), "disposable"); err != nil {
		t.Fatalf("DeleteVault: %v", err)
	}

	// Registry dropped → vault gone.
	if vaultRegistered(t, db, "disposable") {
		t.Error("after delete: \"disposable\" should NOT be registered")
	}
	// Data tombstoned → queries empty.
	if got := keywordHits(t, eng, "disposable", "tariff"); got != 0 {
		t.Errorf("after delete: want 0 hits, got %d", got)
	}
}

// TestLifecycle_Validation: empty/blank names are rejected before any storage
// write (ErrInvalid), matching the engine's other name-validating surfaces.
func TestLifecycle_Validation(t *testing.T) {
	eng, db, dir := lifecycleEngine(t)
	ingestInto(t, db, "work", dir, "solar tariff deficit battery inverter\n")
	ctx := context.Background()
	for _, bad := range []string{"", "   "} {
		if err := eng.RenameVault(ctx, bad, "x"); !errors.Is(err, engine.ErrInvalid) {
			t.Errorf("RenameVault(%q): err=%v, want ErrInvalid", bad, err)
		}
		if err := eng.ClearVault(ctx, bad); !errors.Is(err, engine.ErrInvalid) {
			t.Errorf("ClearVault(%q): err=%v, want ErrInvalid", bad, err)
		}
		if err := eng.DeleteVault(ctx, bad); !errors.Is(err, engine.ErrInvalid) {
			t.Errorf("DeleteVault(%q): err=%v, want ErrInvalid", bad, err)
		}
	}
}
