# Tasks: Score Calibration + Citation Contract

**Input**: Design documents from `/specs/023-score-cite-contract/`

**Prerequisites**: plan.md ✅, spec.md ✅ (US1–US2), research.md ✅ (D1–D5), data-model.md ✅, contracts/ ✅

**Tests**: Included — normalization + threshold test + eval recall@10 unchanged.

**Organization**: US1 (P1) = score normalization MVP; US2 (P2) = chunk_index + citation docs. Go project; stdlib only (no new dep).

## Format: `[ID] [P?] [Story] Description`

---

## Phase 1: User Story 1 — Calibrated scores (Priority: P1) 🎯 MVP

**Goal**: normalize hit scores to [0,1] within a result set (top = 1.0); threshold on the normalized scale; reranker scores used as-is.

**Independent Test**: a query returning N hits → top hit score = 1.0; `--threshold 0.5` filters correctly.

- [X] T001 [US1] Add score normalization in `Engine.Query` (`internal/engine/query.go`) — after the hit-building loop, before the return: if NOT reranked (reranker not configured or RerankFailed), divide every hit's score by the top hit's score (min-max → top=1.0, monotonic); if reranked, the scores are already 0..1 from H09's parseScores — leave as-is
- [X] T002 [US1] Move the threshold filter (`if h.Score < req.Threshold`) to AFTER normalization (`internal/engine/query.go`) — so threshold acts on the normalized [0,1] scale (currently it's applied pre-normalization on raw RRF scores)
- [X] T003 [US1] Normalization test in `internal/engine/` — a query returning multiple hits: top hit's score = 1.0; scores decrease monotonically; `--threshold 0.5` filters correctly; ranking order unchanged (ties preserved)

**Checkpoint**: US1 — scores calibrated to [0,1], threshold on the right scale.

---

## Phase 2: User Story 2 — Citation contract + chunk_index (Priority: P2)

**Goal**: surface `chunk_index` on every hit; document the citation contract.

**Independent Test**: a hit carries `chunk_index` (0-based ordinal); the citation contract is documented.

- [X] T004 [P] [US2] Add `ChunkIndex int` to `QueryHit` (`internal/engine/types.go`) + populate it in the hit-building loop of `Engine.Query` (`internal/engine/query.go`) from the looked-up chunk's existing `ChunkIndex` field (`model.Chunk.ChunkIndex`)
- [X] T005 [P] [US2] Surface `chunk_index` on the REST DTO (`internal/rest/types.go` `queryHit` struct + `internal/rest/engine_adapter.go` `toQueryHits`)
- [X] T006 [P] [US2] Add `int32 chunk_index = 8` to the proto `QueryHit` message (`proto/gorag.proto`) + regen `proto/gen` + surface in `internal/grpc/engine_adapter.go`
- [X] T007 [P] [US2] Surface `chunk_index` on CLI render (`internal/cli/query.go` `queryResult` struct + `toPoisonDTO`-style mapping) + MCP `renderQuery` (`internal/mcp/server.go`)
- [X] T008 [US2] Write `docs/citation-contract.md` (FR-005) — `chunk_id` = canonical anchor (SHA-256, stable); `chunk_index` = ordinal; `document_id` = doc handle; `score` = normalized [0,1] relative confidence; `threshold` = relative-within-result semantics

**Checkpoint**: US2 — chunk_index on every transport; citation contract documented.

---

## Phase 3: Polish & Cross-Cutting Concerns

- [X] T009 Final gates — `go build ./...`, `go vet ./...`, `go test -race -cover ./...` green; `make test-eval` recall@10 unchanged (ranking preserved — SC-006); `go.mod` unchanged (no new dep); run `quickstart.md` scenarios 1–5

---

## Dependencies & Execution Order

- **US1 (Phase 1)**: no deps — start immediately. **MVP.**
- **US2 (Phase 2)**: independent of US1 (chunk_index is orthogonal to score normalization); the 4 transport tasks (T005–T007) are parallel (different files).
- **Polish (Phase 3)**: after both stories.

### Parallel Opportunities

- US2: T005 (REST) ∥ T006 (gRPC/proto) ∥ T007 (CLI/MCP) ∥ T008 (docs) — all different files.

---

## Implementation Strategy

### MVP First (US1 only)

1. T001 (normalize) + T002 (threshold move) + T003 (test)
2. **STOP and VALIDATE**: query → top hit score = 1.0; threshold works on [0,1]

### Incremental Delivery

1. US1 → calibrated scores (**MVP**)
2. + US2 → chunk_index + citation docs
3. Polish → gates

---

## Notes

- Use **tokensave** before modifying: `tokensave_callers` on `QueryHit.Score` to confirm all transport readers; `tokensave_impact` on `Engine.Query` to confirm the normalization insertion point doesn't break callers.
- `[P]` = different files; `[Story]` maps to spec.md.
- Constitution: II (chunk_id stable — content-addressed), V (normalized score + chunk_index on all transports).
- **Stdlib only** — the normalization is simple division; no new dep (Constitution III).
