package ui

import (
	"net/http"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/redact"
)

// settingsDTO is the read-only Effective Configuration projection served at
// GET /api/settings (spec 055, Slice 0). Every value is EFFECTIVE (defaults
// applied where unset) and is sourced from engine.Status + the engine's config —
// the same surfaces the status command and the per-query Query view report, so
// the three can never disagree (FR-007 / SC-002). Read-only: the handler mutates
// nothing (FR-006). cacheStatsDTO / toCacheStatsDTO are reused from bridgeops.go.
type settingsDTO struct {
	Vault      string        `json:"vault"`
	Retrieval  retrievalDTO  `json:"retrieval"`
	Embeddings embeddingsDTO `json:"embeddings"`
	Cache      cacheGroupDTO `json:"cache"`
	Chunking   chunkingDTO   `json:"chunking"`
	Redaction  redactionDTO  `json:"redaction"`
}

type retrievalDTO struct {
	RRFK                 int    `json:"rrf_k"`
	PoolSize             int    `json:"pool_size"`
	Reranker             string `json:"reranker"`
	RerankCandidates     int    `json:"rerank_candidates"`
	RerankRetryOnFailure bool   `json:"rerank_retry_on_failure"`
	AdaptiveDepthEnabled bool   `json:"adaptive_depth_enabled"`
	NearDupHamming       int    `json:"near_dup_hamming"`
}

type embeddingsDTO struct {
	Model               string `json:"model"`
	Dimensions          int    `json:"dimensions"`
	PrefixMode          string `json:"prefix_mode"`
	ResolvedQueryPrefix string `json:"resolved_query_prefix"`
	ResolvedDocPrefix   string `json:"resolved_doc_prefix"`
	StoredConvention    string `json:"stored_convention"`
	OllamaURL           string `json:"ollama_url"`
}

type cacheGroupDTO struct {
	Result    cacheStatsDTO `json:"result"`
	Embedding cacheStatsDTO `json:"embedding"`
}

type chunkingDTO struct {
	ChunkSize      int    `json:"chunk_size"`
	ChunkOverlap   int    `json:"chunk_overlap"`
	BoundaryMode   string `json:"boundary_mode"`
	SectionContext bool   `json:"section_context"`
}

type redactionDTO struct {
	Enabled            bool   `json:"enabled"`
	PatternCount       int    `json:"pattern_count"`
	CustomPatternsPath string `json:"custom_patterns_path"`
}

// handleSettings — spec 055 Slice 0. Projects the running daemon's effective
// configuration (retrieval / embeddings / cache / chunking / redaction) for the
// requested vault. Read-only; degrades gracefully (cache disabled ⇒ zeroed
// stats; a missing custom-pattern file ⇒ built-in count only) rather than
// erroring — a "no data yet" state is never an error (FR-009).
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	vault := vaultFromRequest(r)
	info, err := s.eng.Status(vault)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, toSettingsDTO(info, s.eng.Config(), vault))
}

func toSettingsDTO(i *engine.StatusInfo, cfg config.Config, vault string) settingsDTO {
	return settingsDTO{
		Vault: vault,
		Retrieval: retrievalDTO{
			RRFK:                 cfg.EffectiveRRFK(),
			PoolSize:             cfg.EffectivePoolSize(),
			Reranker:             i.Reranker,
			RerankCandidates:     cfg.RerankCandidates,
			RerankRetryOnFailure: cfg.RerankRetryOnFailure,
			AdaptiveDepthEnabled: i.AdaptiveDepthEnabled,
			NearDupHamming:       cfg.EffectiveNearDupHamming(),
		},
		Embeddings: embeddingsDTO{
			Model:               i.EmbeddingModel,
			Dimensions:          i.Dimensions,
			PrefixMode:          i.ConfiguredPrefix,
			ResolvedQueryPrefix: i.QueryPrefix,
			ResolvedDocPrefix:   i.DocPrefix,
			StoredConvention:    i.EmbeddingConvention,
			OllamaURL:           i.OllamaURL,
		},
		Cache: cacheGroupDTO{
			Result:    toCacheStatsDTO(i.ResultCache),
			Embedding: toCacheStatsDTO(i.EmbeddingCache),
		},
		Chunking: chunkingDTO{
			ChunkSize:      cfg.ChunkSize,
			ChunkOverlap:   cfg.ChunkOverlap,
			BoundaryMode:   "paragraph-sentence-word", // spec 013 cascade (single mode)
			SectionContext: true,                      // spec 025 (always threaded)
		},
		Redaction: redactionDTO{
			Enabled:            cfg.PIIRedactEnabled,
			PatternCount:       redactionPatternCount(cfg),
			CustomPatternsPath: cfg.PIIPatterns,
		},
	}
}

// redactionPatternCount returns the active redaction-pattern count (built-in
// curated set + custom file): len(redact.DefaultPatterns(custom)). A missing or
// unreadable custom file degrades to the built-in count only — a read-only
// display never errors on a bad path.
func redactionPatternCount(cfg config.Config) int {
	custom, _ := redact.LoadCustom(cfg.PIIPatterns)
	return len(redact.DefaultPatterns(custom))
}
