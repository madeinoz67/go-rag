package rest

import (
	"net/http"
)

// delete_document.go is the REST projection of engine.DeleteDoc (spec 050 /
// T006): DELETE /v1/documents/{id} → 204 No Content. Index-only — the source
// file on disk is untouched (FR-011). Mirrors handleAdd/handleReprocess's error
// surface (writeEngineErr: ErrInvalid → 400, ErrNotFound → 404) but returns 204
// (synchronous delete) rather than an IngestSummary (R8).

// handleDeleteDocument is the REST projection of engine.DeleteDoc. 204 on
// success (the doc + its chunks are gone); 400 on an empty id; 404 on an unknown
// id; 401 via the guard.
func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	if err := s.eng.DeleteDoc(r.Context(), "default", r.PathValue("id")); err != nil {
		writeEngineErr(w, err) // ErrInvalid (empty id) → 400; ErrNotFound → 404
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
