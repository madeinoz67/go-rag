package migrate

import "github.com/cockroachdb/pebble"

// v1Bootstrap is the first schema migration, applied to any store that predates
// schema versioning (current version 0).
//
// The v0→v1 artifact IS the schema-version key itself: a pre-versioning store
// has no 0xFF key, and establishing it (to value 1) is the entire point of v1.
// The Runner writes that key via writeVersion after this Up returns, so v1
// carries no additional data-layout transform — hence the no-op body. It exists
// as an explicit, described step (logged on apply) rather than a silent special
// case, and to give future migrations a stable v1 to build on.
//
// Idempotent by construction: re-running it re-executes a no-op, and the Runner
// only calls Up when Version (1) > current, so on a clean v1 store it is never
// reached.
func v1Bootstrap(_ *pebble.DB) error { return nil }
