package migrate

import "github.com/cockroachdb/pebble"

// v3RegisterAuthPrefixes (spec 045) reserves three new key-space prefixes for
// the auth subsystem — PrefixAuthAPIKey (0x17), PrefixAuthAdmin (0x18),
// PrefixAuthSession (0x19), defined in internal/storage/storage.go.
//
// This is a DATA NO-OP. The prefixes are Go constants (no runtime registration
// required), and they hold only NEW records written by the auth package, so
// existing stores have nothing to transform. The migration exists solely to
// advance the schema version — documenting the key-space expansion and arming
// the refuse-downgrade contract: a v3 store opened by a v2 binary is refused,
// never silently misread (constitution: Storage discipline / schema evolution).
//
// Idempotent + crash-safe by construction: it performs no writes, so a replay
// after a crash before writeVersion advances is a no-op.
func v3RegisterAuthPrefixes(_ *pebble.DB) error { return nil }
