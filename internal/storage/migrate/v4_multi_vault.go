// v4 multi-vault key-widening (spec 052 / v2.0 storage model).
//
// The v4 step collapses go-rag's legacy N per-vault Pebble DBs (one under
// ~/.go-rag/vaults/<name>/data per vault) into the single unified store, where
// every vault-scoped key widens from `kind(1)|payload` to `kind(1)|wsPrefix(8)|payload`.
//
// Unlike v1–v3 (in-DB reshapes), this step is filesystem-level: it reads each
// legacy per-vault DB and rewrites its keys into the unified store. The merge
// runs INSIDE the v4 migration's Up (v4MultiVault), which fires from Run() when
// ExpectedVersion >= 4. Because Up receives the already-open unified *pebble.DB,
// the merge writes through that handle (no second open / lock conflict); only
// the legacy per-vault DBs are opened, read-only.
//
// The legacy root is supplied by the engine open path via SetLegacyRoot: it is
// set to vault.Root() only when the store being opened is a unified store (a
// --db-path OUTSIDE the legacy ~/.go-rag/vaults/ tree). On a legacy per-vault
// open the root is unset and v4MultiVault is a harmless no-op, so the
// currently-open per-vault DB is never re-opened as a legacy candidate (no
// self-merge lock conflict). A fresh install (no legacy dirs) yields
// ErrNoLegacyVaults, which v4MultiVault swallows — the unified store stays
// empty and vaults self-register on first write (WriteVaultName).
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
	"log/slog"
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

// legacyRoot is the filesystem root scanned for legacy per-vault DBs
// (<root>/<name>/data) during the v4 migration. It is set by the engine open
// path (SetLegacyRoot) when the store being opened is a unified store outside
// the legacy ~/.go-rag/vaults/ tree. When empty, v4MultiVault is a no-op — the
// fresh-install path and the legacy per-vault open path both leave it unset.
var legacyRoot string

// SetLegacyRoot configures the legacy per-vault root the v4 migration scans.
// The engine open path calls this with vault.Root() when opening a unified
// store (a --db-path outside the legacy vaults tree). Calling it with "" clears
// the setting (v4MultiVault becomes a no-op). It must be called BEFORE
// RunMigrations / Run for the v4 step to observe it.
func SetLegacyRoot(root string) { legacyRoot = root }

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

// legacyCandidate is a discovered legacy per-vault DB (name + its `data` path).
type legacyCandidate struct {
	name string
	path string
}

// legacyCandidates scans root for legacy per-vault DBs (`<name>/data`), skipping
// archived (.prev) and non-vault entries. Returns ErrNoLegacyVaults when root is
// empty, absent, or holds no per-vault DBs (the fresh-install path). This is the
// fail-fast gate: callers use it to short-circuit BEFORE opening or touching the
// unified store, so a "nothing to merge" result never contends on the Pebble
// lock (e.g. when a read-only verification handle is already held).
func legacyCandidates(root string) ([]legacyCandidate, error) {
	if root == "" {
		return nil, ErrNoLegacyVaults
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoLegacyVaults
		}
		return nil, fmt.Errorf("read legacy root %q: %w", root, err)
	}
	var cands []legacyCandidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) == ".prev" || name == ".DS_Store" {
			continue
		}
		dataDir := filepath.Join(root, name, legacyVaultDataName)
		if _, err := os.Stat(dataDir); err != nil {
			continue // no data subdir — not a vault DB
		}
		cands = append(cands, legacyCandidate{name: name, path: dataDir})
	}
	if len(cands) == 0 {
		return nil, ErrNoLegacyVaults
	}
	return cands, nil
}

// mergeLegacyCandidates rewrites each candidate legacy DB into the already-open
// unified store via RewriteLegacyVault and archives each legacy directory to
// `<name>.prev`. Called with a non-empty candidate list (legacyCandidates has
// already gated the empty case). Legacy DBs are opened read-only so an in-flight
// daemon cannot mutate them mid-merge.
func mergeLegacyCandidates(unified *pebble.DB, cands []legacyCandidate, root string) (*MergeReport, error) {
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
		legacyDir := filepath.Join(root, c.name)
		archive := filepath.Join(root, c.name+".prev")
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

// mergeLegacy is the core filesystem merge against an already-open unified
// store. It scans root for legacy per-vault DBs, rewrites each into unified, and
// archives each legacy directory to `<name>.prev`. Returns ErrNoLegacyVaults
// when root is empty, absent, or holds no per-vault DBs (the fresh-install path).
//
// This is the path the v4 migration's Up (v4MultiVault) takes: the unified
// store is already open and passed in, so the merge writes through that single
// handle. The standalone MergeLegacyVaults below wraps it for callers (and
// tests) that own only a path.
func mergeLegacy(unified *pebble.DB, root string) (*MergeReport, error) {
	cands, err := legacyCandidates(root)
	if err != nil {
		return nil, err
	}
	return mergeLegacyCandidates(unified, cands, root)
}

// MergeLegacyVaults opens (or creates) the unified store at unifiedPath, runs
// the filesystem merge against it, and closes it. Returns ErrNoLegacyVaults
// when root is absent or holds no per-vault DBs (the fresh-install path) — and
// crucially returns it WITHOUT opening the unified store, so a "nothing to
// merge" result never contends on the Pebble lock. This is the standalone /
// path-based entry point (used by tests); the v4 migration's Up uses mergeLegacy
// directly against the already-open store to avoid a second open of the same path.
func MergeLegacyVaults(unifiedPath, root string) (*MergeReport, error) {
	// Gate BEFORE opening: a "nothing to merge" result must return without ever
	// opening the unified store, so it never contends on the Pebble lock (e.g.
	// when a read-only verification handle is already held — see TestMergeLegacyVaults).
	cands, err := legacyCandidates(root)
	if err != nil {
		return nil, err // ErrNoLegacyVaults — no open attempted
	}
	if err := os.MkdirAll(filepath.Dir(unifiedPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir unified parent: %w", err)
	}
	unified, err := pebble.Open(unifiedPath, &pebble.Options{Logger: migrateLogger{}})
	if err != nil {
		return nil, fmt.Errorf("open unified %q: %w", unifiedPath, err)
	}
	defer unified.Close()
	return mergeLegacyCandidates(unified, cands, root)
}

// v4MultiVault is the registered Migration.Up for the v4 layout. It performs
// the filesystem merge of legacy per-vault DBs into the already-open unified
// store via mergeLegacy. The legacy root is supplied out-of-band via
// SetLegacyRoot (called by the engine open path when opening a unified store);
// when unset (fresh install, or a legacy per-vault open) it is a no-op.
//
// It writes through the single *pebble.DB handed to Up — no second open of the
// unified store, hence no Pebble lock conflict. Only the legacy per-vault DBs
// are opened, read-only. ErrNoLegacyVaults (no/empty legacy root) is swallowed:
// the common fresh-install case starts with an empty unified store and vaults
// self-register on first write (WriteVaultName); advancing the schema-version
// key to 4 still arms the refuse-newer guard (FR-015).
func v4MultiVault(db *pebble.DB) error {
	report, err := mergeLegacy(db, legacyRoot)
	if err == nil {
		slog.Info("v4 multi-vault merge", "vaults", len(report.Vaults), "archived", len(report.Archived))
		return nil
	}
	if errors.Is(err, ErrNoLegacyVaults) {
		return nil // fresh install or per-vault open — nothing to merge
	}
	return fmt.Errorf("v4 multi-vault merge: %w", err)
}
