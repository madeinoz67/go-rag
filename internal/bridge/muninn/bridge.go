package muninn

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/model"
)

// Bridge is the top-level MuninnDB bridge coordinator. It owns the outbound
// Client (read + write) and the decoupled promotion Processor. Start dials
// MuninnDB + launches the worker pool; Stop drains (bounded, NFR-005) + closes
// the client.
//
// T007 ships the coordinator + engine wiring. The chunk→WriteParams promotion
// seam (the pipeline processJob hook) lands in US1 (T011); the auto-backfill
// walker + live Pause/Resume semantics land in US2 (T015/T016). Until US1, a
// started Bridge is alive but receives no jobs — it is inert infrastructure.
type Bridge struct {
	cfg    config.Config
	client Client
	proc   *Processor
	mapper Mapper // chunk→WriteParams translation (maintainer invariants baked in)
	source ChunkSource

	started  atomic.Bool
	bfCancel context.CancelFunc // stops the (US2) backfill walker on Stop

	// paused gates ONLY the (US2) backfill walker. Incremental promotion is never
	// paused — a pause must not starve the live change-event path.
	paused atomic.Bool

	// backfill progress (US2). Reported via Status; zero until a backfill runs.
	bfMu     sync.Mutex
	backfill BackfillState
}

// BackfillState is the in-memory backfill progress (US2 / data-model.md E6),
// surfaced via Status. Not durable — restart re-walks (free under the UPSERT
// no-op). US2 populates Running/Cursor/Promoted; T007 leaves it zero.
type BackfillState struct {
	Running   bool
	Paused    bool
	Cursor    string // last docID paged
	Promoted  int64
	Skipped   int64
	Failed    int64
	StartedAt int64
}

// New constructs a Bridge from config. The MuninnDB target-vault key is read from
// the GORAG_BRIDGE_TOKEN env (never inlined in config.json, never logged). Dial
// succeeds regardless of whether MuninnDB is currently up — the bridge degrades;
// only a non-loopback endpoint fails (config.Validate is the first gate). Caller
// MUST gate on cfg.EffectiveBridgeEnabled().
func New(ctx context.Context, cfg config.Config, source ChunkSource) (*Bridge, error) {
	token := os.Getenv("GORAG_BRIDGE_TOKEN")
	client, err := Dial(ctx, cfg.EffectiveBridgeEndpoint(), token)
	if err != nil {
		return nil, err
	}
	return newBridge(cfg, client, source), nil
}

// newBridge wires a Bridge over an existing client. Tests pass a FakeClient so the
// coordinator is exercisable without a live MuninnDB; production New dials first.
// source is the US2 backfill corpus reader (nil disables auto-backfill).
func newBridge(cfg config.Config, client Client, source ChunkSource) *Bridge {
	proc := NewProcessor(client, ProcConfig{
		Workers:     cfg.EffectiveBridgeWorkers(),
		MaxInFlight: cfg.EffectiveBridgeMaxInFlight(),
		RatePerSec:  cfg.BridgeRatePerSec,
		BatchSize:   cfg.EffectiveBridgeBatchSize(),
		QueueDepth:  10000,
	})
	return &Bridge{
		cfg:    cfg,
		client: client,
		proc:   proc,
		mapper: Mapper{SourceVault: cfg.EffectiveBridgeSourceVault(), TargetVault: cfg.EffectiveBridgeTargetVault()},
		source: source,
	}
}

// Promote is the pipeline.Promoter hook (T011). It maps the document's chunks to
// WriteParams and enqueues them as an incremental (ModeChangeEvent) promotion.
// Called from processJob (async-after-ACK); non-blocking. v1 promotes every
// ingested document into the single configured target vault — multi-vault source
// filtering is a follow-up.
func (b *Bridge) Promote(doc model.Document, chunks []model.Chunk) {
	if len(chunks) == 0 {
		return
	}
	b.Submit(b.mapper.TargetVault, b.mapper.MapAll(chunks, doc), ModeChangeEvent)
}

// Start launches the worker pool (the client is already dialed in New).
// Idempotent.
func (b *Bridge) Start(ctx context.Context) {
	if !b.started.CompareAndSwap(false, true) {
		return
	}
	b.proc.Start(ctx)
	// US2: auto-backfill the existing corpus on enable (storm-limited via the
	// processor's rate/concurrency caps; pausable). nil source (tests that don't
	// care about backfill) skips the walk.
	if b.cfg.BridgeBackfillAutoOnEnable && b.source != nil {
		bfCtx, cancel := context.WithCancel(ctx)
		b.bfCancel = cancel
		go b.runBackfill(bfCtx)
	}
	slog.Info("bridge: started",
		"endpoint", b.cfg.EffectiveBridgeEndpoint(),
		"source_vault", b.cfg.EffectiveBridgeSourceVault(),
		"target_vault", b.cfg.EffectiveBridgeTargetVault())
}

// Stop drains the processor (bounded by procDrainTimeout) and closes the client.
// Idempotent. Safe to call on a never-started Bridge.
func (b *Bridge) Stop() {
	if !b.started.Load() {
		return
	}
	if b.bfCancel != nil {
		b.bfCancel() // stop the backfill walker before the queue closes
	}
	b.proc.Stop()
	if err := b.client.Close(); err != nil {
		slog.Warn("bridge: client close", "err", err)
	}
	b.started.Store(false)
}

// Submit is the write seam: enqueue already-mapped WriteParams. The chunk→
// WriteParams mapping + the pipeline hook land in US1 (T011); US2's backfill
// walker also feeds this. Pause gates ONLY backfill-sourced jobs — incremental
// promotion is never gated by a pause.
func (b *Bridge) Submit(vault string, items []WriteParams, mode SyncMode) {
	if mode == ModeBackfill && b.paused.Load() {
		return
	}
	b.proc.Submit(PromotionJob{Vault: vault, Items: items, Mode: mode})
}

// Pause gates the (US2) backfill walker. Incremental promotion continues.
func (b *Bridge) Pause() { b.paused.Store(true) }

// Resume releases a backfill pause.
func (b *Bridge) Resume() { b.paused.Store(false) }

// Paused reports the backfill pause flag (status surface).
func (b *Bridge) Paused() bool { return b.paused.Load() }

// Client returns the outbound MuninnDB client — the Memory & Graph view (US3)
// reads through it. Non-nil for a constructed Bridge.
func (b *Bridge) Client() Client { return b.client }

// BridgeStatus is the observable snapshot (FR-017 / contracts/ui-rest.md). The
// target-vault key is deliberately absent — it never leaves the gRPC interceptor.
type BridgeStatus struct {
	Enabled     bool
	Healthy     bool
	Endpoint    string
	SourceVault string
	TargetVault string
	Promoted    int64
	Skipped     int64
	Failed      int64
	CircuitOpen bool
	Backfill    BackfillState
}

// Status returns the observable snapshot.
func (b *Bridge) Status() BridgeStatus {
	s := b.proc.Stats()
	b.bfMu.Lock()
	bf := b.backfill
	b.bfMu.Unlock()
	bf.Paused = b.paused.Load()
	return BridgeStatus{
		Enabled:     true, // a constructed Bridge exists ⇒ enabled
		Healthy:     b.client.Healthy(),
		Endpoint:    b.cfg.EffectiveBridgeEndpoint(),
		SourceVault: b.cfg.EffectiveBridgeSourceVault(),
		TargetVault: b.cfg.EffectiveBridgeTargetVault(),
		Promoted:    s.Promoted,
		Skipped:     s.Skipped,
		Failed:      s.Failed,
		CircuitOpen: b.proc.CircuitOpen(),
		Backfill:    bf,
	}
}
