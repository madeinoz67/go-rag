package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// fakeEmbed satisfies embed.Embedder for hermetic ingestion — no Ollama server.
// Keyword-mode queries read the BM25 index and never call it.
type fakeEmbed struct{}

func (f *fakeEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1.0, 0.0}
	}
	return out, nil
}
func (f *fakeEmbed) Dimensions() int { return 2 }
func (f *fakeEmbed) Model() string   { return "fake" }

var _ embed.Embedder = (*fakeEmbed)(nil)

// newEngineWithCorpus opens a fresh DB, ingests doc, returns an Engine whose
// keyword-mode queries yield real BM25 hits.
func newEngineWithCorpus(t *testing.T, doc string) *engine.Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(), index.NewVector(), nil)
	defer p.Close()
	dp := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(dp, []byte(doc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := p.Ingest(context.Background(), dp, "*"); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return engine.NewWithDB(cfg, db)
}

func TestREST_Health(t *testing.T) {
	eng := newEngineWithCorpus(t, "x")
	srv := httptest.NewServer(New(eng, "").Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health body: %v", err)
	}
	if body["ok"] != true {
		t.Errorf("health ok = %v, want true", body["ok"])
	}
}

func TestREST_Query_HappyPath(t *testing.T) {
	eng := newEngineWithCorpus(t, "the go-rag server performs keyword retrieval over local documents")
	srv := httptest.NewServer(New(eng, "").Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"query": "retrieval", "mode": "keyword", "k": 5})
	resp, err := http.Post(srv.URL+"/v1/query", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out queryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Hits) == 0 {
		t.Fatal("expected >=1 hit for 'retrieval', got 0")
	}
	if out.Hits[0].FilePath == "" {
		t.Error("hit file_path is empty")
	}
	if out.Hits[0].ChunkID == "" {
		t.Error("hit chunk_id is empty")
	}
}

func TestREST_Query_Unauthorized_MissingToken(t *testing.T) {
	eng := newEngineWithCorpus(t, "hello world")
	srv := httptest.NewServer(New(eng, "secret").Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"query": "hello", "mode": "keyword"})
	resp, err := http.Post(srv.URL+"/v1/query", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestREST_Query_Unauthorized_WrongToken(t *testing.T) {
	eng := newEngineWithCorpus(t, "hello world")
	srv := httptest.NewServer(New(eng, "secret").Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"query": "hello", "mode": "keyword"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/query", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer nope")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestREST_Query_Authorized(t *testing.T) {
	eng := newEngineWithCorpus(t, "the server performs keyword retrieval over documents")
	srv := httptest.NewServer(New(eng, "secret").Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"query": "retrieval", "mode": "keyword"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/query", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out queryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Hits) == 0 {
		t.Fatal("expected hits with valid bearer")
	}
}

func TestREST_Query_EmptyQuery_BadRequest(t *testing.T) {
	eng := newEngineWithCorpus(t, "anything")
	srv := httptest.NewServer(New(eng, "").Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"query": "", "mode": "keyword"})
	resp, err := http.Post(srv.URL+"/v1/query", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestREST_Query_InvalidJSON_BadRequest(t *testing.T) {
	eng := newEngineWithCorpus(t, "anything")
	srv := httptest.NewServer(New(eng, "").Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/query", "application/json", bytes.NewReader([]byte("not-json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// slowFakeOllama is an Ollama stand-in that sleeps per embedding request, so a
// call that blocks on embedding latency is detectable. It still returns valid
// (fixed) embeddings so background workers eventually succeed and drain cleanly.
func slowFakeOllama(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		out := struct {
			Embeddings [][]float32 `json:"embeddings"`
		}{}
		for range req.Input {
			out.Embeddings = append(out.Embeddings, []float32{1.0, 0.0})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestREST_Add_AsyncAfterACK is the Principle IV regression guard: an Add
// request must ACK on the durable store and NOT block the client on embedding
// latency. The embedder here sleeps 300ms per request; if Add waited for
// embeddings it would take >=300ms. We assert it returns far faster.
func TestREST_Add_AsyncAfterACK(t *testing.T) {
	const embedDelay = 300 * time.Millisecond
	slow := slowFakeOllama(t, embedDelay)

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.OllamaURL = slow.URL
	cfg.EmbeddingModel = "fake"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) // runs last
	eng := engine.NewWithDB(cfg, db)
	t.Cleanup(eng.Close) // drains the slow background embeddings (still < server close)

	docPath := filepath.Join(dir, "async.txt")
	if err := os.WriteFile(docPath, []byte("async after ack embedding latency test document for go-rag"), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	srv := httptest.NewServer(New(eng, "").Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"path": docPath})
	start := time.Now()
	resp, err := http.Post(srv.URL+"/v1/add", "application/json", bytes.NewReader(body))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("POST /v1/add: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// The embedder sleeps 300ms/request. Async-after-ACK means Add returns on the
	// durable store commit (milliseconds), well before that.
	if elapsed > embedDelay/2 {
		t.Fatalf("Add blocked on embedding latency: %v (want << %v)", elapsed, embedDelay)
	}

	var sum ingestSummary
	if err := json.NewDecoder(resp.Body).Decode(&sum); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if sum.New != 1 {
		t.Fatalf("new = %d, want 1", sum.New)
	}
}
