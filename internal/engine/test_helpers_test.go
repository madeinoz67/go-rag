package engine

import "github.com/madeinoz67/go-rag/internal/storage"

// defaultWS resolves the workspace prefix for the single default vault. Spec
// 052 Step 4 promotes this to a per-call `vault string` parameter.
func defaultWS(db *storage.DB) [8]byte {
	return db.ResolveVaultPrefix("default")
}

// engineWS resolves the default vault's workspace prefix from the Engine's own
// DB handle (test convenience).
func engineWS(e *Engine) [8]byte {
	return defaultWS(e.db)
}
