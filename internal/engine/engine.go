package engine

import (
	"fmt"
	"sync"
	"sync/atomic"

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

	// idxFts/idxVec are the engine's shared in-memory search index (audit H01 /
	// spec 011): seeded once from LoadIndex and reused by every query — no
	// per-query rebuild (the single biggest latency win). The pipeline, watcher,
	// and migrate mutate this same pair in place, so it stays live and current;
	// FTS and Vector are each goroutine-safe, so concurrent query reads +
	// background writes need no Engine-level read/write lock. idxMu guards only
	// the seed-once; reads of the stable pointers are lock-free thereafter.
	idxMu  sync.Mutex
	idxFts *index.FTS
	idxVec *index.Vector

	// qTransformer is the query-transformation seam (audit H05/spec 012): it
	// normalizes (default) or otherwise alters the query before retrieval, applied
	// once at the top of Query so every transport/mode benefits. Default is the
	// pure NormalizingTransformer; a custom one can be set (tests today; future
	// HyDE/multi-query in an adapter) — internal/index stays Ollama-free.
	qTransformer index.QueryTransformer

	// Query caches (audit H06/spec 016): an exact-match result cache and a
	// query-embedding cache, both in-process, bounded, and empty on restart.
	// resultCache maps the full query shape + index epoch → *QueryResult;
	// embedCache maps the embedding profile + prefixed query → its vector.
	// Disabled (nil or capacity 0) = every Get misses, every Put no-ops. epoch
	// is the invalidation counter bumped by the pipeline's OnChange callback at
	// every shared-index mutation (including the async vector-add).
	resultCache *LRU[string, *QueryResult]
	embedCache  *LRU[string, []float32]
	epoch       *atomic.Uint64
}

// newQueryCaches builds the result/embedding caches and epoch from config. When
// QueryCacheEnabled is false both caches are disabled (capacity 0); a per-cache
// capacity of 0 disables just that cache. The epoch is always allocated so
// markIndexChanged works even when caching is off (harmless; it just bumps a
// counter nothing reads).
func newQueryCaches(cfg config.Config) (*LRU[string, *QueryResult], *LRU[string, []float32], *atomic.Uint64) {
	resCap, embCap := cfg.QueryCacheResults, cfg.QueryCacheEmbeddings
	if !cfg.QueryCacheEnabled {
		resCap, embCap = 0, 0
	}
	return NewLRU[string, *QueryResult](resCap), NewLRU[string, []float32](embCap), &atomic.Uint64{}
}

// NewWithDB returns an Engine over a pre-opened database (daemon mode). The
// caller owns the database's lifetime — Engine does not close it. The ingest
// pipeline is created lazily on the first write, so read-only engines (query,
// status, files) never start background workers.
func NewWithDB(cfg config.Config, db *storage.DB) *Engine {
	rc, ec, ep := newQueryCaches(cfg)
	return &Engine{cfg: cfg, db: db, qTransformer: index.NormalizingTransformer{}, resultCache: rc, embedCache: ec, epoch: ep}
}

// NewWithEmbedder returns an Engine that uses em as its embedder for both ingest
// and query, instead of the configured Ollama endpoint. This is the injection
// point the evaluation harness uses to run the real engine.Query path offline
// and reproducibly (spec 004). Production callers use NewWithDB, which leaves
// the embedder nil and falls back to Ollama — so this changes nothing for them.
func NewWithEmbedder(cfg config.Config, db *storage.DB, em embed.Embedder) *Engine {
	rc, ec, ep := newQueryCaches(cfg)
	return &Engine{cfg: cfg, db: db, embedder: em, qTransformer: index.NormalizingTransformer{}, resultCache: rc, embedCache: ec, epoch: ep}
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

// indexes returns the engine's shared in-memory search index (FTS + Vector),
// seeding it once from the persisted corpus via LoadIndex on first access and
// reusing it on every later call (audit H01/spec 011 — no per-query rebuild).
// Both Query and the ingest pipeline use the pair returned here, so writes
// (processJob, DeleteDoc) flow straight into the same indexes queries read.
// Lock ordering: pipeline() acquires pipeMu then idxMu (via this method); Query
// acquires only idxMu — no inversion, and indexes() never reaches back to pipeMu.
func (e *Engine) indexes() (*index.FTS, *index.Vector, error) {
	e.idxMu.Lock()
	defer e.idxMu.Unlock()
	if e.idxFts == nil || e.idxVec == nil {
		fts, vec, err := pipeline.LoadIndex(e.db)
		if err != nil {
			return nil, nil, err
		}
		e.idxFts, e.idxVec = fts, vec
	}
	return e.idxFts, e.idxVec, nil
}

// pipeline returns the engine's long-lived ingest pipeline, creating it on first
// use (concurrency-safe). It is intentionally NOT closed per write — that is what
// makes writes ACK before embeddings finish (async-after-ACK). The pipeline
// shares the engine's seeded index (audit H01/spec 011) so ingest/watcher/migrate
// mutate the same FTS/Vector that queries read.
func (e *Engine) pipeline() (*pipeline.Pipeline, error) {
	e.pipeMu.Lock()
	defer e.pipeMu.Unlock()
	if e.pipe != nil {
		return e.pipe, nil
	}
	if e.cfg.EmbeddingModel == "" {
		return nil, fmt.Errorf("no embedding model configured")
	}
	fts, vec, err := e.indexes() // H01: share the seeded index, not fresh empties.
	if err != nil {
		return nil, err
	}
	e.pipe = pipeline.New(
		e.db,
		chunk.NewSplitter(e.cfg.ChunkSize, e.cfg.ChunkOverlap),
		e.embedderOrOllama(),
		fts, vec,
		e.cfg.Prefixer(), // H07: document-role instruction prefixes
	)
	// H06/spec 016: the pipeline signals every shared-index mutation via this
	// callback so the engine can advance the result-cache epoch. Set under
	// pipeMu before any job flows (workers start in New but only receive jobs
	// once Ingest runs, which is after this returns), so no bump is missed.
	e.pipe.OnChange = e.markIndexChanged
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
	// Drop the shared index too, so a reused engine re-seeds from the current DB
	// state rather than serving a stale in-memory snapshot (audit H01/spec 011).
	e.idxFts, e.idxVec = nil, nil
	// H06/spec 016: drop the query caches as well — they are stale relative to a
	// re-seed, and the epoch resets to 0 on the next construction. In-process
	// only; nothing persisted.
	e.flushCaches()
}
