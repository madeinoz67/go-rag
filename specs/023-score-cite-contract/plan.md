# Implementation Plan: Score Calibration + Citation Contract

**Branch**: `023-score-cite-contract` *(single-author repo — commits directly to `main`)* | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature spec from `/specs/023-score-cite-contract/spec.md` — backlog item **H21** (P2, S).

## Summary

Normalize query hit scores to [0,1] within each result set (top = 1.0, monotonic) so
clients can set meaningful thresholds and judge relevance; surface `chunk_index` (the
existing 0-based ordinal on `model.Chunk`) on every hit; document the citation contract
(`chunk_id` = canonical anchor, `chunk_index` = ordinal, threshold = relative-within-result).
S-effort: a post-retrieval normalization pass + one field added to QueryHit + docs. No new
dependency; ranking order unchanged (eval recall@10 preserved).

## Technical Context

**Language/Version**: Go 1.22+ (pure Go, `CGO_ENABLED=0`).

**Primary Dependencies**: existing only. **No new dependency** (Constitution III).

**Storage**: no change — `chunk_index` is already stored on `model.Chunk` (field
`ChunkIndex int`); just surfaced on `QueryHit`. Score normalization is a compute-in-memory
step post-retrieval.

**Testing**: `go test -race -cover ./...`; a normalization unit test (top=1.0, monotonic,
ties preserved) + a threshold-on-normalized test + `make test-eval` (recall@10 unchanged —
ranking order preserved, SC-006).

**Performance Goals**: normalization is O(N) over the hit set (~5 hits) — negligible.

**Constraints**: ranking order MUST NOT change (only score values); threshold acts on
normalized scores.

## Constitution Check

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I | Local-First | ✅ PASS (N/A) | Pure compute; no network. |
| II | Content-Addressed Identity | ✅ PASS | `chunk_id` (the citation anchor) is SHA-256 content-addressed — stable across re-ingestion. `chunk_index` is the existing ordinal, also stable for unchanged content. No identity change. |
| III | Pure Go | ✅ PASS | Stdlib only (the normalization is simple arithmetic). |
| IV | Async-After-ACK | ✅ PASS (N/A) | Query-path only; no write-path change. |
| V | Extension by Interface | ✅ PASS | Normalized score + `chunk_index` surfaced consistently on all 4 transports (CLI/REST/gRPC/MCP). |

**No violations → Complexity Tracking empty.**

## Project Structure

```text
internal/
├── engine/
│   ├── query.go         # MODIFY — normalize scores post-retrieval (after hit-building, before return);
│   │                    #   apply threshold on normalized scores; add chunk_index to QueryHit
│   ├── types.go         # MODIFY — add ChunkIndex int to QueryHit
│   └── query_test.go (or a new test)  # ADD — normalization + threshold test
├── cli/query.go         # MODIFY — render chunk_index in text output; note normalized scores
├── rest/                # MODIFY — add chunk_index to the REST queryHit DTO
├── grpc/                # MODIFY — add chunk_index to proto QueryHit + regen
├── mcp/                 # MODIFY — surface chunk_index in renderQuery
proto/gorag.proto        # MODIFY — add chunk_index to QueryHit message
docs/citation-contract.md # NEW — the citation contract (FR-005)
```

**Structure Decision**: normalization is a single pass in `Engine.Query`'s hit-building
loop (after `lookupChunk`, before the return). `chunk_index` is added to `QueryHit` +
populated from the looked-up chunk's existing `ChunkIndex` field. Each transport surfaces
both. No new packages; all changes are small field additions + one arithmetic pass.

## Complexity Tracking

> Empty — no Constitution violations.
