package engine_test

import "github.com/madeinoz67/go-rag/internal/storage"

func defaultWS(db *storage.DB) [8]byte {
	return db.ResolveVaultPrefix("default")
}
