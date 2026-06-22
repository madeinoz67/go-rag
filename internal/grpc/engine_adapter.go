package grpc

import (
	"context"
	"errors"

	"github.com/madeinoz67/go-rag/internal/engine"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// toStatusErr maps an engine error to a gRPC status: client-input errors
// (ErrInvalid) → InvalidArgument; everything else (storage/index/embedder
// faults) → Internal. Mirrors REST's writeEngineErr.
func toStatusErr(err error) error {
	if errors.Is(err, engine.ErrInvalid) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// toIngestSummary maps an engine.IngestSummary to its proto projection.
func toIngestSummary(s *engine.IngestSummary) *goragpb.IngestSummary {
	if s == nil {
		return &goragpb.IngestSummary{}
	}
	return &goragpb.IngestSummary{
		New:      int32(s.New),
		Skipped:  int32(s.Skipped),
		Modified: int32(s.Modified),
		Deleted:  int32(s.Deleted),
		Errors:   int32(s.Errors),
	}
}

// Query is the gRPC projection of engine.Query.
func (a *Adapter) Query(ctx context.Context, req *goragpb.QueryRequest) (*goragpb.QueryResponse, error) {
	res, err := a.eng.Query(ctx, engine.QueryRequest{
		Query:     req.GetQuery(),
		K:         int(req.GetK()),
		Mode:      req.GetMode(),
		NoRerank:  req.GetNoRerank(),
		Threshold: req.GetThreshold(),
		RRFK:      int(req.GetRrfK()),
		Filter:    engine.NewFilter(req.GetSource(), req.GetType(), req.GetTags()),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}
	hits := make([]*goragpb.QueryHit, len(res.Hits))
	for i, h := range res.Hits {
		hits[i] = &goragpb.QueryHit{
			ChunkId:    h.ChunkID,
			DocumentId: h.DocumentID,
			Score:      h.Score,
			Content:    h.Content,
			FilePath:   h.FilePath,
			Page:       int32(h.Page),
		}
	}
	return &goragpb.QueryResponse{Hits: hits, RerankFailed: res.RerankFailed}, nil
}

// Status is the gRPC projection of engine.Status.
func (a *Adapter) Status(_ context.Context, _ *goragpb.StatusRequest) (*goragpb.StatusResponse, error) {
	st, err := a.eng.Status()
	if err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.StatusResponse{
		Documents:          int32(st.Documents),
		Chunks:             int32(st.Chunks),
		Embeddings:         int32(st.Embeddings),
		Dimensions:         int32(st.Dimensions),
		EmbeddingModel:     st.EmbeddingModel,
		Reranker:           st.Reranker,
		OllamaUrl:          st.OllamaURL,
		EmbeddingsComplete: st.EmbeddingsComplete,
	}, nil
}

// Add is the gRPC projection of engine.Add. It ACKs fast (async-after-ACK);
// embeddings finish on the engine's background workers after the response.
func (a *Adapter) Add(ctx context.Context, req *goragpb.AddRequest) (*goragpb.IngestSummary, error) {
	res, err := a.eng.Add(ctx, req.GetPath())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return toIngestSummary(res), nil
}

// Scan is the gRPC projection of engine.Scan.
func (a *Adapter) Scan(ctx context.Context, _ *goragpb.ScanRequest) (*goragpb.IngestSummary, error) {
	res, err := a.eng.Scan(ctx)
	if err != nil {
		return nil, toStatusErr(err)
	}
	return toIngestSummary(res), nil
}

// Reprocess is the gRPC projection of engine.Reprocess.
func (a *Adapter) Reprocess(ctx context.Context, req *goragpb.ReprocessRequest) (*goragpb.IngestSummary, error) {
	res, err := a.eng.Reprocess(ctx, req.GetPath())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return toIngestSummary(res), nil
}

// Migrate is the gRPC projection of engine.Migrate.
func (a *Adapter) Migrate(ctx context.Context, _ *goragpb.MigrateRequest) (*goragpb.IngestSummary, error) {
	res, err := a.eng.Migrate(ctx)
	if err != nil {
		return nil, toStatusErr(err)
	}
	return toIngestSummary(res), nil
}

// Files is the gRPC projection of engine.Files.
func (a *Adapter) Files(_ context.Context, _ *goragpb.FilesRequest) (*goragpb.FilesResponse, error) {
	files, err := a.eng.Files()
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := make([]*goragpb.FileEntry, len(files))
	for i, f := range files {
		out[i] = &goragpb.FileEntry{
			FilePath:   f.FilePath,
			FileType:   f.FileType,
			Status:     f.Status,
			ChunkCount: int32(f.ChunkCount),
		}
	}
	return &goragpb.FilesResponse{Files: out}, nil
}

// Dirs is the gRPC projection of engine.Dirs.
func (a *Adapter) Dirs(_ context.Context, _ *goragpb.DirsRequest) (*goragpb.DirsResponse, error) {
	dirs, err := a.eng.Dirs()
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := make([]*goragpb.DirEntry, len(dirs))
	for i, d := range dirs {
		out[i] = &goragpb.DirEntry{
			Dir:    d.Dir,
			Files:  int32(d.Files),
			Chunks: int32(d.Chunks),
		}
	}
	return &goragpb.DirsResponse{Dirs: out}, nil
}

// GetConfig is the gRPC projection of engine.GetConfig.
func (a *Adapter) GetConfig(_ context.Context, req *goragpb.GetConfigRequest) (*goragpb.GetConfigResponse, error) {
	vals, err := a.eng.GetConfig(req.GetKey())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.GetConfigResponse{Values: vals}, nil
}

// SetConfig is the gRPC projection of engine.SetConfig.
func (a *Adapter) SetConfig(_ context.Context, req *goragpb.SetConfigRequest) (*goragpb.SetConfigResponse, error) {
	if err := a.eng.SetConfig(req.GetKey(), req.GetValue()); err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.SetConfigResponse{Key: req.GetKey(), Value: req.GetValue()}, nil
}

// ListVaults is the gRPC projection of engine.ListVaults.
func (a *Adapter) ListVaults(_ context.Context, _ *goragpb.ListVaultsRequest) (*goragpb.ListVaultsResponse, error) {
	vaults, err := a.eng.ListVaults()
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := make([]*goragpb.VaultEntry, len(vaults))
	for i, v := range vaults {
		out[i] = &goragpb.VaultEntry{Name: v.Name, Documents: int32(v.Documents)}
	}
	return &goragpb.ListVaultsResponse{Vaults: out}, nil
}

// Health is the gRPC projection of engine.Health (liveness/readiness).
func (a *Adapter) Health(ctx context.Context, _ *goragpb.HealthRequest) (*goragpb.HealthResponse, error) {
	h := a.eng.Health(ctx)
	return &goragpb.HealthResponse{
		Ok:                h.OK,
		StorageOpen:       h.StorageOpen,
		EmbedderReachable: h.EmbedderReachable,
	}, nil
}
