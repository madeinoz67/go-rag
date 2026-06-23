package pipeline

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// TestEmbeddingModelRecorded verifies the worker now stores the embedding model
// alongside the vector (T048 foundation), so stale embeddings are detectable.
func TestEmbeddingModelRecorded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "embedding model tracking test content with enough words here")
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	_, _ = p.Ingest(context.Background(), dir, "*")
	p.Close() // drain async embedding so 0x04 entries are written

	stats := EmbeddingModelStats(db)
	if stats["fake"] == 0 {
		t.Fatalf("expected embeddings recorded under model 'fake', got %v", stats)
	}
}

// TestLoadIndexReadsLegacyBareVector verifies LoadIndex reads both the current
// {model,vector} embedding format and the legacy bare []float32 format, so
// existing databases keep working after the storage-shape change.
func TestLoadIndexReadsLegacyBareVector(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	bare, _ := json.Marshal([]float32{0.5, 0.5})
	if err := db.SetWithPrefix(storage.PrefixEmbedding, []byte("legacy"), bare); err != nil {
		t.Fatal(err)
	}
	cur, _ := json.Marshal(storedEmbedding{Model: "m", Vector: []float32{0.1, 0.2}})
	if err := db.SetWithPrefix(storage.PrefixEmbedding, []byte("cur"), cur); err != nil {
		t.Fatal(err)
	}

	_, vec, err := LoadIndex(db)
	if err != nil {
		t.Fatal(err)
	}
	hits := vec.Query([]float32{0.5, 0.5}, 5)
	got := map[string]bool{}
	for _, h := range hits {
		got[h.ChunkID] = true
	}
	if !got["legacy"] || !got["cur"] {
		t.Fatalf("LoadIndex must read both legacy and current embeddings; got %v", got)
	}
}
