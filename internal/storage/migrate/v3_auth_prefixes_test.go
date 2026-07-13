package migrate

import "testing"

// TestV3RegisterAuthPrefixes (spec 045): advancing a v2 store to v3 applies
// exactly one migration, lands at ExpectedVersion, and is a safe-to-replay
// no-op. The v3 migration reserves the auth key-space prefixes (0x17/0x18/0x19)
// by version advancement alone — it writes no data records — so the transform
// test asserts version state, and the idempotency test asserts replay applies
// nothing. Constitution: every migration ships idempotency + v(n)→v(n+1) coverage.
func TestV3RegisterAuthPrefixes(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()

	// Advance to v2 first (v1 bootstrap + v2 ContentHash backfill are data no-ops
	// on an empty store).
	if _, err := Run(db, 2, defaultMigrations); err != nil {
		t.Fatalf("migrate to v2: %v", err)
	}

	// v2 → top: applies the remaining migrations (v3..ExpectedVersion) and lands
	// at ExpectedVersion. v4's Up (multi-vault merge) is a no-op here because the
	// legacy root is unset in this package's tests, so it contributes only a
	// version advance.
	applied, err := Run(db, ExpectedVersion, defaultMigrations)
	if err != nil {
		t.Fatalf("migrate to top: %v", err)
	}
	if want := int(ExpectedVersion - 2); applied != want {
		t.Fatalf("v2→top applied = %d, want %d", applied, want)
	}
	if got, err := readVersion(db); err != nil || got != ExpectedVersion {
		t.Fatalf("post-migrate version = %d err=%v, want %d", got, err, ExpectedVersion)
	}

	// Idempotent replay: a store already at ExpectedVersion applies nothing.
	applied, err = Run(db, ExpectedVersion, defaultMigrations)
	if err != nil || applied != 0 {
		t.Fatalf("replay applied=%d err=%v, want 0/<nil>", applied, err)
	}
}
