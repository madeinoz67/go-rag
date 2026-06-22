package engine

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/rerank"
)

// Query runs hybrid/semantic/keyword retrieval and returns ranked, cited hits.
// It is the single implementation shared by the CLI, MCP, REST, and gRPC
// adapters — extracted from the former inline mcp/server.go:query.
func (e *Engine) Query(ctx context.Context, req QueryRequest) (*QueryResult, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query is required: %w", ErrInvalid)
	}
	if req.K <= 0 {
		req.K = 5
	}
	if req.K > 100 {
		req.K = 100
	}

	fts, vec, err := pipeline.LoadIndex(e.db)
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
	// H08/spec 009: apply the effective RRF constant. A per-query override
	// (req.RRFK > 0) wins, else the configured rrf_k, else the default (60). The
	// Retrieval is built fresh per query, so this is the single injection point
	// and every transport gets identical fusion for the same effective k.
	effRRFK := req.RRFK
	if effRRFK <= 0 {
		effRRFK = e.cfg.EffectiveRRFK()
	}
	r.SetRRFK(effRRFK)

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
	return &QueryResult{Hits: out, RerankFailed: rerankFailed}, nil
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
