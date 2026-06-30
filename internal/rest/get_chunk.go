package rest

import (
	"net/http"
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
)

// get_chunk.go is the REST projection of engine.GetChunk (spec 035 / BL-001):
// GET /v1/chunks/{id} → {chunk, document}. The DTOs mirror the proto Chunk /
// DocumentMeta and the CLI output 1:1 (cross-transport parity, Constitution V).

// rfc3339 renders a time as UTC RFC3339, "" for the zero value (so unset
// timestamps serialize as absent rather than "0001-01-01T00:00:00Z").
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// chunkDTO is the REST projection of model.Chunk for GetChunk (spec 035).
type chunkDTO struct {
	ChunkID         string         `json:"chunk_id"`
	DocumentID      string         `json:"document_id"`
	Content         string         `json:"content"`
	ChunkIndex      int            `json:"chunk_index"`
	TotalChunks     int            `json:"total_chunks"`
	PageNumber      int            `json:"page_number"`
	StartChar       int            `json:"start_char"`
	EndChar         int            `json:"end_char"`
	TokenCount      int            `json:"token_count"`
	PreviousChunkID string         `json:"previous_chunk_id,omitempty"`
	NextChunkID     string         `json:"next_chunk_id,omitempty"`
	Poisoning       *poisonVerdict `json:"poisoning,omitempty"`
	SectionContext  []string       `json:"section_context,omitempty"`
	NearDup         *nearDupInfo   `json:"near_dup,omitempty"`
	Kind            string         `json:"kind,omitempty"`
	CreatedAt       string         `json:"created_at,omitempty"`
}

// documentMetaDTO is the REST projection of model.Document (+ spec-029 EnrichInfo
// flattened) for GetChunk (spec 035 US2). id is the identity hash; content_hash
// is the distinct change-detection hash (PRD §7.2).
type documentMetaDTO struct {
	ID               string   `json:"id"`
	ContentHash      string   `json:"content_hash"`
	SourceID         string   `json:"source_id"`
	SourcePath       string   `json:"source_path,omitempty"`
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

type getChunkResponse struct {
	Chunk    chunkDTO         `json:"chunk"`
	Document *documentMetaDTO `json:"document,omitempty"`
}

func toPoisonVerdictDTO(v *model.PoisonVerdict) *poisonVerdict {
	if v == nil {
		return nil
	}
	return &poisonVerdict{
		Level:          string(v.Level),
		Score:          v.Score,
		MatchedPhrases: v.MatchedPhrases,
		Signals: &poisonSignals{
			Repetition:  v.Signals.Repetition,
			Stuffing:    v.Signals.Stuffing,
			Instruction: v.Signals.Instruction,
		},
	}
}

func toNearDupDTO(nd *model.NearDupInfo) *nearDupInfo {
	if nd == nil {
		return nil
	}
	return &nearDupInfo{Siblings: nd.Siblings, Similarity: nd.Similarity}
}

func toChunkDTO(c model.Chunk) chunkDTO {
	return chunkDTO{
		ChunkID:         c.ID,
		DocumentID:      c.DocumentID,
		Content:         c.Content,
		ChunkIndex:      c.ChunkIndex,
		TotalChunks:     c.TotalChunks,
		PageNumber:      c.PageNumber,
		StartChar:       c.StartCharIdx,
		EndChar:         c.EndCharIdx,
		TokenCount:      c.TokenCount,
		PreviousChunkID: c.PreviousChunkID,
		NextChunkID:     c.NextChunkID,
		Poisoning:       toPoisonVerdictDTO(c.Poisoning),
		SectionContext:  c.SectionContext,
		NearDup:         toNearDupDTO(c.NearDup),
		Kind:            c.Kind,
		CreatedAt:       rfc3339(c.CreatedAt),
	}
}

func toDocumentMetaDTO(d model.Document, src model.Source) *documentMetaDTO {
	o := &documentMetaDTO{
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
		o.Tags = d.Enrichment.Tags
		o.Summary = d.Enrichment.Summary
		o.EnrichmentStatus = d.Enrichment.Status
		o.EnrichmentModel = d.Enrichment.Model
		o.EnrichmentAt = rfc3339(d.Enrichment.GeneratedAt)
	}
	return o
}

// handleGetChunk is the REST projection of engine.GetChunk (spec 035 / BL-001):
// GET /v1/chunks/{id} → {chunk, document}. A missing id → 404 via writeEngineErr
// (ErrNotFound); empty/whitespace → 400 (ErrInvalid).
func (s *Server) handleGetChunk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.eng.GetChunk(id)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	resp := getChunkResponse{Chunk: toChunkDTO(res.Chunk)}
	if res.Document.ID != "" { // orphan chunk → document omitted (nil)
		resp.Document = toDocumentMetaDTO(res.Document, res.Source)
	}
	writeJSON(w, http.StatusOK, resp)
}
