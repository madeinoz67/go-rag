package grpc

import (
	"context"

	"github.com/madeinoz67/go-rag/internal/engine"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// get_chunk_context.go is the gRPC projection of engine.GetChunkContext (spec
// 037 / BL-002): resolve a content-addressed chunk_id to that chunk plus up to
// `window` neighbours on each side in one call. Mirrors GetChunk (spec 035) and
// reuses its toChunkPB / toDocumentMetaPB projections, so a context window is
// byte-identical to N GetChunk calls (cross-transport parity, Constitution V).
//
// proto3 int32 has no field presence: a zero `window` is treated as "unspecified"
// and replaced with the default (DefaultChunkContextWindow, 2). A caller that
// wants exactly the target chunk uses GetChunk. Explicit 1..10 passes through;
// >10 / <0 is rejected by the engine as InvalidArgument (FR-004).
func (a *Adapter) GetChunkContext(_ context.Context, req *goragpb.GetChunkContextRequest) (*goragpb.GetChunkContextResponse, error) {
	window := int(req.GetWindow())
	if window == 0 {
		window = engine.DefaultChunkContextWindow()
	}
	res, err := a.eng.GetChunkContext(vaultOrDefault(req.GetVault()), req.GetChunkId(), window)
	if err != nil {
		return nil, toStatusErr(err)
	}
	out := &goragpb.GetChunkContextResponse{
		Chunks:      make([]*goragpb.Chunk, len(res.Chunks)),
		TargetIndex: int32(res.TargetIndex),
	}
	for i, c := range res.Chunks {
		out.Chunks[i] = toChunkPB(c)
	}
	if res.Document.ID != "" { // orphan chunk → document nil (mirrors GetChunk)
		out.Document = toDocumentMetaPB(res.Document, res.Source)
	}
	return out, nil
}
