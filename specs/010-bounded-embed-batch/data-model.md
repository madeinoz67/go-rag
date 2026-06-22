# Data Model: Bounded Embedding Batches (H12)

> No storage entities are added or changed — batching is transport-layer inside
> `Ollama.Embed`. The on-disk embedding record (`storedEmbedding`, prefix 0x04)
> and every identity hash are untouched (Constitution Principle II). This file
> describes the **in-memory behavioral entity** introduced and the **preserved
> result contract**.

## Entities

### Embed batch (in-memory, internal)

A fixed-size slice of input texts sent in one request to the local embedding
service. An internal concept — never visible to callers.

| Aspect | Value |
|--------|-------|
| Size | `embedBatchSize` (constant, default **32**) |
| Ordering | Processed in input order; results concatenated in order |
| Retry | Independent 3-attempt backoff per batch (5xx/network retry, 4xx fail-fast) |
| Integrity | `len(response.Embeddings) == len(batchTexts)` checked per batch |
| Failure | A permanently-failed batch fails the whole `Embed` call (no partial result) |

### Embedding result (preserved contract — UNCHANGED)

The return value of `Embedder.Embed`: one vector per input text, in input order.

| Aspect | Contract |
|--------|----------|
| Shape | `[][]float32` of length `len(texts)`, or `nil` for empty input |
| Order | `out[i]` is the vector for `texts[i]` — hard contract (the pipeline stores `vecs[i]` for chunk `i`) |
| Integrity | A response whose vector count ≠ its text count is an error (now enforced per batch) |
| Dimensionality | Set-once from the first successful response (mutex-guarded) — unchanged |
| Empty input | No request sent; returns `(nil, nil)` — unchanged |

This contract is what US3 pins; batching must not alter any row above.

## Relationships

```text
caller (pipeline.processJob / engine.Query / tests / future provider)
        │  texts []string
        ▼
Embedder.Embed ──► Ollama.Embed  (ONLY the Ollama impl changes)
                        │  split texts into batches of embedBatchSize
                        ▼
                  ┌─────────────────────────────────────────┐
                  │ per batch: POST /api/embed (3× backoff) │   ← bounded request
                  │           integrity check (count==len)  │
                  └─────────────────────────────────────────┘
                        │  concatenate batch results IN ORDER
                        ▼
                  [][]float32  (len == len(texts); unchanged contract)
                        │
                        ▼
                  caller stores vecs[i] for chunk i  (unchanged)
```

## Validation rules (from requirements)

- FR-001 / FR-002: every request carries ≤ `embedBatchSize` (32) texts.
- FR-003: returned vectors are the in-order concatenation of per-batch results.
- FR-004: each batch retried independently on transient failure.
- FR-005: per-batch count-mismatch → error.
- FR-006: a permanently-failed batch → whole-call error, no partial result.
- FR-007: empty input → no request; sub-cap input → behaves as today's single request.
- FR-008: ctx cancellation honoured between/within batches.
- FR-009: the `Embedder` interface and all callers are unchanged.

No state machine; no persistence; no migration.
