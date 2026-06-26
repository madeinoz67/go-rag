package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/madeinoz67/go-rag/internal/enrich"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// ReEnrich re-runs document enrichment (spec 029, US3 back-fill / T016) over
// documents whose Enrichment is nil (pre-feature / not-yet-enriched) or not
// successfully enriched. It uses the configured enricher and is a no-op
// (returns a zero summary) when enrichment is disabled. Chunks are NOT
// re-embedded — only the sidecar is (re)derived from stored chunk text, so it is
// cheap relative to a full re-ingest. Each document's outcome is terminal-
// statused (enriched/failed/nothing-to-enrich); transient errors (model
// unreachable, circuit open) skip that document for a later retry.
func (e *Engine) ReEnrich(ctx context.Context) (*IngestSummary, error) {
	sum := &IngestSummary{}
	if !e.cfg.EffectiveEnrichmentEnabled() {
		return sum, nil
	}
	enrEndpoint := e.cfg.EnrichmentEndpoint
	if enrEndpoint == "" {
		enrEndpoint = e.cfg.OllamaURL
	}
	en := enrich.New(e.cfg.EnrichmentProvider, enrEndpoint, e.cfg.EnrichmentModel, e.cfg.EnrichmentAPIKey)

	// Gather chunk text per document (bounded) from the stored chunks.
	docText := map[string]string{}
	_ = e.db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) != nil {
			return true
		}
		t := docText[c.DocumentID]
		if len(t) > 8000 {
			return true // already capped
		}
		t += c.Content + "\n"
		docText[c.DocumentID] = t
		return true
	})

	_ = e.db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) != nil {
			return true
		}
		// Skip docs already successfully enriched.
		if d.Enrichment != nil && d.Enrichment.Status == model.EnrichStatusDone {
			return true
		}
		info := &model.EnrichInfo{Model: en.Model(), GeneratedAt: time.Now().UTC()}
		tags, summary, err := en.Enrich(ctx, docText[d.ID])
		switch {
		case err == nil:
			info.Tags, info.Summary = tags, summary
			info.Status = model.EnrichStatusDone
		case enrich.IsNothing(err):
			info.Status = model.EnrichStatusNothing
		case enrich.IsPermanent(err):
			info.Status = model.EnrichStatusFailed
		default: // transient: skip this doc, leave sidecar as-is for a later retry
			sum.Errors++
			return true
		}
		d.Enrichment = info
		if dj, merr := json.Marshal(d); merr == nil {
			_ = e.db.SetWithPrefix(storage.PrefixDocument, []byte(d.ID), dj)
			sum.New++
		} else {
			sum.Errors++
		}
		return true
	})
	return sum, nil
}

// ReEnrichSummary renders a ReEnrich result for CLI/MCP (mirrors migrate's render).
func (s IngestSummary) String() string {
	return fmt.Sprintf("re-enriched=%d errors=%d", s.New, s.Errors)
}
