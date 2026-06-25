# Research — Crash-Safe Background Embedder (spec 030)

**Phase 0 output.** Resolves the design decisions for adopting MuninnDB's
embedder-as-retroactive-processor model in go-rag. Grounded in MuninnDB source
(`cmd/muninn/server.go`, `internal/plugin/retroactive.go`) + go-rag's current embed
path (`pipeline.go`, `workers.go`, `load.go`).

---

## R1 — How is "pending-embed" state detected? (the mechanism)

**Decision.** A **durable pending-embed work queue** under a new Pebble prefix
**`0x14`** (`PrefixEmbedQueue`): `processFile` writes the chunk (0x03) AND a pending
record (0x14: `chunkID → {model, status}`) **in one atomic batch**, then ACKs. The
background embedder scans `0x14`, embeds, writes the embedding (0x04), and removes the
0x14 record. "The DB is the queue" (MuninnDB's core insight) — the pending set is
durable, so a crash between ACK and embed leaves the 0x14 record, which the next start
recovers. Failed embeddings carry a `status` in the 0x14 record (not retried forever).

**Rationale.** This is the faithful MuninnDB translation (`DigestEmbed` flag ⇒ a
dedicated work-queue prefix) that is also go-rag-idiomatic (single Pebble,
prefix-partitioned — one new prefix, no schema migration of existing chunks). The
processor scans **only** 0x14 (the pending set), not all chunks — O(pending), not
O(corpus) per pass. Pending/done/failed live in one record. Crash-safe by construction:
the 0x14 write is atomic with the chunk store, so ACK ⇒ a durable pending record ⇒
recoverable.

**Alternatives considered.**
- *A flag byte on each chunk (MuninnDB `DigestEmbed` on the engram).* Rejected — adds a
  field to every chunk record + a migration of existing chunks; a dedicated queue prefix
  achieves the same with no migration and a cleaner processor scan.
- *Scan 0x03 chunks, check for a missing 0x04 embedding (join scan).* Rejected — O(corpus)
  join every pass (wasteful when most are done), and a chunk-without-0x04 is always
  "pending" with no clean permanent-fail marker. The 0x14 queue scans only pending and
  carries status natively.

---

## R2 — Full-decoupled (MuninnDB) vs minimal-recovery?

**Decision.** **Full decoupled, phased for safety.** The embed step **moves off the
per-ingest `processJob`** to the background embedder entirely — `processFile` writes the
chunk + the 0x14 pending record + notifies the embedder; the embedder does ALL embedding
(cross-document micro-batch + circuit breaker), writes 0x04, removes 0x14, and bumps the
index epoch. `processJob` keeps the non-embed async work (FTS indexing, near-dup,
enrichment, status). This is MuninnDB's model (the write path notifies; the processor
embeds) and delivers all of US1 + US2.

**Phased delivery (in tasks.md, not separate specs):**
- **Phase A — crash-safety (US1):** the 0x14 queue + the background embedder with startup
  scan + Notify. processFile writes the queue record; processJob stops embedding; the
  embedder takes over. Crash gap closed.
- **Phase B — throughput/resilience (US2):** cross-document micro-batching + circuit
  breaker in the embedder (these are properties of the processor's loop, layered on Phase A).

**Rationale.** Stephen specified the MuninnDB approach; MuninnDB's write path does NOT
embed (it notifies). Moving embed off processJob is the faithful translation and is what
delivers cross-doc batching (FR-005) + decoupling (FR-003). Phasing lets US1 (the
critical crash-safety gap) land + verify before the throughput layer.

**Alternatives considered.**
- *Minimal (keep processJob embedding; add 0x14 + a recovery-only processor for crash
  leftovers).* Rejected as the primary design — it leaves two embed paths (processJob fast
  + processor recovery), doesn't deliver cross-doc batching on normal ingest (US2), and
  isn't the MuninnDB model. (It IS the lower-risk option if Phase A proves too large —
  noted as a fallback.)

---

## R3 — Where does the background embedder live, and how is it started?

**Decision.** A new **`internal/embedproc`** package (the embedder processor — kept out
of `internal/pipeline` so the pipeline stays an ingest orchestrator, Principle V). The
**daemon** (`internal/daemon`) constructs it over the shared `*storage.DB` + embedder +
index handles and `Start(ctx)`s it alongside the engine — it runs for the daemon's
lifetime, drains on shutdown. `Start()` does an **initial scan** of 0x14 (crash recovery),
then idles on a `Notify` channel (processFile signals on ACK) with a periodic poll
safety-net (MuninnDB: 3s). The CLI one-shot commands (`add`/`reprocess`) that route
through the engine get a short-lived embedder that runs + drains before the process exits
(so a one-shot `add` still embeds before returning).

**Rationale.** MuninnDB starts its retroactive processor in `cmd/muninn/server.go` for the
daemon's lifetime + an initial scan. go-rag's daemon is the natural home; the CLI one-shots
need the embed step to complete before exit (today processJob.Close drains — the embedder's
drain replaces that role). A dedicated package keeps the processor testable in isolation
(mirrors `internal/enrich`).

**Alternatives considered.**
- *Embedder inside `internal/engine`.* Rejected — couples the engine to embed scheduling;
  a package keeps it modular.
- *Daemon-only (CLI one-shots keep processJob).* Rejected — the CLI `add` must embed too
  (spec 029 enrichment depends on the doc being embedded); a uniform embedder for both is
  consistent.

---

## R4 — Circuit breaker + cross-document batching

**Decision.** Reuse the **circuit-breaker pattern from spec 029**
(`internal/enrich/circuit.go`, MuninnDB-verified 5/30s defaults) — extract it to a shared
location (or copy) so the embedder wraps its `Embed` calls: `Allow()` before, `ok()/fail()`
after; an open breaker fast-fails (FR-004). **Cross-document micro-batching:** the embedder
accumulates pending chunk texts up to a `MaxBatchSize` (MuninnDB: provider-defined; go-rag
reuses the H12 batch constant 32 as the cap), issues ONE `Embed` call per micro-batch, and
scatters vectors back — so a bulk ingest of N small docs yields ~N/32 embed calls, not N.

**Rationale.** MuninnDB's processor micro-batches (`embedPlugin.MaxBatchSize()`,
`retroactive.go`). go-rag already has within-call batching (H12, embedBatchSize=32); the
embedder extends it ACROSS documents by accumulating the pending queue into one call. The
circuit breaker is the same resilience primitive spec 029 introduced — reuse, don't
reinvent.

---

## R5 — Interaction with `Migrate` (spec 017/H11) + the shared seeded index (H01)

**Decision.** **Distinct + composable.**
- `Migrate` re-embeds **stale-*model*** documents (drift); it works by `ReprocessAll`.
  The background embedder recovers **missing** embeddings (crash orphans / pending 0x14).
  They don't clobber: Migrate re-derives content (new model); the embedder fills gaps
  (same model). Migrate may also write 0x14 records (so its re-embeds flow through the
  embedder) OR keep its direct path — a plan-level choice, but the two concerns are
  orthogonal.
- **Shared seeded index (H01):** when the embedder writes a vector (0x04 + `vec.Add`), it
  MUST bump the **index epoch** (`indexChanged`/OnChange) so the query result cache (H06)
  invalidates — exactly as `processJob` does today. The embedder holds the same shared
  `(*FTS, *Vector)` + the OnChange hook the engine binds.
- **H07 prefix + H03 guard:** the embedder applies the document-role instruction prefix
  (`prefixer.ApplyAll`) and records `{model, convention}` on the 0x04 record (as processJob
  does) so the mismatch/convention guards still hold.

**Rationale.** The embedder absorbs the embed responsibilities currently in `processJob`
(prefixing, 0x04 record shape, epoch bump, FTS is already separate) so behaviour is
identical — only the scheduling + crash-recovery change (FR-008).

---

## R6 — Status surfacing (the backlog)

**Decision.** `status` reports the embedder's **pending count** (0x14 records) +
**permanently-failed count** (0x14 with status=failed), mirroring how poisoning/near-dup/
enrichment counts are surfaced (`status.go` scans a prefix). Additive fields on
`StatusInfo` + the 4 transport projections (the standard pattern).

**Rationale.** FR-007. The 0x14 queue length IS the pending backlog (a count-prefix scan,
like `NearDupChunks`/`EnrichedDocs`). Surfacing it lets an operator see crash-recovery in
progress or a stuck (failed) embed.
