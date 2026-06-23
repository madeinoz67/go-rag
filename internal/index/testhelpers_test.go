package index

import (
	"testing"

	"github.com/cockroachdb/pebble"
)

// newTestFTS opens a temp Pebble DB and returns a Pebble-backed FTS over it.
// The DB is closed via t.Cleanup.
func newTestFTS(t testing.TB) *FTS {
	t.Helper()
	db, err := pebble.Open(t.TempDir(), &pebble.Options{})
	if err != nil {
		t.Fatalf("open pebble: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewFTS(db)
}
