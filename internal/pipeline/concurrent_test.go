package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIngest_SkipsGoRagDir verifies the database's own directory (.go-rag, e.g. its
// WAL/log files) is never ingested when the ingest root contains it.
func TestIngest_SkipsGoRagDir(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, ".go-rag", "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "000002.log"), []byte("WAL contents must not be ingested"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "real.txt"), "a real document that should be ingested")

	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()
	res, err := p.Ingest(context.Background(), dir, "*")
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 1 {
		t.Fatalf("only the real document should be ingested (.go-rag skipped); got %+v", res)
	}
	if n := p.CountDocuments(); n != 1 {
		t.Fatalf("want 1 document, got %d", n)
	}
}
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
