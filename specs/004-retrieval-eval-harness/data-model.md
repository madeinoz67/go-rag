# Phase 1 — Data Model: Retrieval-Quality Evaluation Harness

> Entities the harness introduces or relies on. Implementation-agnostic shape;
> field types are descriptive, not Go-source. Anchored on the existing
> `engine.QueryHit` (internal/engine/types.go) and Principle II content-addressing.

## Entities

### Golden Query  *(introduced — the unit of the evaluation dataset)*

A human-authored search query paired with the chunks judged relevant for it.

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Stable handle (e.g. `q001`); for per-query breakdown output. |
| `query` | string | The natural-language query, run verbatim through `engine.Query`. |
| `relevant` | list\<chunk_id\> | Chunk IDs (SHA-256, content-addressed) judged relevant. **Join key** to `QueryHit.ChunkID`. |
| `notes` | string (optional) | Free-text annotation for reviewers. |

**Validation**: `id` unique within the dataset; `query` non-empty; each
`relevant` chunk_id is a non-empty string. A query with an empty `relevant` list
is allowed but **excluded from averaged metrics** (see Evaluation Run).

### Relevance Judgment  *(derived, per query)*

The mapping from a retrieved chunk to its relevance for one Golden Query.

- **v1: binary** — a chunk_id is relevant (in the Golden Query's `relevant` set)
  or not. Grade ∈ {0, 1}.
- **Extensible to graded** — the NDCG formula accepts a grade; a future schema
  addition (`"grade": n`) flows through without breaking binary labels.

### Retrieved Ranking  *(existing — `engine.QueryHit`, read-only)*

The ranked results the engine returns for a Golden Query's `query`. Eval consumes
this without modifying it:

| Field (existing) | Used by eval as |
|------------------|-----------------|
| `ChunkID` | join key against `relevant` |
| `Score` | tie-break (descending), then `ChunkID` lexicographic for determinism |
| `DocumentID`, `FilePath`, `Page`, `Content`, `Preview` | per-query breakdown display only |

### Evaluation Run  *(introduced — one pass over the dataset)*

| Field | Type | Notes |
|-------|------|-------|
| `mode` | enum | `offline` \| `ollama` (D3). |
| `embedder` | string | Model/embedder name used (`deterministic-hash` for offline). |
| `retrieval_mode` | string | The `engine.Query` mode forwarded (`hybrid` default). |
| `queries_run` | int | Golden queries actually scored. |
| `queries_skipped` | int | Queries excluded (zero relevant items, or no relevant chunk present in the vault). |
| `per_query` | list\<PerQueryResult\> | One entry per scored query (qid, retrieved chunk_ids, hit ranks, per-metric values). |
| `metrics` | MetricSet | Dataset-wide averages (below). |
| `verdict` | enum | `pass` \| `fail` (only meaningful vs. a baseline/tolerance). |

**Metric semantics** (research.md D4): Recall@k counts a relevant item missing
from top-k as a miss; a query with **zero** relevant items is **skipped**, not
scored 0 (FR-008). Ties broken by `ChunkID` for determinism.

### MetricSet  *(introduced — the headline numbers)*

| Field | Type |
|-------|------|
| `recall_at_5` | float [0,1] |
| `recall_at_10` | float [0,1] |
| `precision_at_5` | float [0,1] |
| `mrr` | float [0,1] |
| `ndcg_at_10` | float [0,1] |

Averaged over `queries_run`. Each is independently unit-testable with
hand-computed expected values.

### Baseline  *(introduced — committed snapshot for the gate)*

| Field | Type | Notes |
|-------|------|-------|
| `mode` | enum | `offline` (the gate runs offline). |
| `recorded_at` | string | Generation timestamp (informational). |
| `metrics` | MetricSet | The committed reference numbers. |

Stored at `testdata/golden/baseline.json`; regenerated deliberately via
`go-rag eval --record-baseline`, never silently (research.md D7).

## Relationships

```text
Golden Query 1──* Relevance Judgment (one per relevant chunk_id)
Golden Query ──runs──> Retrieved Ranking (existing engine.QueryHit)
(Golden Query, Retrieved Ranking) ──scores──> PerQueryResult ──aggregates──> MetricSet
MetricSet ──compared vs──> Baseline (tolerance) ──yields──> Evaluation Run verdict
```

## State transitions

Eval is **stateless and read-only** — no entity lifecycle. The only "state" is
the committed `Baseline` file, which transitions only via an explicit
`--record-baseline` command (append/replace, never auto).

## Validation rules (from requirements)

- FR-002: a committed `testdata/golden/v1.jsonl` MUST parse and every record
  MUST validate (unique id, non-empty query, valid chunk_id list).
- FR-006: an Evaluation Run MUST NOT produce any write to the measured vault
  (verified by a test that snapshots vault key-counts before/after).
- FR-008: zero-relevant-item queries and stale-label queries MUST appear in
  `queries_skipped` with a reason, never crash a metric average.
- SC-004: in `offline` mode the run MUST perform zero network dial-outs
  (verified by a test asserting the deterministic embedder makes no HTTP calls).
