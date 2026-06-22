// Package mcp exposes go-rag operations as Model Context Protocol tools over stdio
// JSON-RPC (PRD G7, Principle V — every CLI op is also an agent tool). All
// operations are thin renderings of the shared internal/engine facade, so MCP
// returns identical results to the CLI, REST, and gRPC transports.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/eval"
	"github.com/madeinoz67/go-rag/internal/storage"
)

const protocolVersion = "2024-11-05"

// Server is an MCP server backed by a go-rag database. It can run over stdio
// (New, opens the DB per call) or as a long-lived daemon (NewWithDB, shared DB).
type Server struct {
	dbPath string
	db     *storage.DB // nil => open per call (stdio); non-nil => shared (daemon)
	cfg    config.Config
}

// New returns an MCP server that opens the database per call (stdio mode).
func New(dbPath string) *Server { return &Server{dbPath: dbPath} }

// NewWithDB returns an MCP server backed by a pre-opened database (daemon mode).
// The caller owns the database's lifetime; it is NOT closed per call.
func NewWithDB(dbPath string, db *storage.DB, cfg config.Config) *Server {
	return &Server{dbPath: dbPath, db: db, cfg: cfg}
}

type rpcReq struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// Serve reads JSON-RPC messages from in and writes responses to out until in closes.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var req rpcReq
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if resp := s.handle(req); resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
}

func (s *Server) handle(req rpcReq) any {
	switch req.Method {
	case "initialize":
		return ok(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "go-rag", "version": "0.1.0"},
		})
	case "notifications/initialized":
		return nil
	case "tools/list":
		return ok(req.ID, map[string]any{"tools": toolDefs()})
	case "tools/call":
		return s.callTool(req)
	}
	return errResp(req.ID, -32601, "method not found: "+req.Method)
}

func (s *Server) callTool(req rpcReq) any {
	name, _ := req.Params["name"].(string)
	args, _ := req.Params["arguments"].(map[string]any)
	out, err := s.dispatch(name, args)
	if err != nil {
		return errResp(req.ID, -32603, err.Error())
	}
	return ok(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": out}},
	})
}

// dispatch routes a tool call. go_rag_init is handled before opening the DB (it
// creates the DB); go_rag_vault_list and go_rag_guide are handled without a
// specific vault's DB. The rest require an existing database and are rendered
// from the shared engine facade. In daemon mode the shared DB is reused; in
// stdio mode it is opened (and closed) per call.
func (s *Server) dispatch(name string, args map[string]any) (string, error) {
	switch name {
	case "go_rag_init":
		return s.initTool(args)
	case "go_rag_vault_list":
		return s.renderVaults()
	case "go_rag_guide":
		return s.guide()
	case "go_rag_eval":
		// Self-provisions a throwaway vault from the golden corpus; does not need
		// (and does not touch) the caller's database.
		return s.renderEval(nil, args)
	}
	if s.db != nil {
		return s.dispatchDB(engine.NewWithDB(s.cfg, s.db), name, args)
	}
	cfg, db, err := engine.Open(s.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	return s.dispatchDB(engine.NewWithDB(cfg, db), name, args)
}

func (s *Server) dispatchDB(eng *engine.Engine, name string, args map[string]any) (string, error) {
	// The engine's ingest pipeline is created lazily on write and drained
	// async-after-ACK; close it here so short-lived per-dispatch engines finish
	// their background embeddings before the MCP response returns (and don't
	// leak worker goroutines). No-op for read-only engines.
	defer eng.Close()
	switch name {
	case "go_rag_query":
		return s.renderQuery(eng, args)
	case "go_rag_status":
		return s.renderStatus(eng)
	case "go_rag_add":
		return s.renderAdd(eng, args)
	case "go_rag_scan":
		return s.renderScan(eng)
	case "go_rag_config":
		return s.renderConfig(eng, args)
	case "go_rag_files":
		return s.renderFiles(eng)
	case "go_rag_dirs":
		return s.renderDirs(eng)
	case "go_rag_reprocess":
		return s.renderReprocess(eng, args)
	case "go_rag_migrate":
		return s.renderMigrate(eng)
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}

// renderEval measures retrieval quality over a golden dataset. It is read-only
// with respect to any real vault: it self-provisions a throwaway vault from the
// (default committed) golden corpus with a deterministic offline embedder, so the
// result is reproducible and needs no Ollama. Numbers are identical to the
// `go-rag eval` CLI because both drive the same engine.Query path (Principle V).
func (s *Server) renderEval(_ *engine.Engine, args map[string]any) (string, error) {
	goldenPath, _ := args["golden"].(string)
	if goldenPath == "" {
		goldenPath = "testdata/golden/v1.jsonl"
	}
	corpus, _ := args["corpus"].(string)
	if corpus == "" {
		corpus = "testdata/golden/corpus/"
	}
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "hybrid"
	}
	k := 10
	if v, ok := args["k"].(float64); ok && v > 0 {
		k = int(v)
	}

	golden, err := eval.LoadGolden(goldenPath)
	if err != nil {
		return "", err
	}
	em := eval.NewDeterministicEmbedder()
	cfg, db, cleanup, err := eval.ProvisionCorpus(context.Background(), corpus, em, "")
	if err != nil {
		return "", err
	}
	defer cleanup()
	run, err := eval.NewEvalRunner(cfg, db, em).Run(context.Background(), golden, mode, k, true)
	if err != nil {
		return "", err
	}
	return eval.FormatRun(run, nil, 0), nil
}

func (s *Server) renderQuery(eng *engine.Engine, args map[string]any) (string, error) {
	req := engine.QueryRequest{Mode: "hybrid", K: 5}
	req.Query, _ = args["query"].(string)
	if v, ok := args["k"].(float64); ok {
		req.K = int(v)
	}
	if v, ok := args["mode"].(string); ok {
		req.Mode = v
	}
	if v, ok := args["no_rerank"].(bool); ok {
		req.NoRerank = v
	}
	if v, ok := args["threshold"].(float64); ok {
		req.Threshold = v
	}
	if v, ok := args["rrf_k"].(float64); ok && v > 0 { // H08/spec 009: per-query RRF override (>0); 0 = config/default
		req.RRFK = int(v)
	}
	res, err := eng.Query(context.Background(), req)
	if err != nil {
		return "", err
	}
	if len(res.Hits) == 0 {
		return "no results", nil
	}
	var b strings.Builder
	if res.RerankFailed { // H09: reranking was attempted but failed — results are fallback-ordered.
		b.WriteString("⚠ reranking failed; showing fallback-ordered results (reranker may be down or mismatched)\n\n")
	}
	for _, h := range res.Hits {
		fmt.Fprintf(&b, "- (score %.3f) %s\n", h.Score, h.Preview)
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Server) renderStatus(eng *engine.Engine) (string, error) {
	st, err := eng.Status()
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("documents: %d, chunks: %d, embeddings: %d, dimensions: %d, model: %s, reranker: %s",
		st.Documents, st.Chunks, st.Embeddings, st.Dimensions, st.EmbeddingModel, st.Reranker)
	if st.EmbeddingDrift {
		out += fmt.Sprintf(", drift: mixed models/dims (%v)", st.ModelCounts)
	}
	return out, nil
}

func (s *Server) renderAdd(eng *engine.Engine, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	res, err := eng.Add(context.Background(), path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("new=%d skipped=%d errors=%d", res.New, res.Skipped, res.Errors), nil
}

func (s *Server) renderScan(eng *engine.Engine) (string, error) {
	res, err := eng.Scan(context.Background())
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("added=%d modified=%d deleted=%d", res.New, res.Modified, res.Deleted), nil
}

func (s *Server) renderReprocess(eng *engine.Engine, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	res, err := eng.Reprocess(context.Background(), path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("reprocessed=%d errors=%d", res.New, res.Errors), nil
}

func (s *Server) renderMigrate(eng *engine.Engine) (string, error) {
	res, err := eng.Migrate(context.Background())
	if err != nil {
		return "", err
	}
	if res.New == 0 && res.Errors == 0 {
		return fmt.Sprintf("up to date: all embeddings use %s", eng.Config().EmbeddingModel), nil
	}
	return fmt.Sprintf("migrated=%d files re-embedded to %s (%d errors)", res.New, eng.Config().EmbeddingModel, res.Errors), nil
}

func (s *Server) renderConfig(eng *engine.Engine, args map[string]any) (string, error) {
	action, _ := args["action"].(string)
	if action == "set" {
		key, _ := args["key"].(string)
		val, _ := args["value"].(string)
		if err := eng.SetConfig(key, val); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s=%s (saved)", key, val), nil
	}
	if key, ok := args["key"].(string); ok && key != "" {
		vals, err := eng.GetConfig(key)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s=%s", key, vals[key]), nil
	}
	vals, err := eng.GetConfig("")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, k := range []string{"ollama_url", "embedding_model", "chunk_size", "chunk_overlap", "db_path", "poll_interval_secs"} {
		if v, ok := vals[k]; ok {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Server) renderFiles(eng *engine.Engine) (string, error) {
	files, err := eng.Files()
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "no files ingested", nil
	}
	lines := make([]string, 0, len(files))
	for _, f := range files {
		lines = append(lines, fmt.Sprintf("%s (%s, %s, %d chunks)", f.FilePath, f.FileType, f.Status, f.ChunkCount))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

func (s *Server) renderDirs(eng *engine.Engine) (string, error) {
	dirs, err := eng.Dirs()
	if err != nil {
		return "", err
	}
	if len(dirs) == 0 {
		return "no files ingested", nil
	}
	var b strings.Builder
	for _, d := range dirs {
		fmt.Fprintf(&b, "%s (%d files, %d chunks)\n", d.Dir, d.Files, d.Chunks)
	}
	return strings.TrimSpace(b.String()), nil
}

// renderVaults lists all vaults with doc counts. No specific vault's DB required.
func (s *Server) renderVaults() (string, error) {
	vaults, err := engine.NewWithDB(config.Config{}, nil).ListVaults()
	if err != nil {
		return "", err
	}
	if len(vaults) == 0 {
		return "no vaults", nil
	}
	var b strings.Builder
	for _, v := range vaults {
		fmt.Fprintf(&b, "%s (%d docs)\n", v.Name, v.Documents)
	}
	return strings.TrimSpace(b.String()), nil
}

// guide returns a context document for the AI agent — what's connected, what's
// available, what's needed. The agent should call this first.
func (s *Server) guide() (string, error) {
	cfg, db, err := engine.Open(s.dbPath)
	dbReady := err == nil

	var b strings.Builder
	b.WriteString("# go-rag Agent Guide\n\n## Status\n\n")
	if !dbReady {
		b.WriteString("**Database not initialized.** Call `go_rag_init` first with an embedding model name, then `go_rag_add` to ingest documents.\n\n## Available Tools\n\n")
		b.WriteString("- **go_rag_init** — Initialize a new database (requires: model name, e.g. `mxbai-embed-large`)\n")
		b.WriteString("- **go_rag_vault_list** — List all available vaults\n")
		b.WriteString("- **go_rag_guide** — This guide (call it after setup changes)\n")
		return b.String(), nil
	}
	defer db.Close()

	eng := engine.NewWithDB(cfg, db)
	st, _ := eng.Status()
	pct := 0
	if st.Documents > 0 {
		pct = st.Embeddings * 100 / st.Documents
	}
	reranker := st.Reranker

	fmt.Fprintf(&b, "- Documents: %d\n", st.Documents)
	fmt.Fprintf(&b, "- Chunks: %d\n", st.Chunks)
	fmt.Fprintf(&b, "- Embeddings: %d (%d%% complete)\n", st.Embeddings, pct)
	fmt.Fprintf(&b, "- Embedding model: %s\n", st.EmbeddingModel)
	fmt.Fprintf(&b, "- Reranker: %s\n", reranker)
	fmt.Fprintf(&b, "- Chunk size: %d tokens, overlap: %d\n", cfg.ChunkSize, cfg.ChunkOverlap)
	fmt.Fprintf(&b, "- Ollama: %s\n\n", st.OllamaURL)

	b.WriteString("## What's Needed\n\n")
	if st.Documents == 0 {
		b.WriteString("**No documents ingested.** Call `go_rag_add` with a directory path to index documents.\n\n")
	}
	if pct < 100 && st.Documents > 0 {
		b.WriteString(fmt.Sprintf("**Embeddings incomplete (%d%%).** Background embedding may still be running, or errors occurred. Query results will be partial.\n\n", pct))
	}
	if reranker == "disabled" {
		b.WriteString("**Reranker disabled.** Set `rerank_model` via `go_rag_config` to enable cross-encoder reranking for better query precision.\n\n")
	}
	if st.Documents > 0 && pct == 100 && reranker != "disabled" {
		b.WriteString("System is fully operational — all documents indexed and embeddings complete.\n\n")
	}

	b.WriteString("## Available Tools\n\n")
	b.WriteString("- **go_rag_query** — Search the database (hybrid semantic + keyword). Params: `query` (required), `k` (results, default 5), `mode` (hybrid|semantic|keyword), `no_rerank` (skip reranker), `threshold` (min score), `rrf_k` (RRF constant override, default 60).\n")
	b.WriteString("- **go_rag_add** — Ingest documents from a file or directory path.\n")
	b.WriteString("- **go_rag_status** — Database health and counts.\n")
	b.WriteString("- **go_rag_files** — List ingested file paths.\n")
	b.WriteString("- **go_rag_dirs** — Per-directory document counts.\n")
	b.WriteString("- **go_rag_scan** — Detect and apply filesystem changes (added/modified/deleted).\n")
	b.WriteString("- **go_rag_reprocess** — Force re-ingest a directory (after reader/config changes).\n")
	b.WriteString("- **go_rag_migrate** — Re-embed all documents to the current model.\n")
	b.WriteString("- **go_rag_config** — Get or set configuration.\n")
	b.WriteString("- **go_rag_init** — Initialize a new database.\n")
	b.WriteString("- **go_rag_vault_list** — List all vaults.\n")
	b.WriteString("- **go_rag_guide** — This guide.\n")
	b.WriteString("- **go_rag_eval** — Measure retrieval quality (recall@k, precision@k, MRR, NDCG@k) over a golden dataset (offline, reproducible).\n\n")

	b.WriteString("## Usage Patterns\n\n")
	b.WriteString("1. **Query**: `go_rag_query(query=\"how does authentication work?\")` — returns ranked chunks with source files.\n")
	b.WriteString("2. **Add documents**: `go_rag_add(path=\"/path/to/docs/\")` — ingests recursively.\n")
	b.WriteString("3. **After adding**: Wait for embeddings to complete (check `go_rag_status` for embedded %).\n")
	b.WriteString("4. **Quick search** (no reranker): `go_rag_query(query=\"...\", no_rerank=true)` — faster, less precise.\n")
	return b.String(), nil
}

func (s *Server) initTool(args map[string]any) (string, error) {
	cfg := config.Default()
	cfg.DBPath = s.dbPath
	if v, ok := args["ollama_url"].(string); ok && v != "" {
		cfg.OllamaURL = v
	}
	if v, ok := args["model"].(string); ok && v != "" {
		cfg.EmbeddingModel = v
	}
	if v, ok := args["watch_dir"].(string); ok && v != "" {
		cfg.WatchDirs = []string{v}
	}
	if v, ok := args["chunk_size"].(float64); ok && v > 0 {
		cfg.ChunkSize = int(v)
	}
	if v, ok := args["chunk_overlap"].(float64); ok && v >= 0 {
		cfg.ChunkOverlap = int(v)
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "nomic-embed-text"
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(cfg.DBPath, "data"), 0o755); err != nil {
		return "", err
	}
	if err := config.Save(filepath.Join(cfg.DBPath, "config.json"), cfg); err != nil {
		return "", err
	}
	return fmt.Sprintf("initialized go-rag at %s (model %s, url %s)", cfg.DBPath, cfg.EmbeddingModel, cfg.OllamaURL), nil
}

// --- JSON-RPC helpers ---

func ok(id any, result any) any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func errResp(id any, code int, msg string) any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "go_rag_query",
			"description": "Hybrid (semantic + keyword) search over the local document database.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string"},
					"k":         map[string]any{"type": "integer", "default": 5},
					"mode":      map[string]any{"type": "string", "enum": []string{"hybrid", "semantic", "keyword"}},
					"no_rerank": map[string]any{"type": "boolean", "default": false},
					"threshold": map[string]any{"type": "number", "default": 0.0},
					"rrf_k":     map[string]any{"type": "integer", "default": 60},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "go_rag_status",
			"description": "Report document/chunk/embedding counts, model, dimensions, and reranker status.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_add",
			"description": "Ingest a file or directory path into the database.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			},
		},
		{
			"name":        "go_rag_init",
			"description": "Initialize a new go-rag database (creates config + data directory).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ollama_url":    map[string]any{"type": "string"},
					"model":         map[string]any{"type": "string"},
					"watch_dir":     map[string]any{"type": "string"},
					"chunk_size":    map[string]any{"type": "integer"},
					"chunk_overlap": map[string]any{"type": "integer"},
				},
			},
		},
		{
			"name":        "go_rag_scan",
			"description": "Scan watched directories once for added/modified/deleted files and apply changes.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_config",
			"description": "Get or set go-rag configuration values (action: get|set).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "enum": []string{"get", "set"}},
					"key":    map[string]any{"type": "string"},
					"value":  map[string]any{"type": "string"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "go_rag_files",
			"description": "List the file paths of every ingested document.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_dirs",
			"description": "List ingested directories with file and chunk counts.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_reprocess",
			"description": "Force re-ingest of a directory (applies the current reader/embedder; bypasses dedup).",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			},
		},
		{
			"name":        "go_rag_migrate",
			"description": "Re-embed all documents whose embeddings use a different model than the current one.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_vault_list",
			"description": "List all available document vaults with doc counts.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_guide",
			"description": "Get a guide for the AI: system status, what's needed, available tools, and usage patterns. Call this first to understand the current state.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_eval",
			"description": "Measure retrieval quality (recall@k, precision@k, MRR, NDCG@k) over a golden dataset. Self-provisions a throwaway vault from the golden corpus with a deterministic offline embedder (no Ollama, reproducible).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"golden": map[string]any{"type": "string", "description": "Path to golden JSONL (default testdata/golden/v1.jsonl)."},
					"corpus": map[string]any{"type": "string", "description": "Source corpus dir (default testdata/golden/corpus/)."},
					"mode":   map[string]any{"type": "string", "enum": []string{"hybrid", "semantic", "keyword"}, "default": "hybrid"},
					"k":      map[string]any{"type": "integer", "default": 10},
				},
			},
		},
	}
}
