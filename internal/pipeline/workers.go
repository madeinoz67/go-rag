package pipeline

import (
	"context"
	"encoding/json"
	"math/bits"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/enrich"
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
	// spec 030: embedding moved to the background embedder (internal/embedproc).
	// processJob now handles only FTS indexing, near-dup clustering, enrichment,
	// and status — the non-embed async-after-ACK work. The embedder is the sole
	// writer of 0x04 (embeddings) + vec.Add; processJob no longer embeds.
	for _, c := range j.chunks {
		p.fts.Index(c.ID, map[string]string{"body": c.Content})
		// H04/spec 019: maintain the 0x11 quarantine index for O(flagged) listing.
		if c.Poisoning != nil && c.Poisoning.Level.Quarantined() {
			if vj, merr := json.Marshal(c.Poisoning); merr == nil {
				_ = p.db.PutQuarantine(c.ID, vj)
			}
		}
	}
	// H06/spec 016: FTS mutations advance the epoch (query cache invalidation).
	// (Vector mutations — vec.Add — are the embedder's responsibility; it bumps
	// the epoch independently via its own OnChange hook.)
	p.indexChanged()
	// H20/spec 026 (R4): near-duplicate fingerprint + cluster, async-after-ACK.
	ndK := p.nearDupK
	if ndK <= 0 {
		ndK = config.DefaultNearDupHamming
	}
	p.clusterNearDup(j.chunks, ndK)
	// spec 029: background document enrichment (tags + summary).
	p.enrichDocument(j.docID, j.chunks)
	// Status is always "embedded" — the chunks are durably stored (0x03) + queued
	// for embedding (0x14). An embed failure is the embedder's concern (it marks
	// the 0x14 record status=failed); the document is "stored" regardless.
	p.markStatus(j.docID, StatusEmbedded)
}

// enrichDocument runs background document enrichment (spec 029): asks the bound
// enricher for tags + summary and writes the EnrichInfo sidecar onto the document
// (read-modify-write, like markStatus). Async-after-ACK; a no-op when no enricher
// is bound. Failures are terminal-statused (failed / nothing-to-enrich) so the
// worker never loops; transient errors (model unreachable, circuit open, ctx
// cancelled) leave the sidecar nil for a later back-fill retry.
func (p *Pipeline) enrichDocument(docID string, chunks []model.Chunk) {
	if p.enricher == nil {
		return
	}
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.Content)
		b.WriteByte('\n')
		if b.Len() > 8000 {
			break
		}
	}
	info := &model.EnrichInfo{Model: p.enricher.Model(), GeneratedAt: time.Now().UTC()}
	tags, summary, err := p.enricher.Enrich(context.Background(), b.String())
	switch {
	case err == nil:
		info.Tags, info.Summary = tags, summary
		info.Status = model.EnrichStatusDone
	case enrich.IsNothing(err):
		info.Status = model.EnrichStatusNothing
	case enrich.IsPermanent(err):
		info.Status = model.EnrichStatusFailed
	default: // transient: leave Enrichment nil this pass (retried on back-fill)
		return
	}
	p.setEnrichment(docID, info)
}

// setEnrichment reads a Document, attaches the EnrichInfo sidecar, and writes it
// back (read-modify-write under the pipeline mutex, mirroring markStatus). The
// sidecar is a separate field from Metadata, so document identity is untouched.
func (p *Pipeline) setEnrichment(docID string, info *model.EnrichInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, ok, _ := p.db.GetWithPrefix(storage.PrefixDocument, []byte(docID))
	if !ok {
		return
	}
	var doc model.Document
	if json.Unmarshal(raw, &doc) != nil {
		return
	}
	doc.Enrichment = info
	dbj, _ := json.Marshal(doc)
	_ = p.db.SetWithPrefix(storage.PrefixDocument, []byte(docID), dbj)
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
