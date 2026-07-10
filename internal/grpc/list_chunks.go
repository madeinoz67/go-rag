package grpc

import (
	"context"

	"github.com/madeinoz67/go-rag/internal/engine"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// list_chunks.go is the gRPC projection of engine.ListChunks (spec 047 T007):
// list a document's chunks in stable order (chunk_index ASC, chunk_id ASC) with
// page_token pagination. Mirrors Adapter.ListDocuments and reuses toChunkPB, so
// each entry is byte-identical to GetChunk/BatchGetChunks' chunk projection
// (cross-transport parity). Empty result is NOT an error (unknown document →
// empty chunks + empty next_page_token), matching engine.ListChunks semantics.
func (a *Adapter) ListChunks(_ context.Context, req *goragpb.ListChunksRequest) (*goragpb.ListChunksResponse, error) {
	res, err := a.eng.ListChunks(req.GetDocumentId(), engine.ListChunksRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, toStatusErr(err) // ErrInvalid → InvalidArgument
	}
	out := &goragpb.ListChunksResponse{
		Chunks:        make([]*goragpb.Chunk, len(res.Chunks)),
		NextPageToken: res.NextPageToken,
	}
	for i, c := range res.Chunks {
		out.Chunks[i] = toChunkPB(c)
	}
	return out, nil
}
