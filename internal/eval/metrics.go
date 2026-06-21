package eval

import "math"

// This file implements standard information-retrieval metrics by hand (pure Go,
// no third-party deps — Principle III). Each metric takes the ranked retrieved
// chunk_id list (in engine rank order) and the set of relevant chunk_ids for one
// query, and returns a value in [0,1]. Formulas follow the book's ch010/App.C
// definitions; see specs/004-retrieval-eval-harness/research.md (D4).
//
// The retrieved list is taken in the order the engine returns it (rank order,
// ties already resolved by score). Callers MUST skip queries with an empty
// relevant set rather than scoring them 0 (see EvalRunner, FR-008).

// RecallAt returns the fraction of relevant items found within the top-k of the
// retrieved ranking. A relevant item absent from the top-k counts as a miss in
// the numerator. If the relevant set is empty the result is 0 (caller skips it).
func RecallAt(retrieved []string, relevant map[string]bool, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	topK := retrieved
	if len(topK) > k {
		topK = topK[:k]
	}
	hits := 0
	for _, id := range topK {
		if relevant[id] {
			hits++
		}
	}
	return float64(hits) / float64(len(relevant))
}

// PrecisionAt returns the fraction of the top-k retrieved items that are
// relevant. Divides by k (the cutoff): retrieved slots beyond the result length
// count as non-relevant, the standard precision@k definition.
func PrecisionAt(retrieved []string, relevant map[string]bool, k int) float64 {
	if k <= 0 {
		return 0
	}
	topK := retrieved
	if len(topK) > k {
		topK = topK[:k]
	}
	hits := 0
	for _, id := range topK {
		if relevant[id] {
			hits++
		}
	}
	return float64(hits) / float64(k)
}

// MRR returns the reciprocal rank of the first relevant hit (1/rank), or 0 if no
// relevant item appears in the retrieved list.
func MRR(retrieved []string, relevant map[string]bool) float64 {
	for i, id := range retrieved {
		if relevant[id] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// NDCGAt returns the normalized discounted cumulative gain at k with binary
// relevance. DCG@k  = Σ rel_i / log2(rank_i + 1) over the top-k retrieved;
// IDCG@k is DCG@k of the ideal ordering (all relevant first); NDCG@k = DCG/IDCG.
// Returns 0 when there are no relevant items (IDCG is 0).
func NDCGAt(retrieved []string, relevant map[string]bool, k int) float64 {
	if k <= 0 || len(relevant) == 0 {
		return 0
	}
	topK := retrieved
	if len(topK) > k {
		topK = topK[:k]
	}
	dcg := 0.0
	for i, id := range topK {
		if relevant[id] {
			dcg += 1.0 / math.Log2(float64(i+2)) // i is 0-based → log2(rank+1)
		}
	}
	// Ideal: as many relevant items as fit in k, ranked first.
	nRel := len(relevant)
	if nRel > k {
		nRel = k
	}
	idcg := 0.0
	for i := 0; i < nRel; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}
