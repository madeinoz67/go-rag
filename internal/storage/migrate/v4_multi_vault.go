// v4 multi-vault key-widening (spec 052 / v2.0 storage model).
//
// The v4 step collapses go-rag's legacy N per-vault Pebble DBs (one under
// ~/.go-rag/vaults/<name>/data per vault) into the single unified store, where
// every vault-scoped key widens from `kind(1)|payload` to `kind(1)|wsPrefix(8)|payload`.
//
// Unlike v1–v3 (in-DB reshapes), this step is filesystem-level: it reads each
// legacy per-vault DB and rewrites its keys into the unified store. The daemon
// therefore calls MergeLegacyVaults (the filesystem merge) BEFORE opening the
// unified store for service; the registered v4MultiVault marker then records
// that the v4 layout is active so the refuse-newer guard (FR-015) catches a
// downgraded binary.
//
// Activation: the v4 marker is REGISTERED in defaultMigrations at Version 4, but
// ExpectedVersion stays at 3 in this landing commit, so Run() skips it (Version >
// expected). The marker is a no-op until the atomic widening commit bumps
// ExpectedVersion 3→4 alongside T004–T005 (storage widening + engine threading).
// That single-line bump is the switch that activates the v4 layout; until it
// lands, the un-widened engine keeps reading the v3 layout. MergeLegacyVaults
// (the filesystem merge) is called by the daemon pre-open when that bump ships.
//
// Idempotent + crash-safe: RewriteLegacyVault checks the vault's VaultMeta key
// and skips a vault that has already been migrated, so a crash before the
// version-key advance is replayed safely (the same legacy DB is re-read; the
// same widened keys land; the registry already exists and is left as-is).
// Legacy per-vault directories are archived (renamed to <name>.prev), never
// deleted — the migration is one-way but reversible by hand.

package migrate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cockroachdb/pebble"

	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// legacyVaultDataName is the Pebble directory name inside a legacy per-vault
// directory. Current layout: ~/.go-rag/vaults/<name>/data.
const legacyVaultDataName = "data"

// migrateBatchSize bounds the in-memory batch used by RewriteLegacyVault. Each
// widened key is small; a few thousand per commit keeps memory flat while
// bounding crash-recovery replay cost.
const migrateBatchSize = 4096

// VaultMigrationStats reports what RewriteLegacyVault did to one legacy vault.
type VaultMigrationStats struct {
	Vault         string // vault name (legacy directory name)
	Widened       int    // vault-scoped keys rewritten as kind|ws|payload
	SkippedGlobal int    // keys left flat (schema/config/auth) — not widened
}

// MergeReport aggregates MergeLegacyVaults across every legacy per-vault DB.
type MergeReport struct {
	Vaults   []VaultMigrationStats
	Archived []string // legacy directories renamed to .prev
}

// ErrNoLegacyVaults is returned by MergeLegacyVaults when the legacy root is
// absent or empty — the common case for a fresh install that starts at v4.
var ErrNoLegacyVaults = errors.New("no legacy per-vault databases to migrate")

// migrateLogger suppresses Pebble's chatty Info logs during the merge (mirrors
// storage.quietLogger, kept local so the migrate package does not import storage
// for a logger — storage is imported only for the prefix table).
type migrateLogger struct{}

func (migrateLogger) Infof(_ string, _ ...any) {}
func (migrateLogger) Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pebble: "+format+"\n", args...)
}

// RewriteLegacyVault copies every vault-scoped key from legacyDB into unified
// with ws prepended after the kind byte, and writes the vault's two registry
// keys (VaultMeta 0x1A|ws→name, VaultNameIndex 0x1B|siphash(name)→ws). Values
// are opaque byte slices — no decoding — so the migration is shape-agnostic and
// cannot misinterpret a payload. Idempotent: if unified already holds a
// VaultMeta key for ws, the vault is treated as already migrated and its keys
// are not re-copied (a crash before the version advance is replayed safely).
//
// Keys whose kind is NOT in storage.VaultScopedKinds (schema-version 0xFF,
// config 0x09, auth 0x17–0x19) are counted as SkippedGlobal and left out — they
// are instance-wide in the unified store, not per-vault, and a legacy per-vault
// DB's copy of them is not authoritative.
func RewriteLegacyVault(unified, legacyDB *pebble.DB, vaultName string) (*VaultMigrationStats, error) {
	ws := keys.VaultPrefix(vaultName)
	stats := &VaultMigrationStats{Vault: vaultName}

	// Idempotency: a VaultMeta entry for ws means this vault already landed in a
	// prior (possibly crashed) run. Re-copying would duplicate nothing (widened
	// keys overwrite identical widened keys), but skipping avoids the work and
	// makes the stats honest.
	if _, closer, err := unified.Get(keys.VaultMetaKey(ws)); err == nil {
		closer.Close()
		stats.Vault = vaultName + " (already migrated)"
		return stats, nil
	}

	// Write the registry keys first so the vault is discoverable by name even if
	// the key-copy below is interrupted (a partial-data vault is still listable
	// and re-runnable).
	regBatch := unified.NewBatch()
	if err := regBatch.Set(keys.VaultMetaKey(ws), []byte(vaultName), nil); err != nil {
		regBatch.Close()
		return nil, fmt.Errorf("write registry for %q: %w", vaultName, err)
	}
	if err := regBatch.Set(keys.VaultNameIndexKey(vaultName), ws[:], nil); err != nil {
		regBatch.Close()
		return nil, fmt.Errorf("write registry for %q: %w", vaultName, err)
	}
	if err := regBatch.Commit(pebble.Sync); err != nil {
		regBatch.Close()
		return nil, fmt.Errorf("write registry for %q: %w", vaultName, err)
	}
	regBatch.Close()

	scoped := make(map[byte]struct{}, len(storage.VaultScopedKinds))
	for _, k := range storage.VaultScopedKinds {
		scoped[k] = struct{}{}
	}

	iter, err := legacyDB.NewIter(nil)
	if err != nil {
		return nil, fmt.Errorf("scan legacy %q: %w", vaultName, err)
	}
	batch := unified.NewBatch()
	pending := 0
	flush := func() error {
		if pending == 0 {
			return nil
		}
		if err := batch.Commit(pebble.Sync); err != nil {
			return fmt.Errorf("commit widened batch for %q: %w", vaultName, err)
		}
		batch.Close()
		batch = unified.NewBatch()
		pending = 0
		return nil
	}

	for valid := iter.First(); valid; valid = iter.Next() {
		// iter.Key() is invalidated on Next — copy for the batch.
		srcKey := append([]byte(nil), iter.Key()...)
		val := append([]byte(nil), iter.Value()...)
		if len(srcKey) < 1 {
			continue
		}
		kind := srcKey[0]
		if _, ok := scoped[kind]; !ok {
			stats.SkippedGlobal++
			continue
		}
		// Widened shape: kind | ws(8) | srcKey[1:].
		widened := make([]byte, 1+8+len(srcKey)-1)
		widened[0] = kind
		copy(widened[1:9], ws[:])
		copy(widened[9:], srcKey[1:])
		if err := batch.Set(widened, val, nil); err != nil {
			iter.Close()
			batch.Close()
			return nil, fmt.Errorf("set widened key: %w", err)
		}
		stats.Widened++
		pending++
		if pending >= migrateBatchSize {
			if err := flush(); err != nil {
				iter.Close()
				return nil, err
			}
		}
	}
	if err := iter.Close(); err != nil {
		batch.Close()
		return nil, fmt.Errorf("close legacy iterator %q: %w", vaultName, err)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	batch.Close()
	return stats, nil
}

// MergeLegacyVaults opens (or creates) the unified store at unifiedPath, iterates
// every legacy per-vault DB under legacyRoot (`<name>/data`), rewrites each into
// the unified store via RewriteLegacyVault, and archives each legacy directory
// to `<name>.prev`. Returns ErrNoLegacyVaults when legacyRoot is absent or holds
// no per-vault DBs (the fresh-install path).
//
// The unified store is opened with the same quiet logger as storage.Open; the
// caller owns closing considerations (the daemon re-opens it for service after
// this returns). Legacy DBs are opened read-only so an in-flight daemon cannot
// mutate them mid-merge.
func MergeLegacyVaults(unifiedPath, legacyRoot string) (*MergeReport, error) {
	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoLegacyVaults
		}
		return nil, fmt.Errorf("read legacy root %q: %w", legacyRoot, err)
	}

	// Collect candidate vault dirs up front so we can fail fast with no partial
	// work if there is nothing to migrate.
	type candidate struct {
		name string
		path string
	}
	var cands []candidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) == ".prev" || name == ".DS_Store" {
			continue
		}
		dataDir := filepath.Join(legacyRoot, name, legacyVaultDataName)
		if _, err := os.Stat(dataDir); err != nil {
			continue // no data subdir — not a vault DB
		}
		cands = append(cands, candidate{name: name, path: dataDir})
	}
	if len(cands) == 0 {
		return nil, ErrNoLegacyVaults
	}

	if err := os.MkdirAll(filepath.Dir(unifiedPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir unified parent: %w", err)
	}
	unified, err := pebble.Open(unifiedPath, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		return nil, fmt.Errorf("open unified %q: %w", unifiedPath, err)
	}
	defer unified.Close()

	report := &MergeReport{}
	for _, c := range cands {
		legacyDB, err := pebble.Open(c.path, &pebble.Options{Logger: migrateLogger{}, ReadOnly: true})
		if err != nil {
			return nil, fmt.Errorf("open legacy vault %q: %w", c.name, err)
		}
		stats, err := RewriteLegacyVault(unified, legacyDB, c.name)
		// Close the read-only handle before archiving (Pebble holds a file lock).
		if cerr := legacyDB.Close(); cerr != nil {
			return nil, fmt.Errorf("close legacy vault %q: %w", c.name, cerr)
		}
		if err != nil {
			return nil, err
		}
		report.Vaults = append(report.Vaults, *stats)

		// Archive the legacy directory (rename to <name>.prev). Never delete —
		// one-way but hand-reversible. If a .prev already exists (a prior
		// interrupted merge), remove the older archive first.
		legacyDir := filepath.Join(legacyRoot, c.name)
		archive := filepath.Join(legacyRoot, c.name+".prev")
		if _, err := os.Stat(archive); err == nil {
			_ = os.RemoveAll(archive)
		}
		if err := os.Rename(legacyDir, archive); err != nil {
			return nil, fmt.Errorf("archive legacy vault %q: %w", c.name, err)
		}
		report.Archived = append(report.Archived, archive)
	}
	return report, nil
}

// v4MultiVault is the registered Migration.Up marker for the v4 layout. The
// actual key-rewrite is filesystem-level (MergeLegacyVaults, called by the
// daemon pre-open); this marker exists so the schema-version key advances to 4
// once the unified store is live, arming the refuse-newer guard (FR-015).
//
// It is a no-op on the DB: by the time the migration runner fires,
// MergeLegacyVaults has already widened every key. Asserting the registry is
// non-empty on a migrated store would be wrong for fresh installs (empty store).
//
// Registered at Version 4 in defaultMigrations (migrate.go) but inactive while
// ExpectedVersion == 3 — see the Activation note atop this file.
func v4MultiVault(_ *pebble.DB) error { return nil }
