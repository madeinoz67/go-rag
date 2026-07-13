package grpc

import (
	"context"

	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// delete_document.go is the gRPC projection of engine.DeleteDoc (spec 050 /
// T007): DeleteDocument(DeleteDocumentRequest{doc_id}) → DeleteDocumentResponse.
// Index-only — the source file on disk is untouched (FR-011). Mirrors
// Adapter.Reprocess's error surface (toStatusErr: ErrInvalid → InvalidArgument,
// ErrNotFound → NotFound via the default Internal path) but returns an empty
// response (synchronous delete, R8) rather than an IngestSummary.

// DeleteDocument is the gRPC projection of engine.DeleteDoc. Empty response on
// success (the doc + its chunks are gone); InvalidArgument on an empty id;
// the engine's ErrNotFound maps through toStatusErr.
func (a *Adapter) DeleteDocument(ctx context.Context, req *goragpb.DeleteDocumentRequest) (*goragpb.DeleteDocumentResponse, error) {
	if err := a.eng.DeleteDoc(ctx, "default", req.GetDocId()); err != nil {
		return nil, toStatusErr(err)
	}
	return &goragpb.DeleteDocumentResponse{}, nil
}
