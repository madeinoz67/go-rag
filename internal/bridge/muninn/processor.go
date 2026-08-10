package muninn

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madeinoz67/go-rag/internal/observe"
)

// SyncMode is the origin of a promotion job (priority + audit).
type SyncMode int

const (
	ModeChangeEvent SyncMode = iota // incremental: fired from processJob (T011)
	ModeBackfill                    // US2: the auto-on-enable corpus walk
)

// PromotionJob is one unit of promotion work handed to the bridgeProc: a batch of
// already-mapped WriteParams destined for one MuninnDB vault. The chunk→WriteParams
// mapping happens upstream (the coordinator + mapper, T007/T009); the processor is
// transport-only and chunk-agnostic.
type PromotionJob struct {
	Vault string
	Items []WriteParams
	Mode  SyncMode
}

// ProcConfig configures the bridgeProc worker pool. All fields have sane defaults
// via NewProcessor; zero values are replaced.
type ProcConfig struct {
	Workers     int // goroutine count draining the queue
	MaxInFlight int // concurrent BatchWrite RPC ceiling (storm-limit)
	RatePerSec  int // token-bucket promotions/sec cap; 0 = unbounded
	BatchSize   int // items per BatchWrite (MuninnDB max 50)
	QueueDepth  int // buffered queue depth (shed when full — FR-011)
}

// ProcStats are the observable counters surfaced via status (FR-017) and the
// gorag_bridge_* metrics (T023). All atomic.
type ProcStats struct {
	Promoted int64 // items that landed as created/updated
	Skipped  int64 // items dropped (queue full, breaker open, below-threshold)
	Failed   int64 // items that failed after the breaker allowed the call
}

// Processor is the decoupled bridge worker pool (the internal/embedproc.Processor
// pattern, NOT enrich-inline — research.md R9). The pipeline's processJob
// non-blockingly Submits PromotionJobs; this pool drains them and writes to
// MuninnDB, bounded by MaxInFlight + RatePerSec + the circuit breaker. A down
// MuninnDB trips the breaker and the pool fast-fails (no stall); a stuck RPC
// cannot wedge shutdown (bounded drain in Stop).
//
// v1 is stateless: jobs are not durable. In-flight work abandoned at shutdown is
// lost — safe under the content-addressed UPSERT no-op (the next backfill
// re-walk re-promotes it for free).
type Processor struct {
	client    Client
	queue     chan PromotionJob
	sem       chan struct{} // MaxInFlight slots
	rl        *rateLimiter
	br        *breaker
	batchSize int

	promoted atomic.Int64
	skipped  atomic.Int64
	failed   atomic.Int64

	workers  int
	cancelFn context.CancelFunc
	wg       sync.WaitGroup
	started  atomic.Bool

	mu     sync.Mutex // guards queue close vs Submit
	closed bool
}

// NewProcessor builds a pool over client with the given config (zero values
// replaced by defaults). The queue is buffered to QueueDepth; Submit sheds (not
// blocks) when full so a slow MuninnDB never back-pressures the ingest pipeline.
func NewProcessor(client Client, cfg ProcConfig) *Processor {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 8
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 50 {
		cfg.BatchSize = 50
	}
	if cfg.QueueDepth <= 0 {
		cfg.QueueDepth = 10000
	}
	p := &Processor{
		client:    client,
		queue:     make(chan PromotionJob, cfg.QueueDepth),
		sem:       make(chan struct{}, cfg.MaxInFlight),
		br:        newBreaker(),
		batchSize: cfg.BatchSize,
		workers:   cfg.Workers,
	}
	if cfg.RatePerSec > 0 {
		p.rl = newRateLimiter(cfg.RatePerSec)
	}
	return p
}

// Submit enqueues a job non-blockingly (FR-011). If the queue is full the job is
// shed and counted as skipped — the caller (the pipeline) never blocks, and the
// dropped promotion is recovered by the next backfill walk (UPSERT no-op). A
// stopped processor also sheds (Submit after Stop is a caller bug, but it must
// not panic).
func (p *Processor) Submit(job PromotionJob) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.skip(len(job.Items))
		return
	}
	select {
	case p.queue <- job:
		p.mu.Unlock()
	default:
		p.mu.Unlock()
		p.skip(len(job.Items))
		slog.Warn("bridge: promotion queue full; shedding job (recovered by next backfill)",
			"vault", job.Vault, "items", len(job.Items))
	}
}

// skip records n shed/skipped items on BOTH the atomic counter (the Status
// surface) and the prometheus counter (/metrics) so the two never drift. Every
// skip path goes through here.
func (p *Processor) skip(n int) {
	p.skipped.Add(int64(n))
	observe.BridgeSkipped(context.Background(), n)
}

// Start launches the worker goroutines. Idempotent.
func (p *Processor) Start(ctx context.Context) {
	if !p.started.CompareAndSwap(false, true) {
		return
	}
	ctx, p.cancelFn = context.WithCancel(ctx)
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx)
	}
}

// procDrainTimeout bounds Stop. Pending jobs are NOT durable (v1 stateless), so a
// stuck BatchWrite must not wedge `go-rag stop`. In-flight work is abandoned on
// timeout; the next backfill re-walk recovers it (the spec 045 embedproc lesson).
const procDrainTimeout = 5 * time.Second

// Stop drains in-flight work, bounded by procDrainTimeout (NFR-005). It closes the
// queue (workers finish their current job), waits, and abandons on timeout.
func (p *Processor) Stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.queue)
	p.mu.Unlock()
	p.started.Store(false)
	if p.cancelFn != nil {
		p.cancelFn() // abort in-flight BatchWrite RPCs
	}
	if p.rl != nil {
		p.rl.Close()
	}
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(procDrainTimeout):
		slog.Warn("bridge: drain timed out; abandoning in-flight promotions (recovered by next backfill)",
			"timeout", procDrainTimeout)
	}
}

// Stats returns a snapshot of the observable counters.
func (p *Processor) Stats() ProcStats {
	return ProcStats{
		Promoted: p.promoted.Load(),
		Skipped:  p.skipped.Load(),
		Failed:   p.failed.Load(),
	}
}

// CircuitOpen reports whether the breaker is currently open (status surface).
func (p *Processor) CircuitOpen() bool {
	p.br.mu.Lock()
	defer p.br.mu.Unlock()
	return p.br.state == stOpen
}

// worker drains the queue. For each job it chunks items into BatchWrite-sized
// groups and writes each under the rate limiter + breaker + MaxInFlight sem.
func (p *Processor) worker(ctx context.Context) {
	defer p.wg.Done()
	for job := range p.queue {
		// Skip cheaply if the breaker is open (no point queueing RPCs that fast-fail).
		if err := p.br.allow(); err != nil {
			p.skip(len(job.Items))
			continue
		}
		p.promoteJob(ctx, job)
	}
}

// promoteJob writes a job's items in BatchWrite-sized chunks. One breaker probe
// spans the whole job (a job is one document's chunks — a single health signal).
func (p *Processor) promoteJob(ctx context.Context, job PromotionJob) {
	for start := 0; start < len(job.Items); start += p.batchSize {
		end := start + p.batchSize
		if end > len(job.Items) {
			end = len(job.Items)
		}
		batch := job.Items[start:end]

		// Rate-limit (storm-limit, NFR-006). Blocks until a token is available or
		// the context is cancelled (shutdown).
		if p.rl != nil {
			if err := p.rl.wait(ctx); err != nil {
				// Shutdown — leave the rest for the next backfill.
				p.skip(len(job.Items) - start)
				return
			}
		}
		// Concurrency cap (MaxInFlight). Acquired before the RPC so the storm-limit
		// holds even if a worker fans out in a future revision.
		select {
		case p.sem <- struct{}{}:
		case <-ctx.Done():
			p.skip(len(job.Items) - start)
			return
		}
		batchStart := time.Now()
		results, err := p.client.BatchWrite(ctx, job.Vault, batch)
		<-p.sem
		observe.BridgeBatchDuration(ctx, time.Since(batchStart)) // T023: RPC latency
		if err != nil {
			p.br.fail()
			p.failed.Add(int64(len(batch)))
			observe.BridgeFailed(ctx, len(batch)) // T023
			slog.Warn("bridge: BatchWrite failed", "vault", job.Vault, "items", len(batch), "err", err)
			continue
		}
		// Per-item outcomes: a result with an empty ID + an error is a failed item;
		// otherwise promoted (created or no-op — the response carries no outcome enum).
		var failed int64
		for _, r := range results {
			if r.ID == "" && r.Error != "" {
				failed++
			}
		}
		p.br.ok()
		promoted := int64(len(batch)) - failed
		p.promoted.Add(promoted)
		if failed > 0 {
			p.failed.Add(failed)
			observe.BridgeFailed(ctx, int(failed)) // T023
		}
		observe.BridgePromoted(ctx, int(promoted)) // T023
	}
}

// rateLimiter is a minimal token-bucket (ratePerSec tokens/sec, burst = ratePerSec
// clamped to [1, 1000]). Written locally to avoid pulling golang.org/x/time/rate
// into the build (Principle III — no new dep for a 30-line primitive). Blocks on
// wait until a token is available or ctx is cancelled.
type rateLimiter struct {
	tokens chan struct{}
	stop   chan struct{}
}

func newRateLimiter(ratePerSec int) *rateLimiter {
	burst := ratePerSec
	if burst < 1 {
		burst = 1
	}
	if burst > 1000 {
		burst = 1000
	}
	rl := &rateLimiter{tokens: make(chan struct{}, burst), stop: make(chan struct{})}
	for i := 0; i < burst; i++ {
		rl.tokens <- struct{}{}
	}
	interval := time.Second / time.Duration(ratePerSec)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-rl.stop:
				return
			case <-t.C:
				select {
				case rl.tokens <- struct{}{}:
				default: // bucket full; drop the token
				}
			}
		}
	}()
	return rl
}

func (rl *rateLimiter) wait(ctx context.Context) error {
	select {
	case <-rl.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (rl *rateLimiter) Close() { close(rl.stop) }
