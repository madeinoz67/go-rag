// Package embedproc is the crash-safe background embedder (spec 030, the MuninnDB
// retroactive-processor approach). It is the SOLE writer of embeddings (prefix 0x04):
// processFile writes the chunk + a durable pending-embed record (0x14) atomically on
// ACK, then notifies this processor, which drains the queue — micro-batching across
// documents, guarded by a circuit breaker. On Start it runs an initial scan of 0x14
// (crash recovery: any pending work left by a previous run is re-embedded).
package embedproc

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// maxBatch is the cross-document micro-batch cap (spec 030 R4; mirrors H12's 32).
const maxBatch = 32

// embedRecord mirrors pipeline.storedEmbedding (the 0x04 record shape) — kept here
// to avoid importing the pipeline package. The JSON shape MUST match exactly so
// pipeline.LoadIndex reads records written by this processor.
type embedRecord struct {
	Model      string    `json:"model,omitempty"`
	Convention string    `json:"convention,omitempty"`
	Vector     []float32 `json:"vector"`
}

// Processor is the crash-safe background embedder. It drains the durable pending-embed
// queue (0x14), embeds in cross-document micro-batches, writes vectors (0x04 +
// vec.Add), bumps the index epoch, and removes the queue records. A circuit breaker
// guards the embed call; an initial scan on Start recovers crash-orphaned work.
type Processor struct {
	db       *storage.DB
	embedder embed.Embedder
	prefixer *embed.Prefixer
	vec      *index.Vector
	onChange func() // epoch bump (H06 cache invalidation)

	br       *breaker
	notifyCh chan struct{}
	cancelFn context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// New returns a Processor over the given handles. vec is the shared in-memory vector
// index (the same *index.Vector the engine seeds via LoadIndex). onChange is the
// engine's epoch-bumper (nil in tests = no cache invalidation).
func New(db *storage.DB, em embed.Embedder, pre *embed.Prefixer, vec *index.Vector, onChange func()) *Processor {
	return &Processor{
		db:       db,
		embedder: em,
		prefixer: pre,
		vec:      vec,
		onChange: onChange,
		br:       newBreaker(),
		notifyCh: make(chan struct{}, 1),
	}
}

// Notify wakes the processor to drain the queue immediately (non-blocking; drops if
// a drain is already pending). Called by processFile on ACK.
func (p *Processor) Notify() {
	select {
	case p.notifyCh <- struct{}{}:
	default:
	}
}

// Start launches the background embedder goroutine. It runs an initial scan (crash
// recovery — re-embeds anything left pending from a previous run), then loops on
// Notify + a 3s poll safety-net until Stop.
func (p *Processor) Start(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	ctx, p.cancelFn = context.WithCancel(ctx)
	p.running = true
	p.mu.Unlock()

	p.wg.Add(1)
	go p.run(ctx)
}

// drainTimeout bounds how long Stop waits for in-flight + final-drain embeddings
// to settle. Pending work is durable in the 0x14 queue and re-driven on next
// start (spec 030 crash-safety), so a stuck drain — e.g. the embedder is
// unreachable and the HTTP call is parked on a long timeout — must not wedge
// process shutdown. The goroutine is abandoned on timeout (it dies when the
// process exits); the queued work recovers next run.
const drainTimeout = 5 * time.Second

// Stop gracefully shuts down the processor (drains in-flight work). Bounded by
// drainTimeout so a stuck embedder call cannot block shutdown forever.
func (p *Processor) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	p.mu.Unlock()
	if p.cancelFn != nil {
		p.cancelFn()
	}
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(drainTimeout):
		slog.Warn("embedproc: drain timed out; abandoning worker (pending embeddings recover on next start)", "timeout", drainTimeout)
	}
}

func (p *Processor) run(ctx context.Context) {
	defer p.wg.Done()

	// Initial scan: crash recovery (US1 / SC-001). Re-embeds anything left pending.
	p.processBatch(context.Background())

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Final drain so a one-shot CLI command embeds before exit. Use a
			// fresh context (ctx is canceled) so the embed call completes.
			p.processBatch(context.Background())
			return
		case <-p.notifyCh:
			p.processBatch(context.Background())
			ticker.Reset(3 * time.Second) // reset poll after explicit notify
		case <-ticker.C:
			p.processBatch(context.Background())
		}
	}
}

// pendingChunk is a dequeued embed-queue item awaiting embedding within one
// processBatch pass: the chunk id (0x14 key) and its text content (read from 0x03).
// Lifted to package level so the per-text isolation fallback (embedOneByOne) can
// share the type with processBatch.
type pendingChunk struct {
	id   string
	text string
}

// embedOneByOne is the per-text isolation fallback (approach D, spec 032 overflow
// hardening) invoked when a whole-batch Embed errors. It re-embeds each text
// individually and returns a sparse result (vec[i]==nil for texts that failed), so
// one bad text no longer kills its 31 batch-mates.
//
// Terminal-vs-transient policy: per-text failures are marked EmbedQueueFailed
// (terminal — processBatch skips them on the next scan) ONLY when at least one text
// in the batch succeeded individually, which proves the embedder backend is healthy
// and the failures are text-specific (over-length past sub-chunk recovery, malformed
// input). If EVERY per-text embed fails the embedder itself is the problem (provider
// down, network, OOM) and all records are left pending (transient) for the next
// tick — the breaker, already tripped by the batch fail, backs off the embedder.
func (p *Processor) embedOneByOne(ctx context.Context, texts []string, batch []pendingChunk) [][]float32 {
	vecs := make([][]float32, len(texts))
	succeeded := 0
	for i, t := range texts {
		res, err := p.embedder.Embed(ctx, []string{t})
		if err != nil {
			slog.Warn("embedproc: per-text embed failed", "chunk_id", batch[i].id, "error", err)
			continue
		}
		if len(res) > 0 {
			vecs[i] = res[0]
			succeeded++
		}
	}
	if succeeded == 0 {
		return vecs // embedder-wide failure — leave everything pending (transient)
	}
	// At least one succeeded → the failures are text-specific; mark them terminal so
	// they stop re-entering the retry set on every tick.
	for i := range texts {
		if vecs[i] != nil {
			continue
		}
		rec, _ := json.Marshal(storage.EmbedQueueItem{
			Model:  p.embedder.Model(),
			Status: storage.EmbedQueueFailed,
		})
		_ = p.db.PutEmbedQueue(batch[i].id, rec)
	}
	return vecs
}

// processBatch drains the 0x14 queue: accumulates up to maxBatch pending chunkIDs +
// their texts, applies the document-role prefix (H07), circuit-breaker-guards the
// embed call, scatters vectors back (0x04 + vec.Add + remove 0x14), and bumps the
// index epoch. On transient failure the queue records stay pending (retried next
// pass); on permanent failure they are marked status=failed (terminal).
func (p *Processor) processBatch(ctx context.Context) {
	for { // drain: process batches of maxBatch until the queue is empty
		var batch []pendingChunk

		_ = p.db.ScanEmbedQueue(func(chunkID string, item storage.EmbedQueueItem) bool {
			if len(batch) >= maxBatch {
				return false // cap per pass; remaining picked up next tick
			}
			if item.Status == storage.EmbedQueueFailed {
				return true // skip permanently failed
			}
			// Read the chunk text from 0x03.
			raw, ok, _ := p.db.GetWithPrefix(storage.PrefixChunk, []byte(chunkID))
			if !ok {
				// Chunk was deleted (orphan queue record) — clean up.
				_ = p.db.DeleteEmbedQueue(chunkID)
				return true
			}
			var c model.Chunk
			if json.Unmarshal(raw, &c) != nil {
				return true
			}
			// spec 043 / BL-010 US2: if PrefixEmbedding already exists (copied from
			// a prior version on re-ingest), skip the Ollama call — vec.Add the
			// existing vector + dequeue.
			if embRaw, ok, _ := p.db.GetWithPrefix(storage.PrefixEmbedding, []byte(chunkID)); ok {
				var rec embedRecord
				if json.Unmarshal(embRaw, &rec) == nil && len(rec.Vector) > 0 {
					p.vec.Add(chunkID, rec.Vector)
					_ = p.db.DeleteEmbedQueue(chunkID)
					return true
				}
				// Malformed/empty vector — fall through to normal embed.
			}
			batch = append(batch, pendingChunk{chunkID, c.Content})
			return true
		})
		if len(batch) == 0 {
			return
		}

		// Apply the document-role prefix (H07).
		texts := make([]string, len(batch))
		for i, it := range batch {
			texts[i] = it.text
		}
		conv := ""
		if p.prefixer != nil {
			conv = p.prefixer.Convention()
			texts = p.prefixer.ApplyAll(embed.RoleDocument, texts)
		}

		// Circuit breaker guard (FR-004).
		if err := p.br.allow(); err != nil {
			return // open: try next tick
		}

		// Embed the whole batch in one call (FR-005 cross-doc batching).
		vecs, err := p.embedder.Embed(ctx, texts)
		if err != nil {
			// Approach D (spec 032 overflow hardening): isolate per-text failures so
			// one bad text (over-length past sub-chunk recovery, OOM, provider glitch)
			// no longer poisons its batch-mates. Re-embed each text individually;
			// successes scatter normally, text-specific failures are marked terminal
			// (EmbedQueueFailed) by embedOneByOne so they stop clogging the retry
			// queue. With the HugotEmbedder sub-chunk fix (B) in place this rarely
			// fires for the 512-token case; it is the structural guard for "one bad
			// text kills the batch." The breaker still records the batch-level fail
			// (embedder-health signal).
			p.br.fail()
			slog.Warn("embedproc: embed batch failed, isolating per-text", "batch_size", len(batch), "error", err)
			vecs = p.embedOneByOne(ctx, texts, batch)
		} else {
			p.br.ok()
		}

		// Scatter vectors: write 0x04 + vec.Add + remove 0x14 per chunk. Only items
		// that produced a vector are scattered + dequeued; per-text failures were
		// marked terminal (or left pending if the whole batch failed) by embedOneByOne.
		for i, it := range batch {
			if i >= len(vecs) || vecs[i] == nil {
				continue
			}
			rec, _ := json.Marshal(embedRecord{
				Model:      p.embedder.Model(),
				Convention: conv,
				Vector:     vecs[i],
			})
			_ = p.db.SetWithPrefix(storage.PrefixEmbedding, []byte(it.id), rec)
			p.vec.Add(it.id, vecs[i])
			_ = p.db.DeleteEmbedQueue(it.id) // embedding landed — remove from queue
		}

		// Bump the index epoch so the query result cache invalidates (H06).
		if p.onChange != nil {
			p.onChange()
		}
	} // drain loop: process next batch until queue empty
}
