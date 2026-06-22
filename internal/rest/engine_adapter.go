package rest

import (
	"encoding/json"
	"net/http"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// handleQuery is the REST projection of engine.Query.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	res, err := s.eng.Query(r.Context(), engine.QueryRequest{
		Query:     req.Query,
		K:         req.K,
		Mode:      req.Mode,
		NoRerank:  req.NoRerank,
		Threshold: req.Threshold,
		RRFK:      req.RRFK,
		Filter:        engine.NewFilter(req.Source, req.Type, req.Tags),
		ContextWindow: req.ContextWindow,
		NoCache:       req.NoCache,
	})
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, queryResponse{Hits: toQueryHits(res.Hits), RerankFailed: res.RerankFailed})
}

// toQueryHits maps engine.QueryHit → the REST DTO, dropping engine-only fields
// (Preview) so the REST payload matches the proto/gRPC projection 1:1 (parity).
func toQueryHits(hits []engine.QueryHit) []queryHit {
	out := make([]queryHit, len(hits))
	for i, h := range hits {
		out[i] = queryHit{
			ChunkID:    h.ChunkID,
			DocumentID: h.DocumentID,
			Score:      h.Score,
			Content:    h.Content,
			FilePath:   h.FilePath,
			Page:       h.Page,
		}
	}
	return out
}

// handleStatus is the REST projection of engine.Status (GET /v1/status).
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	st, err := s.eng.Status()
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Documents:          st.Documents,
		Chunks:             st.Chunks,
		Embeddings:         st.Embeddings,
		Dimensions:         st.Dimensions,
		EmbeddingModel:     st.EmbeddingModel,
		Reranker:           st.Reranker,
		OllamaURL:          st.OllamaURL,
		EmbeddingsComplete: st.EmbeddingsComplete,
	})
}

// handleAdd is the REST projection of engine.Add (POST /v1/add). It ACKs fast
// (async-after-ACK); the response carries the durable-store counts while
// embeddings continue on background workers.
func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	res, err := s.eng.Add(r.Context(), decodePath(w, r))
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIngestSummary(res))
}

// toIngestSummary maps engine.IngestSummary → the REST DTO (parity with proto).
func toIngestSummary(s *engine.IngestSummary) ingestSummary {
	if s == nil {
		return ingestSummary{}
	}
	return ingestSummary{
		New:      s.New,
		Skipped:  s.Skipped,
		Modified: s.Modified,
		Deleted:  s.Deleted,
		Errors:   s.Errors,
	}
}

// decodePath reads a {path} JSON body, writing a 400 on malformed JSON. Shared by
// the add/reprocess handlers (same request shape).
func decodePath(w http.ResponseWriter, r *http.Request) string {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return ""
	}
	return req.Path
}

// handleScan is the REST projection of engine.Scan (POST /v1/scan).
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	res, err := s.eng.Scan(r.Context())
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIngestSummary(res))
}

// handleReprocess is the REST projection of engine.Reprocess (POST /v1/reprocess).
func (s *Server) handleReprocess(w http.ResponseWriter, r *http.Request) {
	res, err := s.eng.Reprocess(r.Context(), decodePath(w, r))
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIngestSummary(res))
}

// handleMigrate is the REST projection of engine.Migrate (POST /v1/migrate).
func (s *Server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	res, err := s.eng.Migrate(r.Context())
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIngestSummary(res))
}

// handleFiles is the REST projection of engine.Files (GET /v1/files).
func (s *Server) handleFiles(w http.ResponseWriter, _ *http.Request) {
	files, err := s.eng.Files()
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	out := make([]fileEntry, len(files))
	for i, f := range files {
		out[i] = fileEntry{FilePath: f.FilePath, FileType: f.FileType, Status: f.Status, ChunkCount: f.ChunkCount}
	}
	writeJSON(w, http.StatusOK, filesResponse{Files: out})
}

// handleDirs is the REST projection of engine.Dirs (GET /v1/dirs).
func (s *Server) handleDirs(w http.ResponseWriter, _ *http.Request) {
	dirs, err := s.eng.Dirs()
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	out := make([]dirEntry, len(dirs))
	for i, d := range dirs {
		out[i] = dirEntry{Dir: d.Dir, Files: d.Files, Chunks: d.Chunks}
	}
	writeJSON(w, http.StatusOK, dirsResponse{Dirs: out})
}

// handleConfigGet is the REST projection of engine.GetConfig (GET /v1/config?key=).
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	vals, err := s.eng.GetConfig(r.URL.Query().Get("key"))
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vals)
}

// handleConfigSet is the REST projection of engine.SetConfig (PUT /v1/config).
func (s *Server) handleConfigSet(w http.ResponseWriter, r *http.Request) {
	var req configPutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.eng.SetConfig(req.Key, req.Value); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{req.Key: req.Value})
}

// handleVaults is the REST projection of engine.ListVaults (GET /v1/vaults).
func (s *Server) handleVaults(w http.ResponseWriter, _ *http.Request) {
	vaults, err := s.eng.ListVaults()
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	out := make([]vaultEntry, len(vaults))
	for i, v := range vaults {
		out[i] = vaultEntry{Name: v.Name, Documents: v.Documents}
	}
	writeJSON(w, http.StatusOK, vaultsResponse{Vaults: out})
}
