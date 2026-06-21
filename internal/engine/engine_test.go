package engine

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newTestEngine opens a fresh empty DB in a temp dir and returns an Engine over
// it. Tests that need config persistence point cfg.DBPath at the same temp dir.
func newTestEngine(t *testing.T) (*Engine, string) {
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
	t.Cleanup(func() { db.Close() })
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "nomic-embed-text"
	return NewWithDB(cfg, db), dir
}

func TestEngine_Status_EmptyDB(t *testing.T) {
	eng, _ := newTestEngine(t)
	st, err := eng.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Documents != 0 || st.Chunks != 0 || st.Embeddings != 0 {
		t.Fatalf("expected zero counts, got %+v", st)
	}
	if st.EmbeddingModel != "nomic-embed-text" {
		t.Fatalf("EmbeddingModel = %q", st.EmbeddingModel)
	}
	if st.Reranker != "disabled" {
		t.Fatalf("Reranker = %q, want disabled", st.Reranker)
	}
}

func TestEngine_FilesDirs_Empty(t *testing.T) {
	eng, _ := newTestEngine(t)
	files, err := eng.Files()
	if err != nil || len(files) != 0 {
		t.Fatalf("Files() = %v, %v", files, err)
	}
	dirs, err := eng.Dirs()
	if err != nil || len(dirs) != 0 {
		t.Fatalf("Dirs() = %v, %v", dirs, err)
	}
}

func TestEngine_GetConfig_All(t *testing.T) {
	eng, _ := newTestEngine(t)
	vals, err := eng.GetConfig("")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	for _, k := range []string{"ollama_url", "embedding_model", "chunk_size", "chunk_overlap", "db_path", "poll_interval_secs"} {
		if _, ok := vals[k]; !ok {
			t.Errorf("GetConfig missing key %q", k)
		}
	}
	if vals["embedding_model"] != "nomic-embed-text" {
		t.Errorf("embedding_model = %q", vals["embedding_model"])
	}
}

func TestEngine_SetConfig_Persists(t *testing.T) {
	eng, dir := newTestEngine(t)
	if err := eng.SetConfig("chunk_size", "256"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	// Re-loaded config from disk reflects the change (engine wrote config.json).
	loaded, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.ChunkSize != 256 {
		t.Fatalf("ChunkSize = %d, want 256", loaded.ChunkSize)
	}
}

func TestEngine_ListVaults_NoError(t *testing.T) {
	eng, _ := newTestEngine(t)
	vaults, err := eng.ListVaults()
	if err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	// Names are stable regardless of count.
	names := make([]string, 0, len(vaults))
	for _, v := range vaults {
		names = append(names, v.Name)
	}
	sort.Strings(names)
}

func TestEngine_Query_Keyword_EmptyDB(t *testing.T) {
	eng, _ := newTestEngine(t)
	// Keyword mode does not invoke the embedder, so this runs without Ollama.
	res, err := eng.Query(t.Context(), QueryRequest{Query: "anything", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("expected no hits on empty DB, got %d", len(res.Hits))
	}
}

func TestEngine_Query_RejectsEmpty(t *testing.T) {
	eng, _ := newTestEngine(t)
	if _, err := eng.Query(t.Context(), QueryRequest{Query: ""}); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestEngine_Query_KClamped(t *testing.T) {
	eng, _ := newTestEngine(t)
	// K > 100 is clamped to 100 (no panic, no error) on an empty DB.
	res, err := eng.Query(t.Context(), QueryRequest{Query: "x", Mode: "keyword", K: 9999})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(res.Hits))
	}
}
