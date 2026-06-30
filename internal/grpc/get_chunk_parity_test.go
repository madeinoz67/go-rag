package grpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madeinoz67/go-rag/internal/rest"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// get_chunk_parity_test.go (package grpc) proves cross-transport parity for
// GetChunk (spec 035 US3, Constitution V, FR-006): the same chunk_id resolves to
// the same chunk and the same parent document over gRPC and REST. MCP (text) and
// CLI (the same DTO shape as REST) project the identical engine.GetChunk method,
// so the engine test (get_chunk_test.go) + each transport's own test cover them;
// this test guards the two structured-JSON daemon transports against drift.

func TestGRPC_GetChunk_ParityWithREST(t *testing.T) {
	eng := newEngineWithCorpus(t, "the get-chunk rpc and the rest endpoint must agree on the resolved chunk and its document")
	gclient, gcleanup := dialBuf(t, NewServer(eng, ""))
	defer gcleanup()
	rsrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer rsrv.Close()
	ctx := context.Background()

	q, err := gclient.Query(ctx, &goragpb.QueryRequest{Query: "agree", Mode: "keyword", K: 5})
	if err != nil || len(q.GetHits()) == 0 {
		t.Fatalf("setup query: err=%v hits=%d", err, len(q.GetHits()))
	}
	id := q.GetHits()[0].GetChunkId()

	g, err := gclient.GetChunk(ctx, &goragpb.GetChunkRequest{ChunkId: id})
	if err != nil {
		t.Fatalf("gRPC GetChunk: %v", err)
	}
	if g.GetDocument() == nil {
		t.Fatal("gRPC: document must be projected (US2)")
	}

	resp, err := http.Get(rsrv.URL + "/v1/chunks/" + id)
	if err != nil {
		t.Fatalf("REST GetChunk: %v", err)
	}
	defer resp.Body.Close()
	var r struct {
		Chunk struct {
			ChunkID    string `json:"chunk_id"`
			DocumentID string `json:"document_id"`
			Content    string `json:"content"`
		} `json:"chunk"`
		Document struct {
			ID       string `json:"id"`
			FilePath string `json:"file_path"`
			FileType string `json:"file_type"`
		} `json:"document"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode REST response: %v", err)
	}

	if r.Chunk.ChunkID != g.GetChunk().GetChunkId() {
		t.Errorf("chunk_id: gRPC=%q REST=%q", g.GetChunk().GetChunkId(), r.Chunk.ChunkID)
	}
	if r.Chunk.DocumentID != g.GetChunk().GetDocumentId() {
		t.Errorf("document_id: gRPC=%q REST=%q", g.GetChunk().GetDocumentId(), r.Chunk.DocumentID)
	}
	if r.Chunk.Content != g.GetChunk().GetContent() {
		t.Error("chunk content differs between gRPC and REST")
	}
	if r.Document.ID != g.GetDocument().GetId() {
		t.Errorf("document.id: gRPC=%q REST=%q", g.GetDocument().GetId(), r.Document.ID)
	}
	if r.Document.FilePath != g.GetDocument().GetFilePath() {
		t.Errorf("document.file_path: gRPC=%q REST=%q", g.GetDocument().GetFilePath(), r.Document.FilePath)
	}
	if r.Document.FileType != g.GetDocument().GetFileType() {
		t.Errorf("document.file_type: gRPC=%q REST=%q", g.GetDocument().GetFileType(), r.Document.FileType)
	}
}
