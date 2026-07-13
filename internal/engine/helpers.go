package engine

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
	"github.com/madeinoz67/go-rag/internal/storage/migrate"
	"github.com/madeinoz67/go-rag/internal/vault"
)

// Open loads the config and opens the Pebble store under <base>/data. It is the
// single constructor for stdio/per-call use; long-lived daemons use NewWithDB
// with an already-open DB. Replaces the openDB helpers formerly duplicated in
// internal/cli/wire.go and internal/mcp/server.go.
func Open(base string) (config.Config, *storage.DB, error) {
	cfg, err := config.Load(filepath.Join(base, "config.json"))
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("no go-rag database here — run `go-rag init` first: %w", err)
	}
	db, err := storage.Open(filepath.Join(base, "data"))
	if err != nil {
		return cfg, nil, err
	}
	// Configure the v4 multi-vault merge (spec 052): only when opening a UNIFIED
	// store — a --db-path that is a SIBLING of the legacy vaults root (same parent
	// directory, e.g. ~/.go-rag/store beside ~/.go-rag/vaults) — point the v4
	// migration at vault.Root() so it rewrites every legacy per-vault DB's keys
	// into this store and archives the legacy dirs. The same-parent guard is what
	// keeps this safe: a legacy per-vault path (~/.go-rag/vaults/<name>) has a
	// different parent and never self-merges (no Pebble lock conflict), and an
	// unrelated/test path never drags in the operator's real vaults.
	vRoot := vault.Root()
	if vRoot != "" && filepath.Dir(filepath.Clean(base)) == filepath.Dir(filepath.Clean(vRoot)) {
		migrate.SetLegacyRoot(vRoot)
	} else {
		migrate.SetLegacyRoot("")
	}
	// Migrate the on-disk schema before serving any operation (migration-on-open,
	// FR-013). Runs under Pebble's single-writer lock; idempotent and replay-safe.
	if err := migrate.RunMigrations(db.Pebble()); err != nil {
		db.Close()
		return cfg, nil, fmt.Errorf("migrate store: %w", err)
	}
	return cfg, db, nil
}

// countPrefix returns the number of keys under one vault-kind range
// (`prefix|ws` ≤ key < `prefix|wsPlus`). Spec 052: vault-scoped, never crosses
// vaults.
func countPrefix(db *storage.DB, ws [8]byte, prefix byte) int {
	lower, upper, err := keys.VaultKindRange(prefix, ws)
	if err != nil {
		return 0
	}
	n := 0
	_ = db.RangeScan(lower, upper, func(_, _ []byte) bool { n++; return true })
	return n
}

// docOf builds a chunkID -> documentID map from ONE vault's persisted chunks,
// used to collapse retrieval hits to one per document.
func docOf(db *storage.DB, ws [8]byte) func(string) string {
	m := map[string]string{}
	lower, upper, err := keys.VaultKindRange(storage.PrefixChunk, ws)
	if err == nil {
		_ = db.RangeScan(lower, upper, func(_, val []byte) bool {
			var c model.Chunk
			if json.Unmarshal(val, &c) == nil {
				m[c.ID] = c.DocumentID
			}
			return true
		})
	}
	return func(id string) string { return m[id] }
}

// lookupChunk returns a stored Chunk by ID within one vault.
func lookupChunk(db *storage.DB, ws [8]byte, chunkID string) (model.Chunk, bool) {
	raw, ok, _ := db.Get(keys.ChunkKey(ws, chunkID))
	if !ok {
		return model.Chunk{}, false
	}
	var c model.Chunk
	if json.Unmarshal(raw, &c) != nil {
		return model.Chunk{}, false
	}
	return c, true
}

// lookupDoc returns a stored Document by ID within one vault.
func lookupDoc(db *storage.DB, ws [8]byte, docID string) (model.Document, bool) {
	raw, ok, _ := db.Get(keys.DocumentKey(ws, docID))
	if !ok {
		return model.Document{}, false
	}
	var d model.Document
	if json.Unmarshal(raw, &d) != nil {
		return model.Document{}, false
	}
	return d, true
}

// preview collapses a chunk to a single-line preview of at most n chars.
func preview(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
