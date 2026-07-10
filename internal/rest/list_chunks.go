package rest

import (
	"net/http"
	"strconv"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// list_chunks.go is the REST projection of engine.ListChunks (spec 047 / T006):
// GET /v1/documents/{document_id}/chunks?page_size=&page_token= →
// { chunks, next_page_token }. The chunk DTO is reused verbatim from GetChunk
// (chunkDTO / toChunkDTO) so each entry is byte-identical across transports.
//
// 200 always (unknown document or empty page → empty array, not 404); 400 for
// invalid-argument (empty document_id, bad page_size / page_token). Mirrors
// handleListDocuments exactly — only the engine call and the per-item
// projection differ.

// listChunksResponse is the REST envelope for ListChunks.
type listChunksResponse struct {
	Chunks        []chunkDTO `json:"chunks"`
	NextPageToken string     `json:"next_page_token,omitempty"`
}

// handleListChunks is the REST projection of engine.ListChunks. 200 always
// (empty result = empty array); 400 for invalid-argument; never 404.
func (s *Server) handleListChunks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := engine.ListChunksRequest{
		PageToken: q.Get("page_token"),
	}
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "page_size must be an integer")
			return
		}
		req.PageSize = n
	}
	res, err := s.eng.ListChunks(r.PathValue("document_id"), req)
	if err != nil {
		writeEngineErr(w, err) // ErrInvalid → 400
		return
	}
	out := listChunksResponse{Chunks: make([]chunkDTO, len(res.Chunks)), NextPageToken: res.NextPageToken}
	for i, c := range res.Chunks {
		out.Chunks[i] = toChunkDTO(c)
	}
	writeJSON(w, http.StatusOK, out)
}
