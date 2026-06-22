---

description: "Task list for H05 — Query transformation seam + normalization"
---

# Tasks: Query Transformation Seam + Normalization (H05)

**Input**: Design documents from `/specs/012-query-transform/` — plan.md, spec.md, research.md, data-model.md, contracts/query-transform.md, quickstart.md

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: The constitution mandates `go test ./...` green on every change, and H05's value (normalization correctness + a live seam) is only provable through tests; the H02 eval harness is the no-regression gate (SC-002). Tests included per FR/acceptance scenario.

**Organization**: Tasks grouped by user story. Foundational phase builds the seam (interface + default + engine wiring); the stories verify it (US1=normalization, US2=seam-live, US3=no-regression).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story (US1, US2, US3) — present only on user-story-phase tasks
- Exact file paths in every description

## Path Conventions

Go single-module repo. This feature lives in `internal/index` (the seam) and `internal/engine` (wiring). Tests alongside source. Paths are repository-relative.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm a green starting point.

- [x] T001 Confirm green starting state: run `make build && make vet && make test` from repo root (capture any pre-existing failures so regressions are unambiguous)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The transformation seam — the `QueryTransformer` interface + pure default normalizer in `internal/index`, wired into `Engine.Query` so every retrieval path gets the transformed query.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete and `make test` is green.

- [x] T002 [P] Create `internal/index/transform.go`: define the `QueryTransformer` interface — `Transform(ctx context.Context, query string) ([]string, error)` (returns one-or-more to future-proof multi-query, FR-005); add the pure default `NormalizingTransformer` (returns `[]string{normalizeQuery(q)}`, error if the result is empty — FR-006); and `normalizeQuery(s string) string` doing Unicode case-fold (`strings.ToLower`) + collapse whitespace runs to one space + `strings.TrimSpace`. Mirror the `Reranker` pattern (interface in `internal/index`, no Ollama import). No dependency on incomplete tasks
- [x] T003 Wire the seam into the engine: in `internal/engine/engine.go` add an unexported field `qTransformer index.QueryTransformer` and default it to `index.NormalizingTransformer{}` in both `NewWithDB` and `NewWithEmbedder`; in `internal/engine/query.go` replace the top-of-`Query` empty-check (`if req.Query == ""`) with a transform step — `transformed, err := e.qTransformer.Transform(ctx, req.Query)` (return the error, which covers empty-after-normalization, FR-006), then `req.Query = transformed[0]` so `checkEmbeddingMismatch`, the H07 query-prefix `queryEmbed`, and `SearchWithRerank` all use the normalized query. Depends on T002

**Checkpoint**: Foundation ready — every query is normalized through the seam before retrieval; existing tests still pass (results unchanged for clean queries). `make build && make vet && make test` green.

---

## Phase 3: User Story 1 — Normalization works (Priority: P1) 🎯 MVP

**Goal**: Cosmetic query variants (case/whitespace) retrieve identically; normalization is idempotent, Unicode-safe, and handles empty. (spec US1, FR-002/FR-006/FR-007/FR-008, SC-001)

**Independent Test**: `normalizeQuery("Some Term") == normalizeQuery("  some   term ")`; idempotent; non-ASCII preserved; whitespace-only → error.

### Tests for User Story 1

- [x] T004 [US1] Add `internal/index/transform_test.go`: unit-test `normalizeQuery` — (a) cosmetic equivalence (`"Some Term"` == `"  some   term "` == `"SOME TERM"`); (b) idempotent (`norm(norm(q)) == norm(q)`); (c) Unicode-safe (accents lowercased, CJK preserved — `"Café"` → `"café"`, `"数据 检索"` whitespace-collapsed only); (d) `NormalizingTransformer.Transform` returns `[]string{normalized}` and an error when the normalized result is empty (`"   "` → err). Depends on T002

**Checkpoint**: User Story 1 functional — normalization is correct and robust.

---

## Phase 4: User Story 2 — The seam is live (custom transformer honored) (Priority: P2)

**Goal**: A caller-supplied transformer is actually used at retrieval time — the seam is a real extension point, not theoretical. (spec US2, FR-003/FR-004, SC-003)

**Independent Test**: Set `e.qTransformer` to a fake that appends a synonym; confirm the results change.

### Tests for User Story 2

- [x] T005 [US2] Add `internal/engine/query_transform_test.go` (package `engine`, so it can set the unexported `e.qTransformer`): build an engine with a fake embedder (`NewWithEmbedder`) over a temp DB, ingest a doc whose content matches a synonym but is a weak match for the bare query, set `e.qTransformer` to a transformer that appends that synonym (e.g. `"auth"` → `"auth credential"`), and assert the result set differs from the default-normalizer run — proving the custom transformer is honored. Depends on T003

**Checkpoint**: User Story 2 functional — the seam works end-to-end; future HyDE/multi-query can plug in here.

---

## Phase 5: User Story 3 — No regression; parity holds (Priority: P2)

**Goal**: Normalization does not regress retrieval quality (SC-002) and results stay identical across transports. (spec US3, FR-001/FR-009, SC-002)

**Independent Test**: eval harness green; cross-transport parity tests green.

### Tests for User Story 3

- [x] T006 [US3] Run the safety gate: `make test-eval` (recall@10/MRR no worse than baseline — SC-002; the harness queries are already clean so normalization is a no-op there, and the gate catches accidental breakage) AND the existing cross-transport parity tests (`internal/engine/parity_test.go`, spec 003) pass unchanged — the transform runs in the shared engine path so CLI/REST/gRPC/MCP stay identical (FR-008). Depends on T003

**Checkpoint**: All user stories independently functional; the change is safe to ship.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Merge gate + backlog hygiene.

- [x] T007 Run `make build && make vet && make test && make test-eval`; run `golangci-lint run` and `govulncheck ./...` (note: both may be unavailable in this env — `golangci-lint` config-version-incompatible, `govulncheck` uninstalled — record as pre-existing env issues, not code regressions, as on prior specs)
- [x] T008 Run the hermetic `quickstart.md` scenarios 1–6 (deterministic embedder, no real Ollama); scenario 7 (real model) is optional
- [x] T009 Mark **H05** complete in `RAG_BOOK_AUDIT_BACKLOG.md`: change the H05 row `- [ ]` → `- [x]` and append a `✅ COMPLETE (spec 012): …` annotation summarizing the seam (QueryTransformer interface in internal/index mirroring Reranker) + the default normalizer + the gates (matching the prior rows' style). Depends on T004–T006 green and T007 passing
- [x] T010 Commit to `main` with Conventional Commits (e.g. `feat(retrieval): query transformation seam + normalization (H05)`) including the backlog update, and push

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: T002 (independent) → T003 (depends T002). **BLOCKS all user stories.**
- **US1 (Phase 3)**: T004 depends on T002.
- **US2 (Phase 4)**: T005 depends on T003.
- **US3 (Phase 5)**: T006 depends on T003.
- **Polish (Phase 6)**: T007/T008 depend on all stories; T009 depends on green stories + gate; T010 is last.

### Critical path

`T001 → T002 → T003 → (US1/US2/US3) → T007 → T009 → T010`. Short — this is a contained, single-seam change.

### Parallel Opportunities

- Phase 2: T002 (`internal/index/transform.go`) is independent of everything → can start immediately; T003 (engine) follows.
- Phases 3–5: the three test tasks target different files (`transform_test.go`, `query_transform_test.go`, the eval/parity gate) and depend only on T002/T003 — logically independent, runnable together once the foundation lands (not marked `[P]` only where they share a file, which they don't here).

---

## Parallel Example: the verification matrix (Phases 3–5)

```bash
# Once T002 + T003 land, the three verifications are independent:
Task: "normalizeQuery unit tests in internal/index/transform_test.go"     # T004 (US1)
Task: "custom-transformer-honored in internal/engine/query_transform_test.go"  # T005 (US2)
Task: "eval gate + parity unchanged (make test-eval + parity_test.go)"    # T006 (US3)
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (Setup) — green baseline.
2. Phase 2 (Foundational) — interface + normalizer + engine wiring; existing tests green.
3. Phase 3 (US1) — normalization correctness proven.
4. **STOP and VALIDATE** — run `transform_test.go`; run quickstart scenarios 1–3.
5. US1 alone delivers the normalization + the seam exists; US2/US3 prove the seam is live and safe.

### Incremental Delivery

1. Setup + Foundational → seam + default normalizer wired; results unchanged.
2. + US1 → normalization correct & robust (MVP).
3. + US2 → seam proven live (custom transformer honored).
4. + US3 → no-regression gate green; parity holds.
5. Polish → gate green, backlog marked, committed to `main`.

---

## Notes

- Same-file tasks are never marked `[P]` (T003 touches `engine.go` + `query.go` sequentially; the test tasks are in separate files).
- **No transport/CLI/config/proto change** — `Engine.Query`'s contract is unchanged; the seam is internal (Principle V). Any task that touches a transport adapter is scope drift.
- **No new public constructor** for the transformer (YAGNI) — the default is wired; the US2 test sets `e.qTransformer` in-package. A constructor option is added when the first *production* custom transformer (HyDE) exists.
- The **vector query/document case asymmetry** (normalize query only, documents embedded verbatim) is accepted and GATED by SC-002; a consistent doc-side normalization (corpus re-embed) is explicitly out of scope.
- `golangci-lint`/`govulncheck` may be unavailable in this env (same as specs 006–011) — record as an env note, do not block. `go vet` + `-race` are the active gates.
