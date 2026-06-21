package rest

// REST request/response DTOs. These mirror the internal/engine structured types
// (and the gRPC/proto messages) 1:1 — adapters carry no independent logic, only
// serialization. See specs/003-rest-grpc-api/data-model.md.

type queryRequest struct {
	Query     string  `json:"query"`
	K         int     `json:"k"`
	Mode      string  `json:"mode"`
	NoRerank  bool    `json:"no_rerank"`
	Threshold float64 `json:"threshold"`
}

type queryHit struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Score      float64 `json:"score"`
	Content    string  `json:"content"`
	FilePath   string  `json:"file_path"`
	Page       int     `json:"page"`
}

type queryResponse struct {
	Hits         []queryHit `json:"hits"`
	RerankFailed bool       `json:"rerank_failed"`
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
