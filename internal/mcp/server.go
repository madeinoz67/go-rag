// Package mcp exposes go-rag operations as Model Context Protocol tools over stdio
// JSON-RPC (PRD G7, Principle V — every CLI op is also an agent tool). All six
// operations are exposed: query, status, add, init, scan, config.
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

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/rerank"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/watcher"
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
// creates the DB); the rest require an existing database. In daemon mode the
// shared DB is reused; in stdio mode it is opened (and closed) per call.
func (s *Server) dispatch(name string, args map[string]any) (string, error) {
	if name == "go_rag_init" {
		return s.initTool(args)
	}
	if s.db != nil {
		return s.dispatchDB(s.cfg, s.db, name, args)
	}
	cfg, db, err := openDB(s.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	return s.dispatchDB(cfg, db, name, args)
}

func (s *Server) dispatchDB(cfg config.Config, db *storage.DB, name string, args map[string]any) (string, error) {
	switch name {
	case "go_rag_query":
		return s.query(cfg, db, args)
	case "go_rag_status":
		return s.status(db)
	case "go_rag_add":
		return s.add(cfg, db, args)
	case "go_rag_scan":
		return s.scan(cfg, db)
	case "go_rag_config":
		return s.configTool(cfg, args)
	case "go_rag_files":
		return s.files(db)
	case "go_rag_dirs":
		return s.dirs(db)
	case "go_rag_reprocess":
		return s.reprocess(cfg, db, args)
	case "go_rag_migrate":
		return s.migrate(cfg, db)
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}

func (s *Server) query(cfg config.Config, db *storage.DB, args map[string]any) (string, error) {
	q, _ := args["query"].(string)
	k := 5
	if v, ok := args["k"].(float64); ok {
		k = int(v)
	}
	mode := "hybrid"
	if v, ok := args["mode"].(string); ok {
		mode = v
	}
	fts, vec, err := pipeline.LoadIndex(db)
	if err != nil {
		return "", err
	}
	em := embed.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	r := index.NewRetrieval(fts, vec, em.Embed)
	var reranker index.Reranker
	if cfg.RerankModel != "" {
		reranker = rerank.New(cfg.OllamaURL, cfg.RerankModel)
	}
	hits, err := r.SearchWithRerank(context.Background(), q, k, index.ParseMode(mode), docOf(db), reranker, func(id string) string {
		c, ok := lookupChunk(db, id)
		if !ok {
			return ""
		}
		return c.Content
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, h := range hits {
		c, ok := lookupChunk(db, h.ChunkID)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "- (score %.3f) %s\n", h.Score, preview(c.Content, 160))
	}
	if b.Len() == 0 {
		return "no results", nil
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Server) status(db *storage.DB) (string, error) {
	return fmt.Sprintf("documents: %d, chunks: %d, embeddings: %d",
		countPrefix(db, storage.PrefixDocument),
		countPrefix(db, storage.PrefixChunk),
		countPrefix(db, storage.PrefixEmbedding)), nil
}

func (s *Server) add(cfg config.Config, db *storage.DB, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	em := embed.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	p := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector())
	defer p.Close()
	res, err := p.Ingest(context.Background(), path, "*")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("new=%d skipped=%d errors=%d", res.New, res.Skipped, res.Errors), nil
}

func (s *Server) initTool(args map[string]any) (string, error) {
	cfg := config.Default()
	cfg.DBPath = s.dbPath
	if v, ok := args["ollama_url"].(string); ok && v != "" {
		cfg.OllamaURL = v
	}
	if v, ok := args["model"].(string); ok && v != "" {
		cfg.OllamaModel = v
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
	if cfg.OllamaModel == "" {
		cfg.OllamaModel = "nomic-embed-text"
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
	return fmt.Sprintf("initialized go-rag at %s (model %s, url %s)", cfg.DBPath, cfg.OllamaModel, cfg.OllamaURL), nil
}

func (s *Server) scan(cfg config.Config, db *storage.DB) (string, error) {
	root := "."
	if len(cfg.WatchDirs) > 0 && cfg.WatchDirs[0] != "" {
		root = cfg.WatchDirs[0]
	}
	em := embed.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	pl := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector())
	defer pl.Close()
	cd := watcher.New(db, pl)
	changes, err := cd.ScanOnce(context.Background(), root, "*")
	if err != nil {
		return "", err
	}
	added, modified, deleted := 0, 0, 0
	for _, c := range changes {
		switch c.Kind {
		case "NEW":
			added++
		case "MODIFIED":
			modified++
		case "DELETED":
			deleted++
		}
	}
	return fmt.Sprintf("added=%d modified=%d deleted=%d", added, modified, deleted), nil
}

func (s *Server) configTool(cfg config.Config, args map[string]any) (string, error) {
	action, _ := args["action"].(string)
	path := filepath.Join(s.dbPath, "config.json")
	switch action {
	case "set":
		key, _ := args["key"].(string)
		val, _ := args["value"].(string)
		if err := cfg.Set(key, val); err != nil {
			return "", err
		}
		if err := cfg.Validate(); err != nil {
			return "", err
		}
		if err := config.Save(path, cfg); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s=%s (saved)", key, val), nil
	default: // "get"
		if key, ok := args["key"].(string); ok && key != "" {
			v, ok := cfg.Get(key)
			if !ok {
				return "", fmt.Errorf("unknown key: %s", key)
			}
			return fmt.Sprintf("%s=%s", key, v), nil
		}
		var b strings.Builder
		for _, k := range []string{"ollama_url", "ollama_model", "chunk_size", "chunk_overlap", "db_path", "poll_interval_secs"} {
			if v, ok := cfg.Get(k); ok {
				fmt.Fprintf(&b, "%s=%s\n", k, v)
			}
		}
		return strings.TrimSpace(b.String()), nil
	}
}

// files lists the paths of every ingested document.
func (s *Server) files(db *storage.DB) (string, error) {
	var lines []string
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			lines = append(lines, fmt.Sprintf("%s (%s, %s, %d chunks)", d.FilePath, d.FileType, d.Status, d.ChunkCount))
		}
		return true
	})
	sort.Strings(lines)
	if len(lines) == 0 {
		return "no files ingested", nil
	}
	return strings.Join(lines, "\n"), nil
}

// dirs groups ingested documents by directory, returning file/chunk counts per dir.
func (s *Server) dirs(db *storage.DB) (string, error) {
	type counts struct{ files, chunks int }
	m := map[string]*counts{}
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			dir := filepath.Dir(d.FilePath)
			e := m[dir]
			if e == nil {
				e = &counts{}
				m[dir] = e
			}
			e.files++
			e.chunks += d.ChunkCount
		}
		return true
	})
	if len(m) == 0 {
		return "no files ingested", nil
	}
	dirs := make([]string, 0, len(m))
	for d := range m {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	var b strings.Builder
	for _, d := range dirs {
		fmt.Fprintf(&b, "%s (%d files, %d chunks)\n", d, m[d].files, m[d].chunks)
	}
	return strings.TrimSpace(b.String()), nil
}

// reprocess force-reingests a path via the pipeline (T047).
func (s *Server) reprocess(cfg config.Config, db *storage.DB, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	em := embed.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	p := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector())
	defer p.Close()
	res, err := p.Reprocess(context.Background(), path, "*")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("reprocessed=%d errors=%d", res.New, res.Errors), nil
}

// migrate re-embeds documents whose embeddings use a different model than the
// configured one (T048).
func (s *Server) migrate(cfg config.Config, db *storage.DB) (string, error) {
	current := cfg.OllamaModel
	stats := pipeline.EmbeddingModelStats(db)
	if len(stats) == 0 {
		return "no tracked embeddings yet", nil
	}
	stale := 0
	for m, n := range stats {
		if m != current {
			stale += n
		}
	}
	if stale == 0 {
		return fmt.Sprintf("up to date: all embeddings use %s", current), nil
	}
	em := embed.NewOllama(cfg.OllamaURL, current)
	p := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector())
	defer p.Close()
	res, err := p.ReprocessAll(context.Background())
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("migrated=%d files re-embedded to %s (%d errors)", res.New, current, res.Errors), nil
}

// --- JSON-RPC helpers ---

func ok(id any, result any) any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func errResp(id any, code int, msg string) any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

// --- db helpers (minimal, mirroring cli/wire.go) ---

func openDB(base string) (config.Config, *storage.DB, error) {
	cfg, err := config.Load(filepath.Join(base, "config.json"))
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("no database — run `go-rag init` or go_rag_init first: %w", err)
	}
	db, err := storage.Open(filepath.Join(base, "data"))
	return cfg, db, err
}

func countPrefix(db *storage.DB, prefix byte) int {
	n := 0
	_ = db.PrefixScanByte(prefix, func(_, _ []byte) bool { n++; return true })
	return n
}

func docOf(db *storage.DB) func(string) string {
	m := map[string]string{}
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			m[c.ID] = c.DocumentID
		}
		return true
	})
	return func(id string) string { return m[id] }
}

func lookupChunk(db *storage.DB, id string) (model.Chunk, bool) {
	raw, ok, _ := db.GetWithPrefix(storage.PrefixChunk, []byte(id))
	if !ok {
		return model.Chunk{}, false
	}
	var c model.Chunk
	if json.Unmarshal(raw, &c) != nil {
		return model.Chunk{}, false
	}
	return c, true
}

func preview(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "go_rag_query",
			"description": "Hybrid (semantic + keyword) search over the local document database.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"k":     map[string]any{"type": "integer", "default": 5},
					"mode":  map[string]any{"type": "string", "enum": []string{"hybrid", "semantic", "keyword"}},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "go_rag_status",
			"description": "Report document/chunk/embedding counts in the database.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_add",
			"description": "Ingest a file or directory path into the database.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required": []string{"path"},
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
				"type": "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required": []string{"path"},
			},
		},
		{
			"name":        "go_rag_migrate",
			"description": "Re-embed all documents whose embeddings use a different model than the current one.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}
