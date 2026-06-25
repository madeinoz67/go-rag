package engine

// filter_test.go (package engine) proves the H14/spec 014 metadata filter scopes
// queries at the engine level (US1, FR-003/004): a filtered query returns only
// matching docs; unfiltered = identical to today.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/index"
)

func TestQuery_Filter_SourceScopes(t *testing.T) {
	e := newCacheEngine(t)
	dir1, dir2 := t.TempDir(), t.TempDir()
	writeDoc2 := func(dir, name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p1 := writeDoc2(dir1, "a.txt", "alpha retrieval document about searching")
	p2 := writeDoc2(dir2, "b.txt", "alpha storage document about persistence")
	if _, err := e.Add(context.Background(), p1, "*"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Add(context.Background(), p2, "*"); err != nil {
		t.Fatal(err)
	}
	waitEmbedded(t, e)

	// Unfiltered: both match.
	all, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Hits) < 2 {
		t.Fatalf("unfiltered: want >=2 hits, got %d", len(all.Hits))
	}

	// Filtered to dir1: only p1's chunks.
	filt, err := e.Query(context.Background(), QueryRequest{
		Query:  "alpha",
		Mode:   "keyword",
		K:      10,
		Filter: &index.Filter{Source: dir1},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range filt.Hits {
		if !strings.HasPrefix(h.FilePath, dir1) {
			t.Errorf("filtered hit from wrong dir: %s (want prefix %s)", h.FilePath, dir1)
		}
	}
	if len(filt.Hits) == 0 {
		t.Error("filter should return >=1 hit from dir1")
	}
}
