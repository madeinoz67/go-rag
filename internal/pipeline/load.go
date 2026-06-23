package pipeline

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// storedEmbedding is the on-disk shape of an embedding under prefix 0x04. The
// model is recorded so model migrations (T048) can detect stale embeddings;
// convention records the instruction-prefix convention so a half-prefixed corpus
// is detectable (audit H07). Older databases stored a bare []float32, and pre-H07
// records omit convention — LoadIndex reads all for backward compat.
type storedEmbedding struct {
	Model      string    `json:"model,omitempty"`
	Convention string    `json:"convention,omitempty"` // H07 prefix-convention provenance ("" = legacy unprefixed)
	Vector     []float32 `json:"vector"`
}

// LoadIndex seeds the FTS and Vector indexes. The FTS is Pebble-backed (audit
// H16/spec 018, pivoted) — no rebuild on cold start; it's queried in place. A
// one-time migration writes the postings from existing chunks for pre-pivot
// vaults. The Vector index is reloaded from persisted embeddings (unchanged).
func LoadIndex(db *storage.DB) (*index.FTS, *index.Vector, error) {
	pebbleDB := db.Pebble()
	fts := index.NewFTS(pebbleDB)

	// H16/spec 018: one-time migration for pre-pivot vaults (no postings yet).
	if !index.HasPostings(pebbleDB) {
		_ = index.MigrateFromChunks(pebbleDB, func(yield func(string, string) bool) {
			_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
				var c model.Chunk
				if json.Unmarshal(val, &c) == nil {
					return yield(c.ID, c.Content)
				}
				return true
			})
		})
	}

	vec := index.NewVector()

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
