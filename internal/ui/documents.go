package ui

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
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

// chunkDTO is the UI projection of model.Chunk. Byte-identical to rest.chunkDTO
// (sidecars via the model types directly, matching the CLI projection — their
// JSON tags are the cross-transport contract).
type chunkDTO struct {
	ChunkID           string               `json:"chunk_id"`
	DocumentID        string               `json:"document_id"`
	Content           string               `json:"content"`
	ChunkIndex        int                  `json:"chunk_index"`
	TotalChunks       int                  `json:"total_chunks"`
	PageNumber        int                  `json:"page_number"`
	StartChar         int                  `json:"start_char"`
	EndChar           int                  `json:"end_char"`
	TokenCount        int                  `json:"token_count"`
	PreviousChunkID   string               `json:"previous_chunk_id,omitempty"`
	NextChunkID       string               `json:"next_chunk_id,omitempty"`
	Poisoning         *model.PoisonVerdict `json:"poisoning,omitempty"`
	SectionContext    []string             `json:"section_context,omitempty"`
	SectionDepth      int                  `json:"section_depth,omitempty"`
	ExtractionQuality float64              `json:"extraction_quality,omitempty"`
	ExtractionMethod  string               `json:"extraction_method,omitempty"`
	Wikilinks         []string             `json:"wikilinks,omitempty"`
	NearDup           *model.NearDupInfo   `json:"near_dup,omitempty"`
	Kind              string               `json:"kind,omitempty"`
	CreatedAt         string               `json:"created_at,omitempty"`
}

// documentsListResponse is the UI envelope for the document list (US1).
type documentsListResponse struct {
	Documents     []documentDTO `json:"documents"`
	NextPageToken string        `json:"next_page_token,omitempty"`
}

// documentChunksResponse is the UI envelope for a document's chunk page (US2).
type documentChunksResponse struct {
	Chunks        []chunkDTO `json:"chunks"`
	NextPageToken string     `json:"next_page_token,omitempty"`
}

// chunkContextResponse is the UI envelope for a chunk's neighbour window (US2).
type chunkContextResponse struct {
	Chunks      []chunkDTO   `json:"chunks"`
	TargetIndex int          `json:"target_index"`
	Document    *documentDTO `json:"document,omitempty"`
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

// toChunkDTO projects a model.Chunk to the wire DTO (spec 047 US2).
func toChunkDTO(c model.Chunk) chunkDTO {
	return chunkDTO{
		ChunkID:           c.ID,
		DocumentID:        c.DocumentID,
		Content:           c.Content,
		ChunkIndex:        c.ChunkIndex,
		TotalChunks:       c.TotalChunks,
		PageNumber:        c.PageNumber,
		StartChar:         c.StartCharIdx,
		EndChar:           c.EndCharIdx,
		TokenCount:        c.TokenCount,
		PreviousChunkID:   c.PreviousChunkID,
		NextChunkID:       c.NextChunkID,
		Poisoning:         c.Poisoning,
		SectionContext:    c.SectionContext,
		SectionDepth:      c.SectionLevel,
		ExtractionQuality: c.ExtractionQuality,
		ExtractionMethod:  c.ExtractionMethod,
		Wikilinks:         c.Wikilinks,
		NearDup:           c.NearDup,
		Kind:              c.Kind,
		CreatedAt:         rfc3339(c.CreatedAt),
	}
}

// writeEngineErr maps an engine error to an HTTP response: ErrInvalid → 400
// "invalid"; ErrNotFound → 404 "not found"; anything else → 500 "internal"
// (no detail leakage). Mirrors rest.writeEngineErr.
func writeEngineErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid")
	case errors.Is(err, engine.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal")
	}
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

// --- spec 047 US2: document detail + chunks + chunk context (read-only) ---

// handleDocumentDetail resolves one document (+ its source, so source_path is
// populated — unlike the list row) for the detail header. GET /api/documents/{id}.
func (s *Server) handleDocumentDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	res, err := s.eng.GetDocument(id)
	if err != nil {
		writeEngineErr(w, err) // ErrNotFound → 404
		return
	}
	writeJSON(w, http.StatusOK, toDocumentDTO(res.Document, res.Source))
}

// handleDocumentChunks lists one document's chunks, paginated. GET
// /api/documents/{id}/chunks. Unknown/empty document → empty page (200), matching
// engine.ListChunks' tolerant empty result (the detail route 404s unknown docs).
func (s *Server) handleDocumentChunks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	req := engine.ListChunksRequest{PageToken: q.Get("page_token")}
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "page_size must be an integer")
			return
		}
		req.PageSize = n
	}
	res, err := s.eng.ListChunks(id, req)
	if err != nil {
		writeEngineErr(w, err) // ErrInvalid (bad page_size/token) → 400
		return
	}
	out := documentChunksResponse{Chunks: make([]chunkDTO, len(res.Chunks)), NextPageToken: res.NextPageToken}
	for i, c := range res.Chunks {
		out.Chunks[i] = toChunkDTO(c)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleChunkContext returns a chunk plus up to `window` neighbours each side
// (+ the parent document). GET /api/documents/{id}/chunks/{chunkID}/context.
func (s *Server) handleChunkContext(w http.ResponseWriter, r *http.Request) {
	chunkID := r.PathValue("chunkID")
	window := 2
	if raw := r.URL.Query().Get("window"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "window must be an integer")
			return
		}
		window = n
	}
	res, err := s.eng.GetChunkContext(chunkID, window)
	if err != nil {
		writeEngineErr(w, err) // ErrInvalid (bad window/empty id) → 400; ErrNotFound (chunk) → 404
		return
	}
	out := chunkContextResponse{Chunks: make([]chunkDTO, len(res.Chunks)), TargetIndex: res.TargetIndex}
	for i, c := range res.Chunks {
		out.Chunks[i] = toChunkDTO(c)
	}
	if res.Document.ID != "" {
		d := toDocumentDTO(res.Document, res.Source)
		out.Document = &d
	}
	writeJSON(w, http.StatusOK, out)
}

// --- spec 047 US3: content search (read-only) ---

// searchHitDTO is the matching-chunk context for one search result (US3): where
// the query matched, so the operator sees the found text without opening the doc.
type searchHitDTO struct {
	ChunkID        string   `json:"chunk_id"`
	ChunkIndex     int      `json:"chunk_index"`
	Snippet        string   `json:"snippet"`
	Score          float64  `json:"score"`
	SectionContext []string `json:"section_context,omitempty"`
}

// searchResultDTO is one ranked search result: the parent document plus its top
// matching chunk (distinct per document, in retrieval rank).
type searchResultDTO struct {
	Document documentDTO  `json:"document"`
	Match    searchHitDTO `json:"match"`
}

// documentsSearchResponse is the UI envelope for a content-search result.
type documentsSearchResponse struct {
	Query   string            `json:"query"`
	Results []searchResultDTO `json:"results"`
}

// handleDocumentsSearch searches the corpus by chunk content (engine.Query) and
// returns the distinct parent documents, ranked by retrieval order (R2). GET
// /api/documents/search?q=&limit=. Name/path matching is folded client-side over
// the ranked result. 400 on empty/missing q; 401 via the guard.
func (s *Server) handleDocumentsSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "limit must be 1..100")
			return
		}
		limit = n
	}
	res, err := s.eng.Query(r.Context(), engine.QueryRequest{Query: q, K: limit})
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	// Distinct parent documents (top hit each), ranked by retrieval order (R2).
	// Carry the matching chunk's snippet so the operator sees the found text.
	seen := make(map[string]bool, len(res.Hits))
	results := make([]searchResultDTO, 0, len(res.Hits))
	for _, h := range res.Hits {
		if h.DocumentID == "" || seen[h.DocumentID] {
			continue
		}
		seen[h.DocumentID] = true
		d, err := s.eng.GetDocument(h.DocumentID)
		if err != nil {
			continue
		}
		results = append(results, searchResultDTO{
			Document: toDocumentDTO(d.Document, d.Source),
			Match: searchHitDTO{
				ChunkID:        h.ChunkID,
				ChunkIndex:     h.ChunkIndex,
				Snippet:        snippet(h.Content, q, 240),
				Score:          h.Score,
				SectionContext: h.SectionContext,
			},
		})
	}
	writeJSON(w, http.StatusOK, documentsSearchResponse{Query: q, Results: results})
}

// snippet returns a ~maxLen-char window of content centered on the first query-term
// match (case-insensitive); ellipsised when truncated. Falls back to the start.
func snippet(content, query string, maxLen int) string {
	if content == "" {
		return ""
	}
	low := strings.ToLower(content)
	idx := strings.Index(low, strings.ToLower(query))
	start := 0
	if idx >= 0 {
		start = idx - maxLen/3
		if start < 0 {
			start = 0
		}
	}
	end := start + maxLen
	if end > len(content) {
		end = len(content)
	}
	s := content[start:end]
	if start > 0 {
		s = "…" + s
	}
	if end < len(content) {
		s += "…"
	}
	return s
}
