package pipeline

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// LoadIndex rebuilds in-memory FTS and Vector indexes from persisted Chunks (0x03)
// and Embeddings (0x04). This lets a fresh process (e.g. `go-rag query`) reconstruct
// the searchable indexes without re-embedding the corpus.
func LoadIndex(db *storage.DB) (*index.FTS, *index.Vector, error) {
	fts := index.NewFTS()
	vec := index.NewVector()

	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			fts.Index(c.ID, map[string]string{"body": c.Content})
		}
		return true
	})

	_ = db.PrefixScanByte(storage.PrefixEmbedding, func(key, val []byte) bool {
		var v []float32
		if json.Unmarshal(val, &v) == nil {
			vec.Add(string(key[1:]), v) // key = 0x04 | chunkID
		}
		return true
	})

	return fts, vec, nil
}
