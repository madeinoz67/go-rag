package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble"
)

// newDB opens a fresh Pebble store in a temp dir for migration tests.
func newDB(t *testing.T) (*pebble.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := pebble.Open(filepath.Join(dir, "store"), &pebble.Options{})
	if err != nil {
		t.Fatalf("pebble open: %v", err)
	}
	return db, func() {
		db.Close()
		os.RemoveAll(dir)
	}
}

// TestRunV0Bootstrap: a fresh store (no schema key) migrates to v1, establishing
// the key.
func TestRunV0Bootstrap(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()

	applied, err := Run(db, 1, defaultMigrations)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if applied != 1 {
		t.Errorf("applied = %d, want 1", applied)
	}
	got, err := readVersion(db)
	if err != nil || got != 1 {
		t.Errorf("version after bootstrap = %d (err %v), want 1", got, err)
	}
}

// TestRunNoOpWhenCurrent: a store already at the expected version is untouched.
func TestRunNoOpWhenCurrent(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()
	if err := writeVersion(db, 1); err != nil {
		t.Fatal(err)
	}

	applied, err := Run(db, 1, defaultMigrations)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if applied != 0 {
		t.Errorf("applied = %d, want 0 (already current)", applied)
	}
}

// TestRunRefusesNewerStore: a store above the binary's expected version is
// refused (FR-015/R9 — no silent misread, no auto-downgrade).
func TestRunRefusesNewerStore(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()
	if err := writeVersion(db, 5); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(db, 1, defaultMigrations); err == nil {
		t.Fatal("expected error for newer-than-supported store, got nil")
	}
}

// TestRunAppliesAscendingRegardlessOfRegistrationOrder: migrations apply in
// ascending version order even when registered out of order.
func TestRunAppliesAscendingRegardlessOfRegistrationOrder(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()

	var order []uint64
	mk := func(v uint64) Migration {
		return Migration{Version: v, Description: "step", Up: func(*pebble.DB) error {
			order = append(order, v)
			return nil
		}}
	}
	// Registered 3,1,2 — must apply 1,2,3.
	ms := []Migration{mk(3), mk(1), mk(2)}

	applied, err := Run(db, 3, ms)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if applied != 3 {
		t.Errorf("applied = %d, want 3", applied)
	}
	want := []uint64{1, 2, 3}
	for i, v := range want {
		if i >= len(order) || order[i] != v {
			t.Fatalf("apply order = %v, want %v", order, want)
		}
	}
}

// TestRunIdempotentReplayAfterPartialCrash: if an Up ran but the version write
// was lost (simulated by rewinding the version key), re-running must succeed and
// leave the store consistent — the defining property of idempotent migrations.
func TestRunIdempotentReplayAfterPartialCrash(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()

	// A migration that writes a data key idempotently (Set is idempotent).
	const dataKey = "datakey"
	idempotentUp := func(d *pebble.DB) error {
		return d.Set([]byte(dataKey), []byte("v"), pebble.Sync)
	}
	ms := []Migration{
		{Version: 1, Description: "bootstrap", Up: v1Bootstrap},
		{Version: 2, Description: "write data key", Up: idempotentUp},
	}

	// First run: applies 1 and 2 cleanly.
	if _, err := Run(db, 2, ms); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Simulate a crash AFTER v2's Up but BEFORE its version write landed:
	// rewind the version key to 1, exactly as if the process died mid-Run.
	if err := writeVersion(db, 1); err != nil {
		t.Fatal(err)
	}

	// Re-run: v2's Up replays (idempotent — harmless), version advances to 2.
	applied, err := Run(db, 2, ms)
	if err != nil {
		t.Fatalf("replay Run: %v", err)
	}
	if applied != 1 {
		t.Errorf("replay applied = %d, want 1 (only v2 re-runs)", applied)
	}
	got, _ := readVersion(db)
	if got != 2 {
		t.Errorf("version after replay = %d, want 2", got)
	}
	// Data key still present and correct (idempotent overwrite is safe).
	val, closer, err := db.Get([]byte(dataKey))
	if err != nil {
		t.Fatalf("data key read: %v", err)
	}
	closer.Close()
	if string(val) != "v" {
		t.Errorf("data key = %q, want %q", val, "v")
	}
}

// TestRunMigrationsUsesExpectedVersion: the package entry point agrees with
// ExpectedVersion and is a no-op on an already-current store.
func TestRunMigrationsUsesExpectedVersion(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations (bootstrap): %v", err)
	}
	got, _ := readVersion(db)
	if got != ExpectedVersion {
		t.Errorf("version = %d, want ExpectedVersion %d", got, ExpectedVersion)
	}
	// Second call is a no-op (no error, version unchanged).
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations (no-op): %v", err)
	}
}

// TestRunEmptyListRefusesNewerStore: an empty migration list MUST NOT bypass the
// refuse-newer check (FR-015) — a store above expected is still refused.
func TestRunEmptyListRefusesNewerStore(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()
	if err := writeVersion(db, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(db, 1, nil); err == nil {
		t.Fatal("expected error for newer-than-supported store with empty migration list, got nil")
	}
}

// TestRunRejectsNonContiguous: a gap in the registry is rejected before any
// migration runs (protects the "current==N implies 1..N applied" witness).
func TestRunRejectsNonContiguous(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()
	noop := func(v uint64) Migration {
		return Migration{Version: v, Description: "step", Up: func(*pebble.DB) error { return nil }}
	}
	if _, err := Run(db, 3, []Migration{noop(1), noop(3)}); err == nil {
		t.Fatal("expected error for non-contiguous migration list (gap at v2), got nil")
	}
}

// TestRunRejectsDuplicate: duplicate versions are rejected (the contiguity check
// also catches duplicates).
func TestRunRejectsDuplicate(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()
	noop := func(v uint64) Migration {
		return Migration{Version: v, Description: "step", Up: func(*pebble.DB) error { return nil }}
	}
	if _, err := Run(db, 2, []Migration{noop(1), noop(1)}); err == nil {
		t.Fatal("expected error for duplicate migration versions, got nil")
	}
}
