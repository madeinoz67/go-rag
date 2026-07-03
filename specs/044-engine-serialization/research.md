# Research: Engine Write-Operation Serialization

**Phase 0 output.** Consolidates the 3-agent research workflow + the 12-agent RedTeam ParallelAnalysis into the Spec Kit research record. No `NEEDS CLARIFICATION` remains.

## R1 — The deadlock root cause (why opMu failed)

**Decision**: the broad `opMu sync.Mutex` on all Engine write methods **deadlocked** because it serialized ALL writes through `processFile`'s blocking `p.queue <- job{...}` send (a cap-64 channel drained by 2 workers). Under concurrent load the queue fills, `Add` blocks holding `opMu`, `Close` (test cleanup) blocks on `opMu`, 601s timeout.

**Rationale**: the send is the LAST statement of `processFile` (pipeline.go ~line 462), AFTER the durable ACK (`storeDocument` + `OnEvent` + `OnNotifyEmbed` have all completed). The durable work is committed; the only thing blocked is the queue push. Embeddings are NOT lost (the embedder reads the durable `0x14` Pebble prefix independently — spec 030). The queue carries ONLY `processJob` work (FTS, near-dup, quarantine, caption, enrich, status).

**Alternatives rejected**: raise the queue capacity (delays saturation, doesn't fix the unbounded-latency send under sustained overload).

## R2 — The per-document lock (Option A — adopted)

**Decision**: a `sync.Map[docID]*sync.Mutex` on the Pipeline. `DeleteDoc`, `ReingestPath`, and `Reprocess`/`ReprocessAll` acquire the docID's lock for the critical section.

**Rationale**: the race is per-document (two operations on the SAME docID). The lock matches the blast radius: same-docID operations serialize, distinct-docID operations run concurrent. `Close` takes only `pipeMu` → never contends for a doc-lock → no deadlock. Lock ordering: `docLock → p.mu` (acyclic — no path acquires `p.mu` then a `docLock`).

**Alternatives rejected**:
- **B (generation counter)**: DeleteDoc can't know its caller's generation. Invasive plumbing.
- **C (ctx-cancellable push)**: doesn't fix the race (orthogonal).
- **D (reingestMu)**: same deadlock hazard as opMu + doesn't cover the watcher's bare `DeleteDoc`.
- **E (non-blocking push with drop)**: silent FTS/quarantine/near-dup holes (the `0x14` fallback only covers embeddings, not `processJob`'s work).

## R3 — The non-blocking queue push (REQUIRED, not separate)

**Decision**: replace `p.queue <- job{...}` with:
```go
select {
case p.queue <- job{...}:
default:
    go func(j job) { p.queue <- j }(job{...})
}
```

**Rationale**: the 12-agent RedTeam proved the per-document lock + the non-blocking push are **inseparable**. `ReingestPath` does `capture → DeleteDoc → Ingest` as one sequential call — the doc-lock MUST cover all three (C14). Holding it across `Ingest → processFile → p.queue <-` (a blocking send) re-introduces the per-docID variant of the opMu stall (C23). The detached sender breaks the chain: `processFile` never blocks; the goroutine parks instead.

Zero data loss: the job IS delivered (the goroutine blocks on the send until a worker drains). The embedder reads `0x14` independently — unaffected.

## R4 — Scope: who is NOT in the race

**Decision**: only `DeleteDoc` + `ReingestPath` + `Reprocess`/`ReprocessAll` acquire the doc-lock. `Add`/`Scan` do NOT (they call `Ingest` which calls `processFile`; they don't touch `reingestDocs` or `DeleteDoc`). `ReleaseChunk`/`ResetChunk`/`RescanPoisoning`/`AddPhrases` do NOT call `DeleteDoc` — they are out of scope.

**Rationale**: verified in the codebase. The `reingestDocs` map is the ONLY shared mutable state that races; it's touched only by `captureReingest`, `DeleteDoc` (the check+consume), and the re-ingest flow. Operations that don't touch it can't race.
