---

description: "Task list for Adaptive Retrieval Depth & Pool-Size Tuning (H22)"
---

# Tasks: Adaptive Retrieval Depth & Pool-Size Tuning (H22)

**Input**: Design documents from `/specs/024-adaptive-retrieval/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md (all present)

**Tests**: INCLUDED — the repo is test-heavy (`go test ./...` is a constitution gate) and FR-010/SC-003 mandate the `make test-eval` recall gate. Each story has focused unit tests + the shared eval gate runs in Polish.

**Organization**: Tasks grouped by user story. Shared types/methods live in Foundational (Phase 2) so the three stories don't fight over `types.go`/`retrieval.go`/`cache.go`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story (US1, US2, US3) — ONLY on user-story-phase tasks
- Every task names an exact file path

## Path Conventions

Pure Go, single binary. Source under `internal/`, proto at `proto/gorag.proto` (+ generated `proto/gen`). No `src/`, no `tests/` dir — Go tests live as `*_test.go` next to the code.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm a green baseline before any change, so regressions are attributable.

- [x] T001 Confirm baseline build/vet/test green: run `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test -race ./...` — all pass on untouched `main`
- [x] T002 [P] Capture the pre-H22 recall baseline: run `make test-eval` and confirm it passes against `testdata/golden/baseline.json` (this is the SC-003 reference every later phase must not regress) — recall@10=1.000 PASS

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared types, config keys, and mechanical seams EVERY story depends on. Done here so US1/US2/US3 don't edit the same struct/method concurrently.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

- [x] T003 Add the `pool_size` config key (field, `DefaultPoolSize=60`, `Default()`, `EffectivePoolSize()`, `Validate()` negative-reject, `Get()`, `Set()`, and the absent-key⇒60 backward-compat rule in `Load()`) in `internal/config/config.go` per `contracts/config-keys.md` — followed the RRFK precedent (Effective accessor handles 0⇒60; no Load() rule needed)
- [x] T004 [P] Add the `adaptive_depth_enabled` config key (field default false, `EffectiveAdaptiveDepthEnabled()`, `Get()`, `Set()`) in `internal/config/config.go` per `contracts/config-keys.md`
- [x] T005 Add `SetPoolSize(n int)` method to `Retrieval` (mirror `SetRRFK`; non-positive ignored so the constructor default 60 stays) in `internal/index/retrieval.go`
- [x] T006 [P] Declare the classifier types — `QueryClassification` struct and `QueryClassifier` interface (K-only, in-process contract per `contracts/classifier-interface.md`) in a NEW `internal/index/classify.go`
- [x] T007 Add the H22 type surfaces to `internal/engine/types.go`: `QueryRequest.PoolSize int`; `QueryResult.EffectiveK/EffectivePool int` + `EffectiveMode string`; `StatusInfo.PoolSize int`, `StatusInfo.AdaptiveDepthEnabled bool`, `StatusInfo.PoolUtilization PoolUtilization`; the `PoolUtilization` struct per `data-model.md` Entity 3 (declarations only — no logic yet)
- [x] T008 Add `classifier index.QueryClassifier` field to the `Engine` struct in `internal/engine/engine.go` (left nil by constructors for now; US2 wires the default)
- [x] T009 Fold the effective depth + pool into the result-cache key: add `EffK int` and `EffPool int` to `cacheKey`, hash both in `cacheKey.hash()`, and thread `effK`/`effPool` params through `resultKey(...)` in `internal/engine/cache.go` per `contracts/status-and-cache.md` §B (cache-key change is safe — in-process only)

**Checkpoint**: Shared types + config + cache seam in place. Default behavior unchanged (pool still resolves to 60, classifier still nil). `go build ./...` green. User-story phases can now proceed.

---

## Phase 3: User Story 1 — Tunable reranker candidate pool (Priority: P1) 🎯 MVP

**Goal**: An operator can grow/shrink the reranker candidate pool per query (and via config), with the default held at 60 and aggregate pool-utilization visible in `status`. Independently shippable.

**Independent Test**: Run the same query with `--pool-size 120` vs `--pool-size 20` vs default (0) and confirm `effective_pool` echoes each value, results/latency move as expected, and `status` shows pool-utilization. (quickstart.md Scenarios 2 + 4.) No code change to the query itself — only a flag/config value.

### Tests for User Story 1

> Write these FIRST; ensure they FAIL (effective_pool absent / always 60) before implementing.

- [ ] T010 [P] [US1] Unit test effective-pool resolution (`req.PoolSize>0` ⇒ override; `0` ⇒ config 60; config `0` ⇒ 60) in `internal/engine/query_test.go`
- [ ] T011 [P] [US1] Unit test `PoolUtilization` tracking (queries counted, averages non-zero after N runs, saturated counter on short corpus) in `internal/engine/status_test.go`
- [ ] T012 [P] [US1] Unit test `Retrieval.SetPoolSize` drives FTS/vector fetch size and rerank pool (mirror the existing `SetRRFK` test shape) in `internal/index/retrieval_test.go`

### Implementation for User Story 1

- [ ] T013 [US1] Resolve the effective pool once in `Engine.Query` (`internal/engine/query.go`): `req.PoolSize > 0 ? req.PoolSize : cfg.EffectivePoolSize()`, then call `r.SetPoolSize(effPool)` next to the existing `r.SetRRFK(effRRFK)`; pass `effPool` into `resultKey(...)`
- [ ] T014 [US1] Populate `QueryResult.EffectivePool` and `EffectiveMode` from the resolved values in `internal/engine/query.go` (US3 echoes these on every transport; US1 sets them)
- [ ] T015 [US1] Add the `--pool-size` CLI flag and plumb it into `engine.QueryRequest{PoolSize: ...}` in `internal/cli/query.go` per `contracts/query-pool-knob.md`
- [ ] T016 [P] [US1] Add the `pool_size` field to the REST query request struct and map `req.PoolSize → QueryRequest` in `internal/rest/server.go` + `internal/rest/engine_adapter.go`
- [ ] T017 [P] [US1] Add `int32 pool_size = 13;` to `proto/gorag.proto` `QueryRequest`, regenerate `proto/gen`, and map the field → `QueryRequest` in `internal/grpc/` per `contracts/query-pool-knob.md`
- [ ] T018 [P] [US1] Add the `pool_size` input property to the `go_rag_query` MCP tool and pass it through in `internal/mcp/server.go`
- [ ] T019 [US1] Surface `PoolSize` (effective configured ceiling) and track/populate `PoolUtilization` (updated after each non-cached query; zero-averages guard when `Queries==0`) in `Engine.Status` (`internal/engine/status.go`)
- [ ] T020 [US1] Register `pool_size` in `knownConfigKeys` (`internal/engine/config.go`) and the CLI config allowlist (`internal/cli/config_cli.go`)

**Checkpoint**: Pool tunable across all four transports; default-OFF query is byte-identical; utilization visible in status. Validate quickstart.md Scenario 2 (incl. the cross-transport parity block). `make test-eval` still green.

---

## Phase 4: User Story 2 — Adaptive retrieval depth via a query classifier (Priority: P2)

**Goal**: With the classifier enabled, a factoid query and a broad/comparative query use different effective retrieval depths (`k`-only; mode never auto-changed); explicit `k` wins; the effective pool shrinks with the recommended `k` (FR-011).

**Depends on**: Phase 2 (types, config `adaptive_depth_enabled`) AND Phase 3 (effective-pool resolution + `EffectiveK`/`EffectivePool` fields exist).

**Independent Test**: Enable `adaptive_depth_enabled`; run a short factoid and a broad comparative query with no explicit `k`; confirm `effective_k` differs (factoid shallow) and `effective_pool` shrinks with it; then set explicit `--k 8` on the factoid and confirm it overrides. (quickstart.md Scenario 3.)

### Tests for User Story 2

- [ ] T021 [P] [US2] Unit test `RuleBasedClassifier.Classify`: short factoid ⇒ small `K>0`; comparative keywords ⇒ `K:0`; empty query ⇒ `K:0`; deterministic in `internal/index/classify_test.go`
- [ ] T022 [P] [US2] Unit test effective-`k` resolution in `Engine.Query`: explicit `k` wins; no explicit + classifier-on + recommendation ⇒ recommended; classifier-off ⇒ default 5 — in `internal/engine/query_test.go`
- [ ] T023 [P] [US2] Unit test FR-011 pool-shrinking: recommended shallow `k` ⇒ `effective_pool == clamp(k+slack, FLOOR, ceiling)`; no recommendation ⇒ full ceiling; floor never below `k` — in `internal/engine/query_test.go`

### Implementation for User Story 2

- [ ] T024 [US2] Implement `RuleBasedClassifier.Classify` (pure-Go heuristics: short non-comparative ⇒ shallow `K`; comparative/listing terms ⇒ `K:0`; else `K:0`) and the `poolSlack`/`poolFloor` constants in `internal/index/classify.go` per `contracts/classifier-interface.md` (tune the comparative token set against the eval harness — record final set in the task commit)
- [ ] T025 [US2] Wire the classifier in the engine constructors: set `e.classifier = index.RuleBasedClassifier{}` when `cfg.EffectiveAdaptiveDepthEnabled()` in `NewWithDB`/`NewWithEmbedder` (`internal/engine/engine.go`); nil otherwise
- [ ] T026 [US2] Resolve effective `k` in `Engine.Query` (`internal/engine/query.go`) before retrieval/cache: `explicit (req.K>0) > recommended (classifier on, `K>0`) > default (5)` per FR-006; populate `QueryResult.EffectiveK`
- [ ] T027 [US2] Apply FR-011 in `Engine.Query`: when the classifier drove `effK` (recommended), set `effPool = clamp(effK + poolSlack, poolFloor, configured-or-override ceiling)` BEFORE `r.SetPoolSize(effPool)`; with no recommendation use the full ceiling (byte-identical default)
- [ ] T028 [US2] Register `adaptive_depth_enabled` in `knownConfigKeys` (`internal/engine/config.go`) and the CLI config allowlist (`internal/cli/config_cli.go`) so the posture is get/set-able

**Checkpoint**: Classifier adapts depth + shrinks pool; explicit `k` wins; disabled ⇒ today's behavior. Validate quickstart.md Scenario 3. `make test-eval` still green (classifier default-off ⇒ baseline unchanged).

---

## Phase 5: User Story 3 — Operator visibility of the tuning knobs (Priority: P2)

**Goal**: From `status` AND the query response, an operator sees the effective pool size, classifier enablement, and the effective depth/mode actually used — so tuning is observable, not hidden.

**Depends on**: Phase 3 (pool/status fields) AND Phase 4 (classifier enablement + `EffectiveK`).

**Independent Test**: Enable pool tuning + classifier, run a query, read `status` + the response — confirm `pool_size`, `adaptive_depth_enabled`, `pool_utilization`, and the effective depth/pool/mode triple are all visible and correct. (quickstart.md Scenario 4.)

### Tests for User Story 3

- [ ] T029 [P] [US3] Unit test `Status` surfaces `PoolSize`, `AdaptiveDepthEnabled`, and `PoolUtilization` together with correct values in `internal/engine/status_test.go`
- [ ] T030 [P] [US3] Cross-transport parity test: a query resolved over CLI/REST/gRPC/MCP returns the same `effective_k`/`effective_pool`/`effective_mode` (extend the existing parity test) in `internal/engine/parity_test.go`

### Implementation for User Story 3

- [ ] T031 [US3] Complete `Engine.Status` (`internal/engine/status.go`): populate `AdaptiveDepthEnabled` from `cfg.EffectiveAdaptiveDepthEnabled()` alongside the US1 `PoolSize`/`PoolUtilization` fields
- [ ] T032 [US3] Populate `QueryResult.EffectiveMode` from `index.ParseMode(req.Mode)` echo in `internal/engine/query.go` (mode is never changed by H22 — surfaced for symmetry/observability)
- [ ] T033 [P] [US3] Surface the new status fields on MCP `go_rag_status`, the REST/gRPC status responses, and the CLI `status` command (add the effective triple to the query response projections on each transport too) — `internal/mcp/server.go`, `internal/rest/server.go`, `internal/grpc/`, `internal/cli/query.go`+status cmd, `proto/gorag.proto` (response fields)
- [ ] T034 [US3] Verify the response effective triple round-trips on all four transports (manual + the T030 parity test)

**Checkpoint**: Full observability loop closed. Validate quickstart.md Scenario 4.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Gates, docs, and the formal no-regression check across all three stories.

- [ ] T035 [P] Run the full quality gate: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test -race -cover ./...` all green (constitution Development & Quality Workflow)
- [ ] T036 Run the recall gate `make test-eval` and confirm recall@10 ≥ baseline with every new behavior default-OFF (FR-010/SC-003) — this is the single formal no-regression acceptance
- [ ] T037 [P] Latency spot-check (SC-001): a factoid query at classifier-reduced depth+pool approaches the keyword-only budget; no query exceeds the hybrid budget — record numbers from quickstart.md Scenario 2/3
- [ ] T038 [P] Update the H06 cache note in docs/comments: differing effective depth (explicit vs recommended) or per-query pool now produces distinct cache entries (R5) — wherever the cache key is documented
- [ ] T039 [P] Optionally extend the H18 audit event to record `effective_k`/`effective_pool` alongside `req.K` in `internal/audit/` (minor enhancement; skip if it risks scope creep)
- [ ] T040 Run the full quickstart.md validation (Scenarios 1–6) end-to-end on an isolated DB (`--db-path <tmp>` + non-default transport addrs per CLAUDE.md)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: After Setup — BLOCKS all user stories.
- **US1 (Phase 3, P1)**: After Foundational. No dependency on other stories → **MVP**.
- **US2 (Phase 4, P2)**: After Foundational + **US1** (reuses `EffectivePool`/`EffectiveK` fields and the pool-resolution seam; adds classifier-driven `k` + FR-011 pool shrinking on top).
- **US3 (Phase 5, P2)**: After **US1 + US2** (surfaces classifier enablement from US2 alongside pool from US1; closes the observability loop).
- **Polish (Phase 6)**: After all desired stories complete.

### Within Each User Story

- Tests written first (FAIL), then implementation.
- Engine resolution logic before transport plumbing.
- One transport at a time is fine, but all four must land before the story's checkpoint (FR-001/FR-009 parity).
- Story checkpoint = quickstart scenario passes + `make test-eval` green.

### Parallel Opportunities

- T003 vs T004 (two config keys, same file — do sequentially OR one author; NOT parallel).
- T006, T007, T009 are different files → parallel after T005.
- Within US1: T016 (REST), T017 (gRPC/proto), T018 (MCP) are different files → parallel after T013/T014 land the engine seam.
- All `[P]`-marked tests are independent files → parallel.
- US1, US2, US3 are NOT parallel with each other (US2 depends on US1; US3 on both) — implement sequentially in priority order.

---

## Parallel Example: User Story 1 transports

```bash
# After T013 (engine pool resolution) + T014 (effective fields) land, fan out the four transports:
Task: "T015 --pool-size CLI flag in internal/cli/query.go"
Task: "T016 pool_size REST field in internal/rest/{server,engine_adapter}.go"
Task: "T017 pool_size proto field 13 + regen + gRPC map in proto/gorag.proto + internal/grpc/"
Task: "T018 pool_size MCP tool param in internal/mcp/server.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 Setup → baseline green.
2. Phase 2 Foundational → shared types/config/cache seam.
3. Phase 3 US1 → pool tunable on all transports + utilization in status.
4. **STOP and VALIDATE**: quickstart Scenario 2 + `make test-eval`.
5. Ship. The pool knob is independently valuable; the classifier (US2) and full observability (US3) can follow.

### Incremental Delivery

1. Setup + Foundational → seam ready.
2. + US1 → pool tuning (MVP) → validate → ship.
3. + US2 → adaptive depth → validate → ship.
4. + US3 → full observability → validate → ship.
5. Each story adds value without breaking prior stories (default-OFF posture guarantees this).

### Notes

- Every `[P]` task is a different file with no dependency on an incomplete task.
- Default-OFF is sacred: FR-007/SC-005 (byte-identical) + FR-010/SC-003 (eval recall) must hold after EVERY phase, not just at the end.
- The result-cache key change (T009) is the one cross-cutting correctness fix — it lands in Foundational and is safe because the cache is in-process/empty-on-restart.
- Commit after each task or logical group; Conventional Commits straight to `main` (single-author repo per CLAUDE.md).
