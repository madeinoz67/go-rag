# Contract — Pebble-backed FTS (Internal; Transparency + Async) (H16, pivoted)

> Phase 1 output. H16 is **internal and transparent** — it changes the FTS
> *backing* (map → Pebble), never the *results* or the *API surface*. No new
> CLI/REST/gRPC/MCP contract. The contract is the transparency invariant + the
> async-readiness semantic.

## 1. Transparency invariant (the core contract)

For any corpus state, a keyword query on the **Pebble-backed FTS** MUST return
**byte-identical** results (chunk IDs, BM25 scores, order) to the **pre-pivot
in-memory FTS**. The BM25 math (k1=1.2, b=0.75, field weights concept=3.0 /
tags=2.0 / body=1.0) is **unchanged** — only the data source shifts from map
lookups to Pebble prefix scans (FR-008/SC-001).

This is the single most important assertion and gets a dedicated diff-test (build
both FTses over the same corpus, query both, diff the hits).

## 2. Async-readiness contract (the behavior change)

Keyword search becomes **eventually consistent** — a chunk is keyword-findable
after its async FTS posting write drains (`processJob`), not immediately at ACK.
This aligns keyword search with vector search (which was always async). The
window is the `processJob` drain time (milliseconds); `waitEmbedded` covers it.

- The ACK (`storeDocument`) carries **only** durable Pebble writes (doc/chunks/
  path/contenthash) — **no BM25 indexing** on the ACK path (Principle IV).
- The async `processJob` writes the FTS postings (same call site as today's
  `fts.Index`, now Pebble-backed).

**What this means for callers**: a test or operator flow that ingests and
immediately queries must `waitEmbedded` (which drains the async workers) before
expecting keyword visibility. This is the SAME pattern already used for vector
search — no new waiting mechanism.

## 3. Durability contract

Postings are Pebble keys — durable by construction (Pebble's WAL). No separate
snapshot, no marker, no checkpoint. A crash may lose the in-flight async batch
(bounded, the same window as vectors); on restart, the durable postings are
served. The FTS is always as current as the last-drained async batch.

## 4. Migration contract (backward-compat)

A pre-pivot vault (no `0x08|"stats"` key) is migrated on first cold start: a
one-time scan of PrefixChunk → tokenize → write postings + DF + stats. No
re-ingestion, no operator action. Gated by the stats-key presence check so it
runs exactly once. After migration, cold start is O(vectors) only.

## 5. Interface change (internal)

`FTS.Delete(chunkID)` → `FTS.Delete(chunkID, content string)` — the Pebble-backed
delete needs the content to re-tokenize (recovering the terms for key
construction). The sole caller (`pipeline/delete.go` `DeleteDoc`) has the content
from the chunk record. No external/transport surface change.

`NewFTS()` → `NewFTS(db *pebble.DB)` — the adapter needs the Pebble handle.
Callers: `LoadIndex` (pipeline) — updated.

## 6. Out-of-contract (not exposed)

- No new CLI flag / REST field / gRPC field / MCP tool. The FTS backing is
  transparent.
- No "force-rebuild" command (the migration backfill covers pre-pivot vaults; a
  manual rebuild is YAGNI).
- No vector-map persistence (FTS-only; clarification 2026-06-23).
- No dedicated FTS worker pool (reuse `processJob`).
- No trigram/fuzzy search (future, `0x07` left free).
