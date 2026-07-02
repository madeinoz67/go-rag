package pipeline

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/events"
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

	// Document-record lifecycle: hold p.mu — the pipeline's document-record
	// mutex (the same lock markStatus/captionImages/setEnrichment take; see the
	// discipline note at workers.go:~325: "PrefixDocument writers MUST take
	// p.mu"). Without it, the embedder's markStatus (Get→Set→publish-EMBEDDED,
	// under p.mu) can interleave with this delete: markStatus's Get sees the
	// record, DeleteDoc removes it + publishes DELETED, markStatus's Set then
	// RE-CREATES it (resurrection) and publishes EMBEDDED strictly after DELETED
	// (spec 040 adversarial-audit finding). Under p.mu the two are mutually
	// exclusive — either DeleteDoc wins (markStatus's Get then returns !ok →
	// no-op, no EMBEDDED) or markStatus completes first (DeleteDoc then removes
	// the embedded record → correct INGESTED/EMBEDDED/DELETED order). The mutex
	// also makes the existed-check + publish atomic, so two concurrent
	// DeleteDocs for the same ID cannot both emit DELETED.
	p.mu.Lock()
	defer p.mu.Unlock()
	// spec 043 / BL-010 FR-005: if this delete is part of a re-ingest (captureReingest marked the docID), suppress the DELETED event — the re-ingest's processFile emits RE_INGESTED instead.
	_, suppressDeleted := p.reingestDocs[docID]
	if suppressDeleted {
		delete(p.reingestDocs, docID)
	}

	// Capture the file path (the DELETED event's SourcePath) + whether a record
	// existed, before the durable delete. Gated on existence so a no-op delete
	// (double-scan, already-gone doc) emits no noise.
	var filePath string
	existed := false
	if raw, ok, _ := db.GetWithPrefix(storage.PrefixDocument, []byte(docID)); ok {
		var d model.Document
		if json.Unmarshal(raw, &d) == nil {
			filePath = d.FilePath
			existed = true
			_ = db.DeleteWithPrefix(storage.PrefixContentHash, []byte(d.ContentHash))
			_ = db.DeleteWithPrefix(storage.PrefixPathDoc, []byte(d.FilePath))
		}
	}
	if err := db.DeleteWithPrefix(storage.PrefixDocument, []byte(docID)); err != nil {
		return err
	}
	// spec 040 / BL-008: publish DELETED while still under p.mu so it is the
	// terminal event for this doc (mirrors markStatus publishing EMBEDDED under
	// p.mu). Every deletion trigger (watcher-detected removal, reprocess) flows
	// through DeleteDoc, so this single chokepoint covers them all. Publish is
	// non-blocking (T004) — the bounded fan-out never enters the <10ms write-ACK
	// path (Principle IV); lock order p.mu→bus.mu is acyclic (markStatus already
	// holds it). `after` is intentionally zero for DELETED (the contract scopes
	// it to INGESTED/EMBEDDED); document_id is the authoritative key.
	if existed && p.OnEvent != nil && !suppressDeleted {
		p.OnEvent(events.DocumentEvent{Type: events.EventDeleted, DocumentID: docID, SourcePath: filePath})
	}
	return nil
}

// chunksOfDoc returns the chunk records belonging to docID (read-only — used by
// the re-ingest delta, spec 043 / BL-010, to capture the old chunk set before
// DeleteDoc). Mirrors DeleteDoc's PrefixChunk scan but does not mutate.
func (p *Pipeline) chunksOfDoc(docID string) []model.Chunk {
	var chunks []model.Chunk
	_ = p.db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil && c.DocumentID == docID {
			chunks = append(chunks, c)
		}
		return true
	})
	return chunks
}
