package ui

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
)

// documents.go is the spec 047 (Slice 1) Documents view: a read-only browse →
// inspect → find surface over the document corpus. It is a presentation adapter
// over internal/engine — it adds no business logic, calls the engine in-process
// (like the Dashboard calls engine.Status), and is gated by the spec 045/046
// Bearer guard. Research: research.md R1–R8; contract: contracts/ui-documents.md.
//
// The DTOs mirror internal/rest's documentMetaDTO / chunkDTO (and the CLI
// projections) field-for-field so a document/chunk served by the UI is
// byte-identical to one served by REST/MCP/CLI (cross-transport parity,
// Constitution V; pinned by a UI parity test, R8).

// documentDTO is the UI projection of model.Document (+ spec-029 EnrichInfo
// flattened). Byte-identical to rest.documentMetaDTO. id is the identity hash;
// content_hash is the distinct change-detection hash (PRD §7.2).
type documentDTO struct {
	ID               string   `json:"id"`
	ContentHash      string   `json:"content_hash"`
	SourceID         string   `json:"source_id"`
	SourcePath       string   `json:"source_path,omitempty"` // empty in list rows (listing skips source resolution)
	FilePath         string   `json:"file_path"`
	FileName         string   `json:"file_name,omitempty"`
	FileType         string   `json:"file_type"`
	MimeType         string   `json:"mime_type,omitempty"`
	ChunkCount       int      `json:"chunk_count"`
	FileSize         int64    `json:"file_size"`
	Status           string   `json:"status"`
	IngestedAt       string   `json:"ingested_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	EnrichmentStatus string   `json:"enrichment_status,omitempty"`
	EnrichmentModel  string   `json:"enrichment_model,omitempty"`
	EnrichmentAt     string   `json:"enrichment_at,omitempty"`
}

// chunkDTO + toChunkDTO are deferred to the US2 detail view (spec 047 Phase 4):
// they project model.Chunk for /api/documents/{id}/chunks. Defined there so the
// byte-parity chunk shape lands with its first consumer (and stays lint-clean).

// documentsListResponse is the UI envelope for the document list (US1).
type documentsListResponse struct {
	Documents     []documentDTO `json:"documents"`
	NextPageToken string        `json:"next_page_token,omitempty"`
}

// rfc3339 renders a time as UTC RFC3339, "" for the zero value (so unset
// timestamps serialize as absent rather than "0001-01-01T00:00:00Z"). Mirrors
// rest.rfc3339 / cli.fmtRFC3339.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// toDocumentDTO projects a model.Document (+ its Source for source_path, + a
// flattened spec-029 EnrichInfo) to the wire DTO. Pass a zero model.Source for
// list rows (the listing skips per-document source resolution for performance).
func toDocumentDTO(d model.Document, src model.Source) documentDTO {
	out := documentDTO{
		ID:          d.ID,
		ContentHash: d.ContentHash,
		SourceID:    d.SourceID,
		SourcePath:  src.Path,
		FilePath:    d.FilePath,
		FileName:    d.FileName,
		FileType:    d.FileType,
		MimeType:    d.MimeType,
		ChunkCount:  d.ChunkCount,
		FileSize:    d.FileSize,
		Status:      d.Status,
		IngestedAt:  rfc3339(d.IngestedAt),
		UpdatedAt:   rfc3339(d.UpdatedAt),
	}
	if d.Enrichment != nil {
		out.Tags = d.Enrichment.Tags
		out.Summary = d.Enrichment.Summary
		out.EnrichmentStatus = d.Enrichment.Status
		out.EnrichmentModel = d.Enrichment.Model
		out.EnrichmentAt = rfc3339(d.Enrichment.GeneratedAt)
	}
	return out
}

// writeEngineErr maps an engine error to an HTTP response: ErrInvalid → 400
// "invalid"; anything else → 500 "internal" (no detail leakage). Mirrors
// rest.writeEngineErr.
func writeEngineErr(w http.ResponseWriter, err error) {
	if errors.Is(err, engine.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal")
}

// handleDocumentsList is the UI projection of engine.ListDocuments (spec 047
// US1): GET /api/documents?page_size=&page_token=&after=&status=&tag= →
// {documents, next_page_token}. `tag` is repeatable (match-any). 200 always
// (empty result = empty array); 400 for invalid argument; never 404.
func (s *Server) handleDocumentsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := engine.ListDocumentsRequest{
		PageToken: q.Get("page_token"),
		After:     q.Get("after"),
		Status:    q.Get("status"),
		Tags:      q["tag"], // repeatable ?tag=a&tag=b → match-any (R3)
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
	out := documentsListResponse{
		Documents:     make([]documentDTO, len(res.Documents)),
		NextPageToken: res.NextPageToken,
	}
	for i, d := range res.Documents {
		out.Documents[i] = toDocumentDTO(d, model.Source{}) // listing has no per-doc source context
	}
	writeJSON(w, http.StatusOK, out)
}
