package grpc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
	grpcc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeEmbed satisfies embed.Embedder for hermetic ingestion — no Ollama server
// required. Keyword-mode queries never call it (they read the BM25 index), so the
// corpus becomes retrievable with zero external deps.
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

const bufSize = 1 << 20

// newEngineWithCorpus opens a fresh DB in a temp dir, ingests doc, and returns an
// Engine over it. Keyword-mode queries on the returned engine yield real BM25 hits.
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
	p := pipeline.New(db, chunk.NewSplitter(512, 50), &fakeEmbed{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
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

// dialBuf serves srv on an in-memory bufconn and returns a client plus a cleanup
// that closes the connection and stops the server. Lets tests exercise the full
// gRPC stack (interceptors included) without binding a real port.
func dialBuf(t *testing.T, srv *grpcc.Server) (goragpb.GoragClient, func()) {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpcc.NewClient("passthrough:///bufnet",
		grpcc.WithContextDialer(dialer),
		grpcc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufnet: %v", err)
	}
	cleanup := func() {
		_ = conn.Close()
		srv.GracefulStop()
		<-serveErr
	}
	return goragpb.NewGoragClient(conn), cleanup
}

func TestGRPC_Query_HappyPath(t *testing.T) {
	eng := newEngineWithCorpus(t, "the go-rag server performs keyword retrieval over local documents")
	client, cleanup := dialBuf(t, NewServer(eng, ""))
	defer cleanup()

	resp, err := client.Query(context.Background(), &goragpb.QueryRequest{
		Query: "retrieval", Mode: "keyword", K: 5,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetHits()) == 0 {
		t.Fatal("expected >=1 hit for 'retrieval', got 0")
	}
	h := resp.GetHits()[0]
	if h.GetContent() == "" {
		t.Error("hit content is empty")
	}
	if h.GetFilePath() == "" {
		t.Error("hit file_path is empty")
	}
}

func TestGRPC_Query_BearerMissing_Rejected(t *testing.T) {
	eng := newEngineWithCorpus(t, "hello world")
	client, cleanup := dialBuf(t, NewServer(eng, "secret"))
	defer cleanup()

	_, err := client.Query(context.Background(), &goragpb.QueryRequest{Query: "hello", Mode: "keyword"})
	if err == nil {
		t.Fatal("expected Unauthenticated for missing bearer, got nil")
	}
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v (%v)", got, err)
	}
}

func TestGRPC_Query_BearerWrong_Rejected(t *testing.T) {
	eng := newEngineWithCorpus(t, "hello world")
	client, cleanup := dialBuf(t, NewServer(eng, "secret"))
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer nope")
	_, err := client.Query(ctx, &goragpb.QueryRequest{Query: "hello", Mode: "keyword"})
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for wrong bearer, got %v (%v)", got, err)
	}
}

func TestGRPC_Query_BearerValid_Accepted(t *testing.T) {
	eng := newEngineWithCorpus(t, "the server performs keyword retrieval over documents")
	client, cleanup := dialBuf(t, NewServer(eng, "secret"))
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer secret")
	resp, err := client.Query(ctx, &goragpb.QueryRequest{Query: "retrieval", Mode: "keyword"})
	if err != nil {
		t.Fatalf("Query with valid bearer: %v", err)
	}
	if len(resp.GetHits()) == 0 {
		t.Fatal("expected hits with valid bearer, got 0")
	}
}

func TestGRPC_Query_EmptyQuery_InvalidArgument(t *testing.T) {
	eng := newEngineWithCorpus(t, "anything")
	client, cleanup := dialBuf(t, NewServer(eng, ""))
	defer cleanup()

	_, err := client.Query(context.Background(), &goragpb.QueryRequest{Query: "", Mode: "keyword"})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty query, got %v (%v)", got, err)
	}
}
