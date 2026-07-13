package grpc

import (
	"context"

	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// batch_get_chunks.go is the gRPC projection of engine.BatchGetChunks (spec 038 /
// BL-003): resolve up to 100 chunks by id in one call, one result per requested id
// in request order. Mirrors GetChunk (spec 035) / GetChunkContext (spec 037) and
// reuses toChunkPB + toDocumentMetaPB, so each result is byte-identical to a
// GetChunk for that id (cross-transport parity).
//
// Per-id error model: a missing id yields a result with a zero-value chunk +
// error="not found"; the call returns NO top-level status for missing ids (only
// INVALID_ARGUMENT for structural input errors via toStatusErr). This is the key
// delta from GetChunk/GetChunkContext, which return status.NotFound for a miss.
func (a *Adapter) BatchGetChunks(_ context.Context, req *goragpb.BatchGetChunksRequest) (*goragpb.BatchGetChunksResponse, error) {
	res, err := a.eng.BatchGetChunks("default", req.GetChunkIds())
	if err != nil {
		return nil, toStatusErr(err) // ErrInvalid → InvalidArgument
	}
	out := &goragpb.BatchGetChunksResponse{Results: make([]*goragpb.BatchGetChunksResult, len(res.Results))}
	for i, it := range res.Results {
		r := &goragpb.BatchGetChunksResult{ChunkId: it.ChunkID, Error: it.Err}
		if it.Err == "" { // found → project chunk + document
			r.Chunk = toChunkPB(it.Chunk)
			if it.Document.ID != "" {
				r.Document = toDocumentMetaPB(it.Document, it.Source)
			}
		}
		out.Results[i] = r
	}
	return out, nil
}
