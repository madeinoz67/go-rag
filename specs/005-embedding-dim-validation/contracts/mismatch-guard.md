# Phase 1 — Contracts: Embedding Mismatch Guard

> The behaviors this feature exposes: a query-time refusal, a per-vector skip
> signal, and a status drift view — all consistent across every transport because
> the guard lives in the one shared `engine.Query`. Entity shapes in
> [../data-model.md](../data-model.md); decisions in [../research.md](../research.md).

---

## 1. Query refusal — `engine.Query` mismatch error (US1, FR-001/FR-003/FR-007)

When a query embedding's **model** or **dimensionality** does not match the corpus
majority, `engine.Query` returns a **sentinel mismatch error** and **no hits**.

**Error contract** (text is identical across transports; only the wire shape
differs):

```text
embedding mismatch: query model=<Q> dim=<Qd> vs corpus model=<C> dim=<Cd>;
re-index under the configured model before querying
```

- `refuse` triggers when `Q ≠ C` (model) **or** `Qd ≠ Cd` (dim).
- Same-dimensionality-but-different-model is still a refusal (a different model is
  a different semantic space).

**Per-transport mapping:**

| Transport | On mismatch |
|-----------|------------|
| CLI (`go-rag query`) | print the error; exit non-zero. |
| REST | the error response with the message (consistent with existing error rendering). |
| gRPC | the coded status carrying the message. |
| MCP (`go_rag_query`) | the JSON-RPC error (`-32603`) carrying the message. |

Because the decision is made once in `engine.Query`, all four refuse identically.

---

## 2. Per-vector skip — `Vector.Query` length guard (US3, FR-005)

When the query matches the corpus majority but a **minority** of stored vectors
differ in length (mid-migration), the scorer **skips** them and counts them
rather than garbage-scoring.

**Contract:** `Vector.Query` scores only stored vectors whose length equals the
query vector's length; mismatched-length stored vectors are excluded and counted.
The query does **not** fail. A `skipped` count is available to the result (logged
on the happy-partial path).

- `cosine()` is never called on a mismatched-length pair (no silent `min` truncation).
- A corrupted/odd-length stored entry is skipped+counted, never panics (edge case).

---

## 3. Status drift view — `Status` / `StatusInfo` (US2, FR-004)

Status reports the **stored** corpus embedding profile and flags drift, so an
operator sees inconsistency without querying.

**Fields (added/changed on `StatusInfo`):**

| Field | Meaning |
|-------|---------|
| `EmbeddingModel` | now the **stored** majority model (was the configured string). |
| `Dimensions` | the **stored** majority dim (was the first record's length). |
| `EmbeddingDrift` | `true` if >1 model **or** >1 dim is stored. |
| `ModelCounts` | per-model counts (e.g. `{nomic-embed-text: 120, mxbai: 3}`). |
| `DimCounts` | per-dim counts. |

**Renderings:** CLI `go-rag status` prints the majority model/dim and, when
`EmbeddingDrift`, a line listing the per-model counts; MCP `go_rag_status` /
REST / gRPC surface the same fields. An empty corpus reports no model/dim and no
drift (not an error).

---

## 4. Provenance read contract (FR-002)

The corpus profile is read from the existing prefix-0x04 records in their
persisted `{Model, Vector}` shape — `model` from `Model`, `dim` from
`len(Vector)`. No new storage key, no schema change; the guard simply reads
provenance that is already written at ingest.
