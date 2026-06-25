package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// fakeEmbed satisfies embed.Embedder for the MCP server test.
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

// populateDB creates a database + config and ingests one document via the pipeline.
func populateDB(t *testing.T, dbPath, ollamaURL, doc string) {
	t.Helper()
	cfg := config.Default()
	cfg.DBPath = dbPath
	cfg.OllamaURL = ollamaURL
	cfg.EmbeddingModel = "fake"
	_ = os.MkdirAll(filepath.Join(dbPath, "data"), 0o755)
	if err := config.Save(filepath.Join(dbPath, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(dbPath, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	defer p.Close()
	dp := filepath.Join(filepath.Dir(dbPath), "doc.txt")
	if err := os.WriteFile(dp, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Ingest(context.Background(), dp, "*"); err != nil {
		t.Fatal(err)
	}
}

func TestMCP_Initialize(t *testing.T) {
	in := strings.NewReader(jsonLine(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}))
	out := new(bytes.Buffer)
	srv := New(t.TempDir())
	if err := srv.Serve(in, out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("invalid response: %v\n%s", err, out.String())
	}
	if resp["id"].(float64) != 1 {
		t.Errorf("id mismatch: %v", resp["id"])
	}
	res := resp["result"].(map[string]any)
	if res["protocolVersion"] == nil {
		t.Error("missing protocolVersion")
	}
}

func TestMCP_ToolsList(t *testing.T) {
	in := strings.NewReader(jsonLine(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}))
	out := new(bytes.Buffer)
	if err := New(t.TempDir()).Serve(in, out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp)
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 3 {
		t.Fatalf("expected >=3 tools, got %d", len(tools))
	}
}

func TestMCP_QueryReturnsResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1.0, 0.0}}})
	}))
	defer srv.Close()

	dbPath := filepath.Join(t.TempDir(), ".go-rag")
	populateDB(t, dbPath, srv.URL, "the go-rag tool does hybrid retrieval over local documents")

	req := jsonLine(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "go_rag_query", "arguments": map[string]any{"query": "retrieval"}},
	})
	in := strings.NewReader(req)
	out := new(bytes.Buffer)
	if err := New(dbPath).Serve(in, out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("invalid response: %v\n%s", err, out.String())
	}
	if e, ok := resp["error"]; ok {
		t.Fatalf("query tool error: %v", e)
	}
	content := resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if content == "no results" {
		t.Fatalf("expected a result, got %q", content)
	}
}

func jsonLine(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b) + "\n"
}

// embed import used by fakeEmbed compile-time satisfaction check.
var _ embed.Embedder = (*fakeEmbed)(nil)

// mcpCall invokes a single tools/call and returns the parsed response.
func mcpCall(t *testing.T, dbPath, tool string, args map[string]any) map[string]any {
	t.Helper()
	req := jsonLine(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	in := strings.NewReader(req)
	out := new(bytes.Buffer)
	if err := New(dbPath).Serve(in, out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	return resp
}

func resultText(t *testing.T, resp map[string]any) string {
	t.Helper()
	if e, ok := resp["error"]; ok {
		t.Fatalf("tool error: %v", e)
	}
	res := resp["result"].(map[string]any)
	content := res["content"].([]any)[0].(map[string]any)
	return content["text"].(string)
}

// repoGoldenAbs returns the absolute path to a committed golden-set file, so the
// MCP eval test is independent of `go test`'s cwd.
func repoGoldenAbs(t *testing.T, rel string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden", rel))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestMCP_Eval(t *testing.T) {
	// go_rag_eval self-provisions from the committed corpus with the deterministic
	// embedder and returns the same numbers as the CLI (Principle V parity).
	args := map[string]any{
		"golden": repoGoldenAbs(t, "v1.jsonl"),
		"corpus": repoGoldenAbs(t, "corpus"),
	}
	text := resultText(t, mcpCall(t, t.TempDir(), "go_rag_eval", args))
	if !strings.Contains(text, "recall@10") || !strings.Contains(text, "queries: scored=") {
		t.Fatalf("unexpected eval output:\n%s", text)
	}
	if !strings.Contains(text, "scored=12") {
		t.Fatalf("expected 12 scored queries, got:\n%s", text)
	}
}

func TestMCP_ToolsListHas18(t *testing.T) {
	in := strings.NewReader(jsonLine(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}))
	out := new(bytes.Buffer)
	if err := New(t.TempDir()).Serve(in, out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp)
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 18 {
		t.Fatalf("expected 18 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tc := range tools {
		names[tc.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"go_rag_query", "go_rag_status", "go_rag_add", "go_rag_init", "go_rag_scan", "go_rag_config", "go_rag_files", "go_rag_dirs", "go_rag_reprocess", "go_rag_migrate", "go_rag_vault_list", "go_rag_guide", "go_rag_eval"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestMCP_Init(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), ".go-rag")
	resp := mcpCall(t, dbPath, "go_rag_init", map[string]any{"model": "m", "ollama_url": "http://x:11434"})
	if resp["error"] != nil {
		t.Fatalf("go_rag_init error: %v", resp["error"])
	}
	if _, err := os.Stat(filepath.Join(dbPath, "config.json")); err != nil {
		t.Fatalf("init must create config.json: %v", err)
	}
}

func TestMCP_ConfigSetGet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), ".go-rag")
	mcpCall(t, dbPath, "go_rag_init", map[string]any{"model": "m"})

	setOut := resultText(t, mcpCall(t, dbPath, "go_rag_config", map[string]any{"action": "set", "key": "chunk_size", "value": "256"}))
	if !strings.Contains(setOut, "saved") {
		t.Errorf("set response should confirm save: %q", setOut)
	}
	getOut := resultText(t, mcpCall(t, dbPath, "go_rag_config", map[string]any{"action": "get", "key": "chunk_size"}))
	if getOut != "chunk_size=256" {
		t.Errorf("get after set should return persisted value: %q", getOut)
	}
}
