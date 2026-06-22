package index

// transform.go defines the query-transformation seam (audit H05 / spec 012).
//
// QueryTransformer is the extension point that alters the query before retrieval.
// The default is a pure normalizer (trim / collapse whitespace / case-fold); future
// advanced transforms (HyDE, multi-query, synonym & acronym expansion) implement
// this same interface in an ADAPTER package, keeping internal/index free of any
// embedding-model dependency — exactly the Reranker pattern (the cross-encoder
// Reranker interface lives here; its Ollama implementation lives in internal/rerank).
//
// The seam is applied once at the top of Engine.Query, so the normalized query
// reaches every downstream consumer identically: the H03 mismatch guard, the H07
// query-role embed, and both retrieval paths (FTS + vector).

import (
	"context"
	"fmt"
	"strings"
)

// QueryTransformer alters the query before retrieval. Transform returns one or more
// queries so a future multi-query transform can yield N sub-queries without changing
// the seam (FR-005); the current retrieval path consumes the first. An unusable
// result (e.g. whitespace-only input normalizing to empty) MUST be an error, never
// a slice containing an empty string (FR-006).
type QueryTransformer interface {
	Transform(ctx context.Context, query string) ([]string, error)
}

// NormalizingTransformer is the default QueryTransformer: Unicode-aware trim +
// whitespace collapse + case-fold. Pure Go (no external dependency). Idempotent
// (FR-007); Unicode-safe, does not corrupt CJK/accents (FR-008).
type NormalizingTransformer struct{}

// Transform normalizes the query and returns it as a single-element slice (the
// current retrieval path consumes one query; the slice is the multi-query seam).
// A whitespace-only input normalizes to empty and is returned as an error (FR-006).
func (NormalizingTransformer) Transform(_ context.Context, query string) ([]string, error) {
	n := normalizeQuery(query)
	if n == "" {
		return nil, fmt.Errorf("query is empty after normalization")
	}
	return []string{n}, nil
}

// normalizeQuery applies the default normalization: Unicode case-fold (ToLower) +
// collapse all whitespace runs to a single space + trim ends. Idempotent.
//
// strings.Fields splits on any Unicode whitespace run and drops empties, so it
// both trims and collapses in one step; joining with a single space yields the
// canonical form. ToLower is applied last so the joined spaces are unaffected.
func normalizeQuery(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
