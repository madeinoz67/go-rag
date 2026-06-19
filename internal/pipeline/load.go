package pipeline

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// storedEmbedding is the on-disk shape of an embedding under prefix 0x04. The
// model is recorded so model migrations (T048) can detect stale embeddings. Older
// databases stored a bare []float32; LoadIndex reads both for backward compat.
type storedEmbedding struct {
	Model  string    `json:"model,omitempty"`
	Vector []float32 `json:"vector"`
}

// LoadIndex rebuilds in-memory FTS and Vector indexes from persisted Chunks (0x03)
// and Embeddings (0x04). Reads both the current {model,vector} embedding format and
// the legacy bare []float32 format.
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
		var se storedEmbedding
		if json.Unmarshal(val, &se) == nil && se.Vector != nil {
			vec.Add(string(key[1:]), se.Vector)
			return true
		}
		// Legacy: bare []float32 (older databases).
		var v []float32
		if json.Unmarshal(val, &v) == nil {
			vec.Add(string(key[1:]), v)
		}
		return true
	})

	return fts, vec, nil
}

// EmbeddingModelStats returns a count of embeddings per model (T048). Embeddings
// stored before model-tracking (bare vectors, empty model) are not counted.
func EmbeddingModelStats(db *storage.DB) map[string]int {
	m := map[string]int{}
	_ = db.PrefixScanByte(storage.PrefixEmbedding, func(_, val []byte) bool {
		var se storedEmbedding
		if json.Unmarshal(val, &se) == nil && se.Model != "" {
			m[se.Model]++
		}
		return true
	})
	return m
}
