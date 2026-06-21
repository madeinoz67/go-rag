package engine

import (
	"fmt"
	"sync"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// Engine is the unified operation surface. It holds an open database and its
// config and exposes one method per go-rag operation. Every transport adapter
// (CLI, MCP, REST, gRPC) constructs an Engine and calls these methods — they
// are the single source of truth for each operation, which is what makes
// cross-transport results identical.
//
// Write operations (Add/Scan/Reprocess/Migrate) share one long-lived ingest
// pipeline, created on first use. Because that pipeline is not closed per
// operation, writes ACK as soon as the durable store commit completes and
// embedding/indexing continues on background workers (async-after-ACK,
// Principle IV). Call Close to drain pending work before discarding the engine
// or closing its database.
type Engine struct {
	cfg config.Config
	db  *storage.DB

	// embedder, when non-nil, overrides the default Ollama embedder for both
	// ingest (the async pipeline) and query. It is used by the evaluation harness
	// to drive the canonical query path offline with a deterministic embedder
	// (spec 004 / FR-007). Every existing caller leaves it nil and gets the
	// unchanged Ollama behavior via embedderOrOllama().
	embedder embed.Embedder

	pipeMu sync.Mutex
	pipe   *pipeline.Pipeline
}

// NewWithDB returns an Engine over a pre-opened database (daemon mode). The
// caller owns the database's lifetime — Engine does not close it. The ingest
// pipeline is created lazily on the first write, so read-only engines (query,
// status, files) never start background workers.
func NewWithDB(cfg config.Config, db *storage.DB) *Engine {
	return &Engine{cfg: cfg, db: db}
}

// NewWithEmbedder returns an Engine that uses em as its embedder for both ingest
// and query, instead of the configured Ollama endpoint. This is the injection
// point the evaluation harness uses to run the real engine.Query path offline
// and reproducibly (spec 004). Production callers use NewWithDB, which leaves
// the embedder nil and falls back to Ollama — so this changes nothing for them.
func NewWithEmbedder(cfg config.Config, db *storage.DB, em embed.Embedder) *Engine {
	return &Engine{cfg: cfg, db: db, embedder: em}
}

// embedderOrOllama returns the injected embedder when one is present, otherwise
// the Ollama embedder derived from config (the historical behavior). Centralizing
// this keeps the query and ingest paths on a single embedder.
func (e *Engine) embedderOrOllama() embed.Embedder {
	if e.embedder != nil {
		return e.embedder
	}
	return embed.NewOllama(e.cfg.OllamaURL, e.cfg.EmbeddingModel)
}

// Config returns the engine's loaded configuration (read-only snapshot).
func (e *Engine) Config() config.Config { return e.cfg }

// DB returns the underlying storage handle (used by adapters that need direct
// access, e.g. for prefix scans not yet wrapped here).
func (e *Engine) DB() *storage.DB { return e.db }

// pipeline returns the engine's long-lived ingest pipeline, creating it on first
// use (concurrency-safe). It is intentionally NOT closed per write — that is what
// makes writes ACK before embeddings finish (async-after-ACK).
func (e *Engine) pipeline() (*pipeline.Pipeline, error) {
	e.pipeMu.Lock()
	defer e.pipeMu.Unlock()
	if e.pipe != nil {
		return e.pipe, nil
	}
	if e.cfg.EmbeddingModel == "" {
		return nil, fmt.Errorf("no embedding model configured")
	}
	e.pipe = pipeline.New(
		e.db,
		chunk.NewSplitter(e.cfg.ChunkSize, e.cfg.ChunkOverlap),
		e.embedderOrOllama(),
		index.NewFTS(),
		index.NewVector(),
		e.cfg.Prefixer(), // H07: document-role instruction prefixes
	)
	return e.pipe, nil
}

// Close drains the ingest pipeline's background workers (pending embeddings and
// indexing). Safe to call on engines that never wrote (no-op) and idempotent.
// Long-lived daemons must call this before closing the underlying database so
// in-flight async writes complete; short-lived (per-request) engines call it to
// avoid leaking worker goroutines.
func (e *Engine) Close() {
	e.pipeMu.Lock()
	defer e.pipeMu.Unlock()
	if e.pipe != nil {
		e.pipe.Close()
		e.pipe = nil
	}
}
