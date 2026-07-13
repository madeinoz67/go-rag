package grpc

import (
	"context"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// list_documents.go is the gRPC projection of engine.ListDocuments (spec 039 /
// BL-007): list documents — ingested_at cursor + status filter + page_token
// pagination. Mirrors the GetChunk family and reuses toDocumentMetaPB, so each
// entry is byte-identical to GetChunk's document projection (cross-transport
// parity). Empty result is NOT an error (empty documents + empty next_page_token).
func (a *Adapter) ListDocuments(_ context.Context, req *goragpb.ListDocumentsRequest) (*goragpb.ListDocumentsResponse, error) {
	res, err := a.eng.ListDocuments(vaultOrDefault(req.GetVault()), engine.ListDocumentsRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
		After:     req.GetAfter(),
		Status:    req.GetStatus(),
		Tags:      req.GetTags(), // spec 047 R3: match-any tag filter (parity with REST/MCP)
	})
	if err != nil {
		return nil, toStatusErr(err) // ErrInvalid → InvalidArgument
	}
	out := &goragpb.ListDocumentsResponse{
		Documents:     make([]*goragpb.DocumentMeta, len(res.Documents)),
		NextPageToken: res.NextPageToken,
	}
	for i, d := range res.Documents {
		out.Documents[i] = toDocumentMetaPB(d, model.Source{}) // listing has no per-doc source context
	}
	return out, nil
}
