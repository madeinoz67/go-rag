package engine

// vault_lifecycle.go (spec 052 / US3 / T019): the three vault-lifecycle
// operations on the unified store — rename (metadata-only), clear (range-
// tombstone the data, keep the registry), delete (clear + drop the registry).
//
// All three operate on the engine's own unified Pebble store: every vault's
// data lives under a fixed 8-byte wsPrefix (frozen at the vault's first write),
// so the in-memory index registries (idxFts/idxVec/epoch, all keyed by ws) need
// no re-keying on rename — only the persisted name→ws registry keys move. Clear
// tombstones every vault-scoped kind for one ws in 19 O(1) range writes; delete
// follows clear with the two registry point-deletes (0x1A + 0x1B).

import (
	"context"
	"fmt"
	"strings"

	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
	vaultpkg "github.com/madeinoz67/go-rag/internal/vault"
)

// CreateVault registers a new empty vault in the unified store. Per the
// registry's design a vault is implicitly created on first write (WriteVaultName);
// this makes that explicit so the vault appears in listings immediately — empty,
// switchable, and writable — without a document. Validates the name (vaultpkg's
// canonical rule: lowercase alphanumeric + hyphens, 1–64), refuses a duplicate,
// then resolves the prefix + writes the registry entry. No document is needed
// and no on-disk directory is created (the unified store holds all vaults).
func (e *Engine) CreateVault(_ context.Context, name string) error {
	name = strings.TrimSpace(name)
	if err := vaultpkg.ValidateName(name); err != nil {
		return fmt.Errorf("create vault: %w: %v", ErrInvalid, err)
	}
	if e.db.VaultNameExists(name) {
		return fmt.Errorf("create vault %q: %w: already exists", name, ErrInvalid)
	}
	ws := e.db.ResolveVaultPrefix(name)
	if err := e.db.WriteVaultName(ws, name); err != nil {
		return fmt.Errorf("create vault %q: %w", name, err)
	}
	return nil
}

// RenameVault renames a vault in-place: metadata-only, sub-millisecond, zero
// data moves. The wsPrefix is frozen at the vault's creation, so the persisted
// VaultMeta (0x1A ws→name) and VaultNameIndex (0x1B siphash(name)→ws) keys are
// rewritten while every data key (which carries ws, not the name) stays put.
// The engine's in-memory index registries are ws-keyed, so they are untouched;
// the store's name→ws cache is refreshed inside store.RenameVault. Cached query
// results remain valid (their cache key carries ws + epoch, neither of which
// changes), so a query under the new name returns the same hits the old name
// would have — the quickstart §3 "rename → query → results present" contract.
func (e *Engine) RenameVault(_ context.Context, oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("rename vault: old and new names are required: %w", ErrInvalid)
	}
	if err := vaultpkg.ValidateName(newName); err != nil {
		return fmt.Errorf("rename vault: %w: %v", ErrInvalid, err)
	}
	if !e.db.VaultNameExists(oldName) {
		return fmt.Errorf("rename vault %q: %w: not found", oldName, ErrNotFound)
	}
	if oldName != newName && e.db.VaultNameExists(newName) {
		return fmt.Errorf("rename vault: %q: %w: already exists", newName, ErrInvalid)
	}
	ws := e.db.ResolveVaultPrefix(oldName)
	if err := e.db.RenameVault(ws, oldName, newName); err != nil {
		return fmt.Errorf("rename vault: %w", err)
	}
	// ws is frozen → idxFts/idxVec/epoch maps (keyed by ws) are unchanged. The
	// store's vaultPrefixCache was repointed oldName→newName inside RenameVault.
	return nil
}

// ClearVault drops every vault-scoped datum for one vault while keeping the
// vault registered (the registry keys 0x1A/0x1B are preserved, so the vault is
// still listed and immediately writable). Implemented as 19 Pebble range
// tombstones — one per vault-scoped kind (storage.VaultScopedKinds) — each
// covering `kind|ws` ≤ key < `kind|wsPlus`. The in-memory FTS/Vector indexes
// for the ws are evicted (re-seeded empty on next access), the per-vault epoch
// is reset to zero, and the query caches are flushed so no stale hits survive.
// The six global kinds (Config, Auth*, VaultMeta, VaultNameIndex) are
// untouched — clearing a vault never affects another vault or instance state.
func (e *Engine) ClearVault(_ context.Context, vault string) error {
	vault = strings.TrimSpace(vault)
	if vault == "" {
		return fmt.Errorf("clear vault: vault name is required: %w", ErrInvalid)
	}
	ws := e.db.ResolveVaultPrefix(vault)

	// 1. Range-tombstone every vault-scoped kind for this ws. Each DeleteRange
	//    is one O(1) Pebble range tombstone; the reclaimed keys are dropped by
	//    background compaction.
	for _, kind := range storage.VaultScopedKinds {
		lower, upper, err := keys.VaultKindRange(kind, ws)
		if err != nil {
			return fmt.Errorf("clear vault %q: kind %#x range: %w", vault, kind, err)
		}
		if err := e.db.DeleteRange(lower, upper); err != nil {
			return fmt.Errorf("clear vault %q: kind %#x tombstone: %w", vault, kind, err)
		}
	}

	// 2. Evict the in-memory FTS/Vector for this ws so the next query re-seeds
	//    from the (now empty) store rather than reading stale postings. Guard
	//    by idxMu to match the indexes() seed path's lock ordering.
	e.idxMu.Lock()
	delete(e.idxFts, ws)
	delete(e.idxVec, ws)
	e.idxMu.Unlock()

	// 3. Reset the per-vault epoch to zero (the vault is reborn empty). Store(0)
	//    on the existing pointer preserves pointer identity for any in-flight
	//    markIndexChanged caller that captured it before the clear; its Add(1)
	//    just re-invalidates, which is harmless post-flush.
	e.epochMu.Lock()
	if entry := e.epoch[ws]; entry != nil {
		entry.Store(0)
	}
	e.epochMu.Unlock()

	// 4. Drop cached results + query embeddings. The result cache is ws-keyed,
	//    so a surgical per-ws eviction isn't supported by the LRU; a clear is a
	//    rare operator action and a global flush (the same hammer Migrate uses)
	//    is the safe choice — no stale hit can survive the tombstone.
	e.flushCaches()

	return nil
}

// DeleteVault removes a vault entirely: ClearVault's data tombstones followed
// by DeleteVaultNameOnly's two registry point-deletes (VaultMeta 0x1A +
// VaultNameIndex 0x1B). After this the vault is gone from listings and
// queries; its wsPrefix is retired (a future vault of the same name would hash
// to the same ws and inherit an empty key space — safe because clear ran
// first). The on-disk vault registry directory is not touched here; that is a
// transport/CLI concern (vaultpkg.Delete removes the directory when called).
func (e *Engine) DeleteVault(ctx context.Context, vault string) error {
	vault = strings.TrimSpace(vault)
	if vault == "" {
		return fmt.Errorf("delete vault: vault name is required: %w", ErrInvalid)
	}
	// The default vault is always present (the daemon's bootstrap vault + the
	// universal fallback); it may be cleared but not deleted.
	if vault == "default" {
		return fmt.Errorf("delete vault: the default vault cannot be deleted: %w", ErrInvalid)
	}
	if err := e.ClearVault(ctx, vault); err != nil {
		return fmt.Errorf("delete vault %q: %w", vault, err)
	}
	ws := e.db.ResolveVaultPrefix(vault)
	if err := e.db.DeleteVaultNameOnly(ws, vault); err != nil {
		return fmt.Errorf("delete vault %q: registry: %w", vault, err)
	}
	return nil
}
