package engine

import (
	"context"
	"fmt"
	"log"

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
	if err := checkEmbeddingMismatch(ctx, em, req.Query, prof); err != nil {
		return nil, err
	}
	r := index.NewRetrieval(fts, vec, em.Embed)
	if e.cfg.RerankRetryOnFailure {
		r.EnableRerankRetry() // H09 US3: optional retry of a failed rerank (off by default).
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
	return &QueryResult{Hits: out, RerankFailed: rerankFailed}, nil
}

// checkEmbeddingMismatch enforces the H03 guard: refuse to score a query whose
// embedding model or dimensionality does not match the corpus's stored majority,
// so a model change without re-index can never produce plausible-but-wrong
// results (or a panic). An empty corpus is not an error. The query dimensionality
// comes from the embedder's reported Dimensions() (definitive for deterministic
// embedders; populated for Ollama after its first response); when still unknown
// the query is probe-embedded once to discover it.
func checkEmbeddingMismatch(ctx context.Context, em embed.Embedder, query string, prof EmbeddingProfile) error {
	if prof.Total == 0 {
		return nil // empty corpus: a query simply returns no results
	}
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
