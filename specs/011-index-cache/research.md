# Phase 0 — Research: Cached Loaded Index (H01)

> Resolves the design decisions before Phase 1. Each item: Decision · Rationale ·
> Alternatives rejected. Grounded in code read this session: `internal/engine/
> engine.go` (Engine struct, lazy `pipeline()` under `pipeMu`, `NewWithDB`),
> `internal/engine/query.go:29` (`pipeline.LoadIndex(e.db)` per query),
> `internal/pipeline/load.go` (`LoadIndex` full scan of chunks+embeddings),
> `internal/pipeline/workers.go` (`processJob` mutates `p.fts`/`p.vec`),
> `internal/pipeline/delete.go` (`DeleteDoc` DB-only), `internal/index/{fts,vector}.go`
> (both **goroutine-safe**, mutex-protected, with exported `Delete`), and
> `internal/watcher/watcher.go` (`ChangeDetector` holds the engine's `*Pipeline`).

## 1. Shared live index, NOT a generation-counter snapshot

**Decision**: The Engine owns one shared `(*FTS, *Vector)` pair, seeded once via
`LoadIndex` (the full historical corpus) and reused by every query. The ingest
pipeline, watcher, and migrate mutate that same pair in place, so it stays live
and current — there is no "invalidation event" and no rebuild-on-write.

**Rationale**: `FTS` and `Vector` are explicitly goroutine-safe (each has its own
`sync.Mutex`; both are documented "Safe for concurrent use" / "Goroutine-safe",
and the pipeline already runs 2 background workers mutating them concurrently).
That makes a shared mutable index safe for concurrent query readers + background
writers with **no Engine-level read/write lock**. It also dissolves the spec's
hardest correctness concern: a *live* index can never pin and serve a stale
"snapshot" (FR-004), because there is no snapshot — queries always see the
current state, and `processJob` adds a chunk's FTS entry and its vector together
(under each index's own mutex) after embeddings land.

**Alternatives rejected**:
- *Generation-counter snapshot (the audit's literal phrasing)*: cache `(gen, FTS,
  Vector)`; bump `gen` on every write; rebuild via `LoadIndex` on the next query
  after a write. Rejected: it moves the per-query O(corpus) cliff onto the write
  path (violates FR-007 — every ingest would force a full rebuild before the next
  query), and a snapshot can pin a pre-embedding state unless invalidation is
  wired to embedding completion (a subtle hazard FR-004 forbids). The live index
  avoids both.
- *Copy-on-write / RCU pointer swap*: build a new index, atomically swap.
  Rejected: unnecessary — `FTS`/`Vector` are already internally locked, so a
  single shared mutable instance is safe. COW adds allocation + complexity for
  no benefit at this scale.

## 2. Seed-once, lazily, under a dedicated mutex

**Decision**: Add `Engine.indexes() (*FTS, *Vector)` which, under a new `idxMu`,
runs `LoadIndex(e.db)` exactly once (seed the full corpus) and returns the shared
pointers on every subsequent call. `e.pipeline()` calls `e.indexes()` first and
passes the shared pair to `pipeline.New(...)` (instead of the current fresh
`NewFTS()`/`NewVector()`); `e.Query()` calls `e.indexes()` and uses them directly
(no `LoadIndex`).

**Rationale**: One seed, paid on first access (query or write), amortized across
all later queries. Lock ordering is `pipeMu → idxMu` (only `pipeline()` acquires
both, in that order; `Query()` acquires only `idxMu`) — no inversion. The seed
completes before `pipeline.New` starts workers (synchronous call), so no race
between the seed scan and worker mutations.

**Alternatives rejected**:
- *Seed eagerly in `NewWithDB`*: penalizes read-only / status-only engines that
  never query. Rejected — keep it lazy.
- *Per-query mutex held for the whole query*: serializes queries. Rejected — only
  the seed-once needs the lock; reads of the already-seeded shared pointers are
  lock-free after the first call (the pointers are stable once set).

## 3. Cache-aware delete: `DeleteDoc` becomes a method on `*Pipeline`

**Decision**: Convert the package-level `pipeline.DeleteDoc(db, docID)` into a
method `(p *Pipeline) DeleteDoc(docID)` that removes the document's chunks from
the DB **and** from `p.fts` / `p.vec` (via the existing exported `FTS.Delete` and
`Vector.Delete`). Update the 4 callers: `reprocess.go:25,44` (already have `p` →
`p.DeleteDoc(...)` instead of `DeleteDoc(p.db, ...)`) and `watcher.go:96,105`
(`cd.pl.DeleteDoc(...)` instead of `pipeline.DeleteDoc(cd.db, ...)` — the
`ChangeDetector` already holds the engine's `*Pipeline` via `watcher.New(e.db, p)`
at `engine/ingest.go:53`).

**Rationale**: This is the cross-component invalidation answer. Because the
pipeline holds the shared `fts`/`vec` (once the engine passes them in), making
delete a method gives cache-aware removal for free across **every** delete path
(migrate/reprocess and watcher) without any new wiring, notification channel, or
generation counter. The watcher already has the pipeline reference — no new
plumbing. FR-003 (delete freshness / no phantom hits) is satisfied directly.

**Alternatives rejected**:
- *Keep `DeleteDoc(db, …)` + a separate invalidation callback/channel*: more
  moving parts; the watcher would need a new handle. Rejected — the method form
  is simpler and the pipeline already owns the index.
- *Invalidate via re-seed on next query after any delete*: reintroduces the
  per-write rebuild (FR-007 violation). Rejected.
- *Engine wraps a new `Engine.Delete`*: there is no engine delete entry today
  (delete happens only via reprocess/watcher); adding one is scope creep.
  Rejected.

## 4. Migrate / Reprocess freshness is automatic

**Decision**: No special migrate handling. `Migrate` → `ReprocessAll` → per doc:
`p.DeleteDoc` (now cache-aware) then re-ingest → `processJob` re-indexes into the
shared `fts`/`vec` (`FTS.Index` replaces on existing ID; `Vector.Add` replaces).
The shared index ends up with the new embeddings.

**Rationale**: Migrate already flows through the pipeline; once the pipeline uses
the shared index and delete is cache-aware, migrate correctness is inherited.
Validated by the existing migrate tests still passing.

## 5. Async-after-ACK correctness (FR-004) is structural, not bolted on

**Decision**: No explicit "invalidate on embedding completion" event. The shared
index is live: `processJob` adds a chunk's FTS entry and vector together (after
`Embed` returns, in the same loop, under each index's own mutex). A query that
runs during the async window sees the index as it currently is — never a pinned
"chunk-only" snapshot masquerading as complete. Once embeddings land, the very
next query sees them; no flush, no generation bump.

**Rationale**: The spec's FR-004 concern ("never cache and serve a vector-less
state") was specifically about *snapshot* caches pinning a moment in time. A live
index has no snapshot to pin, so the concern dissolves. (A query may momentarily
see a chunk in FTS a hair before its vector lands — within `processJob`'s per-
chunk `fts.Index`→`vec.Add` pair — but that yields a slightly-under-scored-but-
present hit, never a phantom or missing one, and resolves on the next call.)

## 6. Single-writer process model — no cross-process coherence

**Decision**: The cache is per-Engine (per-process). go-rag enforces one writer
via the Pebble lock, so exactly one process mutates the indexed data. Each query
process owns its cache and seeds it once on cold start.

**Rationale**: Cross-process cache coherence would require a persistent snapshot
+ invalidation protocol — that is H16's scope, not H01's. A daemon serving many
queries is the beneficiary; a one-shot `go-rag query` CLI invocation cold-starts
every time (inherent to one-shot; out of scope here).

## 7. Results-identical guarantee (FR-008)

**Decision**: The shared index is built by the same `LoadIndex` and mutated by the
same `processJob`/`DeleteDoc` logic. For identical underlying data, query results
are byte-for-byte identical to today's per-query-rebuild path.

**Rationale**: No scoring or ranking logic changes — only *when* the index is
built. The existing parity/eval tests (spec 003, spec 004) anchor this: they must
pass unchanged, proving the cache changes latency, not results.
