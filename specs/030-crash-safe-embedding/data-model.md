# Data Model — Crash-Safe Background Embedder (spec 030)

**Phase 1 output.** Adds **one new Pebble prefix** (`0x14`, the durable pending-embed
queue) and **one new background processor**. Existing prefixes (0x03 chunks, 0x04
embeddings) are unchanged; the embed step moves from `processJob` to the processor.

---

## 1. `PrefixEmbedQueue` — the durable pending-embed queue (NEW, `0x14`)

The work queue the background embedder drains. "The DB is the queue" — durable, so a
crash between ACK and embed leaves a record the next start recovers.

| Key | Value | Lifecycle |
|-----|-------|-----------|
| `0x14 \| chunkID` | `{Model, Status, Attempts}` (JSON) | written atomically with the chunk (0x03) on ACK; **removed** when the embedding lands (0x04 written); **kept + status=failed** on a permanent embed failure |

- **Model**: the embedding model to use (so a model change doesn't silently mis-embed
  pending records — matches the 0x04 convention provenance).
- **Status**: `pending` (default) | `failed` (permanent — not retried indefinitely; FR-004).
- **Attempts**: transient-retry count (bounded; circuit breaker governs the rate).

The queue length (`countPrefix(0x14)`) is the **pending backlog** surfaced in status; the
`status=failed` subset is the **failed backlog**.

---

## 2. The background embedder (NEW processor — `internal/embedproc`)

The self-healing processor (MuninnDB retroactive-processor model):

```text
Start(ctx):
  initial scan of 0x14              # CRASH RECOVERY (US1) — re-embeds anything left pending
  loop:
    wait on Notify chan (processFile signals on ACK) OR poll-tick (3s safety-net)
    drain 0x14 → micro-batch (≤ MaxBatchSize texts)      # cross-doc batching (US2/FR-005)
    circuit.Allow()                                       # FR-004
    vecs = embedder.Embed(ctx, batch texts w/ doc-role prefix)   # H07 prefix
    scatter: for each chunk → write 0x04 {model,convention,vector} + vec.Add + remove 0x14
    indexChanged()                                        # bump epoch (H06 cache invalidation)
    on permanent fail → mark 0x14 status=failed (not retried)
    on transient fail   → leave 0x14 (retried next pass; circuit may open)
```

**Holds:** the shared `(*FTS, *Vector)` + embedder + prefixer + the `OnChange` epoch hook
(the same handles `processJob` uses today) — so vector writes are identical to today
(FR-008). Circuit breaker = the spec-029 primitive (5 fails/30s).

---

## 3. Lifecycle / state transitions

```text
WRITE PATH (processFile, <10ms ACK):
  durable batch: chunk → 0x03  +  pending → 0x14        # ATOMIC → ACK
  Notify(embedder)                                       # wake the processor (near-immediate)

BACKGROUND EMBEDDER (decoupled, the only thing that embeds now):
  0x14 (pending) ──embed ok──▶ 0x04 (vector) + vec.Add + epoch bump + remove 0x14
  0x14 (pending) ──perm fail─▶ 0x14 (status=failed)     # terminal (FR-004)
  0x14 (pending) ──transient─▶ 0x14 (left; retried)     # circuit may open

CRASH RECOVERY (startup):
  Start() initial scan of 0x14 → any pending re-embedded  # US1: no orphans

MIGRATE (orthogonal): re-embeds stale-MODEL docs (drift); the embedder recovers MISSING
  embeddings (crash/pending). Distinct concerns; compose.
```

**processJob loses its embed role** (keeps FTS indexing, near-dup, enrichment, status) —
the embedder is now the sole writer of 0x04. The <10ms ACK is unchanged (the atomic
0x03+0x14 write is still fast; embedding is strictly post-ACK/background — FR-002).

---

## 4. Validation rules (from requirements)

- **FR-001 → crash-safe:** 0x14 is written atomically with 0x03, so ACK ⇒ a durable
  pending record ⇒ `Start()`'s initial scan recovers it. A test: ingest + kill before
  embed + restart → doc retrievable (SC-001).
- **FR-002 → async-after-ACK:** the 0x03+0x14 atomic write stays <10ms; embedding is off
  the ACK path (SC-002).
- **FR-003 → decoupled:** the embedder is the only embedder; processFile never calls Embed.
- **FR-004 → circuit breaker:** 5/30s; permanent fail → 0x14 status=failed (SC-003).
- **FR-005 → cross-doc batch:** one Embed call per ≤MaxBatchSize pending texts (SC-004).
- **FR-006 → idempotent:** embedding a chunk whose 0x04 already exists is a no-op (the
  0x14 was removed on first success; a re-queue re-derives the same vector).
- **FR-007 → status:** pending = countPrefix(0x14 status=pending); failed = countPrefix
  (0x14 status=failed).
- **FR-008 → unchanged outputs:** 0x04 record shape (`{model,convention,vector}`) + vec.Add
  + epoch bump are identical to processJob today (the eval recall is byte-identical — SC-005).
