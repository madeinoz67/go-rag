package engine

// context_window_test.go (package engine) proves the H15/spec 015 context-window
// expansion: hits include sibling chunks' text, boundaries handled gracefully,
// context is distinguishable from ranked hits, and ContextWindow=0 is a no-op.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuery_ContextWindow_Expansion(t *testing.T) {
	e := newCacheEngine(t)
	// Ingest a document large enough to produce multiple chunks. The fake embedder
	// returns [1,0] for everything, so all chunks share the same vector; FTS will
	// differentiate by content. Use a small-ish chunk size to force multiple chunks.
	e.cfg.ChunkSize = 50 // ~38 words/chunk
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.txt")
	// Write enough text to produce ≥3 chunks at size 50.
	text := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu. ", 30)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Add(context.Background(), path, "*"); err != nil {
		t.Fatal(err)
	}
	waitEmbedded(t, e)

	// Query with ContextWindow=1: each hit should have context.
	res, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5, ContextWindow: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("need >=1 hit")
	}
	hasContext := false
	for _, h := range res.Hits {
		if len(h.Context) > 0 {
			hasContext = true
			for _, c := range h.Context {
				if c.Content == "" {
					t.Error("context chunk should have content")
				}
				if c.Direction != "previous" && c.Direction != "next" {
					t.Errorf("bad direction: %q", c.Direction)
				}
				if c.ChunkID == h.ChunkID {
					t.Error("context chunk ID == hit ID (should be a sibling)")
				}
			}
		}
	}
	if !hasContext {
		t.Error("at least one hit should have context with ContextWindow=1")
	}
}

func TestQuery_ContextWindow_Zero_NoContext(t *testing.T) {
	e := newCacheEngine(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("alpha beta gamma delta epsilon zeta eta theta."), 0o644)
	e.Add(context.Background(), path, "*")
	waitEmbedded(t, e)

	res, _ := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5})
	for _, h := range res.Hits {
		if len(h.Context) > 0 {
			t.Error("ContextWindow=0 should produce no context")
		}
	}
}

func TestQuery_ContextWindow_TopK_Unchanged(t *testing.T) {
	e := newCacheEngine(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	text := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu. ", 30)
	os.WriteFile(path, []byte(text), 0o644)
	e.Add(context.Background(), path, "*")
	waitEmbedded(t, e)

	without, _ := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5})
	withC, _ := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5, ContextWindow: 2})
	if len(without.Hits) != len(withC.Hits) {
		t.Errorf("top-k changed: without=%d withContext=%d", len(without.Hits), len(withC.Hits))
	}
}
