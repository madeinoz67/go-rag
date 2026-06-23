package pipeline

import (
	"context"
	"encoding/json"
	"time"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// job is a unit of async indexing work: embed a document's chunks and add them to
// the FTS and vector indexes.
type job struct {
	docID  string
	chunks []model.Chunk
}

// worker drains the queue, embedding and indexing chunks, then updates the
// Document status (pending -> embedded | error).
func (p *Pipeline) worker() {
	defer p.wg.Done()
	for j := range p.queue {
		p.processJob(j)
	}
}

func (p *Pipeline) processJob(j job) {
	texts := make([]string, len(j.chunks))
	for i, c := range j.chunks {
		texts[i] = c.Content
	}

	// H07: embed documents in their trained role. The document-role instruction
	// prefix is prepended to each chunk text before embedding; the prefix never
	// touches the stored Chunk.Content or any identity hash (Principle II). The
	// resolved convention is recorded as provenance so a later query can detect a
	// convention mismatch (US3). A nil prefixer (no prefix in effect) is a no-op.
	conv := ""
	docTexts := texts
	if p.prefixer != nil {
		conv = p.prefixer.Convention()
		docTexts = p.prefixer.ApplyAll(embed.RoleDocument, texts)
	}

	status := StatusEmbedded
	vecs, err := p.embed.Embed(context.Background(), docTexts)
	if err != nil {
		status = StatusError
	} else {
		for i, c := range j.chunks {
			p.fts.Index(c.ID, map[string]string{"body": c.Content})
			if i < len(vecs) {
				p.vec.Add(c.ID, vecs[i])
				// Persist the embedding so a later `query` process can rebuild
				// the in-memory index without re-embedding (data-model 0x04).
				if vj, merr := json.Marshal(storedEmbedding{Model: p.embed.Model(), Convention: conv, Vector: vecs[i]}); merr == nil {
					_ = p.db.SetWithPrefix(storage.PrefixEmbedding, []byte(c.ID), vj)
				}
			}
			// H04/spec 019: maintain the 0x11 quarantine index for O(flagged)
			// listing (US2 ListPoisoned). The verdict already rides the chunk
			// record (sync at ingest); this secondary index is populated ASYNC
			// (off the ACK path, alongside embed/FTS) so listing is fast. Only
			// flagged chunks are indexed.
			if c.Poisoning != nil && c.Poisoning.Level.Quarantined() {
				if vj, merr := json.Marshal(c.Poisoning); merr == nil {
					_ = p.db.PutQuarantine(c.ID, vj)
				}
			}
		}
		// H06/spec 016: vectors just landed asynchronously (post-ACK) — this is
		// the mutation a write-ACK-only epoch bump would miss. Advance the epoch
		// so a query that cached before the vector landed cannot serve a
		// pre-vector result. Only on the success path: an embed failure added no
		// vectors, so the vector index is unchanged.
		p.indexChanged()
		// H11/spec 017: signal the embedding profile so the engine can persist
		// the corpus baseline on first embed (no-op once a baseline exists).
		if p.OnFirstEmbed != nil && len(vecs) > 0 {
			p.OnFirstEmbed(p.embed.Model(), len(vecs[0]), conv)
		}
	}
	p.markStatus(j.docID, status)
}

// markStatus reads a Document, updates its status, and writes it back. The pipeline
// mutex serialises read-modify-write across concurrent workers.
func (p *Pipeline) markStatus(docID, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	raw, ok, _ := p.db.GetWithPrefix(storage.PrefixDocument, []byte(docID))
	if !ok {
		return
	}
	var doc model.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return
	}
	doc.Status = status
	doc.UpdatedAt = time.Now().UTC()
	dbj, _ := json.Marshal(doc)
	_ = p.db.SetWithPrefix(storage.PrefixDocument, []byte(docID), dbj)
}
