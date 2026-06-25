# Tasks: Migration Dry-Run

**Input**: Design documents from `/specs/028-migrate-dry-run/` (audit finding **H24**, Phase 6 §1.8).

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/api.md, quickstart.md — all present.

**Tests**: INCLUDED — this feature's definition-of-done is verification (read-only guarantee FR-003, no-backend FR-004, parity FR-006, preview==execution FR-008 are all assertable). The real-`Migrate` behaviour must be provably unchanged.

**Organization**: US1 (the dry-run itself, CLI) is the MVP; US2 (actionable estimate) is the decision-usefulness layer; US3 (parity across transports) makes it uniform. The engine plan (Phase 2) underpins all three.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- File paths are project-relative (Go module `github.com/madeinoz67/go-rag`).

## Path Conventions (Go)

- Single binary, single entrypoint `cmd/go-rag` (untouched).
- Core work in `internal/engine` (the plan method + types + `Migrate` refactor); one preview path each in `internal/cli`, `internal/rest`, `internal/grpc`, `internal/mcp`, and `proto/`. No storage/config/on-disk change.

## ⚠️ Build-order note (priority vs dependency)

This feature's value is a **read-only guarantee**, so verification (US1/US2/US3
tests) is part of the deliverable, not optional:

- The **engine plan (Phase 2)** is the single source of truth — every transport
  and the real `Migrate` reuse it. Nothing else can start until it exists.
- **US1 (CLI dry-run + read-only/no-backend/preview==execution tests)** is the MVP.
- **US2 (estimate fields)** is delivered by the Phase-2 struct; its task is the
  test proving the estimate is populated and labelled.
- **US3 (REST/gRPC/MCP adapters)** are mutually parallel after Phase 2; the parity
  test gates them.

---

## Phase 1: Setup (Baseline)

**Purpose**: Record the pre-feature green baseline (US1/US3 "nothing changed" claims are measured against it).

- [ ] T001 Run `make build vet test` (or `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`) on `main`; confirm green and record as the pre-feature baseline. No code changes.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The single read-only plan computation + the `Migrate` refactor that makes preview == execution. Blocks every story.

**⚠️ CRITICAL**: Blocks US1, US2, US3.

- [ ] T002 Create `internal/engine/migrate_plan.go`: the `MigrationPlan`, `ModelCount{Model,Count,Stale}`, `DimCount{Dim,Count}`, and `Estimate{StaleEmbeddings,ModelChange,MixedCorpus,Note}` types, plus `Engine.MigratePlan(ctx) (*MigrationPlan, error)` — a pure derive from `pipeline.EmbeddingModelStats(e.db)` + `engine.CorpusProfile(e.db)` + `e.cfg.EmbeddingModel`. Strictly read-only: no `Embedder`, no `flushCaches`, no `ReprocessAll`, no `refreshBaseline`, no epoch bump (R1/R5). `Estimate.Note` states it is an estimate, not a time guarantee (R4). data-model.md §1/§3.
- [ ] T003 Refactor `Engine.Migrate` in `internal/engine/ingest.go` to call `MigratePlan` first and proceed only when `StaleTotal > 0` (replacing the inline stats/stale pre-amble). The real mutate path (`flushCaches` → `ReprocessAll` → `refreshBaselineAfterMigrate`) is unchanged; only the plan computation is shared, so the preview and execution can never disagree (FR-008). Depends T002.

**Checkpoint**: The plan exists and `Migrate` reuses it — story work can begin.

---

## Phase 3: User Story 1 — Preview a migration without triggering it (Priority: P1) 🎯 MVP

**Goal**: An operator can run `migrate --dry-run`, see the full plan, and exit with nothing re-embedded — succeeding even with no embedding backend.

**Independent Test**: `migrate --dry-run` on a stale corpus prints the plan and exits; corpus/cache/baseline/epoch unchanged; succeeds with Ollama unreachable (SC-001/002).

### Implementation for User Story 1

- [ ] T004 [US1] Add the `--dry-run` flag to `migrate` in `internal/cli/migrate.go`: when set, open the DB, build an engine (or call `Engine.MigratePlan` directly), render the plan human-readably (target model, per-source counts with `<- stale` markers, dim distribution, consistency, estimate), and `return` without re-embedding. Replace the existing hand-rolled inline preview so plain `migrate` renders the *same* plan first, then proceeds (retire the duplicate logic — R1). Exit 0 in all cases (empty/clean/mixed). Depends T002.
- [ ] T005 [US1] Create `internal/engine/migrate_plan_test.go` asserting: (a) **read-only** — `MigratePlan` leaves embedding counts/contents, caches, baseline, and index epoch byte-identical before/after (FR-003); (b) **no-backend** — it succeeds and returns a correct plan with the embedder unreachable / never constructed (FR-004); (c) **deterministic** — repeated calls identical (FR-007). Use an isolated temp DB. SC-001/SC-002. Depends T002.
- [ ] T006 [US1] Add a **preview == execution** test (`internal/engine/migrate_plan_test.go`): build a corpus with known stale embeddings, capture `MigratePlan().StaleTotal`, run a real `Migrate`, and assert the count actually re-embedded equals the preview's stale total; after migrate, `MigratePlan()` reports `StaleTotal == 0` (FR-008 / SC-005). Depends T003, T005.

**Checkpoint**: The dry-run works end-to-end on the CLI and is proven read-only, backend-free, and execution-faithful.

---

## Phase 4: User Story 2 — The cost estimate is actionable (Priority: P2)

**Goal**: The plan carries enough to decide "migrate now or later" — stale count, model change, mixed-corpus flag, stored dimensionality, all labelled an estimate.

**Independent Test**: On a mixed corpus the plan reports stale count + dims + `consistent=false` + a labelled estimate; on a clean corpus, zero stale + `consistent=true` (SC-003).

### Implementation for User Story 2

- [ ] T007 [US2] Extend `internal/engine/migrate_plan_test.go` to assert `MigrationPlan` populates the cost fields correctly: on a **mixed** corpus (two models/dims) — `StaleTotal` > 0, `Sources[]` with correct `Stale` flags, `Dimensions[]` reflecting the stored distribution, `Consistent == false`, and `Estimate{ModelChange,MixedCorpus}` set with `Note` labelled approximate; on a **clean** single-model corpus — `StaleTotal == 0`, `Consistent == true`, no dimensionality change. (FR-002/FR-005, SC-003, R2.) Depends T002.

**Checkpoint**: The estimate is decision-useful and honestly labelled.

---

## Phase 5: User Story 3 — Dry-run on every transport, zero side effects (Priority: P3)

**Goal**: The preview is available on REST/gRPC/MCP identically, and on every transport mutates nothing.

**Independent Test**: Invoke the preview over CLI/REST/gRPC/MPC on the same corpus → identical `MigrationPlan`, each leaving the corpus untouched (SC-004).

### Implementation for User Story 3

- [ ] T008 [P] [US3] Add the gRPC surface: `rpc MigratePlan(MigratePlanRequest) returns (MigrationPlan)` plus the `MigrationPlan`/`ModelCount`/`DimCount`/`Estimate` messages to `proto/gorag.proto`; regenerate `proto/gen`; wire the handler in `internal/grpc/engine_adapter.go` to call `Engine.MigratePlan`. contracts/api.md. Depends T002.
- [ ] T009 [P] [US3] Add the REST surface: `POST /v1/migrate/plan` (no body) returning the `MigrationPlan` JSON in `internal/rest/` (route + types + adapter). contracts/api.md. Depends T002.
- [ ] T010 [P] [US3] Add the MCP surface: a `migrate_plan` tool (no args) returning the plan as structured output in `internal/mcp/server.go`. contracts/api.md. Depends T002.
- [ ] T011 [US3] Extend `internal/engine/parity_test.go`: invoke the preview over CLI/REST/gRPC/MCP on the same isolated corpus and assert the returned `MigrationPlan` is byte-identical across transports, and that each leaves corpus/cache/baseline/epoch unchanged (FR-006/FR-003, SC-004). Depends T008, T009, T010.

**Checkpoint**: The preview is uniform and provably side-effect-free everywhere.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final gate and audit bookkeeping.

- [ ] T012 [P] Run the full gate: `make build vet lint test` green; `CGO_ENABLED=0 go build ./...` succeeds (Constitution III); `go mod tidy` clean (no new dependency expected).
- [ ] T013 Update audit tracking: mark finding **H24** done in `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 6 §1.8) with a one-line completion note (spec 028 — read-only `MigratePlan` shared by all transports + `Migrate`; succeeds with no backend).
- [ ] T014 Final gate: commit to `main` with Conventional Commits (e.g. `feat(migrate): dry-run migration plan (H24)`) and push (single-author repo — straight to `main`, per `CLAUDE.md`).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — produces the baseline.
- **Foundational (Phase 2)**: After Setup. **BLOCKS** every story (the engine plan must exist).
- **US1 (Phase 3)**: Depends on Phase 2. MVP — can ship alone.
- **US2 (Phase 4)**: Depends on Phase 2 only (its task is a test of the Phase-2 struct).
- **US3 (Phase 5)**: Depends on Phase 2; its three adapters (T008–T010) are mutually parallel; the parity test (T011) gates them.
- **Polish (Phase 6)**: Depends on all stories complete + green.

### User Story Dependencies

- **US1 (P1)**: After Phase 2. No dependency on US2/US3. (MVP.)
- **US2 (P2)**: After Phase 2. No dependency on US1/US3.
- **US3 (P3)**: After Phase 2. Adapters parallel; parity test last.

### Within Each User Story

- Engine plan (Phase 2) before any consumer.
- Implementation before its verifying test where the test exercises it (T002 → T005/T007; T003 → T006).
- US3 adapters (T008–T010) before the parity test (T011).

### Parallel Opportunities

- **Phase 5**: T008 (gRPC/proto), T009 (REST), T010 (MCP) — different files, all depend only on Phase 2 → run together.
- After Phase 2, US1 (T004–T006), US2 (T007), and US3's adapters (T008–T010) touch largely disjoint files and can fan out.

---

## Parallel Example: After Phase 2

```bash
# US1, US2, and the US3 adapters advance concurrently on disjoint files:
Task: "T004 [US1] CLI --dry-run in internal/cli/migrate.go"
Task: "T007 [US2] estimate-fields test in internal/engine/migrate_plan_test.go"
Task: "T008 [US3] gRPC MigratePlan in proto/ + internal/grpc/"
Task: "T009 [US3] REST /v1/migrate/plan in internal/rest/"
Task: "T010 [US3] MCP migrate_plan tool in internal/mcp/server.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (baseline) → Phase 2 (engine plan + Migrate refactor).
2. Phase 3 (US1 — CLI `--dry-run` + read-only/no-backend/preview==execution tests).
3. **STOP and VALIDATE**: `migrate --dry-run` previews and exits clean; nothing mutated.
4. At this point the operator can see the bill before paying it — US2/US3 add richness and parity.

### Incremental Delivery

1. Setup + Foundational → plan exists, `Migrate` reuses it.
2. US1 (CLI dry-run) → test → the preview works and is proven safe.
3. US2 (estimate) → test → the cost is decision-useful.
4. US3 (transports) → parity test → uniform everywhere.
5. Polish → audit marked, committed to `main`.

### Solo-Author Note

Single-author repo, commits to `main` directly (per `CLAUDE.md`). The parallel
structure is for clarity and agent fan-out, not a team. In practice: Phase 1 → 2 →
3 → (4 ∥ 5) → 6.

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks.
- [Story] label maps a task to its user story for traceability.
- This feature changes **no storage, config, or on-disk shape** and leaves real
  `Migrate` behaviour unchanged — if any task implies a mutation on the dry-run
  path or a behaviour change to `Migrate`, stop (it contradicts FR-003/FR-006).
- The dry-run's read-only + no-backend guarantees are **structural** (the method
  reaches only metadata readers), not flag-gated — keep it that way (R5).
- Commit after each task or logical group; Conventional Commits to `main`.
