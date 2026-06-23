# Phase 1 — Data Model: Score Calibration + Citation Contract (H21)

> No new entities — two field additions to `QueryHit` (a normalized score is computed
> in-memory; `chunk_index` is surfaced from the existing `model.Chunk.ChunkIndex`).

## Changes to QueryHit (internal/engine/types.go)

| Field | Type | Change | Notes |
|-------|------|--------|-------|
| `Score` | float64 | **value changes** (not the type) | now carries the normalized [0,1] score (top = 1.0), not the raw RRF value |
| `ChunkIndex` | int | **NEW field** | 0-based ordinal of the chunk within its source document (from `model.Chunk.ChunkIndex`) |

## Normalized score computation

In `Engine.Query`, after the hit-building loop (which collects hits with raw RRF/reranker
scores), apply normalization **before** the threshold filter + return:

```text
if NOT reranked:
    topScore = max(hit.Score for hit in hits)
    for hit in hits:
        hit.Score = hit.Score / topScore   // top = 1.0, rest scaled
if reranked (RerankFailed=false):
    // reranker scores already 0..1 (H09 parseScores) — use as-is, no normalization
apply threshold on normalized scores
```

Edge cases:
- Single hit → topScore = hit.Score → normalized = 1.0.
- All tied → all become 1.0 (equal).
- Zero hits → no normalization (empty result).
- Reranked → scores already 0..1 → no pass.

## Validation rules

- V1: the top hit's normalized score MUST be 1.0.
- V2: normalized scores MUST be monotonically non-increasing (or equal for ties).
- V3: the ranking order MUST NOT change (normalization is a monotonic transform).
- V4: `--threshold` MUST filter on the normalized score.
- V5: `chunk_index` MUST match the stored `model.Chunk.ChunkIndex` for the same chunk.

## State transitions

None — score normalization is a stateless in-memory transform; `chunk_index` is read from
the already-stored chunk record. No persisted state change.
