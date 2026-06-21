---

description: "Task list for H09 — Reranker Error Surfacing"
---

# Tasks: Reranker Error Surfacing (H09)

**Input**: Design documents from `/specs/006-rerank-error-surfacing/`

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/query-response.md, quickstart.md — all present.

**Tests**: Included. The spec's Acceptance Scenarios are Given/When/Then test specs and CLAUDE.md mandates `go test ./...` green at all times; research.md D7 fixes the strategy (unit → parity → eval).

**Organization**: Tasks grouped by user story. MVP = Setup + Foundational + User Story 1 (operator sees failures via the CLI). US2 completes cross-transport parity; US3 adds opt-in retry.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: User story this task belongs to (US1/US2/US3)
- Exact file paths are in every task

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm a green baseline before touching the read path.

- [x] T001 Confirm baseline is green on a clean checkout: `make build && make vet && make test` (CGO_ENABLED=0, pure Go)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The engine/index core that ALL user stories depend on — fix both silent swallows, add the flag, add the log.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [x] T002 Change `SearchWithRerank` signature to `([]Hit, bool, error)` in `internal/index/retrieval.go`: (a) stop swallowing the candidate-retrieval error — propagate it as the `error` return (FR-009, fixes `hits, _ := r.Search(...)` at line 110); (b) stop swallowing the rerank error / score-length mismatch — return fallback-ordered `hits` + `rerankFailed=true` with a `nil` error (FR-001/002, fixes lines 129–133)
- [x] T003 Add `RerankFailed bool` to `QueryResult` in `internal/engine/types.go`; update the sole caller `engine.Query` in `internal/engine/query.go` to the new `SearchWithRerank` signature and thread `rerankFailed` into `QueryResult.RerankFailed`
- [x] T004 Add the FR-003 failure log in `internal/engine/query.go`: when `RerankFailed`, emit one stdlib `log.Printf` line `rerank failed: model=<m> candidates=<N> scores=<M> err=<e>` (error + model + candidate count + score count only; NEVER query text or candidate content)
- [x] T005 Update every existing caller/test of `SearchWithRerank` to the new signature so the suite compiles (`internal/engine/parity_test.go` and any other references)

**Checkpoint**: Foundation ready — failure is observable at the engine layer; user-story work can begin.

---

## Phase 3: User Story 1 — Know when reranking was skipped (Priority: P1) 🎯 MVP

**Goal**: An operator running a query can tell whether the results came from the reranker or from the unranked fallback — visible via the CLI, with the behavior proven by unit tests.

**Independent Test**: Run `go-rag query` against a vault whose reranker endpoint is dead → results are returned AND a warning prints to stderr AND a log line is emitted (containing no query text); a retrieval-stage failure returns a query error instead of silent empty results.

### Tests for User Story 1 (write first, watch them fail)

- [x] T006 [US1] Unit test: rerank error → returns fallback-ordered hits, `rerankFailed=true`, `nil` error, and a log line is emitted — `internal/index/retrieval_test.go`
- [x] T007 [US1] Unit test: score-count mismatch (`len(scores) != len(hits)`) → same verdict as T006 — `internal/index/retrieval_test.go`
- [x] T008 [US1] Unit test: candidate-retrieval error on the rerank path → returns non-nil `error` and `rerankFailed=false` (FR-009/SC-006) — `internal/index/retrieval_test.go`

### Implementation for User Story 1

- [x] T009 [P] [US1] CLI: emit a stderr warning line `warning: reranking failed; results are in fallback order (see log for details)` when `res.RerankFailed`; keep stdout JSON shape unchanged — `internal/cli/query.go`
- [x] T010 [US1] Integration check: run quickstart.md Scenario 1 (dead reranker → results + stderr warning + log; assert the log contains no query text)

**Checkpoint**: User Story 1 fully functional and independently testable — this is the MVP.

---

## Phase 4: User Story 2 — Consistent failure signal across every interface (Priority: P2)

**Goal**: The same failing-reranker query reports the failure identically over REST, gRPC, and MCP (CLI done in US1).

**Independent Test**: Issue one failing-reranker query over REST/gRPC/MCP → all report `rerank_failed`/warning; a healthy-reranker query over all three reports none (contracts/query-response.md).

### Implementation for User Story 2

- [x] T011 [P] [US2] REST: add `RerankFailed bool` (JSON `"rerank_failed"`) to `queryResponse` and map it from `res.RerankFailed` in `handleQuery` — `internal/rest/types.go`, `internal/rest/engine_adapter.go`
- [x] T012 [P] [US2] Proto: add `bool rerank_failed = 2;` to `message QueryResponse` and regenerate `proto/gen/` via the project's protoc/buf step — `proto/gorag.proto`, `proto/gen/`
- [x] T013 [US2] gRPC: map `res.RerankFailed` → the new proto field in `Adapter.Query` (depends on T012) — `internal/grpc/engine_adapter.go`
- [x] T014 [P] [US2] MCP: in `renderQuery`, prepend a `⚠ reranking failed; showing fallback-ordered results ...` line when `res.RerankFailed` — `internal/mcp/server.go`
- [x] T015 [US2] Parity test: a failing-reranker query reports the flag/warning identically across REST, gRPC, and MCP, and a healthy query reports none — extend `internal/engine/parity_test.go`
- [x] T016 [US2] Integration check: run quickstart.md Scenario 2 (cross-transport parity)

**Checkpoint**: User Stories 1 AND 2 both work — full cross-transport parity.

---

## Phase 5: User Story 3 — Optional recovery retry (Priority: P3)

**Goal**: An operator may opt into one retry with a larger candidate pool before falling back; off by default so the common path adds no latency.

**Independent Test**: With retry enabled and a flaky reranker, a query retries once (transient failure → recovered `RerankFailed=false`; second failure → fallback + `RerankFailed=true`); with retry disabled (default), no retry occurs.

### Implementation for User Story 3

- [x] T017 [US3] Config: add `RerankRetryOnFailure bool` (default `false`) to `Config` with accessor/setter parity to the existing `RerankModel`/`RerankCandidates` handling — `internal/config/config.go`
- [x] T018 [US3] Thread `cfg.RerankRetryOnFailure` (and a retry-pool cap) into `Retrieval` and implement retry in `SearchWithRerank` (`internal/index/retrieval.go`, wired from `internal/engine/query.go`): when enabled and the first rerank fails, re-retrieve with `min(pool*2, 200)` candidates and re-score once; success → `RerankFailed=false`, second failure → fallback + `RerankFailed=true`
- [x] T019 [US3] Tests: retry off (default) → no retry; retry on transient failure → recovered (`RerankFailed=false`); retry on second failure → fallback + `RerankFailed=true` — `internal/index/retrieval_test.go`
- [x] T020 [US3] Integration check: run quickstart.md Scenario 4 (retry behavior)

**Checkpoint**: All three user stories independently functional.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Docs, gates, and backlog hygiene.

- [x] T021 [P] Update any doc comments / MCP help text that imply rerank failure is handled silently (e.g., `go_rag_query` help in `internal/mcp/server.go`, `internal/rerank` package doc) — `internal/mcp/server.go`, `internal/rerank/rerank.go`
- [x] T022 Run the full gate green — `make vet`, `make lint`, `make test`; confirm `make test-eval` shows no recall@10 regression (SC-005); then mark H09 done (`[x]`, update annotation) in `RAG_BOOK_AUDIT_BACKLOG.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup; **BLOCKS all user stories** (signature change + flag + log).
- **User Stories (Phase 3–5)**: each depends on Foundational.
  - US1 → US2 → US3 in priority order (recommended), or US2/US3 in parallel after US1.
- **Polish (Phase 6)**: after the desired stories land.

### User Story Dependencies

- **US1 (P1)**: starts after Foundational; no dependency on other stories. Delivers the MVP.
- **US2 (P2)**: starts after Foundational; builds the transport surface on US1's engine flag. Independently testable.
- **US3 (P3)**: starts after Foundational; adds the config knob + retry path. Independently testable. Does not depend on US1/US2.

### Within Each User Story

- Tests written first and observed failing before the implementation they cover.
- Core behavior before transport surface.
- Integration check (quickstart scenario) last.

### Parallel Opportunities

- Phase 4: T011 (REST), T012 (proto), T014 (MCP) touch different files → parallel; T013 (gRPC) after T012; T015/T016 after T011–T014.
- Phase 6: T021 is independent of T022.

---

## Parallel Example: User Story 2

```bash
# Three independent transport surfaces can be implemented together:
Task: "T011 REST RerankFailed field in internal/rest/{types.go,engine_adapter.go}"
Task: "T012 proto rerank_failed field + regen in proto/gorag.proto + proto/gen/"
Task: "T014 MCP renderQuery warning in internal/mcp/server.go"
# Then, once the proto exists:
Task: "T013 gRPC adapter mapping in internal/grpc/engine_adapter.go"
```

---

## Implementation Strategy

### MVP First (Foundational + User Story 1)

1. Phase 1 Setup → green baseline.
2. Phase 2 Foundational → both swallows fixed, flag + log at the engine layer.
3. Phase 3 User Story 1 → CLI warning + proving unit tests.
4. **STOP and VALIDATE**: a dead-reranker query returns results + stderr warning + log (no query text); a retrieval failure errors. This alone closes the core H09 silent-failure gap.

### Incremental Delivery

5. Phase 4 User Story 2 → full REST/gRPC/MCP parity.
6. Phase 5 User Story 3 → opt-in retry.
7. Phase 6 Polish → gates green, eval regression clean, backlog checkbox.

---

## Notes

- [P] = different files, no dependency on an incomplete task.
- [Story] maps a task to its user story for traceability.
- The feature is docs-light at the code level: one boolean through the existing adapter pipeline plus one signature change. Most risk is in T002 (the core behavior split) and T018 (retry plumbing through the index layer) — both have dedicated tests.
- Commit after each task or logical group; stop at any checkpoint to validate a story independently.
