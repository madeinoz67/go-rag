package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// fakeEmbed is a controllable Embedder for tests (configurable delay to verify
// async-after-ACK timing).
type fakeEmbed struct {
	delay time.Duration
}

func (f *fakeEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1), 0.1}
	}
	return out, nil
}
func (f *fakeEmbed) Dimensions() int { return 2 }
func (f *fakeEmbed) Model() string   { return "fake" }

func newTestPipeline(t *testing.T, embedDelay time.Duration) (*Pipeline, func()) {
	t.Helper()
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	p := New(db, chunk.NewSplitter(512, 50), &fakeEmbed{delay: embedDelay}, index.NewFTS(), index.NewVector())
	return p, func() { p.Close() }
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIngest_Idempotent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello world from a test document with enough words")
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	r1, _ := p.Ingest(context.Background(), dir, "*")
	if r1.New != 1 || r1.Skipped != 0 {
		t.Fatalf("first ingest: want 1 new, got %+v", r1)
	}
	r2, _ := p.Ingest(context.Background(), dir, "*")
	if r2.New != 0 || r2.Skipped != 1 {
		t.Fatalf("second ingest must skip (idempotent): got %+v", r2)
	}
	// Allow async embedding to land.
	time.Sleep(80 * time.Millisecond)
	if n := p.CountDocuments(); n != 1 {
		t.Fatalf("want 1 stored document, got %d", n)
	}
}

func TestIngest_ContentChangeReingests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "version one content here")

	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	r1, _ := p.Ingest(context.Background(), dir, "*")
	if r1.New != 1 {
		t.Fatalf("first ingest: %+v", r1)
	}
	// Change the content -> different ContentHash -> re-ingest as NEW.
	writeFile(t, path, "version two is completely different content")
	r2, _ := p.Ingest(context.Background(), dir, "*")
	if r2.New != 1 {
		t.Fatalf("changed content must re-ingest as NEW: got %+v", r2)
	}
}

func TestIngest_ACKReturnsBeforeEmbedding(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "ack timing test document content")

	p, cleanup := newTestPipeline(t, 400*time.Millisecond) // slow embedder
	defer cleanup()

	start := time.Now()
	r, _ := p.Ingest(context.Background(), dir, "*")
	elapsed := time.Since(start)

	if r.New != 1 {
		t.Fatalf("want 1 new, got %+v", r)
	}
	if elapsed >= 400*time.Millisecond {
		t.Fatalf("ACK must return before slow embedding completes; took %v", elapsed)
	}
}

func TestIngest_UnsupportedExtensionIsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.unknownext"), "mystery")

	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	r, _ := p.Ingest(context.Background(), dir, "*")
	if r.Errors != 1 {
		t.Fatalf("unsupported extension should be an error: got %+v", r)
	}
}
