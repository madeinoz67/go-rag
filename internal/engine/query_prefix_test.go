package engine_test

// query_prefix_test.go proves the query path applies the query-role instruction
// prefix (audit H07 US1). It lives in the external engine_test package alongside
// parity_test.go (same setup idioms). There is no import cycle.

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// recordingEmbed records every text it embeds, thread-safe (the query path and
// any ingest workers may call concurrently).
type recordingEmbed struct {
	mu   sync.Mutex
	seen []string
}

func (r *recordingEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	r.mu.Lock()
	r.seen = append(r.seen, texts...)
	r.mu.Unlock()
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1), 0.3}
	}
	return out, nil
}
func (r *recordingEmbed) Dimensions() int { return 2 }
func (r *recordingEmbed) Model() string   { return "nomic-embed-text" }

var _ embed.Embedder = (*recordingEmbed)(nil)

// TestQuery_AppliesQueryPrefix proves engine.Query embeds the query with the
// query-role prefix: a semantic query reaches the embedder prefixed with
// "search_query: " (US1, FR-009 cross-transport parity via the shared engine path).
func TestQuery_AppliesQueryPrefix(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	em := &recordingEmbed{}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "nomic-embed-text"

	// Populate the corpus via a standalone pipeline with the SAME embedder + a
	// nomic prefixer (documents embedded with "search_document:").
	ppre := cfg.Prefixer()
	pp := pipeline.New(db, chunk.NewSplitter(512, 50), em, index.NewFTS(), index.NewVector(), ppre)
	docPath := dir + "/doc.txt"
	if err := os.WriteFile(docPath, []byte("retrieval augmented generation combines retrieval with a language model"), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := pp.Ingest(context.Background(), docPath, "*"); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	pp.Close()

	before := len(em.snapshot())

	// Query through the engine — the query path builds its own prefixer from cfg.
	eng := engine.NewWithEmbedder(cfg, db, em)
	t.Cleanup(eng.Close)
	res, err := eng.Query(context.Background(), engine.QueryRequest{
		Query: "what is retrieval augmented generation",
		Mode:  "semantic",
		K:     5,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	_ = res

	// At least one new embed call received the query-prefixed text.
	after := em.snapshot()
	found := false
	for i := before; i < len(after); i++ {
		if strings.HasPrefix(after[i], "search_query: ") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("query was not embedded with the query prefix; embedder saw: %v", after[before:])
	}
}

func (r *recordingEmbed) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.seen))
	copy(out, r.seen)
	return out
}
