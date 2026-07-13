package ui

import (
	"net/http"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// dashboardDTO is the GET /api/dashboard/stats response — a projection of
// engine.StatusInfo plus a server-derived vault name. It is a superset of the
// REST statusResponse: the UI surfaces the readiness story (drift_verdict,
// embed_pending/failed) and the active vault. See specs/046 data-model.md.
//
// The UI calls engine.Status() directly — the same method REST /v1/status and
// MCP go_rag_status call — so documents/chunks/embeddings match byte-for-byte
// across transports (cross-transport parity, pinned by TestDashboard_Parity).
type dashboardDTO struct {
	Documents          int    `json:"documents"`
	Chunks             int    `json:"chunks"`
	Embeddings         int    `json:"embeddings"`
	Dimensions         int    `json:"dimensions"`
	EmbeddingModel     string `json:"embedding_model"`
	Reranker           string `json:"reranker"`
	OllamaURL          string `json:"ollama_url"`
	EmbeddingsComplete bool   `json:"embeddings_complete"` // index-health flag (docs==0 || embs>=chunks)
	DriftVerdict       string `json:"drift_verdict"`       // clean|hard-drift|version-warning|unknown|n/a
	HardDrift          bool   `json:"hard_drift"`
	EmbedPending       int    `json:"embed_pending"`
	EmbedFailed        int    `json:"embed_failed"`
	EnrichedDocs       int    `json:"enriched_docs"`
	EnrichmentEnabled  bool   `json:"enrichment_enabled"`
	Vault              string `json:"vault"` // derived from the engine's DBPath
}

// handleDashboardStats projects engine.Status() into a DashboardDTO. Read-only;
// it mutates nothing and triggers no bridge/MuninnDB call (Slice 0 is
// go-rag-native only). 500 with a generic body on engine failure (no leakage).
func (s *Server) handleDashboardStats(w http.ResponseWriter, _ *http.Request) {
	info, err := s.eng.Status("default")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, toDashboardDTO(info, s.deriveVault()))
}

func toDashboardDTO(i *engine.StatusInfo, vault string) dashboardDTO {
	return dashboardDTO{
		Documents:          i.Documents,
		Chunks:             i.Chunks,
		Embeddings:         i.Embeddings,
		Dimensions:         i.Dimensions,
		EmbeddingModel:     i.EmbeddingModel,
		Reranker:           i.Reranker,
		OllamaURL:          i.OllamaURL,
		EmbeddingsComplete: i.EmbeddingsComplete,
		DriftVerdict:       i.DriftVerdict,
		HardDrift:          i.HardDrift,
		EmbedPending:       i.EmbedPending,
		EmbedFailed:        i.EmbedFailed,
		EnrichedDocs:       i.EnrichedDocs,
		EnrichmentEnabled:  i.EnrichmentEnabled,
		Vault:              vault,
	}
}

// deriveVault returns the active vault name from the engine's config DBPath.
// The Pebble data dir lives under <vault>/data, so when the configured path is
// the data dir, the vault name is its parent's base. No engine method exposes
// the vault today; this edge derivation avoids changing the engine for Slice 0.
func (s *Server) deriveVault() string {
	if s.eng == nil {
		return ""
	}
	dbPath := s.eng.Config().DBPath
	if filepath.Base(dbPath) == "data" {
		dbPath = filepath.Dir(dbPath)
	}
	return filepath.Base(dbPath)
}
