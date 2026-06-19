package index

import (
	"context"
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

	kVec    int
	kFTS    int
	poolSize int
}

// NewRetrieval wires an FTS index, a Vector index, and a query embedder.
func NewRetrieval(fts *FTS, vec *Vector, embed EmbedFunc) *Retrieval {
	return &Retrieval{
		fts: fts, vec: vec, embed: embed,
		kVec: 40, kFTS: 60, poolSize: 60,
	}
}

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
