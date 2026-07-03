# Quickstart: Engine Write-Operation Serialization

> Phase 1 output — runnable validation. See [spec.md](./spec.md), [data-model.md](./data-model.md), [research.md](./research.md).

## Prerequisites

- `go test -race` (the race detector is load-bearing — the bug is a logic race on a mutex-guarded map, invisible to the race detector but exercised by concurrent goroutines).

## Scenario 1 — No double events on concurrent same-docID operations (US1, MVP)

1. Build a pipeline with `OnEvent` wired to an event counter.
2. Ingest a doc (captures its docID).
3. Spawn two goroutines: one calls `Reprocess` on the doc's path; the other calls `DeleteDoc` on the same docID simultaneously.
4. **Expect**: exactly one event fires for the docID — either `RE_INGESTED` (if Reprocess wins the doc-lock) or `DELETED` (if the bare DeleteDoc wins). NEVER both.

## Scenario 2 — processFile returns immediately with a full queue (US2)

1. Build a pipeline with the default cap-64 queue.
2. Fill the queue: ingest 65+ docs without starting the workers (or pause the workers).
3. Call `Ingest` on one more doc.
4. **Expect**: `processFile` returns within the normal ACK latency (<10ms). The job is delivered via a detached goroutine (not blocked on the queue send).

## Scenario 3 — No deadlock under concurrent load (regression test)

1. Build a pipeline + engine.
2. Spawn N concurrent `Add` calls on distinct files + one `Reprocess` on a parent dir + one `Close`.
3. **Expect**: all operations complete within the test timeout (no 601s hang). The detached senders absorb the queue pressure; the doc-locks serialize only the Reprocess's same-docID operations.

## Conventions

- All scenarios run under `go test -race` (the concurrency contracts are the load-bearing invariants).
- The doc-lock + the non-blocking push are pure stdlib (`sync.Map`, `sync.Mutex`, goroutines) — no new deps, no migration, no proto change.
