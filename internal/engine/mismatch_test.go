package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// testEmbedder is a configurable embed.Embedder for mismatch tests: its Model()
// and Dimensions() are set explicitly so a test can simulate model/dim drift
// without Ollama. Embed returns a unit vector along axis 0 of the given dim.
type testEmbedder struct {
	model string
	dim   int
}

func (e testEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		if e.dim > 0 {
			v[0] = 1
		}
		out[i] = v
	}
	return out, nil
}
func (e testEmbedder) Dimensions() int { return e.dim }
func (e testEmbedder) Model() string   { return e.model }

// ingestUnder builds a fresh vault and ingests a small markdown corpus under em,
// returning the open db + a config for it (cleanup closes the db). Stored
// embeddings carry em.Model() and dim em.Dimensions().
func ingestUnder(t *testing.T, em testEmbedder) (*storage.DB, config.Config, func()) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	corpusDir := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		t.Fatalf("mkdir corpus: %v", err)
	}
	// A long-enough doc with a small chunk size yields several alpha chunks, so a
	// couple of planted minority vectors cannot outvote the majority.
	if err := os.WriteFile(filepath.Join(corpusDir, "doc.md"),
		[]byte("# Retrieval\n\n"+
			"Chunking splits documents into smaller pieces before embedding. "+
			"Embeddings turn text into dense vectors for similarity search. "+
			"Hybrid retrieval fuses lexical and semantic rankings via fusion. "+
			"Storage keeps every vector and chunk on local disk durably. "+
			"Security keeps the database local and off the network by default. "+
			"Migration re-embeds chunks when the model changes over time.\n"), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = em.Model()
	cfg.WatchDirs = nil
	cfg.ChunkSize = 8 // small → multiple chunks so the majority is unambiguous
	cfg.ChunkOverlap = 2
	eng := NewWithEmbedder(cfg, db, em)
	if _, err := eng.Add(context.Background(), corpusDir); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	eng.Close() // drain async-after-ACK embeddings
	return db, cfg, func() { db.Close() }
}

// plantEmbedding writes a raw storedEmbedding record (prefix 0x04) to simulate a
// mid-migration minority vector under a different model/dim.
func plantEmbedding(t *testing.T, db *storage.DB, chunkID, model string, dim int) {
	t.Helper()
	rec, _ := json.Marshal(struct {
		Model  string    `json:"model,omitempty"`
		Vector []float32 `json:"vector"`
	}{Model: model, Vector: make([]float32, dim)})
	if err := db.SetWithPrefix(storage.PrefixEmbedding, []byte(chunkID), rec); err != nil {
		t.Fatalf("plant embedding: %v", err)
	}
}

// --- US1: refuse a mismatched query ---

func TestQuery_RefusesModelMismatch(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "beta", dim: 4}) // same dim, different model
	_, err := eng.Query(context.Background(), QueryRequest{Query: "retrieval", K: 5, Mode: "hybrid"})
	if !errors.Is(err, ErrEmbeddingMismatch) {
		t.Fatalf("expected ErrEmbeddingMismatch for different model, got %v", err)
	}
}

func TestQuery_RefusesDimMismatch(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 8}) // same model, different dim
	_, err := eng.Query(context.Background(), QueryRequest{Query: "retrieval", K: 5, Mode: "hybrid"})
	if !errors.Is(err, ErrEmbeddingMismatch) {
		t.Fatalf("expected ErrEmbeddingMismatch for different dim, got %v", err)
	}
}

func TestQuery_RefusesSameDimDifferentModel(t *testing.T) {
	// Same dimensionality does NOT make a different model safe (different semantic space).
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "gamma", dim: 4})
	_, err := eng.Query(context.Background(), QueryRequest{Query: "retrieval", K: 5})
	if !errors.Is(err, ErrEmbeddingMismatch) {
		t.Fatalf("expected refusal for same-dim different-model, got %v", err)
	}
}

func TestQuery_HappyPathMatchingModel(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4}) // matches
	res, err := eng.Query(context.Background(), QueryRequest{Query: "retrieval", K: 5, Mode: "hybrid"})
	if err != nil {
		t.Fatalf("matching query must not error, got %v", err)
	}
	// No false alarm on the happy path; results are returned.
	_ = res
}

func TestQuery_EmptyCorpusNoError(t *testing.T) {
	// An empty vault: a query returns no results without a mismatch error.
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	os.MkdirAll(dataDir, 0o755)
	db, _ := storage.Open(dataDir)
	defer db.Close()
	cfg := config.Default()
	cfg.DBPath = dir
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4})
	res, err := eng.Query(context.Background(), QueryRequest{Query: "anything", K: 5})
	if err != nil {
		t.Fatalf("empty corpus must not error, got %v", err)
	}
	if res == nil || len(res.Hits) != 0 {
		t.Fatalf("empty corpus must return no hits, got %+v", res)
	}
}

// --- US2: status drift ---

func TestStatus_ReportsDrift(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()

	// Consistent corpus: no drift.
	st, err := NewWithDB(cfg, db).Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.EmbeddingDrift {
		t.Fatalf("consistent corpus must not report drift")
	}
	if st.EmbeddingModel != "alpha" || st.Dimensions != 4 {
		t.Fatalf("status should report stored majority alpha/4, got %q/%d", st.EmbeddingModel, st.Dimensions)
	}

	// Plant a minority under a different model/dim → drift.
	plantEmbedding(t, db, "stale1", "beta", 8)
	plantEmbedding(t, db, "stale2", "beta", 8)

	st2, _ := NewWithDB(cfg, db).Status()
	if !st2.EmbeddingDrift {
		t.Fatalf("mixed corpus must report drift")
	}
	if st2.ModelCounts["alpha"] == 0 || st2.ModelCounts["beta"] != 2 {
		t.Fatalf("status should report per-model counts, got %+v", st2.ModelCounts)
	}
}

// --- US3: graceful partial degradation ---

func TestQuery_PartialMajoritySucceeds(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	// Minority under beta/8 (mid-migration).
	plantEmbedding(t, db, "stale1", "beta", 8)
	plantEmbedding(t, db, "stale2", "beta", 8)

	// Querying under the MAJORITY (alpha/4) must succeed (minority skipped), not refuse.
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4})
	if _, err := eng.Query(context.Background(), QueryRequest{Query: "retrieval", K: 5}); err != nil {
		t.Fatalf("majority query over mixed corpus must not fail, got %v", err)
	}
}

func TestQuery_PartialMinorityRefused(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	plantEmbedding(t, db, "stale1", "beta", 8)
	plantEmbedding(t, db, "stale2", "beta", 8)

	// Querying under the MINORITY (beta/8) must be refused (it does not match the majority).
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "beta", dim: 8})
	_, err := eng.Query(context.Background(), QueryRequest{Query: "retrieval", K: 5})
	if !errors.Is(err, ErrEmbeddingMismatch) {
		t.Fatalf("minority query must be refused, got %v", err)
	}
}

// --- CorpusProfile unit ---

func TestCorpusProfile_EmptyAndMajority(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	os.MkdirAll(dataDir, 0o755)
	db, _ := storage.Open(dataDir)
	defer db.Close()

	if p := CorpusProfile(db); p.Total != 0 || !p.Consistent {
		t.Fatalf("empty corpus profile = %+v, want zero/consistent", p)
	}
	plantEmbedding(t, db, "c1", "alpha", 4)
	plantEmbedding(t, db, "c2", "alpha", 4)
	plantEmbedding(t, db, "c3", "beta", 8)
	p := CorpusProfile(db)
	if p.MajorityModel != "alpha" || p.MajorityDim != 4 || p.Consistent {
		t.Fatalf("profile = %+v, want majority alpha/4 inconsistent", p)
	}
	if p.Total != 3 || p.ModelCounts["alpha"] != 2 || p.ModelCounts["beta"] != 1 {
		t.Fatalf("profile counts = %+v", p.ModelCounts)
	}
}
