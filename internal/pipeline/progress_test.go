package pipeline

import (
	"context"
	"path/filepath"
	"testing"
)

// TestIngest_Progress verifies OnProgress fires once per file with the pre-counted
// total and 1-based done counter.
func TestIngest_Progress(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "progress test content a here words")
	writeFile(t, filepath.Join(dir, "b.txt"), "progress test content b here words")

	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	var calls, lastDone, lastTotal int
	p.OnProgress = func(done, total int, _ string, _ string) {
		calls++
		lastDone = done
		lastTotal = total
	}

	res, _ := p.Ingest(context.Background(), dir, "*")
	if res.New != 2 {
		t.Fatalf("want 2 new, got %+v", res)
	}
	if calls != 2 {
		t.Fatalf("want 2 progress callbacks, got %d", calls)
	}
	if lastDone != 2 || lastTotal != 2 {
		t.Fatalf("final progress: done=%d total=%d (want 2/2)", lastDone, lastTotal)
	}
}
