package rest

import (
	"encoding/json"
	"net/http"
)

// batch_get_chunks.go is the REST projection of engine.BatchGetChunks (spec 038 /
// BL-003): POST /v1/chunks/batch → { results: [...] }. The chunk/document DTOs are
// reused verbatim from GetChunk (toChunkDTO / toDocumentMetaDTO) so each result is
// byte-identical to a GET /v1/chunks/{id} for that id (cross-transport parity).
//
// Per-id error model: missing ids are in-band `error` fields on a 200 — the call
// NEVER returns 404 for a missing id (contrast with GET /v1/chunks/{id}). 400 is
// reserved for structural invalid-argument (empty list / >100 / empty element).

// batchGetChunksRequest is the REST request body for POST /v1/chunks/batch.
type batchGetChunksRequest struct {
	ChunkIDs []string `json:"chunk_ids"`
}

// batchResultDTO is one positional REST result entry — the requested chunk_id, the
// resolved chunk + document (omitted when not found), and a non-empty error.
type batchResultDTO struct {
	ChunkID  string           `json:"chunk_id"`
	Chunk    *chunkDTO        `json:"chunk,omitempty"`    // nil when not found
	Error    string           `json:"error,omitempty"`    // "not found" when missing
	Document *documentMetaDTO `json:"document,omitempty"` // nil when orphan/not-found
}

type batchGetChunksResponse struct {
	Results []batchResultDTO `json:"results"`
}

// handleBatchGetChunks is the REST projection of engine.BatchGetChunks. 200 with
// per-id errors in-band; 400 for structural invalid-argument; NEVER 404.
func (s *Server) handleBatchGetChunks(w http.ResponseWriter, r *http.Request) {
	var req batchGetChunksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	res, err := s.eng.BatchGetChunks(req.ChunkIDs)
	if err != nil {
		writeEngineErr(w, err) // ErrInvalid → 400 (no 404 path for batch)
		return
	}
	out := batchGetChunksResponse{Results: make([]batchResultDTO, len(res.Results))}
	for i, it := range res.Results {
		d := batchResultDTO{ChunkID: it.ChunkID, Error: it.Err}
		if it.Err == "" { // found → project chunk + document
			cd := toChunkDTO(it.Chunk)
			d.Chunk = &cd
			if it.Document.ID != "" {
				d.Document = toDocumentMetaDTO(it.Document, it.Source)
			}
		}
		out.Results[i] = d
	}
	writeJSON(w, http.StatusOK, out)
}
