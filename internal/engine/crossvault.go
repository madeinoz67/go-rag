package engine

// crossvault.go (US2/T017) implements cross-vault query: when
// QueryRequest.Vaults is non-empty, retrieval fans out across the named vaults.
// Each vault's BM25 + vector lists are gathered, fused via N-way reciprocal
// rank fusion (index.ReciprocalRankFusionN), the merged pool is capped and
// reranked, and threshold/dedup apply after the merge. The reranker is
// vault-agnostic (it scores query×text pairs) so no rerank-layer change is
// needed. The single-vault Engine.Query path is untouched — Query branches here
// before any single-vault work when Vaults is non-empty.

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/observe"
	"github.com/madeinoz67/go-rag/internal/rerank"
)

// vaultSetWS derives a deterministic 8-byte prefix from the sorted vault names.
// It is the WS component of the cross-vault result/embed cache key, so a
// cross-vault query is always distinct from any single-vault key, and two
// cross-vault queries over the same vault set share a key (US2/T017).
func vaultSetWS(vaults []string) [8]byte {
	sv := append([]string(nil), vaults...)
	sort.Strings(sv)
	h := sha256.Sum256([]byte(strings.Join(sv, "\x00")))
	var ws [8]byte
	copy(ws[:], h[:8])
	return ws
}

// crossVaultQuery implements the cross-vault retrieval path (US2/T017). It is
// the single-vault Engine.Query flow generalized to N vaults: per-vault
// BM25+vector retrieval → N-way RRF fusion → cap → rerank → threshold/dedup →
// context. The per-hit source vault is tracked (chunkWS) so chunk/doc/context
// lookups resolve in the correct vault. req.Query is already transformed and
// effK/effRRFK/effPool/effMode are already resolved by the caller.
func (e *Engine) crossVaultQuery(ctx context.Context, req QueryRequest, effK, effRRFK, effPool int, effMode string) (res *QueryResult, err error) {
	cacheWS := vaultSetWS(req.Vaults)
	ctx, span := observe.StartSpan(ctx, observe.SpanQuery, observe.ModeAttr(req.Mode), observe.KAttr(req.K))
	start := time.Now()
	defer func() {
		observe.RecordQuery(ctx, req.Mode, time.Since(start), err)
		hits := 0
		if res != nil {
			hits = len(res.Hits)
			observe.RecordQueryResults(ctx, req.Mode, hits)
		}
		audit.Log(audit.QueryEvent(req.Query, req.Mode, req.K, hits, err))
		observe.SpanError(span, err)
		span.End()
	}()

	mode := index.ParseMode(effMode)

	// Resolve vaults; skip any with an embedding mismatch (graceful degradation
	// — a mid-migration vault is skipped rather than failing the whole query).
	em := e.embedderOrOllama()
	qpre := e.cfg.Prefixer()
	type vaultPlan struct {
		name string
		ws   [8]byte
	}
	var plans []vaultPlan
	for _, name := range req.Vaults {
		vws := e.db.ResolveVaultPrefix(name)
		prof := CorpusProfile(vws, e.db)
		if perr := checkEmbeddingMismatch(ctx, em, qpre, req.Query, prof); perr != nil {
			log.Printf("cross-vault query: skipping vault %q: %v", name, perr)
			continue
		}
		plans = append(plans, vaultPlan{name: name, ws: vws})
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("cross-vault query: no queryable vaults in %v (all empty or embedding-mismatched)", req.Vaults)
	}

	// Combined epoch — the sum across all vaults. Any single vault's mutation
	// changes the sum, invalidating the cross-vault cache entry.
	combinedEpoch := func() uint64 {
		var sum uint64
		for _, p := range plans {
			sum += e.indexEpoch(p.ws)
		}
		return sum
	}
	keyEpoch := combinedEpoch()

	// Result cache check. The key folds in the sorted vault set (via resultKey
	// reading req.Vaults) and the cross-vault WS, so it never collides with a
	// single-vault key.
	if !req.NoCache && e.resultCache.Enabled() {
		if cached, ok := e.resultCache.Get(e.resultKey(req, effRRFK, effK, effPool, keyEpoch, cacheWS)); ok {
			observe.CacheHit(ctx, "result")
			return cached, nil
		}
		observe.CacheMiss(ctx, "result")
	}

	// Shared query-embedding func. The query vector is identical across vaults;
	// cache it under the cross-vault WS so it is distinct from any single-vault
	// embedding cache entry (US2/T017 cache-key requirement).
	queryEmbed := func(ctx context.Context, texts []string) ([][]float32, error) {
		prefixed := qpre.ApplyAll(embed.RoleQuery, texts)
		if !e.embedCache.Enabled() {
			return em.Embed(ctx, prefixed)
		}
		fp := embedFingerprint(em, qpre)
		out := make([][]float32, len(prefixed))
		var missText []string
		var missIdx []int
		for i, t := range prefixed {
			if v, ok := e.embedCache.Get(embedCacheKey(cacheWS, fp, t)); ok {
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
			e.embedCache.Put(embedCacheKey(cacheWS, fp, missText[j]), got[j])
		}
		return out, nil
	}

	// Reranker (vault-agnostic — scores query×text pairs, no vault awareness).
	var reranker index.Reranker
	if e.cfg.RerankModel != "" && !req.NoRerank {
		rerankEndpoint := e.cfg.RerankEndpoint
		if rerankEndpoint == "" {
			rerankEndpoint = e.cfg.OllamaURL
		}
		reranker = rerank.New(e.cfg.RerankProvider, rerankEndpoint, e.cfg.RerankModel, e.cfg.RerankAPIKey)
	}

	// Fan out: for each vault, build a Retrieval and collect its candidate lists
	// (BM25 + vector for hybrid; one list for keyword/semantic). Track ChunkID →
	// source-vault-ws so post-fusion lookups resolve in the correct vault.
	chunkWS := make(map[string][8]byte)
	var allLists [][]index.Hit

	filterOn := req.Filter != nil && !req.Filter.Empty()
	poisonOn := e.cfg.EffectivePoisoningEnabled() && !req.IncludeQuarantined
	for _, p := range plans {
		pws := p.ws
		fts, vec, ierr := e.indexes(pws)
		if ierr != nil {
			return nil, fmt.Errorf("cross-vault query: load index for vault %q: %w", p.name, ierr)
		}
		r := index.NewRetrieval(pws, fts, vec, queryEmbed)
		r.SetRRFK(effRRFK)
		r.SetPoolSize(effPool)
		if filterOn || poisonOn {
			var f index.Filter
			if filterOn {
				f = *req.Filter
			}
			r.SetFilter(func(chunkID string) bool {
				c, ok := lookupChunk(e.db, pws, chunkID)
				if !ok {
					return false
				}
				if poisonOn && c.Poisoning != nil && c.Poisoning.Level.Quarantined() {
					return false
				}
				if filterOn {
					d, ok := lookupDoc(e.db, pws, c.DocumentID)
					if !ok {
						return false
					}
					if !f.Matches(d.FilePath, d.FileType, tagsForDoc(d.Metadata, d.Enrichment)) {
						return false
					}
				}
				return true
			})
		}
		lists, lerr := r.CandidateLists(ctx, req.Query, mode)
		if lerr != nil {
			return nil, fmt.Errorf("cross-vault query: retrieve vault %q: %w", p.name, lerr)
		}
		for _, list := range lists {
			for _, h := range list {
				if _, ok := chunkWS[h.ChunkID]; !ok {
					chunkWS[h.ChunkID] = pws // first vault wins on duplicate ChunkIDs
				}
			}
			allLists = append(allLists, list)
		}
	}

	// Fuse all lists via N-way RRF (same formula as the 2-list path, over N).
	fused := index.ReciprocalRankFusionN(allLists, effRRFK)

	// Vault-aware docOf for CollapseByDoc — looks up each chunk in its source
	// vault so the top-1-per-doc collapse is correctly scoped.
	crossDocOf := func(chunkID string) string {
		c, ok := lookupChunk(e.db, chunkWS[chunkID], chunkID)
		if !ok {
			return ""
		}
		return c.DocumentID
	}

	// Cap the post-RRF pre-rerank pool: min(N*effPool, 2*effPool). This keeps
	// the rerank budget at most 2× the single-vault budget regardless of how
	// many vaults are queried. CollapseByDoc simultaneously keeps top-1 per
	// document (scoped per-vault via crossDocOf).
	poolCap := len(plans) * effPool
	if cap2 := 2 * effPool; cap2 < poolCap {
		poolCap = cap2
	}
	pool := index.CollapseByDoc(fused, poolCap, crossDocOf)

	// Rerank the capped pool (vault-agnostic). On failure, fall back to RRF
	// order and flag rerankFailed (same degradation contract as single-vault).
	chunkText := func(chunkID string) string {
		c, ok := lookupChunk(e.db, chunkWS[chunkID], chunkID)
		if !ok {
			return ""
		}
		return c.Content
	}
	var rerankFailed bool
	if reranker != nil && len(pool) > 0 {
		texts := make([]string, len(pool))
		for i, h := range pool {
			texts[i] = chunkText(h.ChunkID)
		}
		scores, rerr := reranker.Score(ctx, req.Query, texts)
		if rerr != nil || len(scores) != len(pool) {
			log.Printf("cross-vault rerank failed: model=%s candidates=%d scores=%d err=%v",
				reranker.Model(), len(pool), len(scores), rerr)
			rerankFailed = true
		} else {
			type scored struct {
				hit index.Hit
				s   float64
			}
			ss := make([]scored, len(pool))
			for i, h := range pool {
				ss[i] = scored{h, scores[i]}
			}
			sort.Slice(ss, func(i, j int) bool { return ss[i].s > ss[j].s })
			reranked := make([]index.Hit, 0, effK)
			for i := 0; i < effK && i < len(ss); i++ {
				ss[i].hit.Score = ss[i].s
				reranked = append(reranked, ss[i].hit)
			}
			pool = reranked
		}
	}
	if reranker == nil || rerankFailed {
		// No rerank (or rerank failed): truncate the RRF-ordered pool to effK.
		if effK < len(pool) {
			pool = pool[:effK]
		}
	}

	// Build QueryHits — each hit's chunk/doc is looked up in its source vault.
	hits := pool
	out := make([]QueryHit, 0, len(hits))
	for _, h := range hits {
		hws := chunkWS[h.ChunkID]
		c, ok := lookupChunk(e.db, hws, h.ChunkID)
		if !ok {
			continue
		}
		filePath, summary, enrichStatus := "", "", ""
		if d, ok := lookupDoc(e.db, hws, c.DocumentID); ok {
			filePath = d.FilePath
			if d.Enrichment != nil { // spec 029: surface doc summary + status
				summary = d.Enrichment.Summary
				enrichStatus = d.Enrichment.Status
			}
		}
		out = append(out, QueryHit{
			ChunkID:           h.ChunkID,
			DocumentID:        c.DocumentID,
			Score:             h.Score,
			ChunkIndex:        c.ChunkIndex,
			Content:           c.Content,
			FilePath:          filePath,
			Page:              c.PageNumber,
			Preview:           preview(c.Content, 160),
			Poisoning:         c.Poisoning,
			SectionContext:    c.SectionContext,
			SectionLevel:      c.SectionLevel,
			ExtractionMethod:  c.ExtractionMethod,
			ExtractionQuality: c.ExtractionQuality,
			Wikilinks:         c.Wikilinks,
			NearDup:           c.NearDup,
			Summary:           summary,
			EnrichmentStatus:  enrichStatus,
		})
	}

	// Normalize scores to [0,1] within the result set (skip when the reranker
	// succeeded — its scores are already 0..1, same contract as single-vault).
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
	// Threshold on the normalized [0,1] scale.
	if req.Threshold > 0 {
		filtered := out[:0]
		for _, h := range out {
			if h.Score >= req.Threshold {
				filtered = append(filtered, h)
			}
		}
		out = filtered
	}
	// Near-duplicate collapse (opt-in, same as single-vault — purely subtractive).
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

	// Context expansion (per-hit vault lookup so siblings resolve correctly).
	if req.ContextWindow > 0 {
		for i := range out {
			out[i].Context = e.expandContext(chunkWS[out[i].ChunkID], out[i].ChunkID, req.ContextWindow)
		}
	}

	// Pool utilization stats.
	e.poolQueries.Add(1)
	e.poolFetchedSum.Add(uint64(poolCap))
	e.poolKeptSum.Add(uint64(len(out)))
	if len(out) < effK {
		e.poolSaturated.Add(1)
	}

	res = &QueryResult{Hits: out, RerankFailed: rerankFailed,
		EffectiveK: effK, EffectivePool: effPool, EffectiveMode: effMode}

	// Cache store (skip when disabled, rerank failed, or a vault mutated
	// mid-query — any vault's epoch advance changes the combined epoch).
	if e.resultCache.Enabled() && !rerankFailed && combinedEpoch() == keyEpoch {
		e.resultCache.Put(e.resultKey(req, effRRFK, effK, effPool, keyEpoch, cacheWS), res)
	}
	return res, nil
}
