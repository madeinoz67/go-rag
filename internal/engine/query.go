package engine

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/observe"
	"github.com/madeinoz67/go-rag/internal/rerank"
)

// Query runs hybrid/semantic/keyword retrieval and returns ranked, cited hits.
// It is the single implementation shared by the CLI, MCP, REST, and gRPC
// adapters — extracted from the former inline mcp/server.go:query.
func (e *Engine) Query(ctx context.Context, req QueryRequest) (res *QueryResult, err error) {
	ctx, span := observe.StartSpan(ctx, observe.SpanQuery, observe.ModeAttr(req.Mode), observe.KAttr(req.K))
	start := time.Now()
	defer func() {
		observe.RecordQuery(ctx, req.Mode, time.Since(start), err)
		hits := 0
		if res != nil {
			hits = len(res.Hits)
			observe.RecordQueryResults(ctx, req.Mode, hits)
		}
		audit.Log(audit.QueryEvent(req.Query, req.Mode, req.K, hits, err)) // H18 audit (query hashed, never plaintext)
		observe.SpanError(span, err)
		span.End()
	}()
	// H05/spec 012: transform the query (default: normalize) once, before any
	// retrieval path, in the shared engine path so every transport and mode
	// benefits. A transform that yields no usable query (whitespace-only input
	// normalizing to empty) is an error mapped to ErrInvalid so transports return a
	// client error (FR-006). The default normalizer returns exactly one query;
	// multi-query fan-out is future work, so the first is used.
	transformed, err := e.qTransformer.Transform(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(transformed) == 0 {
		return nil, fmt.Errorf("%w: transformer returned no queries", ErrInvalid)
	}
	req.Query = transformed[0]
	if req.Query == "" {
		return nil, fmt.Errorf("query is required: %w", ErrInvalid)
	}
	// H22/spec 024 (US2): resolve effective depth — explicit > recommended >
	// default (FR-006). The classifier recommends k only when the caller has not
	// set it; `classified` records whether it did, so FR-011 pool-shrinking can
	// apply only to a classifier-driven depth. With the classifier off (the
	// default posture) this collapses to today's clamp (default 5).
	classified := false
	if req.K <= 0 {
		if e.classifier != nil {
			if rec := e.classifier.Classify(ctx, req.Query); rec.K > 0 {
				req.K = rec.K
				classified = true
			}
		}
		if req.K <= 0 {
			req.K = 5
		}
	}
	if req.K > 100 {
		req.K = 100
	}
	effK := req.K

	// H08/spec 009: resolve the effective RRF k once (per-query override > config
	// > default). Used both for the result-cache key below and for the retrieval
	// fusion further down, so the key and the actual fusion can never disagree.
	effRRFK := req.RRFK
	if effRRFK <= 0 {
		effRRFK = e.cfg.EffectiveRRFK()
	}

	// H22/spec 024: resolve the effective candidate pool — per-query override >
	// classifier-derived (FR-011) > config ceiling. When the classifier drove the
	// depth this query, the pool shrinks with that k (k+slack, floored/capped);
	// otherwise the full configured ceiling is used (byte-identical default).
	effPool := req.PoolSize
	if effPool <= 0 {
		if classified { // FR-011: reduced depth must actually reduce search cost.
			effPool = index.EffectivePoolFor(effK, index.PoolSlack, index.PoolFloor, e.cfg.EffectivePoolSize())
		} else {
			effPool = e.cfg.EffectivePoolSize()
		}
	}
	effMode := req.Mode
	if effMode == "" {
		effMode = "hybrid"
	}

	// H06/spec 016: result cache. Check before any embed/retrieve work — a hit
	// skips the Ollama round-trip and the whole retrieve/fuse/rerank pipeline.
	// The key folds in the normalized query, every retrieval-affecting input
	// (mode/k/threshold/rrf_k/filter/context_window/rerank), and the current
	// index epoch. NoCache bypasses serving; the freshly-computed result is still
	// stored below so the next non-override caller can hit.
	keyEpoch := e.indexEpoch()
	if !req.NoCache && e.resultCache.Enabled() {
		if cached, ok := e.resultCache.Get(e.resultKey(req, effRRFK, effK, effPool, keyEpoch)); ok {
			observe.CacheHit(ctx, "result") // H17 tie-in
			return cached, nil
		}
		observe.CacheMiss(ctx, "result") // H17 tie-in
	}

	// H01/spec 011: reuse the engine's shared seeded index instead of rebuilding
	// it from disk on every query. The pipeline/watcher/migrate mutate this same
	// pair, so it is always current; FTS/Vector are goroutine-safe for concurrent
	// query reads + background writes.
	fts, vec, err := e.indexes()
	if err != nil {
		return nil, err
	}
	em := e.embedderOrOllama()
	prof := CorpusProfile(e.db)
	// H07: embed the query in its trained role. The query-role instruction prefix
	// is prepended inside the EmbedFunc passed to the index, so the index layer
	// stays dumb (Principle V) and every transport gets identical query encoding.
	// The same prefixer also feeds the mismatch guard's convention check (US3).
	qpre := e.cfg.Prefixer()
	if err := checkEmbeddingMismatch(ctx, em, qpre, req.Query, prof); err != nil {
		return nil, err
	}
	queryEmbed := func(ctx context.Context, texts []string) ([][]float32, error) {
		prefixed := qpre.ApplyAll(embed.RoleQuery, texts)
		// H06/spec 016: query-embedding cache. The query path embeds the (prefixed)
		// query text; cache each (profile, prefixed-text) → vector so a repeated
		// query reuses its vector without an Ollama round-trip — even when the
		// result cache misses (e.g. a different k). The profile fingerprint
		// (embedder model + dim + prefix convention) is part of the key, so a
		// model/convention change evicts by key; Migrate flushes the whole cache.
		// The cache is transparent (a query vector is deterministic in its text +
		// profile), so it is consulted regardless of req.NoCache (that flag is about
		// result freshness, not embed freshness).
		if !e.embedCache.Enabled() {
			return em.Embed(ctx, prefixed)
		}
		fp := embedFingerprint(em, qpre)
		out := make([][]float32, len(prefixed))
		var missText []string
		var missIdx []int
		for i, t := range prefixed {
			if v, ok := e.embedCache.Get(embedCacheKey(fp, t)); ok {
				out[i] = v
			} else {
				missText = append(missText, t)
				missIdx = append(missIdx, i)
			}
		}
		if len(missText) == 0 {
			return out, nil
		}
		got, err := em.Embed(ctx, missText)
		if err != nil {
			return nil, err
		}
		for j, idx := range missIdx {
			out[idx] = got[j]
			e.embedCache.Put(embedCacheKey(fp, missText[j]), got[j])
		}
		return out, nil
	}
	r := index.NewRetrieval(fts, vec, queryEmbed)
	if e.cfg.RerankRetryOnFailure {
		r.EnableRerankRetry() // H09 US3: optional retry of a failed rerank (off by default).
	}
	// H08/spec 009: apply the effective RRF constant (resolved once above so the
	// result-cache key and the actual fusion agree). The Retrieval is built fresh
	// per query; this is the single fusion injection point.
	r.SetRRFK(effRRFK)
	// H22/spec 024: apply the resolved candidate pool (per-query override |
	// config ceiling | classifier-derived in US2). The Retrieval is built fresh
	// per query; this is the single pool injection point, mirroring SetRRFK.
	r.SetPoolSize(effPool)

	// H14/spec 014 + H04/spec 019: build the pre-fusion keep predicate, composing
	// the metadata filter (doc-level) with the poisoning quarantine (chunk-level).
	// Quarantine is default-on (Q1=A): chunks whose verdict is suspicious/quarantine
	// are excluded unless req.IncludeQuarantined. Both apply as a conjunction; an
	// absent Filter and detection-off both collapse to no filter (today's behavior).
	filterOn := req.Filter != nil && !req.Filter.Empty()
	poisonOn := e.cfg.EffectivePoisoningEnabled() && !req.IncludeQuarantined
	if filterOn || poisonOn {
		var f index.Filter
		if filterOn {
			f = *req.Filter // copy to avoid capturing the pointer
		}
		r.SetFilter(func(chunkID string) bool {
			c, ok := lookupChunk(e.db, chunkID)
			if !ok {
				return false
			}
			if poisonOn && c.Poisoning != nil && c.Poisoning.Level.Quarantined() {
				return false
			}
			if filterOn {
				d, ok := lookupDoc(e.db, c.DocumentID)
				if !ok {
					return false
				}
				if !f.Matches(d.FilePath, d.FileType, tagsForDoc(d.Metadata, d.Enrichment)) { // spec 029: merge auto-tags
					return false
				}
			}
			return true
		})
	}

	var reranker index.Reranker
	if e.cfg.RerankModel != "" && !req.NoRerank {
		reranker = rerank.New(e.cfg.OllamaURL, e.cfg.RerankModel)
	}

	hits, rerankFailed, err := r.SearchWithRerank(ctx, req.Query, req.K, index.ParseMode(req.Mode), docOf(e.db), reranker, func(id string) string {
		c, ok := lookupChunk(e.db, id)
		if !ok {
			return ""
		}
		return c.Content
	})
	if err != nil {
		return nil, err
	}

	out := make([]QueryHit, 0, len(hits))
	for _, h := range hits {
		c, ok := lookupChunk(e.db, h.ChunkID)
		if !ok {
			continue
		}
		filePath, summary, enrichStatus := "", "", ""
		if d, ok := lookupDoc(e.db, c.DocumentID); ok {
			filePath = d.FilePath
			if d.Enrichment != nil { // spec 029: surface the doc's summary + status on the hit
				summary = d.Enrichment.Summary
				enrichStatus = d.Enrichment.Status
			}
		}
		out = append(out, QueryHit{
			ChunkID:          h.ChunkID,
			DocumentID:       c.DocumentID,
			Score:            h.Score,
			ChunkIndex:       c.ChunkIndex, // H21/spec 023: citation ordinal
			Content:          c.Content,
			FilePath:         filePath,
			Page:             c.PageNumber,
			Preview:          preview(c.Content, 160),
			Poisoning:        c.Poisoning,      // H04/spec 019: verdict surfaced on every hit (FR-005)
			SectionContext:   c.SectionContext, // H23/spec 025: breadcrumb surfaced on every hit (FR-004)
			NearDup:          c.NearDup,        // H20/spec 026: near-dup context surfaced on every hit (FR-004)
			Summary:          summary,          // spec 029: doc summary surfaced on every hit (FR-010)
			EnrichmentStatus: enrichStatus,     // spec 029: doc enrichment status (FR-010)
		})
	}
	// H21/spec 023: normalize scores to [0,1] within the result set (top = 1.0).
	// Skip when the reranker succeeded (its scores are already 0..1 from H09).
	if reranker == nil || rerankFailed {
		if len(out) > 0 {
			top := out[0].Score
			if top > 0 {
				for i := range out {
					out[i].Score /= top
				}
			}
		}
	}
	// H21/spec 023: threshold on the normalized [0,1] scale (was on raw RRF scores).
	if req.Threshold > 0 {
		filtered := out[:0]
		for _, h := range out {
			if h.Score >= req.Threshold {
				filtered = append(filtered, h)
			}
		}
		out = filtered
	}
	// H20/spec 026 (R7): opt-in near-duplicate collapse — drop a hit if a
	// higher-ranked kept hit is its near-dup sibling (bidirectional: either
	// lists the other in NearDup.Siblings — handles asymmetric sidecars from
	// per-job clustering). Purely subtractive; scores/ranking untouched (FR-007).
	if req.Dedup {
		kept := out[:0]
		for _, h := range out {
			drop := false
			for _, k := range kept {
				if listsSibling(h.NearDup, k.ChunkID) || listsSibling(k.NearDup, h.ChunkID) {
					drop = true
					break
				}
			}
			if !drop {
				kept = append(kept, h)
			}
		}
		out = kept
	}

	// US3 graceful degradation: a mixed corpus (mid-migration) queried by the
	// majority model is scored against the matching vectors only — the minority
	// is skipped by Vector.Query's length guard. Log it so the operator sees that
	// some vectors were excluded rather than silently mis-scored.
	if prof.Total > 0 && !prof.Consistent {
		skipped := prof.Total - prof.DimCounts[prof.MajorityDim]
		log.Printf("embedding drift: corpus has mixed models/dims; %d of %d vectors differ from the %q/%d-dim majority and were skipped",
			skipped, prof.Total, prof.MajorityModel, prof.MajorityDim)
	}
	// H15/spec 015: expand context (sibling chunks) if requested. This is purely
	// additive — after ranking/rerank/collapse; does not affect top-k or ranking.
	if req.ContextWindow > 0 {
		for i := range out {
			out[i].Context = e.expandContext(out[i].ChunkID, req.ContextWindow)
		}
	}
	// H22/spec 024: record aggregate pool utilization. This point is reached only
	// on a freshly-computed (cache-miss) query — the cache-hit path returned
	// earlier — so each distinct computation is counted exactly once and a later
	// cache hit never double-counts. effPool is the candidate budget fetched;
	// len(out) the results kept; a short result set (couldn't fill effK) is
	// "saturated" (small corpus / under-covered topic — the actionable signal).
	e.poolQueries.Add(1)
	e.poolFetchedSum.Add(uint64(effPool))
	e.poolKeptSum.Add(uint64(len(out)))
	if len(out) < effK {
		e.poolSaturated.Add(1)
	}
	res = &QueryResult{Hits: out, RerankFailed: rerankFailed,
		EffectiveK: effK, EffectivePool: effPool, EffectiveMode: effMode} // H22/spec 024 (US3): echo what was actually used
	// H06/spec 016: store the fresh result. NoCache only bypasses SERVING (D5):
	// the freshly-computed result is still stored so the next normal caller can
	// hit. Skip only when disabled, when the reranker failed (degraded — a retry
	// may succeed; FR-009), or when a concurrent corpus mutation advanced the
	// epoch mid-query (this result may be stale relative to the new epoch).
	if e.resultCache.Enabled() && !rerankFailed && e.indexEpoch() == keyEpoch {
		e.resultCache.Put(e.resultKey(req, effRRFK, effK, effPool, keyEpoch), res)
	}
	return res, nil
}

// expandContext follows the chunk's linked list (PreviousChunkID/NextChunkID) up
// to n steps each way, fetching sibling text for reading context (H15/spec 015).
// Missing siblings (boundary chunks, empty IDs) are skipped gracefully. The
// previous entries are reversed so the context reads in document order.
func (e *Engine) expandContext(chunkID string, n int) []ContextChunk {
	var ctx []ContextChunk
	// Previous N: walk backward, then reverse for document order.
	id := chunkID
	for i := 0; i < n; i++ {
		c, ok := lookupChunk(e.db, id)
		if !ok || c.PreviousChunkID == "" {
			break
		}
		prev, ok := lookupChunk(e.db, c.PreviousChunkID)
		if !ok {
			break
		}
		ctx = append(ctx, ContextChunk{ChunkID: prev.ID, Content: prev.Content, Direction: "previous"})
		id = prev.ID
	}
	for i, j := 0, len(ctx)-1; i < j; i, j = i+1, j-1 {
		ctx[i], ctx[j] = ctx[j], ctx[i]
	}
	// Next N: walk forward.
	id = chunkID
	for i := 0; i < n; i++ {
		c, ok := lookupChunk(e.db, id)
		if !ok || c.NextChunkID == "" {
			break
		}
		nxt, ok := lookupChunk(e.db, c.NextChunkID)
		if !ok {
			break
		}
		ctx = append(ctx, ContextChunk{ChunkID: nxt.ID, Content: nxt.Content, Direction: "next"})
		id = nxt.ID
	}
	return ctx
}

// checkEmbeddingMismatch enforces the H03 guard (model/dimensionality) and the
// H07 guard (prefix convention): refuse to score a query whose embedding does not
// match the corpus's stored majority, so a model change — or a convention change
// (toggling instruction prefixes) — without re-embedding can never produce
// plausible-but-wrong results. An empty corpus is not an error. The query
// dimensionality comes from the embedder's reported Dimensions() (definitive for
// deterministic embedders; populated for Ollama after its first response); when
// still unknown the query is probe-embedded once to discover it.
func checkEmbeddingMismatch(ctx context.Context, em embed.Embedder, pre *embed.Prefixer, query string, prof EmbeddingProfile) error {
	if prof.Total == 0 {
		return nil // empty corpus: a query simply returns no results
	}

	// H07 convention guard (US3): a query must use the same prefix convention as
	// the corpus. A mixed-convention corpus is mid-re-embed: its minority vectors
	// share the majority's dimensionality, so the H03 dim guard cannot exclude
	// them — scoring would silently mix conventions (FR-006). Refuse and direct
	// the operator to finish re-embedding. (Graceful skip-by-convention would
	// require convention tagging in the vector index; out of scope for this
	// S-effort item — refusing is the safe, correct behavior for the transient
	// mixed state.)
	qConv := ""
	if pre != nil {
		qConv = pre.Convention()
	}
	if len(prof.ConventionCounts) > 1 {
		return fmt.Errorf("%w: corpus has mixed prefix conventions %v (majority=%q); query convention=%q; finish re-embedding the corpus under one convention before querying",
			ErrEmbeddingMismatch, sortedKeys(prof.ConventionCounts), prof.MajorityConvention, qConv)
	}
	if qConv != prof.MajorityConvention {
		return fmt.Errorf("%w: query prefix convention=%q vs corpus convention=%q; re-embed the corpus under the configured convention before querying (a half-prefixed corpus is never scored silently)",
			ErrEmbeddingMismatch, qConv, prof.MajorityConvention)
	}

	// H03 model/dimensionality guard.
	qModel := em.Model()
	qDim := em.Dimensions()
	if qDim == 0 {
		if vs, err := em.Embed(ctx, []string{query}); err == nil && len(vs) > 0 {
			qDim = len(vs[0])
		}
	}
	if qModel != prof.MajorityModel || (qDim != 0 && qDim != prof.MajorityDim) {
		return fmt.Errorf("%w: query model=%q dim=%d vs corpus model=%q dim=%d; re-index under the configured model before querying",
			ErrEmbeddingMismatch, qModel, qDim, prof.MajorityModel, prof.MajorityDim)
	}
	return nil
}

// sortedKeys returns the sorted keys of m for deterministic error messages.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tagsForDoc returns the effective document tags for filtering: manual
// Metadata["tags"] (frontmatter) ∪ auto-generated Enrichment.Tags (spec 029
// bridge). Both sources are optional and independently absent — the auto-tags
// reach the existing --tags filter with no new query field.
func tagsForDoc(meta map[string]any, enrichment *model.EnrichInfo) []string {
	tags := tagsFromMetadata(meta)
	if enrichment != nil {
		tags = append(tags, enrichment.Tags...)
	}
	return tags
}

// tagsFromMetadata extracts document tags from Metadata["tags"] (H14/spec 014).
// Supports []string, []any (JSON array), or comma-separated string.
func tagsFromMetadata(m map[string]any) []string {
	if m == nil {
		return nil
	}
	v, ok := m["tags"]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return strings.Split(t, ",")
	}
	return nil
}
