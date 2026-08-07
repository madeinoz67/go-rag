# Phase 1 — Citation Contract (H21 / spec 023)

> The stable contract for clients that cite go-rag chunks in LLM-generated responses
> (book §7.3 — "require the model to cite sources… verify those citations"). Every field
> here is surfaced on every transport (CLI/REST/gRPC/MCP).

## The citation anchor: `chunk_id`

`chunk_id` is the **canonical citation anchor**. It is SHA-256 content-addressed (over the
chunk text + its document ID + ordinal) — stable across re-ingestion of unchanged content
(idempotent, Constitution II). A client that cites `chunk_id` can **verify** the citation
by re-querying or by checking the stored chunk.

## Fields on every query hit

| Field | Type | Citation role |
|---|---|---|
| `chunk_id` | string (SHA-256 hex) | **Canonical anchor** — stable, verifiable, content-addressed |
| `chunk_index` | int (0-based) | **Human-readable ordinal** within the source document ("Doc X, chunk 3") |
| `document_id` | string (SHA-256 hex) | **Doc-level handle** — groups chunks from the same source |
| `file_path` | string | **Human-display provenance** — the source file path |
| `page` | int (0 if not paginated) | **Page-level granularity** (PDF only) |
| `score` | float [0,1] | **Calibrated relative confidence** — top hit = 1.0; see below |

## Score semantics

- `score` is **normalized to [0,1] within the result set** (top hit = 1.0; others scaled
  proportionally).
- **`--threshold` is relative-within-result**: 0.5 means "top half of THIS result set," NOT
  "50% confident" across queries. Absolute confidence requires calibration against a golden
  dataset (the H02 eval harness provides the tooling).
- When a reranker is active, `score` is the reranker's output (already 0..1, a
  cross-encoder's max-relative relevance score) — a stronger signal than the RRF baseline.

## Multi-chunk citations

"Doc X, ¶2" resolves to `(document_id=X, chunk_index=2)` → a deterministic, stable
reference to a specific chunk within a document. Re-ingestion of unchanged content yields
the same `chunk_id` + `chunk_index`.

## Stability guarantees

- `chunk_id` + `chunk_index` are stable across re-ingestion of **unchanged** content
  (Constitution II — idempotent).
- If the **chunker** changes (size/overlap), `chunk_index` shifts but `chunk_id` stays
  stable (content-addressed) — the chunk may split/merge, changing its ordinal.
- `score` is NOT stable across queries (it's relative to the result set).
