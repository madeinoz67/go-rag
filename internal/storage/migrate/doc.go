// Package migrate implements go-rag's on-disk schema migration runner.
//
// It is modeled on MuninnDB's internal/storage/migrate: a registry of numbered,
// idempotent Migration steps applied in ascending version order on store open.
// The last-applied version is stored durably (pebble.Sync) under a global meta
// key (0xFF) after each step, so a crash mid-migration leaves the version
// un-advanced and the same idempotent step replays on the next open — no
// backup-copy is required (crash safety = idempotency + per-step fsync).
//
// Absence of the version key is treated as version 0, which is the v0→v1
// bootstrap: the release that introduces schema versioning ships migration v1
// (writing the key), and every pre-versioning store migrates automatically.
package migrate
