package ui

// quarantine.go (spec 053) is the console's quarantine-MANAGEMENT surface for the
// poisoning detector (spec 019 / audit H04). The engine surface is already
// complete and vault-aware (spec 052): ListPoisoned, ReleaseChunk, ResetChunk,
// RescanPoisoning, plus GetChunk for the detail view's full text. This file is
// the 4th adapter (the browser console) over those existing methods — no new
// engine capability, no new transport. It fulfils the standing preference: a
// system with quarantine functionality MUST have a dedicated Quarantine
// Management section (list → see-why → release).
//
// Routes (all spec 045 Bearer-guarded; vault from ?vault= / X-Go-Rag-Vault):
//
//	GET  /api/quarantine/list            → every flagged chunk + verdict (US1)
//	GET  /api/quarantine/{id}/detail     → full chunk text + verdict (US2)
//	POST /api/quarantine/{id}/release    → un-flag a false positive (US3)
//	POST /api/quarantine/{id}/reset      → force a re-scan of one chunk (US3)
//	POST /api/quarantine/rescan          → vault-wide rescan (US3)
//
// Destructive ops (release/reset/rescan) are confirmed client-side (R7); the
// server executes the guarded mutation on receipt. Release/reset/rescan return
// 204; the list is always 200 (an empty vault is a healthy state, not an error).

import (
	"net/http"
	"strings"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/poison"
)

// quarantineListDTO is the UI envelope for GET /api/quarantine/list (US1). The
// response is always 200: a clean vault renders {chunks:[], count:0} (a healthy
// empty state, never an error — FR-009).
type quarantineListDTO struct {
	Chunks []poisonedChunkDTO `json:"chunks"`
	Count  int                `json:"count"`
}

// poisonedChunkDTO is one flagged-chunk row: identity, a 160-char preview, and
// the full verdict (level/score/per-signal breakdown/matched phrases) so the
// operator can triage WHY each chunk was flagged (FR-001/FR-002). The verdict
// embeds model.PoisonVerdict directly — its json tags (level/score/signals/
// matched_phrases) are exactly the contract in data-model.md.
type poisonedChunkDTO struct {
	ChunkID    string              `json:"chunk_id"`
	DocumentID string              `json:"document_id"`
	Preview    string              `json:"preview"`
	Verdict    model.PoisonVerdict `json:"verdict"`
}

// quarantineDetailDTO is the US2 detail payload: the full chunk text (so the
// client can overlay matched-phrase highlights) plus the source document name
// (breadcrumb) and the verdict (signal breakdown + matched phrases). Built from
// Engine.GetChunk — orphan-tolerant (a vanished parent doc yields an empty name).
//
// repetition_matches / stuffing_matches are derived from the chunk content via
// the poison detector's term-extraction (not persisted) so the UI can highlight
// WHAT triggered each signal — MatchedPhrases only carries instruction hits, so
// these give repetition/stuffing their own per-signal highlights.
type quarantineDetailDTO struct {
	ChunkID         string              `json:"chunk_id"`
	DocumentID      string              `json:"document_id"`
	Content         string              `json:"content"`
	DocumentName    string              `json:"document_name,omitempty"`
	Verdict         model.PoisonVerdict `json:"verdict"`
	RepetitionTerms []string            `json:"repetition_matches,omitempty"`
	StuffingTerms   []string            `json:"stuffing_matches,omitempty"`
}

// toQuarantineListDTO projects the engine's []PoisonedChunk into the UI envelope.
// count is derived (len(chunks)) so the client never has to recompute it.
func toQuarantineListDTO(chunks []engine.PoisonedChunk) quarantineListDTO {
	out := quarantineListDTO{Chunks: make([]poisonedChunkDTO, 0, len(chunks))}
	for _, c := range chunks {
		out.Chunks = append(out.Chunks, poisonedChunkDTO{
			ChunkID:    c.ChunkID,
			DocumentID: c.DocumentID,
			Preview:    c.Preview,
			Verdict:    c.Verdict,
		})
	}
	out.Count = len(out.Chunks)
	return out
}

// handleQuarantineList is the UI projection of Engine.ListPoisoned (US1):
// GET /api/quarantine/list?vault=default → every flagged chunk in the active
// vault with its verdict. Always 200 (empty → healthy empty state).
func (s *Server) handleQuarantineList(w http.ResponseWriter, r *http.Request) {
	chunks, err := s.eng.ListPoisoned(vaultFromRequest(r))
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toQuarantineListDTO(chunks))
}

// handleQuarantineDetail is the UI projection of Engine.GetChunk (US2):
// GET /api/quarantine/{id}/detail?vault=default → the full chunk text + verdict
// + source document name. The client overlays the verdict's MatchedPhrases as
// highlighted spans on the content. 404 unknown chunk (ErrNotFound).
func (s *Server) handleQuarantineDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	res, err := s.eng.GetChunk(vaultFromRequest(r), id)
	if err != nil {
		writeEngineErr(w, err) // ErrNotFound → 404
		return
	}
	dto := quarantineDetailDTO{
		ChunkID:         res.Chunk.ID,
		DocumentID:      res.Chunk.DocumentID,
		Content:         res.Chunk.Content,
		DocumentName:    res.Document.FileName,
		RepetitionTerms: poison.RepetitionTerms(res.Chunk.Content),
		StuffingTerms:   poison.StuffingTerms(res.Chunk.Content),
	}
	if res.Chunk.Poisoning != nil {
		dto.Verdict = *res.Chunk.Poisoning
	}
	writeJSON(w, http.StatusOK, dto)
}

// handleQuarantineRelease is the UI projection of Engine.ReleaseChunk (US3):
// POST /api/quarantine/{id}/release?vault=default → permanent false-positive
// override (the chunk re-enters default retrieval; sticky across rescans).
// Confirmation is a client-side gate; the server executes on receipt. 204 / 404.
func (s *Server) handleQuarantineRelease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.eng.ReleaseChunk(vaultFromRequest(r), id); err != nil {
		writeEngineErr(w, err) // ErrNotFound → 404
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleQuarantineReset is the UI projection of Engine.ResetChunk (US3):
// POST /api/quarantine/{id}/reset?vault=default → force a re-scan of one chunk
// (the verdict is recomputed; may restore quarantine). 204 / 404.
func (s *Server) handleQuarantineReset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.eng.ResetChunk(vaultFromRequest(r), id); err != nil {
		writeEngineErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleQuarantineRescan is the UI projection of Engine.RescanPoisoning (US3):
// POST /api/quarantine/rescan?vault=default → re-score every chunk under the
// current detector/thresholds (idempotent for unchanged content). 204 on success.
func (s *Server) handleQuarantineRescan(w http.ResponseWriter, r *http.Request) {
	if _, _, err := s.eng.RescanPoisoning(vaultFromRequest(r)); err != nil {
		writeEngineErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
