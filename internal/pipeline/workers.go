package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/bits"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/caption"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/enrich"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/near"
	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// job is a unit of async indexing work: embed a document's chunks and add them to
// the FTS and vector indexes.
type job struct {
	docID       string
	chunks      []model.Chunk
	images      []reader.ImageRef    // spec 031 US4: transient image bytes for post-ACK captioning (nil for non-PDF / image-less)
	mimeType    string               // document mime type (the caption chunk's GenerateID input)
	spans       []reader.HeadingSpan // spec 031: heading spans for caption SectionContext
	pageOffsets map[int]int          // spec 031: page->byte-offset map for caption SectionContext
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
	// spec 031 US4: background image captioning (writes a synthetic caption chunk).
	// Runs BEFORE enrichment so the enricher's tags + summary include the caption
	// text (image-heavy docs get meaningful tags, not just sparse body text).
	captions := p.captionImages(j)
	// spec 029: background document enrichment (tags + summary) — includes captions.
	p.enrichDocument(j.docID, j.chunks, captions)
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
func (p *Pipeline) enrichDocument(docID string, chunks []model.Chunk, captions string) {
	if p.enricher == nil {
		return
	}
	var b strings.Builder
	if captions != "" {
		b.WriteString(captions) // captions first (high-value for image-heavy docs)
		b.WriteByte('\n')
	}
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

// captionImages runs background image captioning (spec 031 US4): for each image
// the reader extracted, ask the bound captioner for a description and write the
// concatenated captions as a NEW chunk — searchable via FTS + the embedder (a
// fresh ID → fresh fts.Index + a fresh 0x14 embed-queue record → vec.Add),
// avoiding the stale-vector / fts-no-op traps of mutating an existing chunk.
// Async-after-ACK; a no-op when no captioner is bound or the doc has no images
// (SC-006). A transient failure (circuit open / network / ctx) leaves the caption
// chunk unwritten this pass (retry on reprocess). Image bytes ride the job queue
// (in-memory) and are never persisted.
func (p *Pipeline) captionImages(j job) string {
	if p.captioner == nil || len(j.images) == 0 {
		return "" // SC-006: disabled or image-less doc
	}
	ctx := context.Background()
	var lines []string
	var pages []int
	for _, img := range j.images {
		// Cross-document image-caption cache (spec 031 FU): content-addressed by
		// image SHA-256 — the same image in ANY document reuses the cached caption,
		// skipping the vision call. Also deduplicates within a document (a logo on
		// every page → captioned once). Cache value carries the model so a model
		// change triggers a re-caption.
		h := sha256.Sum256(img.Bytes)
		hashKey := hex.EncodeToString(h[:])
		if cached, ok, _ := p.db.GetWithPrefix(storage.PrefixImageCaption, []byte(hashKey)); ok {
			var ci struct {
				Caption string `json:"caption"`
				Model   string `json:"model"`
			}
			if json.Unmarshal(cached, &ci) == nil && ci.Model == p.captioner.Model() && ci.Caption != "" {
				lines = append(lines, fmt.Sprintf("Figure on page %d: %s", img.PageNr, ci.Caption))
				pages = append(pages, img.PageNr)
				continue // cache hit (same model) — skip the vision call
			}
		}
		// Cache miss or model mismatch — caption the image.
		c, err := p.captioner.Caption(ctx, img.Bytes, fmt.Sprintf("page %d, %s", img.PageNr, img.FileType))
		switch {
		case err == nil:
			if t := strings.TrimSpace(c); t != "" {
				lines = append(lines, fmt.Sprintf("Figure on page %d: %s", img.PageNr, t))
				pages = append(pages, img.PageNr)
				// Cache the caption for cross-document reuse.
				cv, _ := json.Marshal(map[string]string{"caption": t, "model": p.captioner.Model()})
				_ = p.db.SetWithPrefix(storage.PrefixImageCaption, []byte(hashKey), cv)
			}
		case caption.IsNothing(err):
			// terminal: skip this image
		case caption.IsPermanent(err):
			// permanent: skip (do not pollute results)
		default:
			return "" // transient (circuit open / ctx / network): leave unwritten, retry on reprocess
		}
	}
	if len(lines) == 0 {
		return ""
	}
	captionsText := strings.Join(lines, "\n")
	captionID := model.GenerateID(captionsText, j.mimeType, map[string]any{"doc": j.docID, "idx": len(j.chunks), "kind": "caption"})
	now := time.Now().UTC()
	cc := model.Chunk{
		ID:          captionID,
		DocumentID:  j.docID,
		Content:     captionsText,
		ChunkIndex:  len(j.chunks),
		TotalChunks: len(j.chunks) + 1, // v1 known display inconsistency: original chunks keep their old TotalChunks (updating N is N RMWs, not worth it)
		CreatedAt:   now,
		Kind:        "caption",
		Caption:     &model.CaptionInfo{Model: p.captioner.Model(), GeneratedAt: now, Status: "done", ImagePages: pages},
	}
	if n := len(j.chunks); n > 0 {
		cc.PreviousChunkID = j.chunks[n-1].ID // link into the chunk linked-list
	}

	// SectionContext: the heading breadcrumb at the first image's page position
	// (spec 031 — captions carry document hierarchy). Uses the heading spans +
	// page offsets threaded from the reader via the job.
	if len(j.spans) > 0 && len(j.pageOffsets) > 0 && len(j.images) > 0 {
		if off, ok := j.pageOffsets[j.images[0].PageNr]; ok {
			cc.SectionContext = resolveBreadcrumb(j.spans, off, nil)
		}
	}

	// Serialize the Document/chunk read-modify-writes with setEnrichment /
	// markStatus. fts.Index + PutEmbedQueueItem are safe under p.mu (fts has its
	// own lock; the queue write is a single Set). NOTE: clusterNearDup is NOT
	// p.mu-gated but it ran earlier in processJob and only touches PrefixChunk by
	// chunk ID; this new caption ID cannot collide, so there is no race.
	p.mu.Lock()
	defer p.mu.Unlock()

	cj, _ := json.Marshal(cc)
	if err := p.db.SetWithPrefix(storage.PrefixChunk, []byte(captionID), cj); err != nil {
		slog.Warn("caption: store caption chunk", "err", err)
		return ""
	}
	// CRITICAL: the embed-queue record MUST be written or the caption is
	// BM25-searchable but NOT vector-searchable (silent half-fail of SC-004).
	// p.embed may be nil for non-engine constructors (caption-only test/eval
	// harnesses); the embedder re-reads the model at drain time, so "" is benign.
	embModel := ""
	if p.embed != nil {
		embModel = p.embed.Model()
	}
	if err := p.db.PutEmbedQueueItem(captionID, embModel); err != nil {
		slog.Warn("caption: queue caption for embed", "err", err)
	}
	p.fts.Index(captionID, map[string]string{"body": captionsText})

	// Point the last original chunk's NextChunkID at the caption (linked-list tail).
	if n := len(j.chunks); n > 0 {
		lastID := j.chunks[n-1].ID
		if raw, ok, _ := p.db.GetWithPrefix(storage.PrefixChunk, []byte(lastID)); ok {
			var lc model.Chunk
			if json.Unmarshal(raw, &lc) == nil {
				lc.NextChunkID = captionID
				if lj, merr := json.Marshal(lc); merr == nil {
					if err := p.db.SetWithPrefix(storage.PrefixChunk, []byte(lastID), lj); err != nil {
						slog.Warn("caption: link last chunk NextChunkID", "err", err)
					}
				}
			}
		}
	}

	// Bump the document's ChunkCount (a non-identity statistic, like Status).
	if raw, ok, _ := p.db.GetWithPrefix(storage.PrefixDocument, []byte(j.docID)); ok {
		var d model.Document
		if json.Unmarshal(raw, &d) == nil {
			d.ChunkCount++
			d.UpdatedAt = now
			if dj, merr := json.Marshal(d); merr == nil {
				if err := p.db.SetWithPrefix(storage.PrefixDocument, []byte(j.docID), dj); err != nil {
					slog.Warn("caption: bump document ChunkCount", "err", err)
				}
			}
		}
	}

	p.indexChanged()            // FTS epoch bump (H06 query-cache invalidation)
	if p.OnNotifyEmbed != nil { // wake the embedder to drain the caption's 0x14 now
		p.OnNotifyEmbed()
	}
	return captionsText
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
//
// SAFETY INVARIANT (spec 031 US4 review): the chunk read-modify-write below is
// UNSYNCHRONIZED (no p.mu). It is safe ONLY because chunk IDs are content-addressed
// and doc-scoped — no concurrent writer touches the same chunk ID. captionImages
// (spec 031 US4) relies on the same invariant for its new caption IDs. Any future
// change that clusters ACROSS documents, or any other concurrent writer to
// PrefixChunk by ID, MUST take p.mu (else last-writer-wins on the JSON blob).
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
