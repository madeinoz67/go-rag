package storage

import (
	"bytes"
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// TestVaultPrefixDeterministic pins the SipHash contract: the same name yields
// the same ws in any call, and two distinct names yield distinct ws. This is
// the property that makes the registry a PRF-backed identity.
func TestVaultPrefixDeterministic(t *testing.T) {
	a1 := keys.VaultPrefix("alpha")
	a2 := keys.VaultPrefix("alpha")
	b := keys.VaultPrefix("beta")
	if a1 != a2 {
		t.Fatalf("VaultPrefix not deterministic: %x vs %x", a1, a2)
	}
	if a1 == b {
		t.Fatalf("distinct names collided: alpha==beta %x", a1)
	}
}

// TestIncrementWSPrefix covers the carry-forward increment used as the
// exclusive upper bound of per-vault ranges, including the all-0xFF overflow.
func TestIncrementWSPrefix(t *testing.T) {
	ws := keys.VaultPrefix("alpha")
	next, err := keys.IncrementWSPrefix(ws)
	if err != nil {
		t.Fatalf("increment: %v", err)
	}
	if next == ws {
		t.Fatal("increment did not change ws")
	}
	// next must be strictly greater (BigEndian) so [ws, next) is a valid range.
	if !bytesLess(ws[:], next[:]) {
		t.Fatalf("next %x not greater than ws %x", next, ws)
	}
	// All-0xFF overflows and fails closed.
	var full [8]byte
	for i := range full {
		full[i] = 0xFF
	}
	if _, err := keys.IncrementWSPrefix(full); err == nil {
		t.Fatal("expected overflow error for all-0xFF ws")
	}
}

func bytesLess(a, b []byte) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// TestWriteVaultNameAndList covers idempotent registration + listing. Writing
// the same vault twice must not duplicate the registry entry.
func TestWriteVaultNameAndList(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	wsAlpha := keys.VaultPrefix("alpha")
	wsBeta := keys.VaultPrefix("beta")
	if err := db.WriteVaultName(wsAlpha, "alpha"); err != nil {
		t.Fatalf("write alpha: %v", err)
	}
	if err := db.WriteVaultName(wsBeta, "beta"); err != nil {
		t.Fatalf("write beta: %v", err)
	}
	// Idempotent: second write of alpha is a no-op (no error, no duplicate).
	if err := db.WriteVaultName(wsAlpha, "alpha"); err != nil {
		t.Fatalf("rewrite alpha: %v", err)
	}

	names, err := db.ListVaultNames()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !containsAll(names, "alpha", "beta") {
		t.Fatalf("list missing vaults: %v", names)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 vaults, got %d (%v)", len(names), names)
	}
}

// TestVaultNameExists covers the registry point-get used by rename's collision
// check and by fail-closed transport resolution.
func TestVaultNameExists(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	if db.VaultNameExists("alpha") {
		t.Fatal("alpha should not exist before write")
	}
	ws := keys.VaultPrefix("alpha")
	if err := db.WriteVaultName(ws, "alpha"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !db.VaultNameExists("alpha") {
		t.Fatal("alpha should exist after write")
	}
}

// TestResolveVaultPrefix_LRUAndPebble covers the three resolution tiers:
// (1) LRU hit after WriteVaultName, (2) Pebble hit after cache eviction, and
// (3) SipHash fallback for a never-registered name.
func TestResolveVaultPrefix_LRUAndPebble(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	ws := keys.VaultPrefix("registered")
	if err := db.WriteVaultName(ws, "registered"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// (1) LRU hit — returns the persisted ws.
	if got := db.ResolveVaultPrefix("registered"); got != ws {
		t.Fatalf("LRU resolve: got %x want %x", got, ws)
	}
	// (2) Pebble hit — evict from cache, resolve must still return ws via 0x1B.
	db.vaultPrefixCache.Remove("registered")
	if got := db.ResolveVaultPrefix("registered"); got != ws {
		t.Fatalf("Pebble resolve: got %x want %x", got, ws)
	}
	// Cache was repopulated by the cold path.
	if got, ok := db.vaultPrefixCache.Get("registered"); !ok || got != ws {
		t.Fatalf("cold path did not repopulate cache: ok=%v ws=%x", ok, got)
	}

	// (3) SipHash fallback — a never-registered name resolves to keys.VaultPrefix.
	fresh := "never-registered"
	want := keys.VaultPrefix(fresh)
	if got := db.ResolveVaultPrefix(fresh); got != want {
		t.Fatalf("fallback resolve: got %x want %x", got, want)
	}
}

// TestRenameVault_MetadataOnly is the load-bearing rename test. It writes a
// Document key under ws, renames the vault, and verifies:
//   - the data key is byte-for-byte unchanged at the same Pebble key (no data
//     moved — rename touched only the two registry keys);
//   - the new name resolves to the SAME ws;
//   - the old name is no longer registered.
func TestRenameVault_MetadataOnly(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	ws := keys.VaultPrefix("alpha")
	if err := db.WriteVaultName(ws, "alpha"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Place a vault-scoped Document key under ws.
	docKey := keys.DocumentKey(ws, "doc1")
	if err := db.Set(docKey, []byte("body")); err != nil {
		t.Fatalf("set doc: %v", err)
	}

	if err := db.RenameVault(ws, "alpha", "beta"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Data key unchanged — zero data moved.
	got, ok, err := db.Get(docKey)
	if err != nil || !ok || !bytes.Equal(got, []byte("body")) {
		t.Fatalf("data moved during rename: ok=%v body=%q err=%v", ok, got, err)
	}
	// New name resolves to the SAME ws (the frozen creation prefix).
	if res := db.ResolveVaultPrefix("beta"); res != ws {
		t.Fatalf("resolve beta: got %x want %x (rename must not rehash data)", res, ws)
	}
	// Old name is gone from the registry.
	if db.VaultNameExists("alpha") {
		t.Fatal("alpha still registered after rename")
	}
	if !db.VaultNameExists("beta") {
		t.Fatal("beta not registered after rename")
	}
}

// TestRenameVault_CollisionRejected verifies a rename to a name that already
// belongs to another vault fails closed.
func TestRenameVault_CollisionRejected(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	wsA := keys.VaultPrefix("alpha")
	wsB := keys.VaultPrefix("beta")
	if err := db.WriteVaultName(wsA, "alpha"); err != nil {
		t.Fatalf("write alpha: %v", err)
	}
	if err := db.WriteVaultName(wsB, "beta"); err != nil {
		t.Fatalf("write beta: %v", err)
	}
	if err := db.RenameVault(wsA, "alpha", "beta"); err == nil {
		t.Fatal("rename to existing name should fail")
	}
}

// TestRenameVault_MismatchRejected verifies a rename with the wrong oldName
// fails closed (the caller's view of the current name must match the store's).
func TestRenameVault_MismatchRejected(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	ws := keys.VaultPrefix("alpha")
	if err := db.WriteVaultName(ws, "alpha"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := db.RenameVault(ws, "WRONG", "beta"); err == nil {
		t.Fatal("rename with wrong oldName should fail")
	}
}

// TestRenameVault_RenameSafety pins research R4: after a rename, resolving the
// new name via the COLD path (cache cleared) must still return the original ws,
// NOT keys.VaultPrefix(newName). The 0x1B index — not the SipHash fallback — is
// authoritative for registered vaults. Without this, a renamed vault would
// silently address a different ws and its data would appear to vanish.
func TestRenameVault_RenameSafety(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	ws := keys.VaultPrefix("alpha")
	if err := db.WriteVaultName(ws, "alpha"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := db.RenameVault(ws, "alpha", "beta"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// The naive SipHash of the new name differs from the frozen ws.
	if naive := keys.VaultPrefix("beta"); naive == ws {
		t.Fatal("precondition: siphash(beta) should differ from ws (else test is vacuous)")
	}
	// Evict every cache entry so resolution must consult 0x1B.
	db.vaultPrefixCache.Remove("beta")
	if got := db.ResolveVaultPrefix("beta"); got != ws {
		t.Fatalf("cold resolve after rename: got %x want %x (rename safety violated)", got, ws)
	}
}

// TestBackfillVaultNames covers the startup sweep that names legacy data: a
// Document key written under a ws with no VaultMeta must gain a placeholder
// name and become listable.
func TestBackfillVaultNames(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	ws := keys.VaultPrefix("orphan")
	// Write a vault-scoped Document key WITHOUT registering the vault — this
	// simulates migrated legacy data whose registry pass has not run.
	docKey := keys.DocumentKey(ws, "doc1")
	if err := db.Set(docKey, []byte("body")); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := db.BackfillVaultNames(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	names, err := db.ListVaultNames()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("expected 1 backfilled vault, got %d (%v)", len(names), names)
	}
	// Placeholder name and resolution match the ws.
	if got := db.ResolveVaultPrefix(names[0]); got != ws {
		t.Fatalf("resolve placeholder %q: got %x want %x", names[0], got, ws)
	}

	// Idempotent: a second backfill adds nothing.
	if err := db.BackfillVaultNames(); err != nil {
		t.Fatalf("backfill 2: %v", err)
	}
	names2, _ := db.ListVaultNames()
	if len(names2) != 1 {
		t.Fatalf("backfill not idempotent: %d (%v)", len(names2), names2)
	}
}

// TestSeedVaultPrefixCache covers the startup seed: after seeding, resolving a
// vault never touched this session hits the cache (hot path), and a renamed
// vault resolves to its frozen ws via the seeded entry.
func TestSeedVaultPrefixCache(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	ws := keys.VaultPrefix("alpha")
	if err := db.WriteVaultName(ws, "alpha"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := db.RenameVault(ws, "alpha", "beta"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// Simulate a fresh process: drop the whole cache, then seed from VaultMeta.
	db.vaultPrefixCache = newVaultCache(vaultCacheCapacity)
	if err := db.SeedVaultPrefixCache(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The seeded cache has the CURRENT name (beta) → frozen ws. A SipHash
	// fallback here would return keys.VaultPrefix("beta") != ws.
	if got := db.ResolveVaultPrefix("beta"); got != ws {
		t.Fatalf("seeded resolve: got %x want %x", got, ws)
	}
	// Confirm it was a cache hit (hot path), not a Pebble read.
	if _, ok := db.vaultPrefixCache.Get("beta"); !ok {
		t.Fatal("seed did not populate cache for beta")
	}
}

// TestDeleteVaultNameOnly covers the registry-only delete (the final step of
// DeleteVault after ClearVault has tombstoned the data).
func TestDeleteVaultNameOnly(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()

	ws := keys.VaultPrefix("alpha")
	if err := db.WriteVaultName(ws, "alpha"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := db.DeleteVaultNameOnly(ws, "alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if db.VaultNameExists("alpha") {
		t.Fatal("alpha still registered after DeleteVaultNameOnly")
	}
	names, _ := db.ListVaultNames()
	if len(names) != 0 {
		t.Fatalf("expected 0 vaults, got %v", names)
	}
}

func containsAll(haystack []string, needles ...string) bool {
	for _, n := range needles {
		found := false
		for _, h := range haystack {
			if h == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
