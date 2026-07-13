package index

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
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

func defaultWS() [8]byte {
	return keys.VaultPrefix("default")
}
