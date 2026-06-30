package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// wikilinks_test.go (package engine) proves spec 036 / BL-004 US1 at the engine
// level: ingesting a Markdown document with [[wikilink]] syntax populates the
// resolved chunk's Wikilinks sidecar (canonical targets, chunk-scoped), the
// query hit carries the same field (FR-009 projection), and GetChunk surfaces
// it. No MuninnDB dependency — this is go-rag-internal (the bridge is a separate
// downstream consumer).

func sliceHas(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// addMarkdown writes a .md file under a temp dir and ingests it, returning the
// path. Mirrors the package's addDoc helper but forces the Markdown reader (so
// wikilink_spans is emitted) instead of the .txt text reader.
func addMarkdown(t *testing.T, e *Engine, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := e.Add(context.Background(), path, "*"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitEmbedded(t, e)
	return path
}

func TestGetChunk_SurfacesWikilinks(t *testing.T) {
	e := newCacheEngine(t)
	body := "# Auth\n\nSee [[authentication]] and [[JWT tokens]] for detail.\n"
	addMarkdown(t, e, body)

	q, err := e.Query(context.Background(), QueryRequest{Query: "authentication", Mode: "keyword", K: 5})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	// The query hit carries Wikilinks (FR-009 projection) — canonical targets
	// located in the hit chunk.
	if !sliceHas(q.Hits[0].Wikilinks, "authentication") || !sliceHas(q.Hits[0].Wikilinks, "JWT tokens") {
		t.Errorf("query hit Wikilinks = %v, want authentication + JWT tokens", q.Hits[0].Wikilinks)
	}
	// GetChunk surfaces the same Wikilinks on the resolved chunk.
	res, err := e.GetChunk(q.Hits[0].ChunkID)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if !sliceHas(res.Chunk.Wikilinks, "authentication") || !sliceHas(res.Chunk.Wikilinks, "JWT tokens") {
		t.Errorf("GetChunk Wikilinks = %v, want authentication + JWT tokens", res.Chunk.Wikilinks)
	}
}

// A Markdown chunk with no wikilinks yields an absent/empty Wikilinks sidecar
// (FR-008, omitempty), never an error.
func TestGetChunk_NoWikilinks_IsAbsent(t *testing.T) {
	e := newCacheEngine(t)
	addMarkdown(t, e, "# Plain\n\nNo links anywhere in this body text.\n")

	q, err := e.Query(context.Background(), QueryRequest{Query: "plain", Mode: "keyword", K: 5})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	res, err := e.GetChunk(q.Hits[0].ChunkID)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if len(res.Chunk.Wikilinks) != 0 {
		t.Errorf("no-link chunk Wikilinks = %v, want absent/empty", res.Chunk.Wikilinks)
	}
}

// T016 (spec 036 US2): determinism + identity safety. Two independent engines
// ingesting the same Markdown doc resolve to identical chunk IDs (the
// wikilink_spans transient is dropped before GenerateID — Constitution II) and
// identical Wikilinks per chunk (FR-006). Grammar: alpha/beta plain,
// concepts/gamma path-preserved.
func TestWikilinks_DeterministicAndIdentitySafe(t *testing.T) {
	body := "# Doc\n\nSee [[alpha]] and [[beta]] and [[concepts/gamma]] here.\n"
	e1 := newCacheEngine(t)
	addMarkdown(t, e1, body)
	e2 := newCacheEngine(t)
	addMarkdown(t, e2, body)

	q1, err := e1.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5})
	if err != nil || len(q1.Hits) == 0 {
		t.Fatalf("e1 query: err=%v hits=%d", err, len(q1.Hits))
	}
	q2, err := e2.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5})
	if err != nil || len(q2.Hits) == 0 {
		t.Fatalf("e2 query: err=%v hits=%d", err, len(q2.Hits))
	}
	if q1.Hits[0].ChunkID != q2.Hits[0].ChunkID {
		t.Errorf("chunk_id differs across engines: %q vs %q (identity not safe)", q1.Hits[0].ChunkID, q2.Hits[0].ChunkID)
	}
	if !wikilinksEq(q1.Hits[0].Wikilinks, q2.Hits[0].Wikilinks) {
		t.Errorf("Wikilinks differ across engines: %v vs %v", q1.Hits[0].Wikilinks, q2.Hits[0].Wikilinks)
	}
	for _, want := range []string{"alpha", "beta", "concepts/gamma"} {
		if !sliceHas(q1.Hits[0].Wikilinks, want) {
			t.Errorf("Wikilinks %v missing %q", q1.Hits[0].Wikilinks, want)
		}
	}
}

// T021 (spec 036 US3): a non-Markdown source (.txt) yields an absent/empty
// Wikilinks sidecar — only the Markdown reader emits wikilink_spans (FR-007).
func TestWikilinks_NonMarkdownIsAbsent(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "plain text with no wikilinks at all, just prose about authentication tokens")
	q, err := e.Query(context.Background(), QueryRequest{Query: "authentication", Mode: "keyword", K: 5})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	if len(q.Hits[0].Wikilinks) != 0 {
		t.Errorf(".txt chunk Wikilinks = %v, want absent/empty", q.Hits[0].Wikilinks)
	}
}

func wikilinksEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
