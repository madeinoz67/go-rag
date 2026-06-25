# Contract — Background Embedder (spec 030)

**Phase 1 output.** This is an **internal architecture contract** — the feature adds no
new external API surface (no new RPC/endpoint/tool; embeddings, query results, and
identity are unchanged — FR-008). The only external-visible change is two **status**
fields (the embedder backlog). This document is the design contract for the processor.

---

## External surface (the only user-visible change)

Two additive fields on `status`, surfaced identically on CLI/REST/gRPC/MCP, omitted when
zero:

| Field | Meaning |
|-------|---------|
| `embed_pending` | count of chunks awaiting embedding (the 0x14 queue length) |
| `embed_failed` | count of chunks whose embedding permanently failed |

No new query/request fields, no new RPC, no new CLI command, no proto message beyond the
two status fields. Retrieval results, scores, and identity are **byte-identical** to today
(FR-008 / SC-005) — the refactor is purely structural (the embed step's scheduling +
crash-recovery change; its outputs do not).

---

## The processor contract (internal)

The background embedder is the **sole writer of embeddings (prefix 0x04)**. Its contract:

1. **Crash-safe (FR-001).** `processFile` writes chunk (0x03) + pending (0x14) **in one
   atomic batch** before ACK. The embedder's `Start()` runs an **initial 0x14 scan**, so
   any pending work left by a crash is re-embedded on the next start — no orphans, no
   manual re-ingest.
2. **Decoupled (FR-003).** The write path (`processFile`) never calls Embed — it writes the
   queue record + notifies. The embedder does all embedding.
3. **Post-ACK (FR-002).** The atomic 0x03+0x14 write is the ACK-time work (<10ms);
   embedding is strictly background. The ACK budget is preserved.
4. **Circuit-breaker-guarded (FR-004).** A breaker (5 fails/30s, the spec-029 primitive)
   wraps Embed; an open breaker fast-fails. Permanent failures mark the 0x14 record
   `status=failed` (terminal); transient failures leave it pending (retried).
5. **Cross-document batched (FR-005).** Pending texts are accumulated into micro-batches
   (≤ MaxBatchSize) for one Embed call — bulk ingest throughput.
6. **Idempotent (FR-006).** Re-embedding already-embedded content is a no-op (the queue
   record is removed on first success; a re-queue reproduces the same vector).
7. **Output-identical (FR-008).** The 0x04 record (`{model, convention, vector}`), the
   `vec.Add`, and the index-epoch bump are identical to today's `processJob` — only the
   scheduling moves. The H07 document-role prefix + H03 mismatch/convention guards are
   preserved.

---

## Composition with existing features

- **`Migrate` (spec 017):** orthogonal — Migrate re-embeds *stale-model* docs (drift);
  the embedder recovers *missing* embeddings (crash/pending). They must not clobber.
- **Shared seeded index (H01) + query cache (H06):** the embedder bumps the index epoch on
  every vector write (as `processJob` does), so cached results invalidate.
- **Enrichment (spec 029):** unaffected — enrichment runs after embed in the ingest flow;
  it consumes the doc, not the embedding schedule.
- **`processJob`:** loses its embed role; keeps FTS indexing (H16), near-dup (H20),
  enrichment (029), and status.

---

## What is explicitly NOT added

- **No new external API** (no RPC/endpoint/tool/CLI command beyond the two status fields).
- **No hot-swap** of the embedder at runtime (MuninnDB's admin.go) — out of scope.
- **No embedding-provider plugins** / no model change (that's `Migrate`).
- **No change to embedding generation/quality** — only scheduling, batching, recovery.
