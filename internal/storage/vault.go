// vault registry (spec 052 / v2.0 storage model).
//
// The registry maps human-readable vault names to 8-byte workspace prefixes
// (wsPrefix) and back. Two GLOBAL Pebble prefixes hold it (no ws in the key —
// the registry records vaults and cannot be scoped by the prefix it maps):
//
//	VaultMeta      0x1A | ws[8]          → name string   (scan to list vaults)
//	VaultNameIndex 0x1B | siphash(name)  → ws[8]         (point-get to resolve)
//
// The 0x1B key is keyed by siphash(name), NOT by ws. This indirection is load-
// bearing: it makes RenameVault a two-key metadata operation (set 0x1A, set
// 0x1B(new), delete 0x1B(old)) instead of a full rehash of every data key. For
// a never-renamed vault siphash(name) == ws; the two diverge only after a rename.
//
// Ported near-verbatim from MuninnDB's internal/storage/vault.go (itself
// adversarially verified), adapted to go-rag's prefix table and key package.

package storage

import (
	"container/list"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// vaultCacheCapacity bounds the in-memory vault-name → wsPrefix cache. At
// single-operator scale the realistic vault count is in the tens; the bound is
// a safety valve, not a working ceiling.
const vaultCacheCapacity = 10_000

// vaultCacheEntry is one node in the vaultCache's doubly-linked list.
type vaultCacheEntry struct {
	name string
	ws   [8]byte
}

// vaultCache is a bounded, thread-safe LRU mapping vault name → wsPrefix. It is
// the hot path of ResolveVaultPrefix: once a name has been resolved (from the
// registry or computed), it is cached for the life of the process (subject to
// the bound). It is seeded at startup from VaultMeta (0x1A) so the SipHash
// fallback in ResolveVaultPrefix stays cold-only (rename safety, research R4).
type vaultCache struct {
	mu    sync.Mutex
	cap   int
	order *list.List               // front = most-recently-used
	elems map[string]*list.Element // name → list element
}

// newVaultCache returns a bounded LRU with the given capacity. A capacity <= 0
// disables eviction (unbounded), which is fine for tests.
func newVaultCache(capacity int) *vaultCache {
	return &vaultCache{
		cap:   capacity,
		order: list.New(),
		elems: make(map[string]*list.Element),
	}
}

// Get returns the wsPrefix for name and whether it was present. A hit promotes
// the entry to the front of the LRU.
func (c *vaultCache) Get(name string) ([8]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.elems[name]; ok {
		c.order.MoveToFront(e)
		return e.Value.(*vaultCacheEntry).ws, true
	}
	return [8]byte{}, false
}

// Add inserts or refreshes name → ws, evicting the least-recently-used entry
// when the bound is exceeded.
func (c *vaultCache) Add(name string, ws [8]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.elems[name]; ok {
		e.Value.(*vaultCacheEntry).ws = ws
		c.order.MoveToFront(e)
		return
	}
	if c.cap > 0 && c.order.Len() >= c.cap {
		if back := c.order.Back(); back != nil {
			delete(c.elems, back.Value.(*vaultCacheEntry).name)
			c.order.Remove(back)
		}
	}
	c.elems[name] = c.order.PushFront(&vaultCacheEntry{name: name, ws: ws})
}

// Remove evicts name from the cache (used by RenameVault to evict the old name).
// A missing name is a no-op.
func (c *vaultCache) Remove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.elems[name]; ok {
		c.order.Remove(e)
		delete(c.elems, name)
	}
}

// ResolveVaultPrefix maps a vault name to its 8-byte wsPrefix. Resolution order:
//  1. In-memory LRU (hot path — populated by WriteVaultName, SeedVaultPrefixCache,
//     and prior cold resolutions).
//  2. VaultNameIndex (0x1B | siphash(name)) point-get — if present, cache + return.
//  3. Fallback: keys.VaultPrefix(name) — CORRECT ONLY FOR NEVER-RENAMED VAULTS.
//
// The fallback exists so a brand-new vault can be written before its registry
// entry is persisted. For a renamed vault the fallback would return the WRONG
// prefix (the creation prefix is frozen; siphash(newName) differs), so the LRU
// is seeded from VaultMeta at startup and RenameVault refreshes the cache — the
// fallback is reached only for names that have never been registered, for which
// it is correct.
func (d *DB) ResolveVaultPrefix(name string) [8]byte {
	if d.vaultPrefixCache != nil {
		if ws, ok := d.vaultPrefixCache.Get(name); ok {
			return ws
		}
	}
	idxKey := keys.VaultNameIndexKey(name)
	val, closer, err := d.db.Get(idxKey)
	if err == nil {
		defer closer.Close()
		if len(val) == 8 {
			var ws [8]byte
			copy(ws[:], val)
			if d.vaultPrefixCache != nil {
				d.vaultPrefixCache.Add(name, ws)
			}
			return ws
		}
	}
	ws := keys.VaultPrefix(name)
	if d.vaultPrefixCache != nil {
		d.vaultPrefixCache.Add(name, ws)
	}
	return ws
}

// SeedVaultPrefixCache populates the in-memory cache from VaultMeta (0x1A). Each
// 0x1A entry is ws → name; inverting it into the cache makes name → ws available
// without a Pebble read on the hot path, and — critically — makes the cache
// authoritative for the CURRENT names so ResolveVaultPrefix never falls back to
// SipHash for a registered (possibly renamed) vault. Called once at startup.
func (d *DB) SeedVaultPrefixCache() error {
	if d.vaultPrefixCache == nil {
		return nil
	}
	iter, err := d.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte{PrefixVaultMeta},
		UpperBound: []byte{PrefixVaultMeta + 1},
	})
	if err != nil {
		return fmt.Errorf("seed vault cache: %w", err)
	}
	defer iter.Close()
	for valid := iter.First(); valid; valid = iter.Next() {
		k := iter.Key()
		if len(k) != 9 || k[0] != PrefixVaultMeta {
			continue
		}
		var ws [8]byte
		copy(ws[:], k[1:9])
		name := string(iter.Value())
		d.vaultPrefixCache.Add(name, ws)
	}
	return iter.Error()
}

// WriteVaultName persists the vault name under both registry keys if they are
// not already present:
//
//	0x1A | ws      → name   (VaultMeta — scan to list)
//	0x1B | siphash(name) → ws   (VaultNameIndex — resolve)
//
// Idempotent: a Pebble existence check on VaultMeta gates the write, and a
// session-local sync.Map short-circuits the check after the first write for a
// given ws. Safe to call on every ingestion. The first write for a vault name
// implicitly creates the vault (research R5 — CreateVault is implicit).
func (d *DB) WriteVaultName(ws [8]byte, name string) error {
	if _, ok := d.vaultNameWritten.Load(ws); ok {
		return nil
	}
	metaKey := keys.VaultMetaKey(ws)
	if _, closer, err := d.db.Get(metaKey); err == nil {
		closer.Close()
		d.vaultNameWritten.Store(ws, struct{}{})
		if d.vaultPrefixCache != nil {
			d.vaultPrefixCache.Add(name, ws)
		}
		return nil
	}
	batch := d.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(metaKey, []byte(name), nil); err != nil {
		return fmt.Errorf("write vault name: %w", err)
	}
	if err := batch.Set(keys.VaultNameIndexKey(name), ws[:], nil); err != nil {
		return fmt.Errorf("write vault name: %w", err)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("write vault name: %w", err)
	}
	d.vaultNameWritten.Store(ws, struct{}{})
	if d.vaultPrefixCache != nil {
		d.vaultPrefixCache.Add(name, ws)
	}
	return nil
}

// VaultNameExists reports whether a vault with the given name is registered
// (i.e. a VaultNameIndex key exists for it). Used by RenameVault's collision
// check and by fail-closed vault resolution in the transports.
func (d *DB) VaultNameExists(name string) bool {
	_, closer, err := d.db.Get(keys.VaultNameIndexKey(name))
	if err == nil {
		closer.Close()
		return true
	}
	return false
}

// ListVaultNames scans VaultMeta (0x1A | ws → name) and returns every known
// vault name. The scan range [0x1A, 0x1B) covers the registry and nothing else.
func (d *DB) ListVaultNames() ([]string, error) {
	iter, err := d.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte{PrefixVaultMeta},
		UpperBound: []byte{PrefixVaultNameIndex},
	})
	if err != nil {
		return nil, fmt.Errorf("list vault names: %w", err)
	}
	defer iter.Close()
	var names []string
	for valid := iter.First(); valid; valid = iter.Next() {
		k := iter.Key()
		if len(k) != 9 || k[0] != PrefixVaultMeta {
			continue
		}
		val := make([]byte, len(iter.Value()))
		copy(val, iter.Value())
		names = append(names, string(val))
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return names, nil
}

// RenameVault atomically renames a vault by updating the two registry keys only
// — zero data keys move, which is the whole point of the siphash(name) index
// indirection. The batch: set VaultMeta → newName, set VaultNameIndex(newName) →
// ws, delete VaultNameIndex(oldName). Fails closed if ws has no VaultMeta entry,
// if the stored name does not match oldName, or if newName already exists.
func (d *DB) RenameVault(ws [8]byte, oldName, newName string) error {
	metaKey := keys.VaultMetaKey(ws)
	val, closer, err := d.db.Get(metaKey)
	if err != nil {
		return fmt.Errorf("vault prefix not registered: %w", err)
	}
	stored := string(val)
	closer.Close()
	if stored != oldName {
		return fmt.Errorf("vault name mismatch: stored %q, expected %q", stored, oldName)
	}
	if newName == oldName {
		return nil
	}
	if d.VaultNameExists(newName) {
		return fmt.Errorf("vault name %q already exists", newName)
	}
	batch := d.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(metaKey, []byte(newName), nil); err != nil {
		return fmt.Errorf("rename vault: %w", err)
	}
	if err := batch.Set(keys.VaultNameIndexKey(newName), ws[:], nil); err != nil {
		return fmt.Errorf("rename vault: %w", err)
	}
	if err := batch.Delete(keys.VaultNameIndexKey(oldName), nil); err != nil {
		return fmt.Errorf("rename vault: %w", err)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("rename vault commit: %w", err)
	}
	if d.vaultPrefixCache != nil {
		d.vaultPrefixCache.Remove(oldName)
		d.vaultPrefixCache.Add(newName, ws)
	}
	// Force WriteVaultName to re-check on the next call for this ws, so a stale
	// oldName association cannot reappear.
	d.vaultNameWritten.Delete(ws)
	return nil
}

// DeleteVaultNameOnly removes the registry keys for a vault (VaultMeta +
// VaultNameIndex) without touching its data. Used by DeleteVault after
// ClearVault has tombstoned the data ranges. Point-deletes only — O(1) keys.
func (d *DB) DeleteVaultNameOnly(ws [8]byte, name string) error {
	_, closer, err := d.db.Get(keys.VaultMetaKey(ws))
	if err == nil {
		closer.Close()
	}
	nameIdx := keys.VaultNameIndexKey(name)
	batch := d.db.NewBatch()
	defer batch.Close()
	if err := batch.Delete(keys.VaultMetaKey(ws), nil); err != nil {
		return fmt.Errorf("delete vault name: %w", err)
	}
	if err := batch.Delete(nameIdx, nil); err != nil {
		return fmt.Errorf("delete vault name: %w", err)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("delete vault name: %w", err)
	}
	if d.vaultPrefixCache != nil {
		d.vaultPrefixCache.Remove(name)
	}
	d.vaultNameWritten.Delete(ws)
	return nil
}

// BackfillVaultNames scans the Document prefix (0x02), extracts the wsPrefix
// from bytes [1:9] of each key, and writes a placeholder name
// (`vault-<hex(ws[:4])>`) for any ws that has data but no VaultMeta entry.
// Called once on startup so legacy data written before vault-name persistence
// (or migrated from a pre-v2 per-vault DB without a registry pass) is
// discoverable by name. Safe to re-run: existing entries are left alone.
func (d *DB) BackfillVaultNames() error {
	seen := make(map[[8]byte]struct{})
	iter, err := d.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte{PrefixDocument},
		UpperBound: []byte{PrefixDocument + 1},
	})
	if err != nil {
		return fmt.Errorf("backfill vault names: %w", err)
	}
	for valid := iter.First(); valid; valid = iter.Next() {
		k := iter.Key()
		// Widened shape is `0x02 | ws(8) | docID` — at least 9 bytes. A legacy
		// (pre-v2) key would be shorter; those are handled by the v3→v4
		// migration before this runs, so anything shorter is skipped.
		if len(k) < 9 {
			continue
		}
		var ws [8]byte
		copy(ws[:], k[1:9])
		seen[ws] = struct{}{}
	}
	if err := iter.Close(); err != nil {
		return err
	}
	for ws := range seen {
		metaKey := keys.VaultMetaKey(ws)
		var name string
		val, closer, getErr := d.db.Get(metaKey)
		if getErr == nil {
			name = string(val)
			closer.Close()
		} else {
			name = fmt.Sprintf("vault-%x", ws[:4])
		}
		idxKey := keys.VaultNameIndexKey(name)
		if _, c, e := d.db.Get(idxKey); e == nil {
			c.Close()
			continue
		}
		batch := d.db.NewBatch()
		if getErr != nil {
			if err := batch.Set(metaKey, []byte(name), nil); err != nil {
				batch.Close()
				return fmt.Errorf("backfill vault %x: %w", ws, err)
			}
		}
		if err := batch.Set(idxKey, ws[:], nil); err != nil {
			batch.Close()
			return fmt.Errorf("backfill vault %x: %w", ws, err)
		}
		if err := batch.Commit(pebble.Sync); err != nil {
			batch.Close()
			return fmt.Errorf("backfill vault %x: %w", ws, err)
		}
		batch.Close()
	}
	return nil
}
