package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestIngest_ConcurrentWorkers ingests many files at once, forcing the pipeline's
// background workers to index into the shared FTS/Vector concurrently. With -race
// this catches any unsynchronized map access; without -race it catches the runtime
// "concurrent map read and map write" fatal that single-file tests miss.
func TestIngest_ConcurrentWorkers(t *testing.T) {
	dir := t.TempDir()
	const n = 30
	for i := 0; i < n; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("doc%d.txt", i)),
			fmt.Sprintf("document number %d with unique content about topic %d and keywords", i, i))
	}

	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	res, err := p.Ingest(context.Background(), dir, "*")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.New != n {
		t.Fatalf("want %d new, got %+v", n, res)
	}

	// Give the two background workers time to index concurrently.
	time.Sleep(150 * time.Millisecond)
	if got := p.CountDocuments(); got != n {
		t.Errorf("want %d documents, got %d", n, got)
	}
}
