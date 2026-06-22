---

description: "Task list for H08 — Configurable RRF constant"
---

# Tasks: Configurable Reciprocal Rank Fusion (RRF) Constant (H08)

**Input**: Design documents from `/specs/009-rrf-config/` — plan.md, spec.md, research.md, data-model.md, contracts/query-rrf-k.md, quickstart.md

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: This project's constitution mandates `go test ./...` green on every change and the H02 eval gate; FR-006 mandates a deterministic pin test. Tests are therefore included where an FR or acceptance scenario requires them — not as exhaustive TDD.

**Organization**: Tasks grouped by user story. Foundational phase holds the shared single-k collapse + config + engine wiring that every story depends on.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story (US1, US2, US3) — present only on user-story-phase tasks
- Exact file paths in every description

## Path Conventions

Go single-module repo. All source under `internal/` and `proto/`; binary at `cmd/go-rag/main.go`. Tests live alongside source (`*_test.go`). Paths below are repository-relative.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish green baseline and resolve the one infrastructure unknown (proto regen) before any code change.

- [x] T001 Confirm green starting state: run `make build && make vet && make test` from repo root; capture any pre-existing failures (none expected) so regressions are unambiguous
- [x] T002 Determine the `proto/gorag.proto` → `proto/gen/gorag.pb.go` regeneration command (inspect `specs/003-rest-grpc-api/` docs and `git log --oneline -- proto/gen/`; identify whether `protoc --go_out=. --go-grpc_out=.` with module flags, or `buf generate`, is used). Record the exact command for T016. If no tooling is committed, decide: install `buf`/protoc plugins vs. hand-edit the additive optional field

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The single-k collapse + `rrf_k` plumbing through config → engine → retrieval. MUST be complete before ANY user story — every story depends on `rrf_k` existing end-to-end on the default path.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete and `make test` is green.

- [x] T003 Collapse `reciprocalRankFusion` to a single symmetric constant and replace `Retrieval.kVec`/`kFTS` with `rrfK` in `internal/index/retrieval.go`: change signature to `reciprocalRankFusion(vectorHits, ftsHits []Hit, k int) []Hit`; formula stays `1/(k+rank+1)` (rank 0-based) using one `k`; replace struct fields `kVec`/`kFTS` with `rrfK int`; set `rrfK: 60` in `NewRetrieval`; update the `Search` hybrid branch to call `reciprocalRankFusion(vHits, fHits, r.rrfK)`; add `func (r *Retrieval) SetRRFK(k int)` that sets `r.rrfK` only when `k > 0` (mirror `EnableRerankRetry`)
- [x] T004 [P] Add `RRFK int json:"rrf_k,omitempty"` to `Config`, method `func (c Config) EffectiveRRFK() int` (returns `c.RRFK` if `>0` else `60`), reject `< 0` in `Validate`, and add an `rrf_k` case to `Config.Get` returning `strconv.Itoa(c.EffectiveRRFK())` — all in `internal/config/config.go`
- [x] T005 [P] Add `RRFK int` field (0 = unset → config/default) to `QueryRequest` in `internal/engine/types.go` (document the resolution rule in the field comment)
- [x] T006 Wire `Engine.Query` in `internal/engine/query.go`: after `index.NewRetrieval(...)`, compute `effective := req.RRFK; if effective <= 0 { effective = e.cfg.EffectiveRRFK() }` and call `r.SetRRFK(effective)` (place it alongside the existing `r.EnableRerankRetry()` call). Depends on T003, T004, T005
- [x] T007 Audit and update existing fusion tests in `internal/index/retrieval_test.go`: review `TestRetrieval_Hybrid_BothListsRankAboveOneList` and any assertion that depends on the old `kVec=40`/`kFTS=60` scores; update expected values for single-`k=60`. Ranking-order assertions (both-lists beats one-list) still hold unchanged. Depends on T003

**Checkpoint**: Foundation ready — `rrf_k` resolves config→engine→retrieval on the default path; `make build && make vet && make test` green. User-story work can now begin.

---

## Phase 3: User Story 1 — Tune retrieval quality per corpus via config (Priority: P1) 🎯 MVP

**Goal**: A user sets `rrf_k` in `.go-rag/config.json` and observes a ranking change; the default (absent key) is 60; invalid values are rejected. (spec US1, FR-001, FR-005)

**Independent Test**: Set `rrf_k` to a non-default value in config, run `go-rag query` on a fixed corpus, confirm the ranking differs from the default run; confirm a config that omits `rrf_k` still loads (default 60).

### Implementation for User Story 1

- [x] T008 [P] [US1] Add `rrf_k` handling to the config get/set command in `internal/cli/config_cli.go` (and the `Get`/`Set` surfaces in `internal/config/config.go`): `config set rrf_k <N>` writes `cfg.RRFK` (reject `< 0`); `config get rrf_k` prints `EffectiveRRFK()`. Depends on T004
- [x] T009 [P] [US1] Test in `internal/config/config_test.go`: a config that omits `rrf_k` (unmarshals to 0) reports `EffectiveRRFK() == 60`; a config with `rrf_k: 120` reports 120; `Validate` rejects `rrf_k: -1` and accepts `rrf_k: 0`. Depends on T004
- [x] T010 [US1] Test in `internal/index/retrieval_test.go`: with a fixed two-list fixture, `SetRRFK(30)` vs `SetRRFK(200)` produce different fused orderings, and the default (`k=60`) ranks a chunk present in both lists above one present in a single list. Depends on T003
- [x] T011 [US1] Test in `internal/index/retrieval_test.go`: `rrf_k` is a silent no-op in `ModeKeyword` and `ModeSemantic` (no error, no ranking change vs. unset). Depends on T003

**Checkpoint**: User Story 1 functional and independently testable — config-driven tuning works, default is safe, validation holds.

---

## Phase 4: User Story 2 — One-off per-query override across all transports (Priority: P2)

**Goal**: A `--rrf-k` flag (CLI) and an `rrf_k` field (REST/gRPC/MCP) override the constant for a single call, honoured identically across all four transports. (spec US2, FR-002, FR-003)

**Independent Test**: Run the same query with `--rrf-k 30` vs `--rrf-k 200` (CLI) and confirm distinct orderings; confirm the same `rrf_k` over CLI/REST/gRPC/MCP returns identical rankings.

### Implementation for User Story 2

- [x] T012 [P] [US2] Add `--rrf-k` flag (cobra `Int`, default `0` = unset) to the query command in `internal/cli/query.go`: map into `engine.QueryRequest{RRFK: v}`; when `cmd.Flags().Changed("rrf-k")` and `v <= 0`, return an error (`--rrf-k must be a positive integer`). Depends on T005
- [x] T013 [P] [US2] Add `RRFK int json:"rrf_k,omitempty"` to `queryRequest` in `internal/rest/types.go` and map `req.RRFK → engine.QueryRequest.RRFK` in `handleQuery` in `internal/rest/engine_adapter.go`. Depends on T005
- [x] T014 [P] [US2] Add `"rrf_k": map[string]any{"type": "integer", "default": 60}` to the `go_rag_query` inputSchema, and read `args["rrf_k"]` (float64 → int) into `req.RRFK` when `> 0`, in `internal/mcp/server.go` (`renderQuery` + the schema map literal). Depends on T005
- [x] T015 [US2] Add `int32 rrf_k = 6;` to `message QueryRequest` in `proto/gorag.proto` and regenerate `proto/gen/gorag.pb.go` using the command from T002. Field 6 is additive + optional → wire-compatible. Depends on T002
- [x] T016 [US2] Map `RRFK: int(req.GetRrfK())` into `engine.QueryRequest` in `Adapter.Query` in `internal/grpc/engine_adapter.go`. Depends on T015
- [x] T017 [US2] Add or extend a cross-transport parity test asserting that the same `rrf_k` produces identical ranked `chunk_id` order over CLI, REST, gRPC, and MCP (locate the existing spec-003 parity test or add `internal/engine/parity_rrf_test.go`). Depends on T012–T016

**Checkpoint**: Stories 1 AND 2 both work independently — per-query override is uniform across every transport.

---

## Phase 5: User Story 3 — Deterministic, documented fusion formula (Priority: P2)

**Goal**: The fusion formula and the `k=60` rationale are stated once, canonically, and pinned by a deterministic test. (spec US3, FR-004, FR-006)

**Independent Test**: A reader finds the formula + rationale in one place; the pin test asserts the exact fused score for known ranks.

### Implementation for User Story 3

- [x] T018 [P] [US3] State the canonical formula `score(d) = Σ 1/(k + rank)` (rank 1-based), `k` default 60, and the rationale (standard RRF per the retrieval book §6.6; the prior asymmetric per-list `kVec`/`kFTS` is removed) in exactly one place: the `internal/index` package doc (`retrieval.go`) and a short note in `README.md`. Remove any contradictory statement. Depends on T003
- [x] T019 [US3] Add a deterministic formula-pin unit test in `internal/index/retrieval_test.go`: a chunk ranked #1 in both lists under `k=60` scores `1/(60+1) + 1/(60+1) = 2/61` (within float tolerance); a chunk in only one list at rank 1 scores `1/61`. Depends on T003

**Checkpoint**: All user stories independently functional; formula documented and pinned.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Merge-gate quality checks that span all stories.

- [x] T020 Measure the default-ranking change (SC-001): run `go-rag eval --embedder offline --baseline testdata/golden/baseline.json` BEFORE and AFTER the collapse; record recall@5/10, MRR, NDCG@10. Optionally `go-rag eval --benchmark scifact` (real embeddings) for direction. Depends on T003
- [x] T021 Re-capture `testdata/golden/baseline.json` for the new `k=60` default via `go-rag eval --embedder offline --record-baseline`; confirm `make test-eval` is green. Note the re-baseline in the README/changelog (intentional default change, not a regression). Depends on T020
  — **OUTCOME: re-capture NOT required.** `make test-eval` passes against the existing baseline (recall@10 1.000 → 1.000, drop −0.00pt ≤ 2.00). The golden set is recall@10-saturated, so the collapse to single-k=60 is recall-neutral; no breach, no re-baseline. SC-001 met: no retrieval regression.
- [x] T022 Run `quickstart.md` scenarios 1–7 against an **isolated DB** (`--db-path <tmp>`, non-default transport ports per project CLAUDE.md); fix any gap surfaced
- [x] T023 Run `make vet` and `golangci-lint run` and `govulncheck ./...`; resolve findings (note: `golangci-lint` may be config-version-incompatible in this env — if so, record it as a pre-existing env issue, not a code regression)
  — **OUTCOME: `go vet ./...` clean. `golangci-lint` env-broken (config version "" unsupported — same pre-existing issue as specs 006/007/008, not a code regression). `govulncheck` not installed in this env. Build green.**
- [x] T024 Commit to `main` with Conventional Commits (e.g. `feat(retrieval): configurable single-k RRF (H08)`) and push

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately. T002 (proto regen) unblocks T015.
- **Foundational (Phase 2)**: Depends on Phase 1. **BLOCKS all user stories.**
- **US1 (Phase 3)**: Depends on Phase 2. No dependency on US2/US3.
- **US2 (Phase 4)**: Depends on Phase 2 (T005). The gRPC sub-chain (T015→T016) also depends on T002. No dependency on US1/US3.
- **US3 (Phase 5)**: Depends on Phase 2 (T003). No dependency on US1/US2.
- **Polish (Phase 6)**: T020/T021 depend on the collapse (T003); T022 depends on all stories; T024 is last.

### Critical path

`T001 → T003 → T006 → (US1 done) → T022 → T024` for the MVP+merge spine, with `T002 → T015 → T016 → T017` as the parallel gRPC chain that must also land before merge (Principle V parity).

### Parallel Opportunities

- Phase 2: T004 (`config.go`), T005 (`types.go`) run in parallel; T003 (`retrieval.go`) is independent of both; T006 then wires them.
- Phase 4: T012 (CLI), T013 (REST), T014 (MCP) are three different files → parallel; the gRPC chain T015→T016 is sequential and parallel to the other three.
- Phase 5: T018 (docs) and T019 (pin test) are different files → parallel.
- Across stories: once Phase 2 is done, US1 (Phase 3), US2 (Phase 4), and US3 (Phase 5) can proceed in parallel if staffed.

---

## Parallel Example: User Story 2 (cross-transport override)

```bash
# Three independent transport adapters, launched together:
Task: "Add --rrf-k flag in internal/cli/query.go"            # T012
Task: "Add rrf_k to REST in internal/rest/{types,engine_adapter}.go"  # T013
Task: "Add rrf_k to MCP in internal/mcp/server.go"           # T014

# gRPC chain runs separately (depends on proto regen T002):
Task: "Add proto field 6 + regen in proto/"                  # T015 → T016
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (Setup) — confirm green + determine proto regen.
2. Phase 2 (Foundational) — collapse + config + engine wiring; `make test` green.
3. Phase 3 (US1) — config-driven tuning works, default safe, validation holds.
4. **STOP and VALIDATE** — run quickstart scenarios 1, 3, 4.
5. Note: merge still requires T021 (eval re-baseline) so `make test-eval` stays green — do not push without it.

### Incremental Delivery

1. Setup + Foundational → foundation ready.
2. + US1 → config tuning demoable (MVP).
3. + US2 → flag + REST/gRPC/MCP override + parity.
4. + US3 → documented + pinned formula.
5. Polish → eval gate green, lint clean, committed to `main`.

---

## Notes

- Same-file tasks are never marked `[P]` (e.g. T003 covers all `retrieval.go` changes cohesively).
- `Adding a struct field` (T005) is non-breaking: REST/gRPC/MCP adapters compile and keep working (zero value) until US2 wires each transport — this is what enables incremental delivery.
- The eval baseline re-capture (T021) is **merge-blocking**, not optional: the H02 gate (`make test-eval`) will fail against the old baseline after the collapse, and loosening tolerance is explicitly rejected (research.md §5).
- `golangci-lint` may be unavailable/incompatible in this env (seen on prior specs) — record as an env note, do not block on it.
