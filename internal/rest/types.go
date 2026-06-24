package rest

// REST request/response DTOs. These mirror the internal/engine structured types
// (and the gRPC/proto messages) 1:1 — adapters carry no independent logic, only
// serialization. See specs/003-rest-grpc-api/data-model.md.

type queryRequest struct {
	Query              string   `json:"query"`
	K                  int      `json:"k"`
	Mode               string   `json:"mode"`
	NoRerank           bool     `json:"no_rerank"`
	Threshold          float64  `json:"threshold"`
	RRFK               int      `json:"rrf_k,omitempty"`               // H08/spec 009
	PoolSize           int      `json:"pool_size,omitempty"`           // H22/spec 024: candidate-pool override; 0 = config/default (60)
	Source             string   `json:"source,omitempty"`
	Type               string   `json:"type,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	ContextWindow      int      `json:"context_window,omitempty"`
	NoCache            bool     `json:"no_cache,omitempty"`            // H06/spec 016: bypass the result cache for this query
	IncludeQuarantined bool     `json:"include_quarantined,omitempty"` // H04/spec 019: return poisoning-flagged chunks
}

type queryHit struct {
	ChunkID    string         `json:"chunk_id"`
	DocumentID string         `json:"document_id"`
	Score      float64        `json:"score"`
	Content    string         `json:"content"`
	FilePath   string         `json:"file_path"`
	Page       int            `json:"page"`
	ChunkIndex int            `json:"chunk_index"`         // H21/spec 023
	Poisoning  *poisonVerdict `json:"poisoning,omitempty"` // H04/spec 019
}

// poisonVerdict is the REST projection of model.PoisonVerdict (H04/spec 019). Field
// names mirror the gRPC/proto projection 1:1 (cross-transport parity, spec 003).
type poisonVerdict struct {
	Level          string         `json:"level"`
	Score          float64        `json:"score"`
	Signals        *poisonSignals `json:"signals,omitempty"`
	MatchedPhrases []string       `json:"matched_phrases,omitempty"`
}

type poisonSignals struct {
	Repetition  float64 `json:"repetition"`
	Stuffing    float64 `json:"stuffing"`
	Instruction float64 `json:"instruction"`
}

// poisonedChunk is one entry in the quarantine listing (H04/spec 019, US2). The
// verdict carries the per-signal breakdown so a caller can see why it was flagged.
type poisonedChunk struct {
	ChunkID    string         `json:"chunk_id"`
	DocumentID string         `json:"document_id"`
	Preview    string         `json:"preview"`
	Verdict    *poisonVerdict `json:"verdict"`
}

type poisonResponse struct {
	Flagged []poisonedChunk `json:"flagged"`
}

type queryResponse struct {
	Hits         []queryHit `json:"hits"`
	RerankFailed bool       `json:"rerank_failed"`
	EffectiveK    int    `json:"effective_k"`    // H22/spec 024
	EffectivePool int    `json:"effective_pool"` // H22/spec 024
	EffectiveMode string `json:"effective_mode"` // H22/spec 024
}

// statusResponse mirrors engine.StatusInfo / proto StatusResponse (parity).
type statusResponse struct {
	Documents          int    `json:"documents"`
	Chunks             int    `json:"chunks"`
	Embeddings         int    `json:"embeddings"`
	Dimensions         int    `json:"dimensions"`
	EmbeddingModel     string `json:"embedding_model"`
	Reranker           string `json:"reranker"`
	OllamaURL          string `json:"ollama_url"`
	EmbeddingsComplete bool   `json:"embeddings_complete"`
	PoolSize             int                `json:"pool_size"`              // H22/spec 024
	AdaptiveDepthEnabled bool               `json:"adaptive_depth_enabled"` // H22/spec 024
	PoolUtilization      poolUtilizationDTO `json:"pool_utilization"`       // H22/spec 024
}

// poolUtilizationDTO mirrors engine.PoolUtilization / proto PoolUtilization (parity).
type poolUtilizationDTO struct {
	Queries    uint64  `json:"queries"`
	AvgFetched float64 `json:"avg_fetched"`
	AvgKept    float64 `json:"avg_kept"`
	Saturated  uint64  `json:"saturated"`
}

// ingestSummary mirrors engine.IngestSummary / proto IngestSummary (parity).
type ingestSummary struct {
	New      int `json:"new"`
	Skipped  int `json:"skipped"`
	Modified int `json:"modified"`
	Deleted  int `json:"deleted"`
	Errors   int `json:"errors"`
}

// addRequest is the body for POST /v1/add.
type addRequest struct {
	Path string `json:"path"`
}

// fileEntry / filesResponse mirror engine.FileEntry / proto (parity).
type fileEntry struct {
	FilePath   string `json:"file_path"`
	FileType   string `json:"file_type"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
}
type filesResponse struct {
	Files []fileEntry `json:"files"`
}

// dirEntry / dirsResponse mirror engine.DirEntry / proto (parity).
type dirEntry struct {
	Dir    string `json:"dir"`
	Files  int    `json:"files"`
	Chunks int    `json:"chunks"`
}
type dirsResponse struct {
	Dirs []dirEntry `json:"dirs"`
}

// configPutRequest is the body for PUT /v1/config.
type configPutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// vaultEntry / vaultsResponse mirror engine.VaultEntry / proto (parity).
type vaultEntry struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
}
type vaultsResponse struct {
	Vaults []vaultEntry `json:"vaults"`
}
