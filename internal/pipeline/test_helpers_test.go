package pipeline

import (
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

func wsOf(p *Pipeline) [8]byte {
	return p.db.ResolveVaultPrefix("default")
}

func vaultRange(t *testing.T, kind byte, ws [8]byte) ([]byte, []byte) {
	t.Helper()
	lower, upper, err := keys.VaultKindRange(kind, ws)
	if err != nil {
		t.Fatalf("vault range for kind %x: %v", kind, err)
	}
	return lower, upper
}

func scanVaultKind(t *testing.T, db *storage.DB, kind byte, ws [8]byte, fn func(key, value []byte) bool) {
	t.Helper()
	lower, upper := vaultRange(t, kind, ws)
	if err := db.RangeScan(lower, upper, fn); err != nil {
		t.Fatalf("range scan kind %x: %v", kind, err)
	}
}
