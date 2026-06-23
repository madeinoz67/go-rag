# Phase 0 — Research: Score Calibration + Citation Contract (H21)

> Resolves the design questions (spec 023). The spec had no NEEDS CLARIFICATION.

## D1 — Normalization method: min-max within the result set

**Decision**: min-max normalization. After the hit-building loop (before returning), divide
each hit's score by the top hit's score: `normalized = score / topScore`. The top hit
becomes 1.0; all others scale proportionally (≤1.0). Ties stay equal.

**Rationale**: parameter-free, deterministic, preserves ranking order, and gives a client a
[0,1] number they can threshold against. The book §8.2 treats score as a confidence proxy
for ambiguity detection / escalation — min-max gives a relative confidence within the result
set (honest: it's NOT absolute confidence across queries).

**Alternatives rejected**: softmax (temperature parameter — introduces a tuning knob without
a calibration dataset); Platt scaling / logistic calibration (needs a labeled calibration
set — out of scope for a local tool); z-score normalization (can produce negative values —
not a natural [0,1] scale for thresholding).

## D2 — Threshold semantics: relative-within-result

**Decision**: `--threshold` applies to the NORMALIZED score (0..1). Documented as
**relative-within-result** — "0.5 means top half of THIS result set," NOT "50% confident."
Absolute confidence would require a calibrated probability model (out of scope).

**Rationale**: honest framing. A client that needs absolute confidence should use the eval
harness (H02) to calibrate thresholds against a golden dataset, then set the threshold
appropriately. go-rag provides the calibrated [0,1] scale; the client owns the absolute
interpretation.

## D3 — Reranked results: use reranker scores as-is

**Decision**: when a reranker is active, its scores (already normalized to 0..1 by H09's
`parseScores` max-relative normalization) ARE the normalized scores. No additional min-max
pass. The normalization pass applies ONLY to non-reranked (RRF-fused) results.

**Rationale**: double-normalizing (RRF min-max → then reranker scores) would be wrong; the
reranker's scores are already the "right" calibrated values. The engine checks: if
`RerankFailed=false` (rerank succeeded), the hits already carry reranker scores → use them.
If rerank was not configured or failed (fallback), apply min-max to the RRF scores.

## D4 — chunk_index: the existing ordinal, surfaced

**Decision**: add `ChunkIndex int` to `QueryHit` (types.go). Populate from the looked-up
chunk's existing `ChunkIndex` field (`model.Chunk.ChunkIndex`, already stored at ingest
time, 0-based ordinal within the document). Surface on all 4 transports.

**Rationale**: `ChunkIndex` is already on `model.Chunk` (set at ingest in `processFile`,
line ~240: `ChunkIndex: i`). It's just not currently projected onto `QueryHit`. Adding it
is a one-field addition + one lookup (the chunk is already loaded via `lookupChunk` in the
hit-building loop).

## D5 — Citation contract (docs)

**Decision**: a `docs/citation-contract.md` documenting:
- `chunk_id` — the canonical citation anchor (SHA-256 content-addressed, stable across
  re-ingestion of unchanged content; idempotent — Constitution II).
- `chunk_index` — the 0-based ordinal within the source document (human-readable).
- `document_id` — the doc-level handle.
- `file_path` + `page` — human-display provenance.
- `score` — the normalized [0,1] score within the result set (relative confidence).
- `threshold` — relative-within-result semantics.

**Rationale**: the audit §1.5 says the contract is "undocumented." This is a documentation
deliverable (FR-005), not code.

## FR mapping

| Spec item | Resolved by |
|---|---|
| FR-001 normalize [0,1] | D1 |
| FR-002 threshold on normalized | D2 |
| FR-003 reranker scores as-is | D3 |
| FR-004 chunk_index on hit | D4 |
| FR-005 document citation contract | D5 |
| FR-006 surfaced on all transports | D4 (field addition) |
| FR-007 ranking order unchanged | D1 (min-max preserves order) |

Ready for Phase 1.
