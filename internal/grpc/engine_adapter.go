package grpc

import (
	"context"
	"errors"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
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
		Query:              req.GetQuery(),
		K:                  int(req.GetK()),
		Mode:               req.GetMode(),
		NoRerank:           req.GetNoRerank(),
		Threshold:          req.GetThreshold(),
		RRFK:               int(req.GetRrfK()),
		PoolSize:           int(req.GetPoolSize()), // H22/spec 024
		Filter:             engine.NewFilter(req.GetSource(), req.GetType(), req.GetTags()),
		ContextWindow:      int(req.GetContextWindow()),
		NoCache:            req.GetNoCache(),
		IncludeQuarantined: req.GetIncludeQuarantined(),
		Dedup:              req.GetDedup(),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}
	hits := make([]*goragpb.QueryHit, len(res.Hits))
	for i, h := range res.Hits {
		hits[i] = &goragpb.QueryHit{
			ChunkId:          h.ChunkID,
			DocumentId:       h.DocumentID,
			Score:            h.Score,
			Content:          h.Content,
			FilePath:         h.FilePath,
			Page:             int32(h.Page),
			Poisoning:        toPoisoningPB(h.Poisoning), // H04/spec 019
			ChunkIndex:       int32(h.ChunkIndex),        // H21/spec 023
			SectionContext:   h.SectionContext,           // H23/spec 025 (FR-004)
			NearDup:          toNearDupPB(h.NearDup),     // H20/spec 026 (FR-004)
			Summary:          h.Summary,                  // spec 029 (FR-010)
			EnrichmentStatus: h.EnrichmentStatus,         // spec 029 (FR-010)
		}
	}
	return &goragpb.QueryResponse{Hits: hits, RerankFailed: res.RerankFailed,
		EffectiveK: int32(res.EffectiveK), EffectivePool: int32(res.EffectivePool), EffectiveMode: res.EffectiveMode}, nil // H22/spec 024
}

// toPoisoningPB maps the engine verdict to the proto projection (H04/spec 019).
// nil verdict → nil (clean/unscored), so clean corpora serialize identically to pre-019.
func toNearDupPB(nd *model.NearDupInfo) *goragpb.NearDup {
	if nd == nil {
		return nil
	}
	return &goragpb.NearDup{
		Siblings:   nd.Siblings,
		Similarity: nd.Similarity,
	}
}

func toPoisoningPB(v *model.PoisonVerdict) *goragpb.Poisoning {
	if v == nil {
		return nil
	}
	return &goragpb.Poisoning{
		Level:          string(v.Level),
		Score:          v.Score,
		MatchedPhrases: v.MatchedPhrases,
		Signals: &goragpb.PoisoningSignals{
			Repetition:  v.Signals.Repetition,
			Stuffing:    v.Signals.Stuffing,
			Instruction: v.Signals.Instruction,
		},
	}
}

// Status is the gRPC projection of engine.Status.
func (a *Adapter) Status(_ context.Context, _ *goragpb.StatusRequest) (*goragpb.StatusResponse, error) {
	st, err := a.eng.Status()
	if err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.StatusResponse{
		Documents:            int32(st.Documents),
		Chunks:               int32(st.Chunks),
		Embeddings:           int32(st.Embeddings),
		Dimensions:           int32(st.Dimensions),
		EmbeddingModel:       st.EmbeddingModel,
		Reranker:             st.Reranker,
		OllamaUrl:            st.OllamaURL,
		EmbeddingsComplete:   st.EmbeddingsComplete,
		PoolSize:             int32(st.PoolSize),                      // H22/spec 024
		AdaptiveDepthEnabled: st.AdaptiveDepthEnabled,                 // H22/spec 024
		PoolUtilization:      toPoolUtilizationPB(st.PoolUtilization), // H22/spec 024
		EnrichmentEnabled:    st.EnrichmentEnabled,                    // spec 029
		EnrichedDocs:         int32(st.EnrichedDocs),                  // spec 029
	}, nil
}

// toPoolUtilizationPB maps the engine aggregate to the proto projection (H22/spec 024).
func toPoolUtilizationPB(u engine.PoolUtilization) *goragpb.PoolUtilization {
	return &goragpb.PoolUtilization{
		Queries:    u.Queries,
		AvgFetched: u.AvgFetched,
		AvgKept:    u.AvgKept,
		Saturated:  u.Saturated,
	}
}

// Add is the gRPC projection of engine.Add. It ACKs fast (async-after-ACK);
// embeddings finish on the engine's background workers after the response.
func (a *Adapter) Add(ctx context.Context, req *goragpb.AddRequest) (*goragpb.IngestSummary, error) {
	res, err := a.eng.Add(ctx, req.GetPath(), req.GetGlob())
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

// MigratePlan is the gRPC projection of engine.MigratePlan (H24/spec 028) — the
// read-only migration preview. It never re-embeds and needs no backend.
func (a *Adapter) MigratePlan(_ context.Context, _ *goragpb.MigratePlanRequest) (*goragpb.MigrationPlan, error) {
	plan, err := a.eng.MigratePlan()
	if err != nil {
		return nil, toStatusErr(err)
	}
	return toMigrationPlanPB(plan), nil
}

// toMigrationPlanPB maps the engine plan to its proto projection (H24/spec 028).
func toMigrationPlanPB(p *engine.MigrationPlan) *goragpb.MigrationPlan {
	if p == nil {
		return &goragpb.MigrationPlan{}
	}
	srcs := make([]*goragpb.ModelCount, len(p.Sources))
	for i, s := range p.Sources {
		srcs[i] = &goragpb.ModelCount{Model: s.Model, Count: int32(s.Count), Stale: s.Stale}
	}
	dims := make([]*goragpb.DimCount, len(p.Dimensions))
	for i, d := range p.Dimensions {
		dims[i] = &goragpb.DimCount{Dim: int32(d.Dim), Count: int32(d.Count)}
	}
	return &goragpb.MigrationPlan{
		TargetModel: p.TargetModel,
		Total:       int32(p.Total),
		StaleTotal:  int32(p.StaleTotal),
		Sources:     srcs,
		Dimensions:  dims,
		Consistent:  p.Consistent,
		Estimate: &goragpb.Estimate{
			StaleEmbeddings: int32(p.Estimate.StaleEmbeddings),
			ModelChange:     p.Estimate.ModelChange,
			MixedCorpus:     p.Estimate.MixedCorpus,
			Note:            p.Estimate.Note,
		},
	}
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
		Ready:             h.Ready,        // H11/spec 017: readiness (false on hard drift)
		DriftVerdict:      h.DriftVerdict, // H11/spec 017
	}, nil
}

// ListPoisoned is the gRPC projection of engine.ListPoisoned (H04/spec 019).
func (a *Adapter) ListPoisoned(_ context.Context, _ *goragpb.ListPoisonedRequest) (*goragpb.ListPoisonedResponse, error) {
	flagged, err := a.eng.ListPoisoned()
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := make([]*goragpb.PoisonedChunk, len(flagged))
	for i, f := range flagged {
		out[i] = &goragpb.PoisonedChunk{
			ChunkId:    f.ChunkID,
			DocumentId: f.DocumentID,
			Preview:    f.Preview,
			Verdict:    toPoisoningPB(&f.Verdict),
		}
	}
	return &goragpb.ListPoisonedResponse{Flagged: out}, nil
}

// ReleaseChunk is the gRPC projection of engine.ReleaseChunk (H04/spec 019).
func (a *Adapter) ReleaseChunk(_ context.Context, req *goragpb.ReleaseChunkRequest) (*goragpb.PoisonActionResponse, error) {
	if err := a.eng.ReleaseChunk(req.GetChunkId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.PoisonActionResponse{ChunkId: req.GetChunkId(), Status: "released"}, nil
}

// ResetChunk is the gRPC projection of engine.ResetChunk (H04/spec 019).
func (a *Adapter) ResetChunk(_ context.Context, req *goragpb.ResetChunkRequest) (*goragpb.PoisonActionResponse, error) {
	if err := a.eng.ResetChunk(req.GetChunkId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.PoisonActionResponse{ChunkId: req.GetChunkId(), Status: "reset"}, nil
}

// RescanPoisoning is the gRPC projection of engine.RescanPoisoning (H04/spec 019).
func (a *Adapter) RescanPoisoning(_ context.Context, _ *goragpb.RescanPoisoningRequest) (*goragpb.RescanPoisoningResponse, error) {
	rescored, flagged, err := a.eng.RescanPoisoning()
	if err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.RescanPoisoningResponse{Rescored: int32(rescored), Flagged: int32(flagged)}, nil
}
