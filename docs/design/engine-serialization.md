# Engine Operation Serialization — Design

**Status:** design · **Date:** 2026-07-03 · **Problem:** the concurrent-DeleteDoc / reingestDocs suppression race (spec 043 adversarial-review HIGH finding) + the pipeline forward-progress stall that defeated the broad opMu fix.

**Informed by:** 3-agent research workflow (deadlock root cause, design options, embed-queue analysis).

## 1. The problem (two issues, one root cause)

### 1.1 The concurrent-DeleteDoc race (the actual bug)

`Reprocess`/`ReprocessAll`/`ReingestPath` call `captureReingest(path, docID)` which marks `reingestDocs[docID]=true`, then calls `DeleteDoc(docID)` which checks + consumes the mark under `p.mu` (suppressing the `DELETED` event — FR-005). If a **concurrent** operation calls `DeleteDoc` for the **same docID** between `captureReingest` and the re-ingest's `DeleteDoc`, it consumes the mark → the re-ingest's `DeleteDoc` finds it gone → emits `DELETED` → the consumer sees `DELETED` + `RE_INGESTED` (double event).

**Scope:** only operations that call `DeleteDoc` on the same docID concurrently. From the code: `Reprocess`, `ReprocessAll`, `ReingestPath`, and the watcher's `ScanOnce` DELETED path (`watcher.go`). `ReleaseChunk`/`ResetChunk`/`RescanPoisoning`/`AddPhrases` do **NOT** call `DeleteDoc` — they are out of scope.

### 1.2 The opMu deadlock (why the broad fix failed)

The attempted `opMu sync.Mutex` on all Engine write methods **deadlocked** (601s timeout). Root cause: `Add` holds `opMu` → calls `pl.Ingest` → `processFile` → `p.queue <- job{...}` (a **blocking** channel send to the cap-64 job queue). When the queue is full (2 workers can't keep up under concurrent load), `Add` parks holding `opMu`. `Close` (test cleanup) blocks on `opMu` → timeout.

This is **forward-progress starvation**, not a true cyclic deadlock: the queue send is unbounded in latency (the workers are slow under caption/enrich network calls), and `opMu` serializes every write through that unbounded send.

**Key architectural fact:** the `p.queue` push carries ONLY `processJob` work (FTS indexing, near-dup clustering, caption, enrich, status). It does **NOT** carry embedding work — the embedder reads the durable `0x14` Pebble prefix independently. So a blocked `p.queue` send is NOT a durability loss, just a forward-progress stall.

## 2. The design options evaluated

| Option | Fixes the race? | Deadlocks? | Blast radius | Verdict |
|---|---|---|---|---|
| **A — per-document lock** (`sync.Map[docID]*sync.Mutex`) | ✅ Yes | ❌ No (Close never contends) | Minimal (same-docID only) | **✅ ADOPTED** |
| B — generation counter on reingestDocs | ✅ Yes | No | Invasive (DeleteDoc can't know its generation) | ❌ Rejected |
| C — ctx-cancellable queue push | ❌ No (orthogonal) | No | — | ❌ Doesn't fix the race |
| D — reingestMu (serialize reingests only) | ✅ Partial | Same hazard + doesn't cover watcher's bare DeleteDoc | Coarse (global reingest serialization) | ❌ Rejected |
| E — non-blocking queue push with drop | ❌ No (orthogonal) | No | Silent FTS/quarantine/near-dup holes | ❌ Rejected |
| ~~opMu~~ (attempted + reverted) | ✅ Yes | ✅ Deadlock | Global | ❌ Deadlocked |

## 3. The decision: per-document lock (Option A)

### 3.1 The mechanism

A `sync.Map[string]*sync.Mutex` on the Pipeline, keyed by docID. Each operation that touches a document's lifecycle (`DeleteDoc`, `ReingestPath`, and the `captureReingest + DeleteDoc` pair in `Reprocess`/`ReprocessAll`) acquires the docID's lock for the duration of the critical section.

### 3.2 Why it works

- **Fixes the race:** same-docID operations serialize. The `reingestDocs` mark is consumed by the SAME operation that set it (no concurrent `DeleteDoc` can interleave).
- **Does NOT deadlock:** the doc-lock is held across `Ingest → processFile → p.queue <-`, a potentially blocking send. But the workers that drain the queue do NOT take any doc-lock. `Close` does NOT take doc-locks (only `pipeMu`). So a doc-lock holder may stall on a full queue, but nothing waiting on the doc-lock is on the `Close`/shutdown path — no cycle.
- **Preserves concurrency:** distinct-docID operations run fully concurrent. The US3 guard (`TestConcurrent_AddQuery_NoCorruption` — 24 concurrent adders on distinct files) is unaffected.
- **Lock ordering:** `docLock → p.mu` (DeleteDoc takes docLock then p.mu; captureReingest takes p.mu only). Consistent — docLock always outermost. No cycle.

### 3.3 The implementation shape

**Two changes ship together** (the RedTeam proved they are inseparable):

**Change 1 — Non-blocking queue push** (pipeline.go, the `p.queue <- job{...}` at the end of `processFile`):
```go
// Replace the bare blocking send:
//   p.queue <- job{...}
// With a non-blocking select + detached sender fallback:
select {
case p.queue <- job{...}:
default:
    go func(j job) { p.queue <- j }(job{...})
}
```
This ensures `processFile` never blocks on a full queue — the caller (which may hold a doc-lock) returns immediately. The detached goroutine parks on the send instead, and the worker pool drains it as capacity frees up. Zero data loss: the job is delivered; `processJob` runs it; the embedder's 0x14 path is unaffected.

**Change 2 — Per-document lock** (pipeline struct + the three callers):
```go
// Pipeline struct:
docLocks sync.Map // docID → *sync.Mutex

// Helper:
func (p *Pipeline) docLock(docID string) func() {
    actual, _ := p.docLocks.LoadOrStore(docID, &sync.Mutex{})
    mu := actual.(*sync.Mutex)
    mu.Lock()
    return func() { mu.Unlock() }
}
```

**Where to acquire:**
1. `DeleteDoc` — lock the docID for the entire body (so any concurrent reingest's DeleteDoc waits).
2. `ReingestPath` — lock the docID across `captureReingest + DeleteDoc + Ingest` (the lock covers the full sequential path; Change 1 ensures `Ingest → processFile → p.queue <-` never blocks under the lock).
3. `Reprocess`/`ReprocessAll` — lock each docID around the `captureReingest + DeleteDoc` pair (the Ingest is batched after the scan loop; the Ingest re-discovers the file independently, and `takeReingest` in `processFile` consumes the capture. If the file is deleted by a concurrent operation between the unlock and the Ingest, `takeReingest` still fires — the capture was already consumed, so `processFile` sees `isReingest=false` and emits `INGESTED` normally).

**Where NOT to acquire:**
- `Add`/`Scan` — they call `Ingest` which calls `processFile`. They don't touch `reingestDocs` or `DeleteDoc`. They are NOT in the race.
- `ReleaseChunk`/`ResetChunk`/`RescanPoisoning`/`AddPhrases` — they don't call `DeleteDoc`. Out of scope.
- `Query`/`GetChunk`/etc. — read-only. No lock needed.

### 3.4 The map-growth concern

`sync.Map` entries grow unbounded by docID count. Mitigation: docIDs are content-addressed (SHA-256), bounded by corpus size. A periodic cleanup of unlocked entries (or a sharded map with eviction) is a future refinement, not a blocker — the map's memory footprint is ~16 bytes per docID (pointer to a zero-value mutex), trivial for a local-first single-user system.

## 4. Complementary hardening (optional, separate from the race fix)

### 4.1 Non-blocking queue push (defense-in-depth)

The `p.queue <- job{...}` send is the pipeline's single unbounded-latency point. Making it non-blocking eliminates the forward-progress stall that defeated `opMu`. Three options:

- **(a) Detached sender** (`go func() { p.queue <- j }()`): zero loss, breaks the chain. Cost: unbounded parked goroutines under extreme overload (strictly better than a 601s hang).
- **(b) Durable process queue** (PrefixProcessQueue 0x15): mirrors the 0x14 pattern. Workers drain both. Crash-safe. Largest change.
- **(c) Inline FTS on default**: run FTS+near-dup synchronously, drop caption/enrich to the queue only when capacity exists. Keeps the doc keyword-retrievable immediately.

**Recommendation:** **(a) the detached sender is REQUIRED, not optional.** The RedTeam (12-agent ParallelAnalysis) found that the per-document lock's scope is incoherent for `ReingestPath` (the watcher's MODIFIED path) without the non-blocking push. `ReingestPath` does `capture → DeleteDoc → Ingest` as one sequential call — the doc-lock MUST cover all three (otherwise a concurrent DeleteDoc between unlock and Ingest re-opens the race). But holding the lock across `Ingest → processFile → p.queue <-` (a blocking send) re-introduces the per-docID variant of the opMu stall. The detached sender (§4.1a) breaks this chain: `processFile` never blocks on the queue push, so the doc-lock holder never stalls. **The non-blocking push and the per-document lock ship together as one fix** — they are not separable.

Option (b) (durable process queue) is a future hardening if sustained overload becomes a real scenario. Option (c) (inline FTS) is an alternative to (a) if the detached-goroutine concern is material.

### 4.2 Bounded Close

`Engine.Close` holds `pipeMu` for the duration of `pipe.Close()` (which drains the entire job queue via `wg.Wait`). Under overload this is unbounded. A context-deadline drain (accept loss of best-effort async work on timeout) would make Close bounded-latency. Separate from this design.

## 5. RedTeam verdict (12-agent ParallelAnalysis) + resolution

**6 of 12 agents validated C5** (the per-document lock fixes the race — strong foundation).
**6 of 12 agents attacked C14** (the lock-scope ambiguity for `ReingestPath`).

**The critical finding:** the original design's §4.1 (non-blocking push) was framed as "separate from the race fix." The RedTeam proved this is **wrong**: `ReingestPath` does `capture → DeleteDoc → Ingest` as one sequential call, so the doc-lock MUST cover all three. Holding the lock across `Ingest → processFile → p.queue <-` (a blocking send) re-introduces the per-docID variant of the opMu stall (C23). The design's claims C15 ("lock around capture+delete only"), C11 ("held across the blocking send"), C21 ("does NOT change the push"), and C17 ("detached sender is separate") were **mutually contradictory** for `ReingestPath`.

**Resolution (incorporated above):** the non-blocking queue push (§4.1a, the detached sender) and the per-document lock (§3) **ship together as one fix**. §3.3 now explicitly states both changes. §4.1's recommendation is updated from "separate" to "REQUIRED." The design is now internally consistent: the doc-lock covers the full `ReingestPath` critical section, and Change 1 (the non-blocking push) ensures the lock-holder never stalls on the queue.

## 6. What this design does NOT do

- Does NOT serialize all Engine writes (the `opMu` approach). Only same-docID operations serialize.
- Does NOT change the pipeline's async-after-ACK model (Principle IV intact).
- Does NOT add a new storage prefix (no migration). The `sync.Map` is in-memory.
- Does NOT touch the embedder (it reads 0x14 independently).
- Does NOT change the `p.queue` push behavior visible to the worker (jobs still arrive in order, none lost) — the detached sender changes only WHO blocks (a goroutine, not the caller holding the doc-lock).
