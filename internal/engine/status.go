package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"time"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// baselineRecordedAt formats the baseline timestamp for status display ("" when
// zero / no baseline).
func baselineRecordedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Status returns corpus counts, the active embedding model, and embedding
// reachability metadata. The model/dimensionality reported are the **stored**
// majority (from the embedding profile), not the configured values, so a
// model/config mismatch or a mid-migration drift is visible without querying
// (audit H03, US2).
//
// H11/spec 017: also reports the corpus baseline vs live (model/dim/convention/
// ollama-version) and the drift verdict, computed LIVE on each call (distinct
// from the cached boot verdict /health reads).
func (e *Engine) Status() (*StatusInfo, error) {
	docs := countPrefix(e.db, storage.PrefixDocument)
	chunks := countPrefix(e.db, storage.PrefixChunk)
	embs := countPrefix(e.db, storage.PrefixEmbedding)
	prof := CorpusProfile(e.db)

	// H11/spec 017: live drift verdict (re-fetches the Ollama version; this is
	// the on-demand detailed view — /health reads the cached boot verdict).
	dv := e.computeDriftVerdict(context.Background())
	embModel := e.cfg.EmbeddingModel
	dims := 0
	if prof.Total > 0 {
		embModel = prof.MajorityModel
		dims = prof.MajorityDim
	}
	reranker := e.cfg.RerankModel
	if reranker == "" {
		reranker = "disabled"
	}
	complete := docs == 0 || embs >= chunks
	// H07: surface the resolved prefix convention in effect (config mode + the
	// role prefixes the prefixer will apply), the corpus's stored majority
	// convention, and a drift flag when more than one convention is present.
	pre := e.cfg.Prefixer()
	cfgMode := e.cfg.EmbeddingPrefix
	if cfgMode == "" {
		cfgMode = "auto"
	}
	// H04/spec 019: poisoning summary — flagged count (0x11 index), sources, merged
	// phrase-list size, and the effective enabled/threshold state.
	poisonFlagged := countPrefix(e.db, storage.PrefixPoisonQuar)

	// H20/spec 026: near-duplicate chunk count — chunks with NearDup siblings.
	nearDupChunks := 0
	_ = e.db.PrefixScanByte(storage.PrefixChunk, func(_, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil && c.NearDup != nil {
			nearDupChunks++
		}
		return true
	})
	// spec 029: documents carrying a non-nil Enrichment sidecar.
	enrichedDocs := 0
	_ = e.db.PrefixScanByte(storage.PrefixDocument, func(_, v []byte) bool {
		var d model.Document
		if json.Unmarshal(v, &d) == nil && d.Enrichment != nil {
			enrichedDocs++
		}
		return true
	})
	// spec 030: embedder backlog — pending + failed counts from the 0x14 queue.
	embedPending, embedFailed := 0, 0
	_ = e.db.ScanEmbedQueue(func(_ string, item storage.EmbedQueueItem) bool {
		if item.Status == storage.EmbedQueueFailed {
			embedFailed++
		} else {
			embedPending++
		}
		return true
	})
	poisonSrcs, _ := e.ListThreatSources()
	return &StatusInfo{
		Documents:                docs,
		Chunks:                   chunks,
		Embeddings:               embs,
		Dimensions:               dims,
		EmbeddingModel:           embModel,
		Reranker:                 reranker,
		OllamaURL:                e.cfg.OllamaURL,
		EmbeddingsComplete:       complete,
		EmbeddingDrift:           prof.Total > 0 && !prof.Consistent,
		ModelCounts:              prof.ModelCounts,
		DimCounts:                prof.DimCounts,
		EmbeddingConvention:      prof.MajorityConvention,
		EmbeddingConventionDrift: prof.Total > 0 && len(prof.ConventionCounts) > 1,
		ConventionCounts:         prof.ConventionCounts,
		ConfiguredPrefix:         cfgMode,
		QueryPrefix:              pre.ForRole(embed.RoleQuery),
		DocPrefix:                pre.ForRole(embed.RoleDocument),
		ResultCache:              e.resultCache.Stats(), // H06/spec 016
		EmbeddingCache:           e.embedCache.Stats(),  // H06/spec 016
		// H11/spec 017: corpus baseline vs live + drift verdict (live-computed above).
		CorpusBaselineModel:      dv.BaselineModel,
		CorpusBaselineDim:        dv.BaselineDim,
		CorpusBaselineConvention: dv.BaselineConvention,
		CorpusBaselineOllamaVer:  dv.BaselineVersion,
		CorpusBaselineRecordedAt: baselineRecordedAt(dv.BaselineRecordedAt),
		LiveOllamaVersion:        dv.LiveVersion,
		DriftVerdict:             dv.Verdict,
		HardDrift:                dv.Hard,
		VersionDrift:             dv.Verdict == VerdictVersionWarning,
		// H04/spec 019
		PoisoningEnabled:   e.cfg.EffectivePoisoningEnabled(),
		PoisonThresholdSus: e.cfg.EffectivePoisonThresholdSuspicious(),
		PoisonThresholdQua: e.cfg.EffectivePoisonThresholdQuarantine(),
		PoisonFlagged:      poisonFlagged,
		PoisonSources:      len(poisonSrcs),
		PoisonPhrases:      len(e.mergedPhrases()),

		// H22/spec 024: adaptive-retrieval observability.
		PoolSize:             e.cfg.EffectivePoolSize(), // configured candidate-pool ceiling
		AdaptiveDepthEnabled: e.cfg.EffectiveAdaptiveDepthEnabled(),
		PoolUtilization:      e.poolUtilization(),

		// H20/spec 026: near-duplicate observability.
		NearDupChunks: nearDupChunks,

		// spec 029: document enrichment observability.
		EnrichmentEnabled: e.cfg.EffectiveEnrichmentEnabled(),
		CaptioningEnabled: e.cfg.EffectiveCaptioningEnabled(),
		EnrichedDocs:      enrichedDocs,

		// spec 030: crash-safe embedder backlog.
		EmbedPending: embedPending,
		EmbedFailed:  embedFailed,
	}, nil
}

// poolUtilization reduces the atomic pool-tracking counters (H22/spec 024) to the
// aggregate PoolUtilization signal for Status. Means are computed at read time
// (running sums ÷ query count); a fresh process (Queries==0) reports zero
// averages with no division by zero.
func (e *Engine) poolUtilization() PoolUtilization {
	q := e.poolQueries.Load()
	u := PoolUtilization{
		Queries:   q,
		Saturated: e.poolSaturated.Load(),
	}
	if q > 0 {
		u.AvgFetched = float64(e.poolFetchedSum.Load()) / float64(q)
		u.AvgKept = float64(e.poolKeptSum.Load()) / float64(q)
	}
	return u
}

// Files lists every ingested document, sorted by file path.
func (e *Engine) Files() ([]FileEntry, error) {
	var out []FileEntry
	_ = e.db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			out = append(out, FileEntry{
				FilePath:   d.FilePath,
				FileType:   d.FileType,
				Status:     d.Status,
				ChunkCount: d.ChunkCount,
			})
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out, nil
}

// Dirs groups ingested documents by directory, returning file/chunk counts.
func (e *Engine) Dirs() ([]DirEntry, error) {
	type counts struct{ files, chunks int }
	m := map[string]*counts{}
	_ = e.db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			dir := filepath.Dir(d.FilePath)
			entry := m[dir]
			if entry == nil {
				entry = &counts{}
				m[dir] = entry
			}
			entry.files++
			entry.chunks += d.ChunkCount
		}
		return true
	})
	dirs := make([]string, 0, len(m))
	for d := range m {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	out := make([]DirEntry, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, DirEntry{Dir: d, Files: m[d].files, Chunks: m[d].chunks})
	}
	return out, nil
}
