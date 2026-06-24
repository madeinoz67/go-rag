package index

// classify.go defines the query-classification seam (audit H22 / spec 024).
//
// QueryClassifier is the extension point that recommends a retrieval depth (k)
// for a query, applied by the engine ONLY when the caller has not set k
// explicitly (explicit > recommended > default). It is the same seam shape as
// QueryTransformer (transform.go) and Reranker: the interface + a pure-Go
// default (RuleBasedClassifier) live in this package; a future model-based
// classifier implements the same interface in an ADAPTER package, keeping
// internal/index free of any embedding-model dependency (FR-008, Constitution V).
//
// A classifier recommends depth ONLY — never retrieval mode (hybrid/semantic/
// keyword). Mode remains the caller's explicit choice (clarification Q1).

import (
	"context"
	"strings"
)

// QueryClassification is a classifier's recommendation for one query. K is the
// ONLY recommendation it carries — a classifier never recommends or changes
// retrieval mode. K == 0 means "no recommendation" (the engine applies the
// caller's default); a positive K is a shallow-or-standard depth suggestion.
type QueryClassification struct {
	K         int    // recommended retrieval depth; 0 == "no recommendation"
	Rationale string // human-readable, surfaced in status/log only; never affects ranking
}

// QueryClassifier recommends a retrieval depth for a query. It runs entirely
// in-process and MUST NOT call the embedding server or any network service
// (FR-008), so the index package stays dependency-free. Applied by the engine
// only when the caller has not set k explicitly (FR-006).
//
// Implementations MUST be deterministic (same query ⇒ same classification) so
// the result-cache key, which folds the effective k, stays stable, and MUST
// degrade gracefully: an empty-after-normalization query returns K:0 (never
// panics), and any unrecognised shape returns K:0 so a misclassification can
// never reduce quality below the baseline.
type QueryClassifier interface {
	Classify(ctx context.Context, query string) QueryClassification
}

// PoolSlack / PoolFloor (H22/spec 024, FR-011) govern the candidate pool derived
// from a classifier-recommended depth: the pool is k+PoolSlack, floored at
// PoolFloor so a tiny k never starves rerank. Starting values, chosen to be
// obviously safe; tunable against the eval harness (SC-001/SC-003). Promote to
// config only if per-corpus tuning proves necessary.
const (
	PoolSlack = 10
	PoolFloor = 20
)

// comparativeTerms marks broad/comparative query shapes that should keep the
// full default depth (a shallow pool would starve them). Pure lexical signal —
// no model, no network (FR-005/FR-008). The exact set is a tuning dial.
var comparativeTerms = map[string]bool{
	"compare": true, "comparison": true, "comparisons": true, "comparing": true,
	"vs": true, "versus": true,
	"differ": true, "difference": true, "differences": true, "different": true,
	"between": true, "across": true,
	"all": true, "every": true, "each": true,
	"list": true, "summarize": true, "overview": true, "survey": true,
}

// hasComparative reports whether the (already-normalized) query contains a
// comparative/listing term — a signal that a shallow pool would hurt recall.
func hasComparative(query string) bool {
	for _, tok := range strings.Fields(query) {
		if comparativeTerms[tok] {
			return true
		}
	}
	return false
}

// RuleBasedClassifier is the default QueryClassifier (audit H22/spec 024,
// FR-005): a pure-Go, model-free heuristic that recommends a shallow retrieval
// depth only for obvious short factoid lookups. Everything else returns K:0 (no
// recommendation ⇒ the caller's default depth applies), so a misclassification
// can only ever fail to speed a query up, never reduce its recall below the
// baseline. It never recommends mode.
//
// Buckets: empty ⇒ K:0; comparative/listing ⇒ K:0 (broad queries want full
// depth); ≤3 non-comparative tokens ⇒ K:3 (shallow factoid); else ⇒ K:0.
type RuleBasedClassifier struct{}

// Classify maps a query shape to a depth recommendation. The query is expected
// to already be normalized (the engine runs the QueryTransformer first); Fields
// re-splits defensively. Deterministic; graceful on empty input.
func (RuleBasedClassifier) Classify(_ context.Context, query string) QueryClassification {
	n := len(strings.Fields(query))
	switch {
	case n == 0:
		return QueryClassification{K: 0, Rationale: "empty query — no recommendation"}
	case hasComparative(query):
		return QueryClassification{K: 0, Rationale: "comparative/listing query — defer to default depth"}
	case n <= 3:
		return QueryClassification{K: 3, Rationale: "short factoid lookup"}
	default:
		return QueryClassification{K: 0, Rationale: "standard query — no recommendation"}
	}
}

// EffectivePoolFor computes the classifier-derived candidate pool for a
// recommended depth k (FR-011): k plus PoolSlack, floored at floor and capped at
// ceiling. Floor protects rerank from a starvingly small k; ceiling is the
// operator's configured cap (applied last, so it wins if floor>ceiling — a
// misconfiguration). Used only when the classifier recommended k AND no per-query
// pool override was set; otherwise the full configured ceiling is used.
func EffectivePoolFor(k, slack, floor, ceiling int) int {
	p := k + slack
	if p < floor {
		p = floor
	}
	if p > ceiling {
		p = ceiling
	}
	return p
}
