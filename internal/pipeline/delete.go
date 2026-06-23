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
	// Save chunkID + content during the scan — the Pebble-backed FTS.Delete
	// needs the content to re-tokenize (recover terms for key construction).
	type chunkRef struct{ id, content string }
	var chunks []chunkRef
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil && c.DocumentID == docID {
			chunks = append(chunks, chunkRef{id: c.ID, content: c.Content})
		}
		return true
	})
	for _, ch := range chunks {
		_ = db.DeleteWithPrefix(storage.PrefixChunk, []byte(ch.id))
		_ = db.DeleteWithPrefix(storage.PrefixEmbedding, []byte(ch.id))
		// H01/spec 011 + H16/spec 018: keep the index fresh — no phantom hits.
		if p.fts != nil {
			p.fts.Delete(ch.id, ch.content)
		}
		if p.vec != nil {
			p.vec.Delete(ch.id)
		}
	}
	// H06/spec 016: removals mutated the searchable corpus — advance the
	// result-cache epoch so subsequent queries never serve a now-deleted hit.
	if len(chunks) > 0 {
		p.indexChanged()
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
