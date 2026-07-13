package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/madeinoz67/go-rag/internal/caption"
	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/embedproc"
	"github.com/madeinoz67/go-rag/internal/enrich"
	"github.com/madeinoz67/go-rag/internal/events"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/redact"
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

	// embedProc is the crash-safe background embedder (spec 030). Started in
	// pipeline() alongside the pipeline; stopped in Close() before the pipeline
	// drains. nil until pipeline() is first called.
	embedProc *embedproc.Processor

	pipeMu sync.Mutex
	pipe   *pipeline.Pipeline

	// idxFts/idxVec are the engine's shared in-memory search index (audit H01 /
	// spec 011): seeded once per vault from LoadIndex and reused by every query —
	// no per-query rebuild (the single biggest latency win). The pipeline,
	// watcher, and migrate mutate the same per-vault pair in place, so it stays
	// live and current; FTS and Vector are each goroutine-safe, so concurrent
	// query reads + background writes need no Engine-level read/write lock. idxMu
	// guards only the lazy map seed; reads of the stable pointers are lock-free
	// thereafter. Lock ordering: pipeMu → idxMu. Never invert it.
	idxMu  sync.Mutex
	idxFts map[[8]byte]*index.FTS
	idxVec map[[8]byte]*index.Vector

	// qTransformer is the query-transformation seam (audit H05/spec 012): it
	// normalizes (default) or otherwise alters the query before retrieval, applied
	// once at the top of Query so every transport/mode benefits. Default is the
	// pure NormalizingTransformer; a custom one can be set (tests today; future
	// HyDE/multi-query in an adapter) — internal/index stays Ollama-free.
	qTransformer index.QueryTransformer

	// classifier is the query-classification seam (audit H22/spec 024): it
	// recommends a retrieval depth k for a query when the caller has not set one
	// (explicit > recommended > default). nil when adaptive_depth_enabled is false
	// (the default posture) — no classification occurs and behavior is byte-
	// identical to pre-H22. The default RuleBasedClassifier is pure Go and lives
	// in internal/index; a future model-based classifier implements the same
	// interface in an adapter (internal/index stays embedder-free, FR-008).
	classifier index.QueryClassifier

	// Query caches (audit H06/spec 016): an exact-match result cache and a
	// query-embedding cache, both in-process, bounded, and empty on restart.
	// resultCache maps the full query shape + index epoch → *QueryResult;
	// embedCache maps the embedding profile + prefixed query → its vector.
	// Disabled (nil or capacity 0) = every Get misses, every Put no-ops. epoch is
	// the per-vault invalidation counter bumped by the pipeline's OnChange
	// callback at every shared-index mutation (including the async vector-add).
	resultCache *LRU[string, *QueryResult]
	embedCache  *LRU[string, []float32]
	// epochMu guards lazy epoch-entry allocation. It is separate from idxMu
	// because epoch bumps happen on background worker paths and must not contend
	// with the pipeMu → idxMu seed path or risk deadlock by accidental inversion.
	epochMu sync.Mutex
	epoch   map[[8]byte]*atomic.Uint64

	// drift (audit H11/spec 017) caches the embedding-drift verdict + live
	// Ollama version computed at boot / after migrate. /health reads it (fast);
	// Status recomputes live.
	drift driftCache

	// bus (spec 040 / BL-008) is the in-process document lifecycle event bus —
	// the pub-sub substrate for the WatchDocuments gRPC server-stream. The
	// engine owns it (single instance, process-lifetime) and binds the
	// pipeline's OnEvent hook to bus.Publish in pipeline() so INGESTED/EMBEDDED
	// events flow without the pipeline importing the engine. In-memory only
	// (no persisted state — the MVP default; a Pebble-backed follow-on would
	// add cross-restart resume, migration-gated). nil only on a manually-
	// constructed Engine that bypassed NewWithDB (defensive guards below).
	bus *events.Bus

	// poolUtil* (H22/spec 024) are the aggregate pool-utilization counters
	// (atomic, process-lifetime). Recorded once per freshly-computed query (a
	// cache hit reuses an already-counted result and is not double-counted) and
	// reduced to the PoolUtilization aggregate in Status. In-memory only.
	poolQueries    atomic.Uint64
	poolFetchedSum atomic.Uint64 // sum of effective pools observed
	poolKeptSum    atomic.Uint64 // sum of results returned
	poolSaturated  atomic.Uint64 // queries that couldn't fill the requested depth
}

// newQueryCaches builds the result/embedding caches and per-vault epoch registry
// from config. When
// QueryCacheEnabled is false both caches are disabled (capacity 0); a per-cache
// capacity of 0 disables just that cache. The epoch map is always allocated so
// markIndexChanged can lazily create per-vault counters even when caching is off
// (harmless; it just bumps counters nothing reads).
func newQueryCaches(cfg config.Config) (*LRU[string, *QueryResult], *LRU[string, []float32], map[[8]byte]*atomic.Uint64) {
	resCap, embCap := cfg.QueryCacheResults, cfg.QueryCacheEmbeddings
	if !cfg.QueryCacheEnabled {
		resCap, embCap = 0, 0
	}
	return NewLRU[string, *QueryResult](resCap), NewLRU[string, []float32](embCap), make(map[[8]byte]*atomic.Uint64)
}

// NewWithDB returns an Engine over a pre-opened database (daemon mode). The
// caller owns the database's lifetime — Engine does not close it. The ingest
// pipeline is created lazily on the first write, so read-only engines (query,
// status, files) never start background workers.
func NewWithDB(cfg config.Config, db *storage.DB) *Engine {
	rc, ec, ep := newQueryCaches(cfg)
	return &Engine{cfg: cfg, db: db, bus: events.New(), qTransformer: index.NormalizingTransformer{}, classifier: newClassifier(cfg), resultCache: rc, embedCache: ec, epoch: ep}
}

// newClassifier returns the default rule-based classifier when adaptive depth is
// enabled (audit H22/spec 024), else nil — the default posture, in which no
// classification occurs and behavior is byte-identical to pre-H22.
func newClassifier(cfg config.Config) index.QueryClassifier {
	if cfg.EffectiveAdaptiveDepthEnabled() {
		return index.RuleBasedClassifier{}
	}
	return nil
}

// NewWithEmbedder returns an Engine that uses em as its embedder for both ingest
// and query, instead of the configured Ollama endpoint. This is the injection
// point the evaluation harness uses to run the real engine.Query path offline
// and reproducibly (spec 004). Production callers use NewWithDB, which leaves
// the embedder nil and falls back to Ollama — so this changes nothing for them.
func NewWithEmbedder(cfg config.Config, db *storage.DB, em embed.Embedder) *Engine {
	rc, ec, ep := newQueryCaches(cfg)
	return &Engine{cfg: cfg, db: db, embedder: em, bus: events.New(), qTransformer: index.NormalizingTransformer{}, classifier: newClassifier(cfg), resultCache: rc, embedCache: ec, epoch: ep}
}

// embedderOrOllama returns the injected embedder when one is present, otherwise
// the Ollama embedder derived from config (the historical behavior). Centralizing
// this keeps the query and ingest paths on a single embedder.
func (e *Engine) embedderOrOllama() embed.Embedder {
	if e.embedder != nil {
		return e.embedder
	}
	embEndpoint := e.cfg.EmbeddingEndpoint
	if embEndpoint == "" {
		embEndpoint = e.cfg.OllamaURL
	}
	return embed.New(e.cfg.EmbeddingProvider, embEndpoint, e.cfg.EmbeddingModel, e.cfg.EmbeddingAPIKey)
}

// Config returns the engine's loaded configuration (read-only snapshot).
func (e *Engine) Config() config.Config { return e.cfg }

// DB returns the underlying storage handle (used by adapters that need direct
// access, e.g. for prefix scans not yet wrapped here).
func (e *Engine) DB() *storage.DB { return e.db }

// Events returns the engine's document lifecycle event bus (spec 040 / BL-008).
// The bus is the pub-sub substrate for the WatchDocuments gRPC server-stream.
// Always non-nil for engines built via NewWithDB / NewWithEmbedder; a defensive
// nil guard is here for engines assembled by hand in tests.
func (e *Engine) Events() *events.Bus { return e.bus }

// indexes returns the engine's per-vault shared in-memory search index (FTS +
// Vector), seeding it once from the persisted corpus via LoadIndex on first
// access and reusing it on every later call (audit H01/spec 011 — no per-query
// rebuild). Both Query and the ingest pipeline use the pair returned here, so
// writes (processJob, DeleteDoc) flow straight into the same indexes queries
// read. Lock ordering: pipeline() acquires pipeMu then idxMu (via this method);
// Query acquires only idxMu — no inversion, and indexes() never reaches back to
// pipeMu.
func (e *Engine) indexes(ws [8]byte) (*index.FTS, *index.Vector, error) {
	e.idxMu.Lock()
	defer e.idxMu.Unlock()
	if e.idxFts == nil {
		e.idxFts = make(map[[8]byte]*index.FTS)
	}
	if e.idxVec == nil {
		e.idxVec = make(map[[8]byte]*index.Vector)
	}
	fts, okFts := e.idxFts[ws]
	vec, okVec := e.idxVec[ws]
	if !okFts || !okVec || fts == nil || vec == nil {
		fts, vec, err := pipeline.LoadIndex(ws, e.db)
		if err != nil {
			return nil, nil, err
		}
		e.idxFts[ws], e.idxVec[ws] = fts, vec
	}
	return e.idxFts[ws], e.idxVec[ws], nil
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
	ws := e.db.ResolveVaultPrefix("default")
	fts, vec, err := e.indexes(ws) // H01: share the seeded index, not fresh empties.
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
	// H11/spec 017: persist the corpus baseline on first embed.
	e.pipe.OnFirstEmbed = e.handleFirstEmbed
	// spec 040 / BL-008: route the pipeline's lifecycle events (INGESTED after
	// the durable storeDocument commit; EMBEDDED after the status write-back)
	// onto the engine-owned bus, so the WatchDocuments gRPC stream receives
	// them. The method-value matches func(events.DocumentEvent); Publish is
	// non-blocking (T004), so this never enters the <10ms write-ACK path
	// (Principle IV). Bound under pipeMu alongside the other hooks, before any
	// job flows, so no event is missed.
	if e.bus != nil {
		e.pipe.OnEvent = e.bus.Publish
	}
	// H04/spec 019: bind the poisoning detector (default-on, Q2=A). nil when
	// poisoning_enabled is false. The detector scores against the MERGED phrase
	// list (built-in + managed sources, US4 FR-012/013) via poisonDetector().
	if e.cfg.EffectivePoisoningEnabled() {
		e.pipe.SetDetector(e.poisonDetector())
	}
	// H19/spec 022: bind the secret/PII redactor (opt-in, default off).
	if e.cfg.PIIRedactEnabled {
		custom, _ := redact.LoadCustom(e.cfg.PIIPatterns)
		e.pipe.SetRedactor(redact.NewScanner(redact.DefaultPatterns(custom)))
	}
	// spec 029: bind the document enricher (opt-in, default off). nil when
	// enrichment_enabled is false. Produces tags + summary async-after-ACK.
	if e.cfg.EffectiveEnrichmentEnabled() {
		enrEndpoint := e.cfg.EnrichmentEndpoint
		if enrEndpoint == "" {
			enrEndpoint = e.cfg.OllamaURL
		}
		e.pipe.SetEnricher(enrich.New(e.cfg.EnrichmentProvider, enrEndpoint, e.cfg.EnrichmentModel, e.cfg.EnrichmentAPIKey))
	}
	// spec 031 US4: bind the image captioner (opt-in, default off). nil when
	// captioning_enabled is false or captioning_model is empty. Produces a
	// synthetic caption chunk async-after-ACK.
	if e.cfg.EffectiveCaptioningEnabled() && e.cfg.CaptioningModel != "" {
		capEndpoint := e.cfg.CaptioningEndpoint
		if capEndpoint == "" {
			capEndpoint = e.cfg.OllamaURL
		}
		e.pipe.SetCaptioner(caption.New(e.cfg.CaptioningProvider, capEndpoint, e.cfg.CaptioningModel, e.cfg.CaptioningAPIKey))
	}
	// spec 030: construct + start the crash-safe background embedder. It drains
	// the durable 0x14 pending-embed queue, micro-batches across documents, and
	// is circuit-breaker-guarded. On Start it runs an initial scan (crash recovery).
	em := e.embedderOrOllama()
	e.embedProc = embedproc.New(e.db, em, e.cfg.Prefixer(), vec, e.pipe.OnChange)
	e.embedProc.Start(context.Background())
	e.pipe.OnNotifyEmbed = e.embedProc.Notify
	return e.pipe, nil
}

// Close drains the ingest pipeline's background workers (pending embeddings and
// indexing). Safe to call on engines that never wrote (no-op) and idempotent.
// Long-lived daemons must call this before closing the underlying database so
// in-flight async writes complete; short-lived (per-request) engines call it to
// avoid leaking worker goroutines.
// EnsureEmbedder seeds the shared index and starts the background embedder if not
// already running (spec 030). Called by the daemon (serve) on startup so the
// crash-recovery scan runs before serving — a restart after a crash recovers
// pending 0x14 embeddings even if no write triggers pipeline(). Tests don't call
// this (no goroutine leak); the daemon and CLI one-shots do.
func (e *Engine) EnsureEmbedder() error {
	ws := e.db.ResolveVaultPrefix("default")
	_, vec, err := e.indexes(ws)
	if err != nil {
		return err
	}
	if e.embedProc == nil {
		e.embedProc = embedproc.New(e.db, e.embedderOrOllama(), e.cfg.Prefixer(), vec, e.markIndexChanged)
		e.embedProc.Start(context.Background())
	}
	return nil
}

func (e *Engine) Close() {
	e.pipeMu.Lock()
	defer e.pipeMu.Unlock()
	// Close the event bus first so any live WatchDocuments subscriber unblocks
	// via its !ok branch (spec 040 audit follow-up #2). Harmless with no
	// subscribers; idempotent. Done before stopping the embedder/pipeline so a
	// handler never lingers past the rest of shutdown.
	if e.bus != nil {
		e.bus.Close()
	}
	if e.embedProc != nil {
		e.embedProc.Stop() // spec 030: drain pending embeddings before the pipeline
		e.embedProc = nil
	}
	if e.pipe != nil {
		e.pipe.Close()
		e.pipe = nil
	}
	// Drop every per-vault shared index so a reused engine re-seeds from the
	// current DB state rather than serving stale in-memory snapshots (audit
	// H01/spec 011).
	e.idxMu.Lock()
	for ws := range e.idxFts {
		delete(e.idxFts, ws)
	}
	for ws := range e.idxVec {
		delete(e.idxVec, ws)
	}
	e.idxFts, e.idxVec = nil, nil
	e.idxMu.Unlock()
	// H06/spec 016: drop the query caches as well — they are stale relative to a
	// re-seed, and the epoch resets to 0 on the next construction. In-process
	// only; nothing persisted.
	e.flushCaches()
}
