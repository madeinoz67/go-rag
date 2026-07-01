package rest

import (
	"net/http"
	"strconv"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
)

// list_documents.go is the REST projection of engine.ListDocuments (spec 039 /
// BL-007): GET /v1/documents?page_size=&page_token=&after=&status= →
// { documents, next_page_token }. The document DTO is reused verbatim from
// GetChunk (documentMetaDTO) so each entry is byte-identical across transports.
//
// Note: the listing does not resolve the per-document Source (no N source gets on
// a scan), so source_path is empty in the listing projection — file_path (on the
// Document) is the listing's identifier. Consistent across all four transports.

// listDocumentsResponse is the REST envelope for ListDocuments.
type listDocumentsResponse struct {
	Documents     []*documentMetaDTO `json:"documents"`
	NextPageToken string             `json:"next_page_token,omitempty"`
}

// handleListDocuments is the REST projection of engine.ListDocuments. 200 always
// (empty result = empty array); 400 for invalid-argument; never 404.
func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := engine.ListDocumentsRequest{
		PageToken: q.Get("page_token"),
		After:     q.Get("after"),
		Status:    q.Get("status"),
	}
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "page_size must be an integer")
			return
		}
		req.PageSize = n
	}
	res, err := s.eng.ListDocuments(req)
	if err != nil {
		writeEngineErr(w, err) // ErrInvalid → 400
		return
	}
	out := listDocumentsResponse{Documents: make([]*documentMetaDTO, len(res.Documents)), NextPageToken: res.NextPageToken}
	for i, d := range res.Documents {
		out.Documents[i] = toDocumentMetaDTO(d, model.Source{}) // listing has no per-doc source context
	}
	writeJSON(w, http.StatusOK, out)
}
