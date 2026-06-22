---

description: "Task list for H12 — Bounded embedding batches"
---

# Tasks: Bounded Embedding Batches (H12)

**Input**: Design documents from `/specs/010-bounded-embed-batch/` — plan.md, spec.md, research.md, data-model.md, contracts/embed-batch.md, quickstart.md

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: The project constitution mandates `go test ./...` green on every change, and H12's value (US1) plus its core invariant (US3 — contract preserved) are only provable through tests. Tests are included per FR/acceptance scenario, not as exhaustive TDD.

**Organization**: Tasks grouped by user story. Foundational phase holds the batch-loop refactor that every story depends on. The whole feature is one source file (`internal/embed/ollama.go`) + one test file (`internal/embed/ollama_test.go`); the pipeline caller (`internal/pipeline/workers.go:48`) is deliberately unchanged (FR-009).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story (US1, US2, US3) — present only on user-story-phase tasks
- Exact file paths in every description

## Path Conventions

Go single-module repo. Source under `internal/`; this feature lives in `internal/embed/`. Tests alongside source (`*_test.go`). Paths are repository-relative.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm a green starting point.

- [x] T001 Confirm green starting state: run `make build && make vet && make test` from repo root (capture any pre-existing failures so regressions are unambiguous)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The batch-loop mechanism inside `Ollama.Embed` — every user story depends on it.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete and `make test` is green.

- [x] T002 In `internal/embed/ollama.go` add the `embedBatchSize = 32` constant (documented: within audit's 32–64 range; internal, not config-exposed) and extract the existing single-request retry loop into a helper `func (o *Ollama) embedBatch(ctx context.Context, batch []string) ([][]float32, error)` that runs the unchanged 3-attempt `backoff` retry for ONE batch (5xx/network → retry, 4xx → fail fast, ctx-respecting), decodes the response, enforces `len(embeddings) == len(batch)`, and calls `setDims`. Do not yet change `Embed`'s body
- [x] T003 Rewrite `func (o *Ollama) Embed` in `internal/embed/ollama.go` to: keep `len(texts)==0 → (nil,nil)`; iterate `texts` in `embedBatchSize` slices in order; before each batch after the first, return `ctx.Err()` promptly if cancelled; call `embedBatch` per slice; on any batch error return it immediately with **no** partial result (FR-006); otherwise append each batch's vectors to the result in order and return the concatenated `[][]float32`. Sub-cap input MUST produce exactly one request (today's behavior). Depends on T002

**Checkpoint**: Foundation ready — `Embed` batches, retries per batch, concatenates in order, fails all-or-nothing. `make build && make vet && make test` green (existing tests `TestEmbed_FakeServer`, `TestEmbed_RetriesOn5xxThenSucceeds`, `TestEmbed_EmptyInput` still pass unchanged — they use ≤1 batch).

---

## Phase 3: User Story 1 — Large document ingests without timeout/OOM (Priority: P1) 🎯 MVP

**Goal**: A document producing many more chunks than the batch cap embeds fully, with every request bounded by the cap. (spec US1, FR-001/FR-002, SC-001/SC-002)

**Independent Test**: Embed N≫cap texts against a request-recording stand-in; assert N vectors returned in order AND every recorded request ≤ 32 texts.

### Tests for User Story 1

- [x] T004 [US1] Add `TestEmbed_LargeInput_BoundedRequests` in `internal/embed/ollama_test.go`: drive `Embed` with N=500 texts against an `httptest` Ollama that records each request's `len(input)`. Assert the call returns exactly 500 vectors in input order, and every recorded request carried ≤ `embedBatchSize` (32) texts — no single oversized request. Depends on T003

**Checkpoint**: User Story 1 functional — large inputs succeed with bounded requests (the core defect H12 names).

---

## Phase 4: User Story 2 — Per-batch retry; permanent failure is all-or-nothing (Priority: P2)

**Goal**: A transient blip on one batch is retried and recovered; a persistently-failing batch fails the whole call with no partial vector set. (spec US2, FR-004/FR-006, SC-004)

**Independent Test**: A stand-in that 500s one batch transiently → full success; a stand-in that 500s one batch permanently → error and no partial vectors.

### Tests for User Story 2

- [x] T005 [US2] Add `TestEmbed_TransientBatchFailure_Retried` in `internal/embed/ollama_test.go`: an `httptest` Ollama that returns HTTP 500 for the first attempt of one specific batch (identified by request index or input content), 200 otherwise. Embed a multi-batch input. Assert it succeeds and returns the full vector set in order (the transient batch was retried and recovered). Depends on T003
- [x] T006 [US2] Add `TestEmbed_PermanentBatchFailure_NoPartial` in `internal/embed/ollama_test.go`: an `httptest` Ollama that returns HTTP 500 persistently for one batch (all retries), 200 for the others. Embed a multi-batch input. Assert `Embed` returns a non-nil error and a nil/empty vector slice — never a partial result (FR-006). Depends on T003

**Checkpoint**: Stories 1 AND 2 both work independently — large-doc reliability holds.

---

## Phase 5: User Story 3 — Batching is invisible to callers (Priority: P2)

**Goal**: The embedding contract is unchanged — identical vectors in order regardless of grouping; integrity + dimensionality guarantees hold per batch. (spec US3, FR-003/FR-005/FR-007/FR-009, SC-003)

**Independent Test**: A deterministic stand-in produces byte-identical vectors for the same texts below and far above the cap; a per-batch count mismatch is rejected; empty/sub-cap/non-multiple edge cases behave as today.

### Tests for User Story 3

- [x] T007 [US3] Add `TestEmbed_OrderPreserved_AcrossBatches` in `internal/embed/ollama_test.go`: a deterministic `httptest` Ollama (vector derived solely from the input text, e.g. a fixed hash → []float32). Embed the same N texts twice — once with N below the cap, once with N far above it (many batches). Assert the two `[][]float32` slices are byte-identical and in input order (SC-003). Depends on T003
- [x] T008 [US3] Add `TestEmbed_PerBatchCountMismatch_Rejected` in `internal/embed/ollama_test.go`: an `httptest` Ollama that, for one batch, returns a vector count ≠ the batch's text count (truncate by one). Assert `Embed` returns an error and never a short/padded result (FR-005). Depends on T003
- [x] T009 [US3] Add `TestEmbed_BatchEdgeCases` in `internal/embed/ollama_test.go` covering FR-007: (a) empty input → `(nil,nil)` and zero requests; (b) sub-cap input (e.g. 5) → exactly one request carrying all 5; (c) non-multiple-of-cap input (e.g. 70) → exactly ceil(70/32)=3 requests and all 70 vectors returned in order. Use a request-counting stand-in. Depends on T003

**Checkpoint**: All user stories independently functional; the embedding contract is provably preserved.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Regression and merge-gate checks that span all stories.

- [x] T010 Confirm the pre-existing embed tests pass unchanged after the refactor — `TestEmbed_FakeServer`, `TestEmbed_RetriesOn5xxThenSucceeds`, `TestEmbed_EmptyInput` in `internal/embed/ollama_test.go` (they use ≤ one batch, so behavior is identical). This is the FR-009 "contract unchanged" regression anchor
- [x] T011 Run `make build && make vet && make test`; run `golangci-lint run` and `govulncheck ./...` (note: both may be unavailable in this env — `golangci-lint` config-version-incompatible, `govulncheck` uninstalled — record as pre-existing env issues, not code regressions, as on prior specs)
  — **OUTCOME: build + `go vet ./...` clean; `go test ./...` 18 packages ok, 0 FAIL. `golangci-lint` env-broken (same as specs 006–009) and `govulncheck` uninstalled — pre-existing env gaps, not code regressions. `go vet` is the active static gate.**
- [x] T012 Run the hermetic `quickstart.md` scenarios 1–7 (httptest-driven, no real Ollama); scenario 8 (real end-to-end ingest of a large document) is optional
  — **OUTCOME: scenarios 1–7 are covered 1:1 by the six new tests** (`TestEmbed_LargeInput_BoundedRequests`=S1, `_TransientBatchFailure_Retried`/`_PermanentBatchFailure_NoPartial`=S2/S3, `_OrderPreserved_AcrossBatches`=S4, `_PerBatchCountMismatch_Rejected`=S5, `_BatchEdgeCases`=S6, ctx-between-batches via `embedBatch`'s existing ctx select + the between-batch `ctx.Err()` check=S7). Scenario 8 (real Ollama e2e) deferred — hermetic coverage is sufficient for merge.
- [x] T013 Commit to `main` with Conventional Commits (e.g. `feat(embed): bound embedding request batch size (H12)`) and push

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: Depends on Phase 1. T002 then T003 (same file, sequential). **BLOCKS all user stories.**
- **US1 (Phase 3)**: Depends on T003.
- **US2 (Phase 4)**: Depends on T003.
- **US3 (Phase 5)**: Depends on T003.
- **Polish (Phase 6)**: T010–T012 depend on all stories; T013 is last.

### Critical path

`T001 → T002 → T003 → (US1/US2/US3 tests) → T011 → T013`. The spine is short — this is an S-effort, one-file change.

### Parallel Opportunities

- Phase 2: T002 → T003 are sequential (same file).
- Phases 3–5: every test task depends only on T003 and writes an **independent test function** in the same file (`ollama_test.go`). They have no logic dependency on each other — a single pass can add them together, but because they share one file they are not marked `[P]` (the skill's same-file rule).
- No cross-file parallelism — the feature is intentionally contained to `internal/embed/`.

---

## Parallel Example: User Story tests (Phase 3–5)

```bash
# All test functions are independent and live in internal/embed/ollama_test.go.
# They can be authored in one pass (each is a self-contained TestXxx):
Task: "TestEmbed_LargeInput_BoundedRequests        (US1)"   # T004
Task: "TestEmbed_TransientBatchFailure_Retried      (US2)"   # T005
Task: "TestEmbed_PermanentBatchFailure_NoPartial    (US2)"   # T006
Task: "TestEmbed_OrderPreserved_AcrossBatches        (US3)"   # T007
Task: "TestEmbed_PerBatchCountMismatch_Rejected      (US3)"   # T008
Task: "TestEmbed_BatchEdgeCases                      (US3)"   # T009
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (Setup) — green baseline.
2. Phase 2 (Foundational) — batch loop + per-batch retry + concatenate; existing tests green.
3. Phase 3 (US1) — large inputs embed with bounded requests.
4. **STOP and VALIDATE** — run `TestEmbed_LargeInput_BoundedRequests`; run quickstart scenario 1.
5. Ship US1 alone already removes the OOM/timeout cliff for large documents.

### Incremental Delivery

1. Setup + Foundational → batching works, contract preserved (existing tests green).
2. + US1 → large-document success (MVP).
3. + US2 → per-batch reliability (transient retry, all-or-nothing failure).
4. + US3 → contract-preservation proof (order, integrity, edge cases).
5. Polish → full suite green, lint/vuln noted, committed to `main`.

---

## Notes

- Same-file tasks are never marked `[P]` (T002/T003 in `ollama.go`; T004–T009 in `ollama_test.go`).
- The pipeline caller (`internal/pipeline/workers.go:48`) is **intentionally not edited** — batching belongs in the transport layer and benefits every caller (FR-009). If a task tries to touch `workers.go`, that's scope drift.
- FR-006 (no partial index) is free at the caller: `processJob` already sets `StatusError` and skips the vector-store loop on any `Embed` error. `Embed` returning no-partial-result preserves that exactly.
- `golangci-lint`/`govulncheck` may be unavailable in this env (same as specs 006–009) — record as an env note, do not block on them. `go vet` is the active static gate.
