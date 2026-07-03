# Feature Specification: Engine Write-Operation Serialization

**Feature Branch**: `044-engine-serialization` *(single-author repo — work commits to `main`; slug identifies the spec)*
**Created**: 2026-07-03
**Status**: Draft
**Input**: The concurrent-DeleteDoc race (spec 043 adversarial-review HIGH finding) + the pipeline queue-push stall that defeated the broad `opMu` fix. Technical design: [`docs/design/engine-serialization.md`](../../docs/design/engine-serialization.md) (per-document lock + non-blocking queue push, 12-agent RedTeam-validated).

## User Scenarios & Testing

### User Story 1 — No double events on concurrent re-ingest (Priority: P1) 🎯 MVP

A document is being re-ingested by `Reprocess` (or the watcher's MODIFIED path). Simultaneously, a second operation touches the same document — a watcher DELETED scan, or a concurrent `Reprocess` on a parent directory that overlaps. Today, the second operation's `DeleteDoc` can consume the `reingestDocs` suppression flag between `captureReingest` and the re-ingest's own `DeleteDoc`, causing BOTH a `DELETED` and a `RE_INGESTED` event for the same document. The consumer (the MuninnDB bridge) double-counts — it promotes ADDED chunks twice and tags UNCHANGED chunks as superseded.

**Why this priority**: the double-event is a correctness bug in the event contract (FR-005 of spec 043). A consumer cannot trust the stream if a re-ingest can emit both DELETED and RE_INGESTED.

**Independent Test**: run two concurrent operations on the same document; assert exactly one event fires (RE_INGESTED — not DELETED + RE_INGESTED).

**Acceptance Scenarios**:
1. **Given** a document tracked by go-rag, **When** `Reprocess` and a concurrent watcher scan both touch it, **Then** exactly one `RE_INGESTED` event fires (no `DELETED`).
2. **Given** two concurrent `Reprocess` calls on overlapping paths, **When** both touch the same document, **Then** one emits `RE_INGESTED` and the other is a no-op (or emits `INGESTED` — no double `DELETED`).

### User Story 2 — No forward-progress stall under concurrent load (Priority: P2)

Under concurrent write load (multiple `Add` calls + a `Reprocess` + the watcher), the pipeline's job queue (`p.queue`, cap-64) can fill. Today, a full queue blocks `processFile`'s queue push — which blocks the caller (which may hold an engine-level lock or be a daemon request handler). This manifests as a hang under sustained concurrent writes (the 601s timeout that defeated the `opMu` approach). The non-blocking queue push (detached sender) ensures `processFile` never blocks on the queue, eliminating the stall.

**Why this priority**: without this, the per-document lock (US1) would re-introduce the stall for `ReingestPath` (which holds the doc-lock across `Ingest → processFile → p.queue <-`). The two changes are inseparable.

**Independent Test**: fill the job queue to capacity, then call `processFile`; assert it returns immediately (the job is delivered via a detached goroutine, not blocked).

**Acceptance Scenarios**:
1. **Given** the job queue is full (64 pending jobs, 0 workers draining), **When** `processFile` completes, **Then** it returns within the normal ACK latency (not blocked on the queue push).
2. **Given** sustained concurrent `Add` calls that exceed the worker drain rate, **When** the system runs for 60s, **Then** no `Add` call hangs (the queue absorbs via detached senders; goroutines park but the caller never blocks).

### Edge Cases

- A document re-ingested while a `Close` is in progress — the `Close` drains the queue (via `wg.Wait`); the re-ingest's `processFile` pushes (via detached sender if queue is full); the job is consumed before the workers exit. No data loss.
- A `Reprocess` that scans 1000 documents — each gets a doc-lock sequentially (inside the scan loop). The locks are acquired + released per-doc (not held across the entire scan). Unrelated docs are unaffected.
- A docID whose lock is held by a stalled detached sender (the queue is full + the sender is parked) — subsequent operations on that docID wait. This is bounded by the worker drain rate (eventually the queue drains, the sender completes, the lock releases). No deadlock (workers + Close don't take doc-locks).

## Requirements

### Functional Requirements

- **FR-001**: Two concurrent operations touching the SAME document ID MUST NOT produce duplicate lifecycle events (no DELETED + RE_INGESTED for the same re-ingest).
- **FR-002**: `processFile`'s queue push (`p.queue <- job`) MUST NOT block the calling goroutine when the queue is full. A detached goroutine MAY park on the send instead.
- **FR-003**: The per-document lock MUST cover the full critical section in `ReingestPath` (capture + DeleteDoc + Ingest), not just capture + DeleteDoc.
- **FR-004**: The per-document lock MUST be acquired in `DeleteDoc` so that any concurrent operation on the same docID waits.
- **FR-005**: Operations on DISTINCT document IDs MUST run concurrently (no global serialization).
- **FR-006**: `Engine.Close` MUST NOT contend for a per-document lock (it takes only `pipeMu`).
- **FR-007**: No job (processJob payload: FTS indexing, near-dup clustering, caption, enrich, status) MUST be lost by the non-blocking push — the detached sender delivers it eventually.
- **FR-008**: The lock ordering MUST be `docLock → p.mu` (acyclic — no path acquires `p.mu` then a `docLock`).

### Key Entities

- **docLock** — a `*sync.Mutex` per document ID, stored in a `sync.Map` on the Pipeline. Serializes same-docID operations.
- **Detached sender** — a goroutine spawned by the non-blocking queue push when the channel is full. Parks on the send; delivers the job when capacity frees.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Two concurrent operations on the same docID produce exactly one lifecycle event (no double DELETED + RE_INGESTED).
- **SC-002**: `processFile` returns within its normal latency (<10ms, Principle IV) even when the job queue is full.
- **SC-003**: `go test -race` detects no data race on the reingestDocs map or the docLock acquisition.
- **SC-004**: The existing concurrency test (`TestConcurrent_AddQuery_NoCorruption`) still passes (distinct-doc concurrency preserved).

## Assumptions

- The technical approach is the per-document lock + the detached-sender non-blocking push, as specified in [`docs/design/engine-serialization.md`](../../docs/design/engine-serialization.md) (12-agent RedTeam-validated; the two changes ship together).
- The `sync.Map` docID→lock map grows by docID count (~16 bytes per docID, bounded by corpus size). No eviction in the MVP; a periodic cleanup of unlocked entries is a future refinement.
- `ReleaseChunk`/`ResetChunk`/`RescanPoisoning`/`AddPhrases` do NOT call `DeleteDoc` and are NOT in scope (they can't trigger the race).
- The detached sender's parked goroutines are bounded by the queue capacity (at most 64 parked senders before workers drain). Under extreme overload this could grow — acceptable for a local-first single-user system; a durable process queue (PrefixProcessQueue 0x15) is a future hardening.
- Out of scope: bounded-latency Close (§4.2 of the design doc), durable process queue (§4.1b).
