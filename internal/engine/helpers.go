package engine

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
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
	return cfg, db, nil
}

// countPrefix returns the number of keys under a single-byte prefix.
func countPrefix(db *storage.DB, prefix byte) int {
	n := 0
	_ = db.PrefixScanByte(prefix, func(_, _ []byte) bool { n++; return true })
	return n
}

// docOf builds a chunkID -> documentID map from persisted chunks, used to
// collapse retrieval hits to one per document.
func docOf(db *storage.DB) func(string) string {
	m := map[string]string{}
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			m[c.ID] = c.DocumentID
		}
		return true
	})
	return func(id string) string { return m[id] }
}

// lookupChunk returns a stored Chunk by ID.
func lookupChunk(db *storage.DB, chunkID string) (model.Chunk, bool) {
	raw, ok, _ := db.GetWithPrefix(storage.PrefixChunk, []byte(chunkID))
	if !ok {
		return model.Chunk{}, false
	}
	var c model.Chunk
	if json.Unmarshal(raw, &c) != nil {
		return model.Chunk{}, false
	}
	return c, true
}

// lookupDoc returns a stored Document by ID.
func lookupDoc(db *storage.DB, docID string) (model.Document, bool) {
	raw, ok, _ := db.GetWithPrefix(storage.PrefixDocument, []byte(docID))
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
