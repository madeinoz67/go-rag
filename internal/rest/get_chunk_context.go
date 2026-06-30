package rest

import (
	"net/http"
	"strconv"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// get_chunk_context.go is the REST projection of engine.GetChunkContext (spec
// 037 / BL-002): GET /v1/chunks/{id}/context?window=N → {chunks, target_index,
// document}. The chunk / document DTOs are reused verbatim from GetChunk
// (toChunkDTO / toDocumentMetaDTO) so the window is byte-identical to N GET
// /v1/chunks/{id} calls (cross-transport parity, Constitution V).
//
// `window` query param: absent → default 2; present → integer in [0,10] (0 means
// exactly the target, ≡ GetChunk); out-of-range or non-integer → 400.

// getContextResponse is the REST envelope for GetChunkContext: the ordered
// window [predecessors][target][successors], the requested chunk's index, and the
// parent document (nil when the chunk is an orphan).
type getContextResponse struct {
	Chunks      []chunkDTO       `json:"chunks"`
	TargetIndex int              `json:"target_index"`
	Document    *documentMetaDTO `json:"document,omitempty"`
}

// handleGetChunkContext is the REST projection of engine.GetChunkContext.
// Errors: 404 for a missing/cross-vault id (ErrNotFound via writeEngineErr); 400
// for an empty/whitespace id (ErrInvalid) or an out-of-range / non-integer window.
func (s *Server) handleGetChunkContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	window := engine.DefaultChunkContextWindow()
	if q := r.URL.Query(); q.Has("window") {
		n, err := strconv.Atoi(q.Get("window"))
		if err != nil || n < 0 || n > engine.MaxChunkContextWindow() {
			writeError(w, http.StatusBadRequest, "window must be an integer 0.."+strconv.Itoa(engine.MaxChunkContextWindow()))
			return
		}
		window = n
	}
	res, err := s.eng.GetChunkContext(id, window)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	out := getContextResponse{Chunks: make([]chunkDTO, len(res.Chunks)), TargetIndex: res.TargetIndex}
	for i, c := range res.Chunks {
		out.Chunks[i] = toChunkDTO(c)
	}
	if res.Document.ID != "" { // orphan chunk → document omitted (nil)
		out.Document = toDocumentMetaDTO(res.Document, res.Source)
	}
	writeJSON(w, http.StatusOK, out)
}
