package migrate

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"sort"

	"github.com/cockroachdb/pebble"
)

// schemaVersionKey is the global Pebble key holding the last-applied schema
// version (big-endian uint64). The 0xFF prefix is reserved for store metadata —
// it sits outside the 0x01–0x15 data-prefix range in internal/storage/storage.go,
// so it never collides with data keys. Absence of the key is treated as version 0,
// which is the v0→v1 bootstrap (the release introducing schema versioning migrates
// every pre-versioning store automatically). Mirrors MuninnDB's migrationVersionKey.
var schemaVersionKey = []byte{0xFF, 's', 'c', 'h', 'e', 'm', 'a', '_', 'v', 'e', 'r'}

// ExpectedVersion is the schema version this binary understands. Stores below
// this are migrated up on open; stores above this are refused (FR-015/R9 — no
// silent misread, no auto-downgrade). Bump this when adding a new migration.
const ExpectedVersion uint64 = 3

// Migration is a single, idempotent schema transform over the Pebble store.
type Migration struct {
	Version     uint64
	Description string
	Up          func(*pebble.DB) error
}

// v1Bootstrap establishes the schema-version key (see v1_bootstrap.go).
// Add future migrations here in version order; registration order is irrelevant
// (Run sorts ascending). Each Up MUST be idempotent — it is replayed if a prior
// run crashed before the version key was advanced (crash safety = idempotency +
// per-step fsync, per research R8; no backup copy is required).
var defaultMigrations = []Migration{
	{Version: 1, Description: "introduce schema-version key (v0→v1 bootstrap)", Up: v1Bootstrap},
	{Version: 2, Description: "backfill per-chunk ContentHash sidecar (spec 043 / BL-010)", Up: v2ContentHash},
	{Version: 3, Description: "reserve auth key-space prefixes (spec 045)", Up: v3RegisterAuthPrefixes},
	// Version 4 (spec 052): multi-vault unified-store key-widening marker. The
	// actual key-rewrite is filesystem-level (migrate.MergeLegacyVaults, called by
	// the daemon pre-open); this Up is a no-op that advances the schema-version
	// key so the refuse-newer guard arms. INACTIVE while ExpectedVersion stays 3 —
	// Run() skips Version > expected. Activated by the one-line ExpectedVersion
	// bump that ships with the storage widening (T004–T005).
	{Version: 4, Description: "multi-vault unified-store key-widening (spec 052) — marker; merge runs pre-open", Up: v4MultiVault},
}

// Run applies every migration in ms whose Version is greater than the store's
// current version (and ≤ expected), in ascending order, fsyncing the version key
// via pebble.Sync after each successful Up. It returns the count applied and the
// first error. A store whose current version exceeds expected is refused.
//
// Crash safety: if an Up succeeds but the process dies before writeVersion
// lands, the version key stays un-advanced, so the next open re-runs the same
// migration — which is safe because every Up is idempotent.
func Run(db *pebble.DB, expected uint64, ms []Migration) (int, error) {
	sorted := append([]Migration(nil), ms...) // copy; don't mutate caller's slice
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	// Validate the registry before touching the store: migrations MUST be
	// contiguous from 1 with no gaps or duplicates, so "current == N" stays a sound
	// witness that migrations 1..N all applied (the property the idempotent-replay
	// crash-safety model relies on). An empty list is allowed and validates nothing;
	// the refuse-newer check below still runs unconditionally (FR-015).
	if len(sorted) > 0 {
		if sorted[0].Version != 1 {
			return 0, fmt.Errorf("migrations must start at version 1 (found v%d)", sorted[0].Version)
		}
		for i := 1; i < len(sorted); i++ {
			if sorted[i].Version != sorted[i-1].Version+1 {
				return 0, fmt.Errorf("migrations must be contiguous with no gaps or duplicates (v%d then v%d)", sorted[i-1].Version, sorted[i].Version)
			}
		}
	}

	current, err := readVersion(db)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if current > expected {
		return 0, fmt.Errorf("store schema version %d is newer than this binary supports (%d); upgrade go-rag", current, expected)
	}

	applied := 0
	for _, m := range sorted {
		if m.Version <= current || m.Version > expected {
			continue
		}
		slog.Info("applying schema migration", "version", m.Version, "description", m.Description)
		if err := m.Up(db); err != nil {
			return applied, fmt.Errorf("schema migration v%d (%s): %w", m.Version, m.Description, err)
		}
		if err := writeVersion(db, m.Version); err != nil {
			return applied, fmt.Errorf("persist schema version v%d: %w", m.Version, err)
		}
		applied++
	}
	return applied, nil
}

// RunMigrations is the engine entry point on store open: it applies the default
// migration set up to ExpectedVersion and refuses stores newer than this binary.
// A one-line slog notice is emitted when a migration actually runs, so the
// one-time cost is visible (stderr/log) rather than perceived as a hang.
func RunMigrations(db *pebble.DB) error {
	applied, err := Run(db, ExpectedVersion, defaultMigrations)
	if err != nil {
		return err
	}
	if applied > 0 {
		slog.Info("store schema migrated", "steps", applied, "version", ExpectedVersion)
	}
	return nil
}

func readVersion(db *pebble.DB) (uint64, error) {
	val, closer, err := db.Get(schemaVersionKey)
	if err == pebble.ErrNotFound {
		return 0, nil // v0 bootstrap: no key yet
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	if len(val) != 8 {
		return 0, fmt.Errorf("corrupt schema-version value (len=%d)", len(val))
	}
	return binary.BigEndian.Uint64(val), nil
}

func writeVersion(db *pebble.DB, v uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)
	return db.Set(schemaVersionKey, buf, pebble.Sync)
}
