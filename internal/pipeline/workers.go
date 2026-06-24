package pipeline

import (
	"context"
	"encoding/json"
	"math/bits"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/near"
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
	// H20/spec 026 (R4): near-duplicate fingerprint + cluster, async-after-ACK
	// (near-dup is eventual-consistency-tolerant like BM25 — no ACK urgency,
	// unlike poisoning). Runs regardless of embed success (independent axis).
	ndK := p.nearDupK
	if ndK <= 0 {
		ndK = config.DefaultNearDupHamming
	}
	p.clusterNearDup(j.chunks, ndK)
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

// minNearDupTokens is the minimum chunk length (tokens) to fingerprint — below
// this the SimHash is unreliable (too few features → spurious matches, R10).
const minNearDupTokens = 8

// clusterNearDup fingerprints each chunk (SimHash), indexes it under 0x13, scans
// for siblings within Hamming k, and persists a NearDup sidecar on each chunk that
// has near-duplicates (audit H20 / spec 026). Async-after-ACK. Short chunks are
// skipped (R10). Siblings are pairwise/asymmetric (this chunk lists its existing
// siblings); query-time collapse detects pairs bidirectionally.
func (p *Pipeline) clusterNearDup(chunks []model.Chunk, k int) {
	// Pass 1: fingerprint + index under 0x13.
	for _, c := range chunks {
		if c.TokenCount < minNearDupTokens {
			continue
		}
		_ = p.db.PutNearDup(c.ID, near.SimHash(c.Content))
	}
	// Pass 2: scan for siblings; persist the sidecar on chunks that have any.
	for _, c := range chunks {
		if c.TokenCount < minNearDupTokens {
			continue
		}
		fp, ok := p.db.GetNearDup(c.ID)
		if !ok {
			continue
		}
		var siblings []string
		best := 0.0
		_ = p.db.ScanNearDup(func(otherID string, otherFP uint64) bool {
			if otherID == c.ID {
				return true
			}
			if near.HammingNear(fp, otherFP, k) {
				siblings = append(siblings, otherID)
				if sim := 1 - float64(bits.OnesCount64(fp^otherFP))/64; sim > best {
					best = sim
				}
			}
			return true
		})
		if len(siblings) == 0 {
			continue
		}
		// Read-modify-write the chunk record to attach the sidecar (mirrors
		// engine.putChunk / RescanPoisoning).
		raw, ok, _ := p.db.GetWithPrefix(storage.PrefixChunk, []byte(c.ID))
		if !ok {
			continue
		}
		var stored model.Chunk
		if json.Unmarshal(raw, &stored) != nil {
			continue
		}
		stored.NearDup = &model.NearDupInfo{Siblings: siblings, Similarity: best}
		if cj, merr := json.Marshal(stored); merr == nil {
			_ = p.db.SetWithPrefix(storage.PrefixChunk, []byte(c.ID), cj)
		}
	}
}
