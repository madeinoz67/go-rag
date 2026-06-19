package pipeline

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// DeleteDoc removes a Document and all its Chunks, Embeddings, and index entries
// (used by the watcher on MODIFIED/DELETED — research Q10: hard delete). FTS/vector
// in-memory entries are dropped by reloading indexes on the next query.
func DeleteDoc(db *storage.DB, docID string) error {
	var chunkIDs []string
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil && c.DocumentID == docID {
			chunkIDs = append(chunkIDs, c.ID)
		}
		return true
	})
	for _, cid := range chunkIDs {
		_ = db.DeleteWithPrefix(storage.PrefixChunk, []byte(cid))
		_ = db.DeleteWithPrefix(storage.PrefixEmbedding, []byte(cid))
	}

	if raw, ok, _ := db.GetWithPrefix(storage.PrefixDocument, []byte(docID)); ok {
		var d model.Document
		if json.Unmarshal(raw, &d) == nil {
			_ = db.DeleteWithPrefix(storage.PrefixContentHash, []byte(d.ContentHash))
			_ = db.DeleteWithPrefix(storage.PrefixPathDoc, []byte(d.FilePath))
		}
	}
	return db.DeleteWithPrefix(storage.PrefixDocument, []byte(docID))
}
