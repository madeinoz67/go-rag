package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// openDB loads the config and opens the Pebble store under <dbPath>/data.
func openDB(base string) (config.Config, *storage.DB, error) {
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

// buildDocOf maps chunkID -> documentID from persisted chunks (for result collapse).
func buildDocOf(db *storage.DB) func(string) string {
	m := map[string]string{}
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			m[c.ID] = c.DocumentID
		}
		return true
	})
	return func(chunkID string) string { return m[chunkID] }
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
