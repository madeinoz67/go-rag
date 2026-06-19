package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestReprocess_BypassesDedup proves Reprocess re-processes unchanged files (NEW),
// where plain Ingest would skip them (SKIPPED) via content-hash dedup. (T047)
func TestReprocess_BypassesDedup(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "document content for the reprocess dedup bypass test with enough words")

	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	// First Ingest: NEW.
	r1, _ := p.Ingest(context.Background(), dir, "*")
	if r1.New != 1 {
		t.Fatalf("ingest: want 1 new, got %+v", r1)
	}
	// Second Ingest: idempotent -> SKIPPED (content-hash dedup).
	r2, _ := p.Ingest(context.Background(), dir, "*")
	if r2.New != 0 || r2.Skipped != 1 {
		t.Fatalf("re-ingest: want 0 new 1 skipped, got %+v", r2)
	}
	// Reprocess: bypasses dedup -> NEW again, not skipped.
	r3, _ := p.Reprocess(context.Background(), dir, "*")
	if r3.New != 1 || r3.Skipped != 0 {
		t.Fatalf("reprocess: want 1 new 0 skipped (bypass dedup), got %+v", r3)
	}
	// Re-processed, not duplicated.
	if n := p.CountDocuments(); n != 1 {
		t.Fatalf("want 1 document after reprocess, got %d", n)
	}
}

func TestIsUnder(t *testing.T) {
	for _, c := range []struct {
		path, root string
		want       bool
	}{
		{"note.md", ".", true},                       // current dir contains relative paths
		{"./vault/note.md", ".", true},               // cleaned ./ form
		{"vault/note.md", "vault", true},             // descendant
		{"vault", "vault", true},                     // exact
		{"other/x.md", "vault", false},               // sibling, not under
		{"/a/b/c.md", "/a/b", true},                  // absolute descendant
		{"/a/other/c.md", "/a/b", false},             // absolute sibling
		{"vault2/x.md", "vault", false},              // name prefix, not a dir child
	} {
		if got := isUnder(c.path, c.root); got != c.want {
			t.Errorf("isUnder(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}

// TestReprocess_CleansStaleEntries verifies Reprocess also removes tracked
// documents under root that are no longer on disk (re-ingest won't re-add them).
func TestReprocess_CleansStaleEntries(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone.txt")
	writeFile(t, gone, "a file that will be deleted before reprocess")

	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	_, _ = p.Ingest(context.Background(), dir, "*")
	if n := p.CountDocuments(); n != 1 {
		t.Fatalf("setup: want 1 doc, got %d", n)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Reprocess(context.Background(), dir, "*")
	if n := p.CountDocuments(); n != 0 {
		t.Fatalf("after reprocess with the file deleted: want 0 docs, got %d", n)
	}
}
