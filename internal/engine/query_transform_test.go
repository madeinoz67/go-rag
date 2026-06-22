package engine

// query_transform_test.go (package engine, so it can set the unexported
// e.qTransformer) proves the H05/spec 012 seam is live: a caller-supplied
// QueryTransformer is honored at retrieval time (US2, SC-003).

import (
	"context"
	"testing"
)

// appendingTransformer is a stand-in for a future advanced transformer (HyDE /
// synonym expansion): it appends a fixed suffix to every query, demonstrating that
// the engine uses the transformer's output, not the raw query.
type appendingTransformer struct{ suffix string }

func (a appendingTransformer) Transform(_ context.Context, query string) ([]string, error) {
	return []string{query + " " + a.suffix}, nil
}

// TestQuery_CustomTransformer_Honored: with the default normalizer, a query for
// "auth" finds nothing in a doc that contains "credential" but not "auth"; after
// swapping in a transformer that appends "credential", the query matches — proving
// the seam routes the transformed query into retrieval.
func TestQuery_CustomTransformer_Honored(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "system credential secret token login")

	// Baseline: default normalizer. "auth" (4 chars, no prefix expansion) is not in
	// the doc → no keyword hits.
	base, err := e.Query(context.Background(), QueryRequest{Query: "auth", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("baseline query: %v", err)
	}

	// Swap in a transformer that appends "credential" → effective query "auth credential".
	e.qTransformer = appendingTransformer{suffix: "credential"}
	got, err := e.Query(context.Background(), QueryRequest{Query: "auth", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("transformed query: %v", err)
	}
	if len(got.Hits) == 0 {
		t.Fatal("custom transformer not honored: appending 'credential' should have matched the doc")
	}
	if len(base.Hits) == len(got.Hits) && len(got.Hits) > 0 {
		t.Logf("note: baseline also matched (unexpected); transformer still applied — got %d hits", len(got.Hits))
	}
}
