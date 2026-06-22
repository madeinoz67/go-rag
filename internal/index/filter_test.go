package index

import (
	"testing"
)

// TestFilter_Matches covers the Filter predicate (H14/spec 014, US1):
// source glob/prefix, type exact, tag conjunction, empty dimensions.
func TestFilter_Matches(t *testing.T) {
	cases := []struct {
		name     string
		f        Filter
		path     string
		ftype    string
		tags     []string
		want     bool
	}{
		{"empty filter = match", Filter{}, "any/path.md", "markdown", nil, true},
		{"source prefix match", Filter{Source: "docs/"}, "docs/notes.md", "markdown", nil, true},
		{"source prefix no-match", Filter{Source: "docs/"}, "other/notes.md", "markdown", nil, false},
		{"source glob match", Filter{Source: "*.md"}, "notes.md", "markdown", nil, true},
		{"source glob no-match", Filter{Source: "*.md"}, "notes.txt", "text", nil, false},
		{"type exact match", Filter{Type: "markdown"}, "any.md", "markdown", nil, true},
		{"type case-insensitive", Filter{Type: ".MD"}, "any.md", "markdown", nil, false}, // FileType is "markdown" not ".md"
		{"type no-match", Filter{Type: "pdf"}, "any.md", "markdown", nil, false},
		{"single tag match", Filter{Tags: []string{"security"}}, "any.md", "markdown", []string{"security", "ops"}, true},
		{"single tag no-match", Filter{Tags: []string{"security"}}, "any.md", "markdown", []string{"ops"}, false},
		{"no doc tags + tag filter", Filter{Tags: []string{"x"}}, "any.md", "markdown", nil, false},
		{"multi-tag conjunction all-present", Filter{Tags: []string{"a", "b"}}, "any.md", "markdown", []string{"a", "b", "c"}, true},
		{"multi-tag conjunction missing-one", Filter{Tags: []string{"a", "b"}}, "any.md", "markdown", []string{"a", "c"}, false},
		{"conjunction all dims match", Filter{Source: "docs/", Type: "markdown", Tags: []string{"x"}}, "docs/a.md", "markdown", []string{"x"}, true},
		{"conjunction source fails", Filter{Source: "docs/", Type: "markdown", Tags: []string{"x"}}, "other/a.md", "markdown", []string{"x"}, false},
		{"conjunction type fails", Filter{Source: "docs/", Type: "pdf", Tags: []string{"x"}}, "docs/a.md", "markdown", []string{"x"}, false},
		{"conjunction tag fails", Filter{Source: "docs/", Type: "markdown", Tags: []string{"x"}}, "docs/a.md", "markdown", []string{"y"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.f.Matches(c.path, c.ftype, c.tags); got != c.want {
				t.Errorf("Matches = %v, want %v", got, c.want)
			}
		})
	}
}

// TestFilter_Empty covers the Empty check.
func TestFilter_Empty(t *testing.T) {
	if !(Filter{}).Empty() {
		t.Error("zero-value Filter should be Empty")
	}
	if (Filter{Source: "x"}).Empty() {
		t.Error("Filter with Source should not be Empty")
	}
	if (Filter{Tags: []string{"x"}}).Empty() {
		t.Error("Filter with Tags should not be Empty")
	}
}

// TestRetrieval_SetFilter_PreFusion (H14 US2, FR-005): a keep predicate drops
// candidates from the fused results — non-matching chunks never reach the output.
func TestRetrieval_SetFilter_PreFusion(t *testing.T) {
	fts := NewFTS()
	vec := NewVector()
	fts.Index("keep1", map[string]string{"body": "alpha keyword"})
	fts.Index("drop1", map[string]string{"body": "alpha other"})
	vec.Add("keep1", []float32{1.0, 0.0})
	vec.Add("drop1", []float32{0.9, 0.1})

	// Without filter: both chunks appear.
	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	hits, err := r.Search(nil, "alpha", 5, ModeHybrid, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("without filter: want >=2 hits, got %d", len(hits))
	}

	// With filter: drop1 is excluded; keep1 remains.
	r2 := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	r2.SetFilter(func(chunkID string) bool { return chunkID != "drop1" })
	hits2, err := r2.Search(nil, "alpha", 5, ModeHybrid, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits2 {
		if h.ChunkID == "drop1" {
			t.Errorf("drop1 should have been filtered out pre-fusion; got %v", hits2)
		}
	}
	if len(hits2) == 0 {
		t.Error("keep1 should remain after filter")
	}
}
