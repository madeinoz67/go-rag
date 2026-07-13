package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// query.go is the spec 048 (Slice 2) Query view: a read-only query → inspect →
// tune surface over Engine.Query. It is a fourth adapter over the engine (a
// peer to REST/gRPC/MCP/CLI), NOT a REST proxy — handleQuery calls s.eng.Query
// in-process, exactly as dashboard.go calls engine.Status and documents.go calls
// engine.ListDocuments. Research: research.md R1–R12; contract:
// contracts/ui-query.md.
//
// The DTOs mirror internal/rest's queryRequest / queryHit / queryResponse
// field-for-field (R4) so a hit served by the UI is byte-identical to one served
// by REST when context_window=0 — the UI's only addition is Context (sibling
// chunks), which is omitempty and therefore invisible at the default. The UI
// package does NOT import internal/rest (each transport owns its DTOs).

// validQueryModes is the exhaustive set of retrieval modes the engine accepts
// (engine.QueryRequest.Mode). Empty is also valid — the engine resolves it to
// the "hybrid" default — so the handler only rejects a NON-empty unknown mode.
var validQueryModes = map[string]bool{
	"hybrid":   true,
	"semantic": true,
	"keyword":  true,
}

// queryRequestDTO mirrors rest.queryRequest 1:1 (R4). The engine.Filter is
// expanded to flat source/type/tags fields, matching how REST + the CLI present
// it; the handler composes them via engine.NewFilter.
type queryRequestDTO struct {
	Query              string   `json:"query"`
	K                  int      `json:"k"`
	Mode               string   `json:"mode"`
	NoRerank           bool     `json:"no_rerank"`
	Threshold          float64  `json:"threshold"`
	RRFK               int      `json:"rrf_k,omitempty"`
	PoolSize           int      `json:"pool_size,omitempty"`
	Source             string   `json:"source,omitempty"`
	Type               string   `json:"type,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	ContextWindow      int      `json:"context_window,omitempty"`
	NoCache            bool     `json:"no_cache,omitempty"`
	IncludeQuarantined bool     `json:"include_quarantined,omitempty"`
	Dedup              bool     `json:"dedup,omitempty"`
}

// queryResponseDTO mirrors rest.queryResponse 1:1 (R4).
type queryResponseDTO struct {
	Hits          []queryHitDTO `json:"hits"`
	RerankFailed  bool          `json:"rerank_failed"`
	EffectiveK    int           `json:"effective_k"`
	EffectivePool int           `json:"effective_pool"`
	EffectiveMode string        `json:"effective_mode"`
}

// queryHitDTO mirrors rest.queryHit 1:1 in field order + json tags, adding
// Context (sibling chunks) as the only UI-only field. Context is omitempty, so
// a context_window=0 query serializes byte-identically to the REST projection
// (cross-transport parity, pinned by TestUIQuery_Parity).
type queryHitDTO struct {
	ChunkID           string            `json:"chunk_id"`
	DocumentID        string            `json:"document_id"`
	Score             float64           `json:"score"`
	Content           string            `json:"content"`
	FilePath          string            `json:"file_path"`
	Page              int               `json:"page"`
	ChunkIndex        int               `json:"chunk_index"`
	Poisoning         *poisonVerdictDTO `json:"poisoning,omitempty"`
	SectionContext    []string          `json:"section_context,omitempty"`
	SectionDepth      int               `json:"section_depth,omitempty"`
	ExtractionQuality float64           `json:"extraction_quality,omitempty"`
	ExtractionMethod  string            `json:"extraction_method,omitempty"`
	Wikilinks         []string          `json:"wikilinks,omitempty"`
	NearDup           *nearDupDTO       `json:"near_dup,omitempty"`
	Summary           string            `json:"summary,omitempty"`
	EnrichmentStatus  string            `json:"enrichment_status,omitempty"`
	Context           []contextChunkDTO `json:"context,omitempty"`
}

// poisonVerdictDTO mirrors rest.poisonVerdict (field-parallel to
// model.PoisonVerdict). Field order matches REST so the serialized poisoning
// object is byte-identical across the two transports.
type poisonVerdictDTO struct {
	Level          string            `json:"level"`
	Score          float64           `json:"score"`
	Signals        *poisonSignalsDTO `json:"signals,omitempty"`
	MatchedPhrases []string          `json:"matched_phrases,omitempty"`
}

// poisonSignalsDTO mirrors rest.poisonSignals.
type poisonSignalsDTO struct {
	Repetition  float64 `json:"repetition"`
	Stuffing    float64 `json:"stuffing"`
	Instruction float64 `json:"instruction"`
}

// nearDupDTO mirrors rest.nearDupInfo (field-parallel to model.NearDupInfo).
type nearDupDTO struct {
	Siblings   []string `json:"siblings,omitempty"`
	Similarity float64  `json:"similarity,omitempty"`
}

// contextChunkDTO is the UI projection of engine.ContextChunk — the sibling
// chunks around a hit when context_window > 0. is_before distinguishes the left
// (previous) and right (next) neighbours for rendering. The engine exposes
// ChunkID + Content + Direction (no chunk_index on siblings), so chunk_id is
// carried verbatim (data-model.md's chunk_index is not available on
// engine.ContextChunk; chunk_id is the faithful projection).
type contextChunkDTO struct {
	ChunkID  string `json:"chunk_id"`
	Content  string `json:"content"`
	IsBefore bool   `json:"is_before"`
}

// toQueryHitDTO projects an engine.QueryHit to the UI DTO, mirroring REST's
// toQueryHits field-for-field and adding the sibling Context (UI-only, R7: the
// hit payload carries everything the detail view needs — no second round-trip).
func toQueryHitDTO(h engine.QueryHit) queryHitDTO {
	out := queryHitDTO{
		ChunkID:           h.ChunkID,
		DocumentID:        h.DocumentID,
		Score:             h.Score,
		Content:           h.Content,
		FilePath:          h.FilePath,
		Page:              h.Page,
		ChunkIndex:        h.ChunkIndex,
		SectionContext:    h.SectionContext,
		SectionDepth:      h.SectionLevel,
		ExtractionQuality: h.ExtractionQuality,
		ExtractionMethod:  h.ExtractionMethod,
		Wikilinks:         h.Wikilinks,
		Summary:           h.Summary,
		EnrichmentStatus:  h.EnrichmentStatus,
	}
	if h.Poisoning != nil {
		out.Poisoning = &poisonVerdictDTO{
			Level:          string(h.Poisoning.Level),
			Score:          h.Poisoning.Score,
			MatchedPhrases: h.Poisoning.MatchedPhrases,
			Signals: &poisonSignalsDTO{
				Repetition:  h.Poisoning.Signals.Repetition,
				Stuffing:    h.Poisoning.Signals.Stuffing,
				Instruction: h.Poisoning.Signals.Instruction,
			},
		}
	}
	if h.NearDup != nil {
		out.NearDup = &nearDupDTO{Siblings: h.NearDup.Siblings, Similarity: h.NearDup.Similarity}
	}
	if len(h.Context) > 0 {
		out.Context = make([]contextChunkDTO, len(h.Context))
		for i, c := range h.Context {
			out.Context[i] = contextChunkDTO{
				ChunkID:  c.ChunkID,
				Content:  c.Content,
				IsBefore: c.Direction == "previous", // "previous" = left sibling = is_before
			}
		}
	}
	return out
}

// toQueryResponseDTO projects an engine.QueryResult to the UI response DTO.
func toQueryResponseDTO(res *engine.QueryResult) queryResponseDTO {
	out := queryResponseDTO{
		RerankFailed:  res.RerankFailed,
		EffectiveK:    res.EffectiveK,
		EffectivePool: res.EffectivePool,
		EffectiveMode: res.EffectiveMode,
		Hits:          make([]queryHitDTO, len(res.Hits)),
	}
	for i, h := range res.Hits {
		out.Hits[i] = toQueryHitDTO(h)
	}
	return out
}

// handleQuery is the UI projection of engine.Query (spec 048): POST /api/query
// (JSON body) → engine.Query in-process → queryResponseDTO. Strictly read-only
// (mutates nothing — FR-009). Mirrors rest.handleQuery 1:1 in QueryRequest
// composition and field mapping. Validation (R11): empty/whitespace query → 400
// "empty query"; a non-empty unknown mode → 400 "invalid mode". Engine errors
// flow through writeQueryEngineErr (R10), which routes the two operator-
// actionable failures (embedding mismatch, embedder unreachable) to
// distinguishable responses and falls through to writeEngineErr otherwise.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "empty query")
		return
	}
	// Empty mode is valid (engine resolves it to the "hybrid" default); only a
	// NON-empty unknown mode is rejected (parity with the engine's mode set).
	if req.Mode != "" && !validQueryModes[req.Mode] {
		writeError(w, http.StatusBadRequest, "invalid mode")
		return
	}
	engReq := engine.QueryRequest{
		Query:              req.Query,
		K:                  req.K,
		Mode:               req.Mode,
		NoRerank:           req.NoRerank,
		Threshold:          req.Threshold,
		RRFK:               req.RRFK,
		PoolSize:           req.PoolSize,
		Filter:             engine.NewFilter(req.Source, req.Type, req.Tags),
		ContextWindow:      req.ContextWindow,
		NoCache:            req.NoCache,
		IncludeQuarantined: req.IncludeQuarantined,
		Dedup:              req.Dedup,
	}
	res, err := s.eng.Query(r.Context(), vaultFromRequest(r), engReq)
	if err != nil {
		writeQueryEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toQueryResponseDTO(res))
}

// writeQueryEngineErr maps an engine.Query error to an HTTP response, routing
// the two operator-actionable failures (R10) to distinguishable contract
// responses and delegating the generic tail to writeEngineErr (reused, not
// duplicated):
//   - ErrEmbeddingMismatch → 400 {"error":"embedding mismatch","detail":...}
//     (query model/dim/convention ≠ corpus; client suggests re-embed/switch).
//   - embedder unreachable (the raw Ollama network error has no sentinel, so it
//     is detected by its connection-failure signature) → 503
//     {"error":"embedder unavailable","detail":...} (client suggests keyword mode).
//   - ErrInvalid → 400, ErrNotFound → 404, anything else → 500 (via writeEngineErr).
//
// The frontend (T012) maps these signals to plain guidance. Rerank failure is
// NOT an error — it is surfaced as rerank_failed:true on a 200 response.
func writeQueryEngineErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, engine.ErrEmbeddingMismatch) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "embedding mismatch", "detail": err.Error()})
		return
	}
	if errors.Is(err, engine.ErrInvalid) || errors.Is(err, engine.ErrNotFound) {
		writeEngineErr(w, err)
		return
	}
	if isEmbedderUnreachable(err) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "embedder unavailable", "detail": err.Error()})
		return
	}
	writeEngineErr(w, err)
}

// embedderUnreachableHints are the substrings that indicate the embedder (local
// Ollama) could not be reached — the engine surfaces the raw network error from
// the embed call. The match is case-insensitive. This is a best-effort signal:
// the engine exposes no sentinel for "embedder down", and Ollama-URL config +
// any connection failure is the definitive marker. Used only to pick the 503
// contract response (vs the generic 500); the detail string is the error verbatim.
var embedderUnreachableHints = []string{
	"connection refused",
	"dial tcp",
	"no such host",
	"connect: connection",
	"websocket: bad handshake",
	"ollama", // appears in the engine's resolved URL/error context
}

func isEmbedderUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, hint := range embedderUnreachableHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}
