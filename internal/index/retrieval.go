package index

import (
	"context"
	"log"
	"sort"
)

// Mode selects the retrieval strategy.
type Mode int

const (
	ModeHybrid Mode = iota
	ModeSemantic
	ModeKeyword
)

// ParseMode maps a string ("hybrid"|"semantic"|"keyword") to a Mode.
func ParseMode(s string) Mode {
	switch s {
	case "semantic":
		return ModeSemantic
	case "keyword":
		return ModeKeyword
	default:
		return ModeHybrid
	}
}

// EmbedFunc embeds query text (the retrieval side of the Embedder interface).
type EmbedFunc func(ctx context.Context, texts []string) ([][]float32, error)

// Retrieval fuses BM25 (FTS) and vector search via Reciprocal Rank Fusion
// (PRD §4.3). K constants per the PRD: vector 40, FTS 60.
type Retrieval struct {
	fts   *FTS
	vec   *Vector
	embed EmbedFunc

	kVec     int
	kFTS     int
	poolSize int

	// retryRerank (H09 US3): when true, a failed rerank is retried once against a
	// larger candidate pool before the retrieval degrades to fallback order. Off
	// by default; the engine enables it from config so the common path incurs no
	// extra latency.
	retryRerank bool
}

// NewRetrieval wires an FTS index, a Vector index, and a query embedder.
func NewRetrieval(fts *FTS, vec *Vector, embed EmbedFunc) *Retrieval {
	return &Retrieval{
		fts: fts, vec: vec, embed: embed,
		kVec: 40, kFTS: 60, poolSize: 60,
	}
}

// EnableRerankRetry enables the H09 US3 behaviour: a failed rerank is retried
// once against a larger candidate pool (capped at maxRetryPool) before the
// retrieval degrades to fallback-ordered hits. Off by default.
func (r *Retrieval) EnableRerankRetry() { r.retryRerank = true }

// Search runs retrieval in the given mode, returning the top-k chunk hits. docOf
// (optional) maps a chunkID to its document ID; when non-nil, hits are collapsed to
// the top-1 per document (research Q8).
func (r *Retrieval) Search(ctx context.Context, query string, k int, mode Mode, docOf func(string) string) ([]Hit, error) {
	switch mode {
	case ModeKeyword:
		return collapseByDoc(r.fts.Search(query, r.poolSize), k, docOf), nil
	case ModeSemantic:
		hits, err := r.semantic(ctx, query)
		if err != nil {
			return nil, err
		}
		return collapseByDoc(hits, k, docOf), nil
	default: // hybrid
		fHits := r.fts.Search(query, r.poolSize)
		vHits, err := r.semantic(ctx, query)
		if err != nil {
			return nil, err
		}
		fused := reciprocalRankFusion(vHits, fHits, r.kVec, r.kFTS)
		return collapseByDoc(fused, k, docOf), nil
	}
}

func (r *Retrieval) semantic(ctx context.Context, query string) ([]Hit, error) {
	if r.embed == nil {
		return nil, nil
	}
	vecs, err := r.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	return r.vec.Query(vecs[0], r.poolSize), nil
}

// Reranker is an optional second-pass scorer that takes (query, candidate texts)
// and returns a normalised relevance score per candidate (0.0–1.0). Implemented
// by internal/rerank; the index package stays free of Ollama dependencies.
type Reranker interface {
	Score(ctx context.Context, query string, candidates []string) ([]float64, error)
	// Model returns the reranker model identifier, used only for failure logging
	// (H09 FR-003).
	Model() string
}

// maxRetryPool caps the candidate pool used for a H09 US3 rerank retry, bounding
// the cost of the optional second attempt.
const maxRetryPool = 200

// rerankFail carries the detail of a failed rerank attempt for a single log line
// (H09 FR-003): how many candidates were presented, how many scores came back,
// and the reranker error (nil when only a count mismatch occurred). It never
// carries query text or candidate content.
type rerankFail struct {
	candidates int
	scores     int
	err        error
}

// SearchWithRerank retrieves candidates via RRF, then optionally reranks them
// with a cross-encoder-style model. If reranker is nil, behaves exactly like
// Search. It returns (hits, rerankFailed, err):
//   - err is non-nil only on a retrieval-stage failure (propagated, FR-009) — a
//     failed retrieval has no candidates to degrade to, so it surfaces as a query
//     error rather than silent empty results.
//   - rerankFailed is true only when reranking was attempted but failed (rerank
//     error or score-count mismatch): the returned hits are in fallback (RRF)
//     order and the failure is logged once (FR-001/002/003).
//
// chunkText looks up the text for a chunkID (caller provides the DB lookup).
func (r *Retrieval) SearchWithRerank(ctx context.Context, query string, k int, mode Mode, docOf func(string) string, reranker Reranker, chunkText func(string) string) ([]Hit, bool, error) {
	if reranker == nil {
		hits, err := r.Search(ctx, query, k, mode, docOf)
		return hits, false, err
	}

	pool := r.poolSize
	if pool < k {
		pool = k
	}

	hits, fail, err := r.attemptRerank(ctx, query, k, mode, docOf, reranker, chunkText, pool)
	if err != nil {
		return nil, false, err // FR-009: retrieval-stage failure propagates.
	}
	if fail == nil {
		return hits, false, nil // rerank succeeded.
	}

	// H09 US3: optional single retry against a larger candidate pool (off by
	// default; enabled via EnableRerankRetry). Only a larger pool can change a
	// deterministic length-mismatch or transient outcome.
	if r.retryRerank {
		retryPool := pool * 2
		if retryPool > maxRetryPool {
			retryPool = maxRetryPool
		}
		if retryPool > pool {
			h2, fail2, err2 := r.attemptRerank(ctx, query, k, mode, docOf, reranker, chunkText, retryPool)
			if err2 != nil {
				return nil, false, err2 // a retry retrieval failure also propagates.
			}
			if fail2 == nil {
				return h2, false, nil // recovered.
			}
			// retry also degraded: prefer its (larger-pool) fallback hits + detail.
			hits, fail = h2, fail2
		}
	}

	// Final degradation (FR-001/002/003): log once — error cause + model + counts,
	// never query text or candidate content — and return fallback-ordered hits
	// flagged as rerank-failed.
	log.Printf("rerank failed: model=%s candidates=%d scores=%d err=%v",
		reranker.Model(), fail.candidates, fail.scores, fail.err)
	return hits, true, nil
}

// attemptRerank retrieves `pool` candidates and reranks them. Returns
// (hits, fail, err): err is non-nil only on a retrieval-stage failure (propagated);
// fail is non-nil only when reranking was attempted but failed (hits are then
// fallback-ordered, truncated to k); both nil means rerank succeeded. It never
// logs — the caller decides whether a failure is final (and worth one log line).
func (r *Retrieval) attemptRerank(ctx context.Context, query string, k int, mode Mode, docOf func(string) string, reranker Reranker, chunkText func(string) string, pool int) ([]Hit, *rerankFail, error) {
	hits, err := r.Search(ctx, query, pool, mode, docOf)
	if err != nil {
		return nil, nil, err // FR-009
	}
	if len(hits) == 0 {
		return hits, nil, nil
	}

	texts := make([]string, len(hits))
	for i, h := range hits {
		texts[i] = chunkText(h.ChunkID)
	}
	n := len(hits)
	scores, err := reranker.Score(ctx, query, texts)
	if err != nil || len(scores) != n {
		if k < len(hits) {
			hits = hits[:k]
		}
		return hits, &rerankFail{candidates: n, scores: len(scores), err: err}, nil
	}

	type scored struct {
		hit   Hit
		score float64
	}
	ss := make([]scored, len(hits))
	for i, h := range hits {
		ss[i] = scored{h, scores[i]}
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].score > ss[j].score })

	out := make([]Hit, 0, k)
	for i := 0; i < k && i < len(ss); i++ {
		ss[i].hit.Score = ss[i].score
		out = append(out, ss[i].hit)
	}
	return out, nil, nil
}

// reciprocalRankFusion merges two ranked lists: score(d) = Σ 1/(k + rank+1).
func reciprocalRankFusion(vectorHits, ftsHits []Hit, kVec, kFTS int) []Hit {
	scores := map[string]float64{}
	for rank, h := range vectorHits {
		scores[h.ChunkID] += 1.0 / float64(kVec+rank+1)
	}
	for rank, h := range ftsHits {
		scores[h.ChunkID] += 1.0 / float64(kFTS+rank+1)
	}
	out := make([]Hit, 0, len(scores))
	for id, s := range scores {
		out = append(out, Hit{ChunkID: id, Score: s})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ChunkID < out[j].ChunkID
	})
	return out
}

// collapseByDoc keeps the top-1 hit per document (when docOf is non-nil) and
// truncates to k.
func collapseByDoc(hits []Hit, k int, docOf func(string) string) []Hit {
	if docOf == nil {
		if k > 0 && k < len(hits) {
			return hits[:k]
		}
		return hits
	}
	seen := map[string]bool{}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		d := docOf(h.ChunkID)
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, h)
		if k > 0 && len(out) >= k {
			break
		}
	}
	return out
}
