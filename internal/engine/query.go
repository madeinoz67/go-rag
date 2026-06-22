package engine

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/rerank"
)

// Query runs hybrid/semantic/keyword retrieval and returns ranked, cited hits.
// It is the single implementation shared by the CLI, MCP, REST, and gRPC
// adapters — extracted from the former inline mcp/server.go:query.
func (e *Engine) Query(ctx context.Context, req QueryRequest) (*QueryResult, error) {
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
	if req.K <= 0 {
		req.K = 5
	}
	if req.K > 100 {
		req.K = 100
	}

	// H08/spec 009: resolve the effective RRF k once (per-query override > config
	// > default). Used both for the result-cache key below and for the retrieval
	// fusion further down, so the key and the actual fusion can never disagree.
	effRRFK := req.RRFK
	if effRRFK <= 0 {
		effRRFK = e.cfg.EffectiveRRFK()
	}

	// H06/spec 016: result cache. Check before any embed/retrieve work — a hit
	// skips the Ollama round-trip and the whole retrieve/fuse/rerank pipeline.
	// The key folds in the normalized query, every retrieval-affecting input
	// (mode/k/threshold/rrf_k/filter/context_window/rerank), and the current
	// index epoch. NoCache bypasses serving; the freshly-computed result is still
	// stored below so the next non-override caller can hit.
	keyEpoch := e.indexEpoch()
	if !req.NoCache && e.resultCache.Enabled() {
		if cached, ok := e.resultCache.Get(e.resultKey(req, effRRFK, keyEpoch)); ok {
			return cached, nil
		}
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
		return em.Embed(ctx, qpre.ApplyAll(embed.RoleQuery, texts))
	}
	r := index.NewRetrieval(fts, vec, queryEmbed)
	if e.cfg.RerankRetryOnFailure {
		r.EnableRerankRetry() // H09 US3: optional retry of a failed rerank (off by default).
	}
	// H08/spec 009: apply the effective RRF constant (resolved once above so the
	// result-cache key and the actual fusion agree). The Retrieval is built fresh
	// per query; this is the single fusion injection point.
	r.SetRRFK(effRRFK)

	// H14/spec 014: apply the optional metadata filter (source/type/tags) as a
	// pre-fusion keep predicate. The engine resolves chunk→document→attributes
	// via the existing lookupChunk/lookupDoc resolvers. nil/empty filter = no-op.
	if req.Filter != nil && !req.Filter.Empty() {
		f := *req.Filter // copy to avoid capturing the pointer
		r.SetFilter(func(chunkID string) bool {
			c, ok := lookupChunk(e.db, chunkID)
			if !ok {
				return false
			}
			d, ok := lookupDoc(e.db, c.DocumentID)
			if !ok {
				return false
			}
			return f.Matches(d.FilePath, d.FileType, tagsFromMetadata(d.Metadata))
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
		if h.Score < req.Threshold {
			continue
		}
		c, ok := lookupChunk(e.db, h.ChunkID)
		if !ok {
			continue
		}
		filePath := ""
		if d, ok := lookupDoc(e.db, c.DocumentID); ok {
			filePath = d.FilePath
		}
		out = append(out, QueryHit{
			ChunkID:    h.ChunkID,
			DocumentID: c.DocumentID,
			Score:      h.Score,
			Content:    c.Content,
			FilePath:   filePath,
			Page:       c.PageNumber,
			Preview:    preview(c.Content, 160),
		})
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
	res := &QueryResult{Hits: out, RerankFailed: rerankFailed}
	// H06/spec 016: store the fresh result. Skip when bypassed/disabled, when the
	// reranker failed (degraded — a retry may succeed; FR-009), or when a
	// concurrent corpus mutation advanced the epoch mid-query (this result may be
	// stale relative to the new epoch — better to recompute next time).
	if !req.NoCache && e.resultCache.Enabled() && !rerankFailed && e.indexEpoch() == keyEpoch {
		e.resultCache.Put(e.resultKey(req, effRRFK, keyEpoch), res)
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
