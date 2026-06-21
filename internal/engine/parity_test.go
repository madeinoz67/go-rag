package engine_test

// parity_test.go lives in the external test package engine_test (not engine) so
// it can import internal/rest and internal/grpc — the two adapters whose results
// it compares against the facade. There is no import cycle: both adapters import
// the non-test engine package, not this file.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/engine"
	goraggrpc "github.com/madeinoz67/go-rag/internal/grpc"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/rest"
	"github.com/madeinoz67/go-rag/internal/storage"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
	grpcc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// fakeEmbed satisfies embed.Embedder for hermetic ingestion (no Ollama). Keyword
// queries never call it — they read the BM25 index populated at ingest time.
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

// fastFakeOllama is an in-process Ollama stand-in that returns fixed embeddings
// instantly. Used when a test exercises engine.Add (whose lazy pipeline embeds
// via cfg.OllamaURL) so background embeddings succeed and drain cleanly.
func fastFakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// openEngine opens a fresh DB in a temp dir and returns an Engine over it.
// Cleanup (drain workers, then close DB) is registered in the right LIFO order.
func openEngine(t *testing.T, ollamaURL string) *engine.Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.OllamaURL = ollamaURL
	cfg.EmbeddingModel = "fake"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) // runs last
	eng := engine.NewWithDB(cfg, db)
	t.Cleanup(eng.Close) // runs first — drain async workers before db.Close
	return eng
}

// sharedEngine ingests doc into a temp DB via a standalone pipeline and returns
// one Engine over it. All three transports in the parity test share this single
// Engine instance — that is the structural reason their results must agree
// (spec 003 FR-002/003).
func sharedEngine(t *testing.T, doc string) *engine.Engine {
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
	t.Cleanup(func() {
		// No engine writes happen for this corpus (ingested via a separate
		// pipeline below), so the engine's own pipeline is never created.
	})
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

// writeDoc writes a temp file and returns its path (an Add target).
func writeDoc(t *testing.T, dir, name, content string) string {
	t.Helper()
	dp := filepath.Join(dir, name)
	if err := os.WriteFile(dp, []byte(content), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	return dp
}

// dialGRPC serves a gRPC server over the engine on an in-memory bufconn and
// returns a client. Server + connection are cleaned up via t.Cleanup.
func dialGRPC(t *testing.T, eng *engine.Engine) goragpb.GoragClient {
	t.Helper()
	srv := goraggrpc.NewServer(eng, "")
	lis := bufconn.Listen(1 << 20)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.GracefulStop()
		<-serveErr
	})
	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpcc.DialContext(context.Background(), "bufnet",
		grpcc.WithContextDialer(dialer),
		grpcc.WithTransportCredentials(insecure.NewCredentials()),
		grpcc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("dial bufnet: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return goragpb.NewGoragClient(conn)
}

// --- canonical hit comparison (FR-002) ---

type canonHit struct {
	ChunkID    string
	DocumentID string
	Score      float64
	Content    string
	FilePath   string
	Page       int
}

func fromEngine(h engine.QueryHit) canonHit {
	return canonHit{h.ChunkID, h.DocumentID, h.Score, h.Content, h.FilePath, h.Page}
}

// restQueryHit mirrors internal/rest's JSON DTO without importing its unexported
// type. Field tags match rest/types.go exactly.
type restQueryHit struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Score      float64 `json:"score"`
	Content    string  `json:"content"`
	FilePath   string  `json:"file_path"`
	Page       int     `json:"page"`
}
type restQueryResponse struct {
	Hits         []restQueryHit `json:"hits"`
	RerankFailed bool           `json:"rerank_failed"`
}

func fromREST(h restQueryHit) canonHit {
	return canonHit{h.ChunkID, h.DocumentID, h.Score, h.Content, h.FilePath, h.Page}
}
func fromGRPC(h *goragpb.QueryHit) canonHit {
	return canonHit{h.GetChunkId(), h.GetDocumentId(), h.GetScore(), h.GetContent(), h.GetFilePath(), int(h.GetPage())}
}

func assertHitsEqual(t *testing.T, label string, got, want []canonHit) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: hit count %d != engine %d", label, len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ChunkID != w.ChunkID || g.DocumentID != w.DocumentID ||
			g.Content != w.Content || g.FilePath != w.FilePath || g.Page != w.Page {
			t.Errorf("%s hit[%d] identity mismatch:\n got  %+v\n want %+v", label, i, g, w)
		}
		if math.Abs(g.Score-w.Score) > 1e-9 {
			t.Errorf("%s hit[%d] score %.12g != engine %.12g", label, i, g.Score, w.Score)
		}
	}
}

// --- transport invocation helpers ---

func queryOverREST(t *testing.T, baseURL, q, mode string, k int) []restQueryHit {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": q, "mode": mode, "k": k})
	resp, err := http.Post(baseURL+"/v1/query", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("REST query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("REST status = %d, want 200", resp.StatusCode)
	}
	var out restQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode REST response: %v", err)
	}
	return out.Hits
}

type restIngestSummary struct {
	New      int `json:"new"`
	Skipped  int `json:"skipped"`
	Modified int `json:"modified"`
	Deleted  int `json:"deleted"`
	Errors   int `json:"errors"`
}

func addOverREST(t *testing.T, baseURL, path string) restIngestSummary {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"path": path})
	resp, err := http.Post(baseURL+"/v1/add", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("REST add: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("REST add status = %d, want 200", resp.StatusCode)
	}
	var out restIngestSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode REST add response: %v", err)
	}
	return out
}

// TestCrossTransport_QueryParity runs one query through the facade, REST, and
// gRPC — all backed by the same Engine — and asserts the structured hits are
// identical. This is the FR-002 acceptance test for US1.
func TestCrossTransport_QueryParity(t *testing.T) {
	eng := sharedEngine(t, "the go-rag server performs keyword retrieval over local documents")

	const (
		q  = "retrieval"
		k  = 5
		md = "keyword"
	)

	// Reference: facade directly.
	ref, err := eng.Query(context.Background(), engine.QueryRequest{Query: q, Mode: md, K: k})
	if err != nil {
		t.Fatalf("engine.Query: %v", err)
	}
	if len(ref.Hits) == 0 {
		t.Fatal("need >=1 reference hit for a meaningful parity test — corpus did not match query")
	}
	want := make([]canonHit, len(ref.Hits))
	for i, h := range ref.Hits {
		want[i] = fromEngine(h)
	}

	// REST.
	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer restSrv.Close()
	restCanon := make([]canonHit, 0, len(ref.Hits))
	for _, h := range queryOverREST(t, restSrv.URL, q, md, k) {
		restCanon = append(restCanon, fromREST(h))
	}
	assertHitsEqual(t, "REST", restCanon, want)

	// gRPC (in-process bufconn).
	client := dialGRPC(t, eng)
	resp, err := client.Query(context.Background(), &goragpb.QueryRequest{Query: q, Mode: md, K: int32(k)})
	if err != nil {
		t.Fatalf("gRPC Query: %v", err)
	}
	grpcCanon := make([]canonHit, 0, len(resp.GetHits()))
	for _, h := range resp.GetHits() {
		grpcCanon = append(grpcCanon, fromGRPC(h))
	}
	assertHitsEqual(t, "gRPC", grpcCanon, want)
}

// TestCrossTransport_ReadAfterWrite_Idempotent verifies the US2 write contract:
// a document added over one transport is immediately queryable over another
// (FR-003 read-after-write), and re-adding the same path is idempotent across
// transports (FR-007, new:0). All through one shared Engine.
func TestCrossTransport_ReadAfterWrite_Idempotent(t *testing.T) {
	ollama := fastFakeOllama(t)
	eng := openEngine(t, ollama.URL)

	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer restSrv.Close()
	grpcClient := dialGRPC(t, eng)

	dir := t.TempDir()
	doc := writeDoc(t, dir, "raw.txt",
		"the go-rag server exposes a cross transport read after write contract for documents")

	// Add over REST → new:1.
	if sum := addOverREST(t, restSrv.URL, doc); sum.New != 1 {
		t.Fatalf("first REST add: new=%d, want 1", sum.New)
	}

	// Query over gRPC immediately — the doc must be retrievable (FR-003). Keyword
	// mode reads the synchronously-stored chunks, so this needs no embedding.
	resp, err := grpcClient.Query(context.Background(), &goragpb.QueryRequest{
		Query: "transport", Mode: "keyword", K: 5,
	})
	if err != nil {
		t.Fatalf("gRPC read-after-write query: %v", err)
	}
	if len(resp.GetHits()) == 0 {
		t.Fatal("read-after-write failed: gRPC query found nothing after a REST add")
	}

	// Re-add the same path over REST → idempotent (FR-007): content hash dedup.
	if sum := addOverREST(t, restSrv.URL, doc); sum.New != 0 || sum.Skipped < 1 {
		t.Errorf("REST re-add: new=%d skipped=%d, want new=0 skipped>=1", sum.New, sum.Skipped)
	}

	// Re-add the same path over gRPC → idempotent.
	gsum, err := grpcClient.Add(context.Background(), &goragpb.AddRequest{Path: doc})
	if err != nil {
		t.Fatalf("gRPC re-add: %v", err)
	}
	if gsum.GetNew() != 0 || gsum.GetSkipped() < 1 {
		t.Errorf("gRPC re-add: new=%d skipped=%d, want new=0 skipped>=1", gsum.GetNew(), gsum.GetSkipped())
	}
}

// --- full-surface parity (US3, T031) ---

// getJSON does a GET and decodes into T.
func getJSON[T any](t *testing.T, url string) T {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return v
}

// REST response mirrors (tags match internal/rest DTOs exactly).
type restStatusResponse struct {
	Documents          int    `json:"documents"`
	Chunks             int    `json:"chunks"`
	Embeddings         int    `json:"embeddings"`
	Dimensions         int    `json:"dimensions"`
	EmbeddingModel     string `json:"embedding_model"`
	Reranker           string `json:"reranker"`
	OllamaURL          string `json:"ollama_url"`
	EmbeddingsComplete bool   `json:"embeddings_complete"`
}
type restFileEntry struct {
	FilePath   string `json:"file_path"`
	FileType   string `json:"file_type"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
}
type restFilesResponse struct {
	Files []restFileEntry `json:"files"`
}
type restDirEntry struct {
	Dir    string `json:"dir"`
	Files  int    `json:"files"`
	Chunks int    `json:"chunks"`
}
type restDirsResponse struct {
	Dirs []restDirEntry `json:"dirs"`
}
type restVaultEntry struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
}
type restVaultsResponse struct {
	Vaults []restVaultEntry `json:"vaults"`
}

// TestCrossTransport_FullSurfaceParity asserts the full read operation surface
// (status, files, dirs, config, vaults) returns identical structured results over
// REST, gRPC, and the facade — all backed by one Engine (FR-002 full surface).
func TestCrossTransport_FullSurfaceParity(t *testing.T) {
	eng := sharedEngine(t, "full surface parity corpus document for go-rag status files dirs config")
	t.Cleanup(eng.Close)
	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer restSrv.Close()
	client := dialGRPC(t, eng)
	ctx := context.Background()

	// --- status ---
	rs := getJSON[restStatusResponse](t, restSrv.URL+"/v1/status")
	gs, err := client.Status(ctx, &goragpb.StatusRequest{})
	if err != nil {
		t.Fatalf("gRPC Status: %v", err)
	}
	es, _ := eng.Status()
	if rs.Documents != int(gs.GetDocuments()) || rs.Documents != es.Documents {
		t.Errorf("status.documents rest=%d grpc=%d engine=%d", rs.Documents, gs.GetDocuments(), es.Documents)
	}
	if rs.Chunks != int(gs.GetChunks()) || rs.EmbeddingModel != gs.GetEmbeddingModel() || rs.EmbeddingModel != es.EmbeddingModel {
		t.Errorf("status chunks/model: rest=%+v grpc=%+v engine=%+v", rs, gs, es)
	}
	if rs.Reranker != gs.GetReranker() || rs.EmbeddingsComplete != gs.GetEmbeddingsComplete() {
		t.Errorf("status reranker/complete: rest=%+v grpc=%+v", rs, gs)
	}

	// --- files ---
	rf := getJSON[restFilesResponse](t, restSrv.URL+"/v1/files")
	gf, err := client.Files(ctx, &goragpb.FilesRequest{})
	if err != nil {
		t.Fatalf("gRPC Files: %v", err)
	}
	ef, _ := eng.Files()
	if len(rf.Files) != len(gf.GetFiles()) || len(rf.Files) != len(ef) {
		t.Fatalf("files len rest=%d grpc=%d engine=%d", len(rf.Files), len(gf.GetFiles()), len(ef))
	}
	if len(rf.Files) > 0 {
		rf0, gf0, ef0 := rf.Files[0], gf.GetFiles()[0], ef[0]
		if rf0.FilePath != gf0.GetFilePath() || rf0.FilePath != ef0.FilePath ||
			rf0.ChunkCount != int(gf0.GetChunkCount()) || rf0.ChunkCount != ef0.ChunkCount {
			t.Errorf("files[0] rest=%+v grpc=%+v engine=%+v", rf0, gf0, ef0)
		}
	}

	// --- dirs ---
	rd := getJSON[restDirsResponse](t, restSrv.URL+"/v1/dirs")
	gd, err := client.Dirs(ctx, &goragpb.DirsRequest{})
	if err != nil {
		t.Fatalf("gRPC Dirs: %v", err)
	}
	if len(rd.Dirs) != len(gd.GetDirs()) {
		t.Errorf("dirs len rest=%d grpc=%d", len(rd.Dirs), len(gd.GetDirs()))
	}

	// --- config (all keys) ---
	rc := getJSON[map[string]string](t, restSrv.URL+"/v1/config")
	gc, err := client.GetConfig(ctx, &goragpb.GetConfigRequest{})
	if err != nil {
		t.Fatalf("gRPC GetConfig: %v", err)
	}
	ec, _ := eng.GetConfig("")
	if !reflect.DeepEqual(rc, gc.GetValues()) {
		t.Errorf("config rest != grpc: %v vs %v", rc, gc.GetValues())
	}
	if !reflect.DeepEqual(rc, ec) {
		t.Errorf("config rest != engine: %v vs %v", rc, ec)
	}

	// --- vaults ---
	rv := getJSON[restVaultsResponse](t, restSrv.URL+"/v1/vaults")
	gv, err := client.ListVaults(ctx, &goragpb.ListVaultsRequest{})
	if err != nil {
		t.Fatalf("gRPC ListVaults: %v", err)
	}
	if len(rv.Vaults) != len(gv.GetVaults()) {
		t.Errorf("vaults len rest=%d grpc=%d", len(rv.Vaults), len(gv.GetVaults()))
	}
}

// --- H09: rerank-failure parity (FR-004 / SC-003) ---

// rerankFailingOllama is an in-process Ollama stand-in that serves embeddings for
// ingest/query (so the corpus builds and the H03 guard passes) but fails rerank
// calls (/api/generate → 500), forcing a rerank failure to verify H09 surfacing.
func rerankFailingOllama(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/generate") {
			http.Error(w, "model not found", http.StatusInternalServerError)
			return
		}
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

// openRerankEngine is openEngine with a rerank model configured, so engine.Query
// builds a reranker (pointed at ollamaURL) for H09 cross-transport parity tests.
func openRerankEngine(t *testing.T, ollamaURL string) *engine.Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.OllamaURL = ollamaURL
	cfg.EmbeddingModel = "fake"
	cfg.RerankModel = "reranker"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eng := engine.NewWithDB(cfg, db)
	t.Cleanup(eng.Close)
	return eng
}

// queryOverRESTFull returns the full REST query response (hits + rerank_failed),
// for H09 parity assertions.
func queryOverRESTFull(t *testing.T, baseURL, q, mode string, k int) restQueryResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": q, "mode": mode, "k": k})
	resp, err := http.Post(baseURL+"/v1/query", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("REST query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("REST status = %d, want 200", resp.StatusCode)
	}
	var out restQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode REST response: %v", err)
	}
	return out
}

// TestCrossTransport_RerankFailureParity (H09): a query whose reranker fails
// reports the failure identically over the facade, REST, and gRPC — and still
// returns fallback-ordered results. Keyword mode reads synchronously-stored
// chunks, so no async-embedding wait is needed.
func TestCrossTransport_RerankFailureParity(t *testing.T) {
	ollama := rerankFailingOllama(t)
	eng := openRerankEngine(t, ollama.URL)

	dir := t.TempDir()
	doc := writeDoc(t, dir, "rerank.txt",
		"rerank failure parity corpus document for the go-rag retrieval surface")
	if _, err := eng.Add(context.Background(), doc); err != nil {
		t.Fatalf("Add: %v", err)
	}

	const (
		q  = "rerank"
		md = "keyword"
		k  = 5
	)

	// Facade.
	ref, err := eng.Query(context.Background(), engine.QueryRequest{Query: q, Mode: md, K: k})
	if err != nil {
		t.Fatalf("engine.Query: %v", err)
	}
	if len(ref.Hits) == 0 {
		t.Fatal("need >=1 hit so a rerank is actually attempted")
	}
	if !ref.RerankFailed {
		t.Fatal("engine: rerank should have failed (RerankFailed=true) against the failing Ollama")
	}

	// REST.
	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer restSrv.Close()
	rr := queryOverRESTFull(t, restSrv.URL, q, md, k)
	if !rr.RerankFailed {
		t.Error("REST: rerank_failed should be true (FR-004 parity)")
	}
	if len(rr.Hits) == 0 {
		t.Error("REST: should still return fallback-ordered hits (FR-007)")
	}

	// gRPC.
	client := dialGRPC(t, eng)
	gresp, err := client.Query(context.Background(), &goragpb.QueryRequest{Query: q, Mode: md, K: int32(k)})
	if err != nil {
		t.Fatalf("gRPC Query: %v", err)
	}
	if !gresp.GetRerankFailed() {
		t.Error("gRPC: rerank_failed should be true (FR-004 parity)")
	}
	if len(gresp.GetHits()) == 0 {
		t.Error("gRPC: should still return fallback-ordered hits (FR-007)")
	}
}
