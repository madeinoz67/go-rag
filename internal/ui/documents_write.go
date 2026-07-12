package ui

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// documents_write.go is the spec 050 (Slice 4) Documents write surface: the
// console's FIRST write actions. Three guarded routes over the engine, all
// in-process (a 5th adapter over the same write path the CLI/REST/gRPC/MCP
// transports use — cross-transport parity holds by construction):
//
//   - POST   /api/documents               → Engine.Add        (add by server-side path)
//   - DELETE /api/documents/{id}          → Engine.DeleteDoc  (remove; index-only, FR-011)
//   - POST   /api/documents/{id}/reingest → resolve path → Engine.Reprocess (re-derive)
//
// Add + reingest reuse the already-cross-transport engine methods (R2); remove
// drives the new Engine.DeleteDoc wrapper (R3, shipped cross-transport by this
// slice). Confirmation for the two destructive actions (DELETE, reingest) is a
// client-side UX gate (R7) — the server executes the guarded mutation on receipt.
//
// DTOs mirror internal/rest's ingestSummary + the proto IngestSummary
// (snake_case) so an add/reingest ACK is byte-identical across UI/REST/gRPC/MCP.
// Helpers (writeJSON / writeError / writeEngineErr) are the package's existing
// ones (documents.go) — not redefined here.

// addRequestDTO is the body of POST /api/documents (R4). path is required (the
// server-side file or directory the daemon reads); glob is optional (default ""
// → Engine.Add uses "*"). No tags field — Engine.Add has no tags parameter
// (tags come from enrichment, spec 029).
type addRequestDTO struct {
	Path string `json:"path"`
	Glob string `json:"glob,omitempty"`
}

// ingestSummaryDTO is the ACK body for add + reingest (R8). It projects
// engine.IngestSummary field-parallel to the REST/proto IngestSummary so the
// payload is byte-identical across transports (parity). add/reingest ACK fast
// (async-after-ACK, Principle IV); embedding continues on background workers and
// surfaces in Operations (spec 049).
type ingestSummaryDTO struct {
	New      int    `json:"new"`
	Skipped  int    `json:"skipped"`
	Errors   int    `json:"errors"`
	Path     string `json:"path"`
	Modified int    `json:"modified,omitempty"` // scan only; 0 for add/reingest (kept for shape parity)
	Deleted  int    `json:"deleted,omitempty"`  // scan only; 0 for add/reingest (kept for shape parity)
}

// toIngestSummaryDTO projects an engine.IngestSummary (+ the path operated on)
// to the wire DTO. Mirrors rest.toIngestSummary / grpc.toIngestSummary.
func toIngestSummaryDTO(s *engine.IngestSummary, path string) ingestSummaryDTO {
	return ingestSummaryDTO{
		New:      s.New,
		Skipped:  s.Skipped,
		Errors:   s.Errors,
		Path:     path,
		Modified: s.Modified,
		Deleted:  s.Deleted,
	}
}

// handleDocumentAdd is the UI projection of Engine.Add (US1): POST /api/documents
// with {path, glob?} → ingestSummaryDTO 200. ACKs fast; embedding continues async
// and surfaces in Operations. Errors: 400 empty path / invalid body; engine errors
// (unreadable path, permission denied) via writeEngineErr (FR-009 — no silent
// failures, no partial ingest).
func (s *Server) handleDocumentAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	res, err := s.eng.Add(r.Context(), req.Path, req.Glob)
	if err != nil {
		writeEngineErr(w, err) // ErrInvalid → 400; else 500
		return
	}
	writeJSON(w, http.StatusOK, toIngestSummaryDTO(res, req.Path))
}

// handleDocumentRemove is the UI projection of Engine.DeleteDoc (US2):
// DELETE /api/documents/{id} → 204 No Content. Index-only — the source file on
// disk is untouched (FR-011). Synchronous (the doc is gone when the handler
// returns), so 204 rather than a summary (R8). Errors: 404 unknown id; 400 empty
// id. Confirmation is a client-side gate (R7); the server executes the guarded
// mutation on receipt.
func (s *Server) handleDocumentRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.eng.DeleteDoc(r.Context(), id); err != nil {
		writeEngineErr(w, err) // ErrInvalid (empty) → 400; ErrNotFound → 404
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDocumentReingest is the UI projection of Engine.Reprocess (US3):
// POST /api/documents/{id}/reingest → resolve the doc's source path from its ID,
// then Engine.Reprocess(sourcePath) → ingestSummaryDTO 200. The operator clicks
// "Reingest"; the handler derives the path (R5). Errors: 404 unknown id; 404
// "source not found" when the source file no longer exists (distinct from a
// successful empty reingest); engine errors via writeEngineErr.
//
// Path resolution: GetDocument returns the document with FilePath = the absolute
// source path as ingested (filepath.Walk joins the root the operator passed to
// Add). That absolute path is what Engine.Reprocess takes — parity with
// `go-rag reprocess <path>`. An orphan doc (synthetic / caption chunks, no
// resolvable source) reingests to a clear "source not found" rather than a 500.
func (s *Server) handleDocumentReingest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	res, err := s.eng.GetDocument(id)
	if err != nil {
		writeEngineErr(w, err) // ErrNotFound → 404 (unknown id)
		return
	}
	sourcePath := res.Document.FilePath
	if sourcePath == "" {
		// Orphan document (synthetic / caption chunks): no source to reingest.
		writeError(w, http.StatusNotFound, "source not found")
		return
	}
	if _, statErr := os.Stat(sourcePath); statErr != nil && os.IsNotExist(statErr) {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}
	sum, err := s.eng.Reprocess(r.Context(), sourcePath)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIngestSummaryDTO(sum, sourcePath))
}
