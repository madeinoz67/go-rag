package pipeline

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// DeleteDoc removes a Document and all its Chunks, Embeddings, and index entries
// (used by the watcher on MODIFIED/DELETED and by reprocess/migrate — research Q10:
// hard delete). It is a method on *Pipeline (audit H01/spec 011) so it also drops
// the document's chunks from the shared in-memory FTS/Vector indexes — the cache
// is live, not rebuilt per query, so deletes must update it in place or the next
// query would serve phantom hits.
func (p *Pipeline) DeleteDoc(docID string) error {
	db := p.db
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
		// H01/spec 011: keep the shared in-memory index fresh — no phantom hits.
		if p.fts != nil {
			p.fts.Delete(cid)
		}
		if p.vec != nil {
			p.vec.Delete(cid)
		}
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
