package grpc

import (
	"context"
	"testing"

	goragpb "github.com/madeinoz67/go-rag/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// get_chunk_test.go (package grpc) proves spec 035 US1 over the wire: GetChunk
// returns the chunk for a valid id, codes.NotFound for a missing id, and
// codes.InvalidArgument for an empty id. The parent DocumentMeta is projected
// onto the response in US2 (T010).

func TestGRPC_GetChunk_HappyPath(t *testing.T) {
	eng := newEngineWithCorpus(t, "the get-chunk rpc resolves a content-addressed identifier over gRPC")
	client, cleanup := dialBuf(t, NewServer(eng, ""))
	defer cleanup()
	ctx := context.Background()

	q, err := client.Query(ctx, &goragpb.QueryRequest{Query: "resolves", Mode: "keyword", K: 5})
	if err != nil || len(q.GetHits()) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.GetHits()))
	}
	id := q.GetHits()[0].GetChunkId()

	got, err := client.GetChunk(ctx, &goragpb.GetChunkRequest{ChunkId: id})
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if got.GetChunk().GetChunkId() != id {
		t.Errorf("chunk_id = %q, want %q", got.GetChunk().GetChunkId(), id)
	}
	if got.GetChunk().GetContent() == "" {
		t.Error("chunk content is empty")
	}
	if got.GetChunk().GetDocumentId() == "" {
		t.Error("chunk document_id is empty")
	}
}

func TestGRPC_GetChunk_NotFound(t *testing.T) {
	eng := newEngineWithCorpus(t, "hello world")
	client, cleanup := dialBuf(t, NewServer(eng, ""))
	defer cleanup()

	_, err := client.GetChunk(context.Background(), &goragpb.GetChunkRequest{
		ChunkId: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00",
	})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("expected NotFound for a missing id, got %v (%v)", got, err)
	}
}

func TestGRPC_GetChunk_InvalidArgument(t *testing.T) {
	eng := newEngineWithCorpus(t, "hello world")
	client, cleanup := dialBuf(t, NewServer(eng, ""))
	defer cleanup()

	_, err := client.GetChunk(context.Background(), &goragpb.GetChunkRequest{ChunkId: ""})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for an empty id, got %v (%v)", got, err)
	}
}

// TestGRPC_GetChunk_DocumentMeta_Projected (spec 035 US2, FR-005) proves the
// response carries the parent document's metadata in the same call, with the
// identity hash (id) and change-detection hash (content_hash) both present and
// distinct (PRD §7.2).
func TestGRPC_GetChunk_DocumentMeta_Projected(t *testing.T) {
	eng := newEngineWithCorpus(t, "the get-chunk rpc carries parent document metadata in the same call")
	client, cleanup := dialBuf(t, NewServer(eng, ""))
	defer cleanup()
	ctx := context.Background()

	q, err := client.Query(ctx, &goragpb.QueryRequest{Query: "parent", Mode: "keyword", K: 5})
	if err != nil || len(q.GetHits()) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.GetHits()))
	}
	id := q.GetHits()[0].GetChunkId()

	got, err := client.GetChunk(ctx, &goragpb.GetChunkRequest{ChunkId: id})
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	doc := got.GetDocument()
	if doc == nil {
		t.Fatal("US2: document metadata must be projected onto the response (FR-005)")
	}
	if doc.GetId() == "" {
		t.Error("document.id is empty")
	}
	if doc.GetContentHash() == "" {
		t.Error("document.content_hash is empty (change-detection hash, distinct from id)")
	}
	if doc.GetId() == doc.GetContentHash() {
		t.Error("id and content_hash collapsed — they must be distinct (PRD §7.2)")
	}
	if doc.GetFilePath() == "" || doc.GetFileType() == "" || doc.GetStatus() == "" {
		t.Errorf("document core fields incomplete: file_path=%q file_type=%q status=%q",
			doc.GetFilePath(), doc.GetFileType(), doc.GetStatus())
	}
}
