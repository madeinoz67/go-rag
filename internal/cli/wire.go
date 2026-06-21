package cli

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// openDB loads the config and opens the Pebble store under <base>/data. Delegates
// to engine.Open (the single implementation) — the inline version that used to
// live here was an exact duplicate.
func openDB(base string) (config.Config, *storage.DB, error) {
	return engine.Open(base)
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
