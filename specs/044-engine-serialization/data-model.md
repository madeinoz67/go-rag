# Data Model: Engine Write-Operation Serialization

**Phase 1 output.** See [spec.md](./spec.md) + [research.md](./research.md) + [`docs/design/engine-serialization.md`](../../docs/design/engine-serialization.md).

## Entities

### docLock (new — per-document serialization primitive)

- **Type**: `*sync.Mutex` stored in a `sync.Map` (`Pipeline.docLocks`), keyed by `docID` (string).
- **Semantics**: serializes same-docID operations. Acquired by `DeleteDoc` (the entire body), `ReingestPath` (capture + delete + ingest), and `Reprocess`/`ReprocessAll` (capture + delete per docID in the scan loop).
- **Lifecycle**: created lazily via `LoadOrStore` on first acquisition for a docID. Never explicitly removed (bounded by corpus size; ~16 bytes per entry). A future cleanup of unlocked entries is a refinement.
- **Lock ordering**: `docLock → p.mu` (DeleteDoc takes docLock then p.mu; captureReingest takes p.mu only). Consistent — docLock always outermost.
- **Concurrency safety**: `sync.Map.LoadOrStore` is goroutine-safe; `sync.Mutex.Lock` is goroutine-safe. No additional synchronization needed.

### Detached sender (new — non-blocking queue fallback)

- **Type**: a goroutine spawned by `processFile`'s queue push when `p.queue` (cap-64) is full.
- **Semantics**: `select { case p.queue <- job: default: go func(j job) { p.queue <- j }(job) }`. The goroutine parks on the blocking send; `processFile` returns immediately. The worker pool drains the queue as capacity frees; the goroutine completes when its send lands.
- **Bounds**: at most ~64 parked goroutines (the queue capacity) before workers start draining. Under extreme sustained overload this could grow — acceptable for local-first single-user. A durable process queue (PrefixProcessQueue 0x15) is a future hardening.
- **Data safety**: zero loss. The job IS delivered (the goroutine blocks until it can). The embedder reads `0x14` independently. `processJob` (FTS, near-dup, quarantine, caption, enrich, status) runs when the worker dequeues the job.

## State / computation

### The critical sections

**DeleteDoc**: `docLock(docID) → p.mu (the existing guard) → [delete chunks + doc record + indexes] → [reingestDocs check + DELETED emit] → unlock(p.mu) → unlock(docLock)`. The docLock wraps the entire body so a concurrent reingest's DeleteDoc waits.

**ReingestPath**: `docLock(docID) → captureReingest(path, docID) → DeleteDoc(docID) → Ingest(ctx, path, "*") → unlock(docLock)`. The lock covers the full sequential path. The non-blocking push ensures `Ingest → processFile → p.queue <-` never blocks under the lock.

**Reprocess/ReprocessAll**: inside the scan loop, per-docID: `docLock(docID) → captureReingest(path, docID) → DeleteDoc(docID) → unlock(docLock)`. The lock covers capture+delete. The Ingest runs after the scan loop (batched); the reingest map entry is consumed by `processFile`'s `takeReingest` independently.

### The non-blocking queue push

**Location**: `processFile`, the LAST statement (pipeline.go ~line 462). Replaces `p.queue <- job{...}`.

**Before**: `p.queue <- job{...}` (blocks when the 64-slot queue is full).

**After**: `select { case p.queue <- job{...}: default: go func(j job) { p.queue <- j }(job{...}) }` (never blocks the caller).
