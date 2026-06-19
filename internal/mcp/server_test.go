package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// fakeOllama backs the MCP server's embedder during the query tool test.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readBody(r)
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
}

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

// populateDB creates a database + config and ingests one document via the pipeline.
func populateDB(t *testing.T, dbPath, ollamaURL, doc string) {
	t.Helper()
	cfg := config.Default()
	cfg.DBPath = dbPath
	cfg.OllamaURL = ollamaURL
	cfg.OllamaModel = "fake"
	_ = os.MkdirAll(filepath.Join(dbPath, "data"), 0o755)
	if err := config.Save(filepath.Join(dbPath, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(dbPath, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(), index.NewVector())
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
