package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

type fakeEmbed struct{}

func (f *fakeEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1), 0.1}
	}
	return out, nil
}
func (f *fakeEmbed) Dimensions() int { return 2 }
func (f *fakeEmbed) Model() string   { return "fake" }

func newDetector(t *testing.T) (*ChangeDetector, *pipeline.Pipeline, *storage.DB) {
	t.Helper()
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pl := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	return New(db, pl), pl, db
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kindsOf(changes []Change) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Kind
	}
	return out
}

func TestScan_NewSkipModifyDelete(t *testing.T) {
	dir := t.TempDir()
	cd, pl, _ := newDetector(t)
	defer pl.Close()
	path := filepath.Join(dir, "a.txt")

	// NEW
	writeFile(t, path, "initial content for the watcher test")
	ch, _ := cd.ScanOnce(context.Background(), dir, "*")
	if len(ch) != 1 || ch[0].Kind != "NEW" {
		t.Fatalf("first scan: want [NEW], got %+v", kindsOf(ch))
	}
	if n := pl.CountDocuments(); n != 1 {
		t.Fatalf("after NEW: want 1 doc, got %d", n)
	}

	// SKIPPED (unchanged)
	ch, _ = cd.ScanOnce(context.Background(), dir, "*")
	if len(ch) != 1 || ch[0].Kind != "SKIPPED" {
		t.Fatalf("second scan: want [SKIPPED], got %+v", kindsOf(ch))
	}
	if n := pl.CountDocuments(); n != 1 {
		t.Fatalf("after SKIP: want 1 doc, got %d", n)
	}

	// MODIFIED (content changed -> old replaced)
	writeFile(t, path, "completely different content now")
	ch, _ = cd.ScanOnce(context.Background(), dir, "*")
	if len(ch) != 1 || ch[0].Kind != "MODIFIED" {
		t.Fatalf("modified scan: want [MODIFIED], got %+v", kindsOf(ch))
	}
	if n := pl.CountDocuments(); n != 1 {
		t.Fatalf("after MODIFY: want 1 doc (old replaced), got %d", n)
	}

	// DELETED
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ch, _ = cd.ScanOnce(context.Background(), dir, "*")
	if len(ch) != 1 || ch[0].Kind != "DELETED" {
		t.Fatalf("delete scan: want [DELETED], got %+v", kindsOf(ch))
	}
	if n := pl.CountDocuments(); n != 0 {
		t.Fatalf("after DELETE: want 0 docs, got %d", n)
	}
}

func TestScan_UnsupportedExtensionSkipped(t *testing.T) {
	dir := t.TempDir()
	cd, pl, _ := newDetector(t)
	defer pl.Close()
	writeFile(t, filepath.Join(dir, "x.weirdext"), "mystery")

	ch, _ := cd.ScanOnce(context.Background(), dir, "*")
	// Unsupported extension is not a tracked change (ingest errors, no doc stored).
	for _, c := range ch {
		if c.Kind == "NEW" {
			t.Fatalf("unsupported extension should not be ingested as NEW: %+v", c)
		}
	}
}

// keep time import used if extended
var _ = time.Second
