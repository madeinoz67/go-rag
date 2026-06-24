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

import "context"

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
