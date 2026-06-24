---

description: "Task list for Near-Duplicate Chunk Detection (spec 026, audit H20)"
---

# Tasks: Near-Duplicate Chunk Detection

**Input**: Design documents from `/specs/026-near-dup-detection/` — plan.md, spec.md, research.md, data-model.md, contracts/api.md, quickstart.md.

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/api.md, quickstart.md — all present from `/speckit-plan`.

**Tests**: INCLUDED. The spec defines measurable Success Criteria (SC-001…005) and an Independent Test per user story; this is a correctness- (detection precision) and cross-transport-parity-critical feature in a test-heavy Go repo, so test tasks are first-class.

**Organization**: Tasks grouped by user story. Each story is an independently testable increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: Which user story (US1/US2/US3) — Setup/Foundational/Polish have no label
- Every task names an exact file path

## Path Conventions (Go)

- Source under `internal/<pkg>/` (one package per PRD subsystem, per `CLAUDE.md`).
- Tests co-located: `internal/<pkg>/<file>_test.go`. Parity tests: `internal/engine/parity_test.go`.
- Generated protobuf: `proto/gen/`; schema: `proto/gorag.proto`.
- Build gate: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...` (Constitution III; `make build vet test`).

## ⚠️ Build-order note (priority vs dependency)

The spec labels **US1 = P1** (highest user value: near-dups collapsed in results) and **US2 = P2** (correctness: every chunk knows its near-dup relationships). But US1's collapse reads the `NearDup` sidecar that US2's ingest-time clustering populates — US1 cannot pass its independent test until US2 exists. So phases are **built in dependency order** (Foundational → US2 → US1 → US3); `[US#]` labels and P1/P2/P3 priorities are preserved for spec traceability. Identical pattern to spec 025.

---

## Phase 1: Setup (Baseline)

**Purpose**: Confirm the repo is green before changes (baseline for SC-004).

- [ ] T001 Run `make build vet test` (or `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`) on `main`; confirm green and record as the pre-feature baseline. No code changes.

**Checkpoint**: Clean baseline established.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared infrastructure all three stories depend on. No story work can begin until this phase is complete.

**⚠️ CRITICAL**: Blocks US1, US2, and US3.

- [ ] T002 [P] Add `NearDupInfo` type (`{Siblings []string; Similarity float64}`) and `Chunk.NearDup *NearDupInfo` field (`json:"near_dup,omitempty"`) to `internal/model/model.go`, with the doc comment from `data-model.md` §1. Non-identity sidecar (do NOT touch `GenerateID`/`ContentHash`). Research R3.
- [ ] T003 [P] Add `PrefixNearDup byte = 0x13` to `internal/storage/storage.go` (next free prefix after `PrefixThreatSrc 0x12`) and `Put/Get/ScanNearDup` helpers mirroring the quarantine helpers in `internal/storage/poison.go`. Research R5.
- [ ] T004 [P] Create `internal/near/simhash.go`: `SimHash(text string) uint64` (64-bit, `crypto/sha256` + `math/bits`, pure stdlib) and `HammingNear(a, b uint64, k int) bool` (popcount ≤ k). Research R1.
- [ ] T005 [P] Add `near_dup_hamming` config (default `k=3`) and a minimum-chunk-length guard to `internal/config/config.go` (+ effective-resolver, mirroring `EffectivePoisonThreshold…`). Research R9/R10.
- [ ] T006 [P] Add unit tests in `internal/near/simhash_test.go`: a doc and its small-edit revision hash close (Hamming ≤ k); distinct passages hash far (> k); `HammingNear` boundary; `SimHash` deterministic; short/empty text handled.

**Checkpoint**: Shared model field + `0x13` index + SimHash core + config ready. Story phases can begin.

---

## Phase 3: User Story 2 — Every chunk knows its near-duplicate relationships (Priority: P2) 🏗 built first

> **Built before US1** because US1's collapse reads the sidecar populated here. See build-order note.

**Goal**: Each chunk carries its pairwise near-dup siblings (`NearDup *NearDupInfo`), computed at ingest; chunk geometry/identity unchanged (FR-007/FR-003).

**Independent Test** (SC-001/SC-005, no query): ingest a doc + a near-identical revision and a pair sharing one copied section; enumerate stored chunks; assert near-dup siblings are correct and distinct chunks carry none.

### Implementation for User Story 2

- [ ] T007 [US2] Compute each chunk's `SimHash` on the ACK path in `processFile` (`internal/pipeline/pipeline.go`) — same site as `SectionContext` — and write it to `0x13` via `PutNearDup`; skip chunks below the min-length guard. Research R4/R5/R6 (fingerprint over the indexed `Chunk.Content`).
- [ ] T008 [US2] Add async sibling-clustering to `processJob` (`internal/pipeline/workers.go`): for each new chunk, `ScanNearDup`-compare fingerprints within Hamming `k`, and `putChunk` with `NearDup = &NearDupInfo{Siblings, Similarity}` when siblings exist (mirror `engine/poison.go` `putChunk`). Research R4/R8 (pairwise; eventally-consistent, same window as BM25).
- [ ] T009 [US2] Add pipeline tests (`internal/pipeline/section_test.go` or a new `neardup_test.go`): ingest a doc + near-identical revision → enumerate chunks → assert correct `NearDup.Siblings` (US2-sc1); a copied section across two docs → those chunks are cross-document near-dups (US2-sc2); clearly-distinct chunks → no siblings (FR-009, US2-sc3); short chunks → not fingerprinted (R10); chunk count/text/identity unchanged vs pre-feature (FR-007/FR-003, US2-sc4).

**Checkpoint**: US2 independently testable — enumerate chunks, near-dup siblings correct; geometry/identity intact.

---

## Phase 4: User Story 1 — Query results aren't dominated by near-identical passages (Priority: P1) 🎯 MVP value

> Builds on US2's populated `Chunk.NearDup`. This is the user-facing value the spec ranks P1.

**Goal**: An opt-in `dedup` collapses near-duplicate hits to one representative (highest-scored) per group, post-ranking; `near_dup` surfaced identically on every transport (FR-004/FR-005).

**Independent Test**: ingest a doc + near-identical revision, query a shared phrase; with `dedup` the pair occupies one slot (collapsed), without it both appear; identical across CLI/REST/gRPC.

### Implementation for User Story 1

- [ ] T010 [P] [US1] Add `NearDup *model.NearDupInfo` to `engine.QueryHit` and copy `c.NearDup` in the hit-building loop in `internal/engine/query.go` (alongside the `SectionContext` copy); add `Dedup bool` to `engine.QueryRequest` in `internal/engine/types.go`. Contracts/api.md.
- [ ] T011 [US1] Implement opt-in post-ranking collapse in `internal/engine/query.go`: when `req.Dedup`, after the threshold filter, drop a hit if a higher-ranked kept hit lists it as a sibling (read via the existing `lookupChunk`). Purely subtractive; scores/ranking untouched (FR-007). Research R7. Depends T010.
- [ ] T012 [P] [US1] Add `near_dup` to `queryHit` and `dedup` to `queryRequest` in `internal/rest/types.go`; map both in the REST adapter (`internal/rest/engine_adapter.go`).
- [ ] T013 [P] [US1] Add `message NearDup { repeated string siblings = 1; double similarity = 2; }`, `NearDup near_dup = 10;` on `QueryHit`, and `bool dedup = 14;` on `QueryRequest` in `proto/gorag.proto`; regenerate `proto/gen`.
- [ ] T014 [P] [US1] Map `NearDup` + `Dedup` in the gRPC adapter (`internal/grpc/engine_adapter.go`).
- [ ] T015 [P] [US1] Render `near_dup` in the MCP hit response and pass `dedup` through `renderQuery` arg parsing (`internal/mcp/server.go`).
- [ ] T016 [P] [US1] Add a `--dedup` flag and a `near_dup:` render line/field to the CLI query command (`internal/cli/query.go`, `renderResults`), omitted when absent.
- [ ] T017 [US1] Extend the cross-transport parity suite (`internal/engine/parity_test.go`): a near-dup fixture asserting `near_dup` is byte-identical across REST/gRPC/MCP and `dedup=true` collapses identically (SC-002). Depends T010–T016.
- [ ] T018 [US1] Add an end-to-end test (`internal/engine/query_test.go` or pipeline): ingest a doc + near-identical revision → query a shared phrase → assert `dedup` collapses the pair to one representative (US1 acceptance scenario 1).

**Checkpoint**: US1 independently testable — a query with `dedup` collapses near-dups identically across all four transports.

---

## Phase 5: User Story 3 — Operators can see and control near-duplicate handling (Priority: P3)

**Goal**: `status` reports near-dup counts; pre-feature / no-near-dup chunks degrade gracefully; back-fill via Reprocess (FR-008, US3).

**Independent Test** (SC-005): after ingesting near-dups, `status` shows `near_dup_chunks > 0`; a pre-feature chunk record loads with absent `near_dup`; `Reprocess` re-derives it.

### Implementation for User Story 3

- [ ] T019 [P] [US3] Add `NearDupChunks int` to `engine.StatusInfo` (`internal/engine/types.go`), compute it in `internal/engine/status.go` (count chunks with non-nil `NearDup`), and project it on REST/gRPC/MCP/CLI status (`near_dup_chunks`). US3-sc1.
- [ ] T020 [US3] Add a migration test (`internal/model/model_test.go`): a chunk JSON in the pre-feature shape (no `near_dup` key) unmarshals cleanly with `NearDup == nil`, round-trips, and is returned on a hit with the field absent — no parse/read failure (US3-sc3 / FR-008).
- [ ] T021 [US3] Add a back-fill test (`internal/pipeline/reprocess_test.go`): `Reprocess` on a pre-feature heading-bearing doc re-derives `NearDup`; and a no-near-dup doc's chunks stay `NearDup == nil` (graceful). Research R3 (no cheap rescan — back-fill is re-ingest).

**Checkpoint**: US3 independently testable — observability + migration safety verified.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Cross-story validation and project hygiene.

- [ ] T022 [P] Run the retrieval-eval harness (spec 004) pre- vs post-feature under the same embedding model: with `dedup` **off**, assert no regression vs baseline (FR-007); with `dedup` **on**, assert reduced top-k redundancy without losing relevant coverage (SC-004).
- [ ] T023 [P] Execute the full `quickstart.md` runbook (Scenarios A–G) against an ISOLATED DB (`--db-path <tmp>`) with NON-DEFAULT `--rest-addr/--grpc-addr` (per repo `CLAUDE.md` smoke rule); confirm SC-001…005.
- [ ] T024 Update audit tracking: mark finding **H20** done in `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 6 §1.1) with a one-line completion note (spec 026).
- [ ] T025 Final gate: `make build vet lint test` green; `CGO_ENABLED=0 go build ./...` succeeds (Constitution III); `go mod tidy` clean; commit to `main` with Conventional Commits (e.g. `feat(near): near-duplicate chunk detection (H20)`).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: Depends on Phase 1 — **BLOCKS all user stories**.
- **US2 (Phase 3)**: Depends on Phase 2. Built first — it populates `Chunk.NearDup`, which US1 collapses.
- **US1 (Phase 4)**: Depends on **Phase 2 + US2 (Phase 3)** — collapse reads the sidecar US2 writes.
- **US3 (Phase 5)**: Depends on Phase 2 (and benefits from US2's ingest); mostly verification of graceful-absence + observability + back-fill.
- **Polish (Phase 6)**: Depends on Phases 3–5.

### User Story Dependencies (priority vs build order)

- **US2 (P2)** — built first. Depends only on Foundational. Independently testable (enumerate chunks, no query). The correctness core.
- **US1 (P1)** — depends on **US2**. Independently testable via query + cross-transport parity. The user-visible value.
- **US3 (P3)** — depends on Foundational; mostly verification.

> The spec ranks US1 highest by *user value*; the *build* order is US2 → US1 because collapsing a near-dup presupposes a correctly-populated one. `[US#]`/P# labels preserved for `spec.md` traceability (same pattern as spec 025).

### Within Each User Story

- Pure helpers before pipeline wiring.
- Pipeline wiring before transport surfacing.
- Each story's checkpoint is independently verifiable before moving on.

### Parallel Opportunities

- **Phase 2**: T002–T006 are different files → fully parallel.
- **Phase 4 (US1 surfacing)**: T010, T012, T013, T014, T015, T016 are different files → parallel; then T017 (parity) + T018 (e2e) integrate.
- **Phase 5**: T019 parallel with T020/T021 where files differ.
- **Phase 6**: T022, T023, T024 independent → parallel.

---

## Parallel Example: Phase 2 (Foundational)

```text
Task: "NearDupInfo + Chunk.NearDup in internal/model/model.go"             (T002)
Task: "PrefixNearDup 0x13 + storage helpers in internal/storage/"          (T003)
Task: "SimHash + HammingNear in internal/near/simhash.go"                  (T004)
Task: "near_dup_hamming config in internal/config/config.go"               (T005)
Task: "SimHash unit tests in internal/near/simhash_test.go"                (T006)
```

## Parallel Example: Phase 4 (US1 surfacing — all different files)

```text
Task: "engine.QueryHit.NearDup + Dedup + collapse in internal/engine/"     (T010/T011)
Task: "REST near_dup/dedup in internal/rest/types.go + adapter"            (T012)
Task: "proto NearDup + fields + regen proto/gen"                           (T013)
Task: "gRPC adapter mapping in internal/grpc/engine_adapter.go"            (T014)
Task: "MCP render + dedup in internal/mcp/server.go"                       (T015)
Task: "CLI --dedup + near_dup render in internal/cli/query.go"             (T016)
```

---

## Implementation Strategy

### MVP First (deliver the P1 user value)

The smallest increment delivering measurable value is **US1's collapse**, but it requires US2's detection underneath. MVP scope:

1. Phase 1 — baseline (T001).
2. Phase 2 — Foundational (T002–T006): model field, `0x13`, SimHash core, config.
3. Phase 3 — US2 (T007–T009): fingerprint-on-ACK + async clustering + correctness tests.
4. Phase 4 — US1 (T010–T018): collapse + four-transport surfacing + parity.
5. **STOP and VALIDATE**: `quickstart.md` Scenario A — ingest a doc + revision, query with `dedup`, confirm the pair collapses to one representative identically across transports.

At this point the feature's headline value ("query results aren't dominated by near-identical passages") is shippable.

### Incremental Delivery

1. Foundational → shared infra ready.
2. + US2 → enumerate chunks, each carries correct near-dup siblings (testable without query).
3. + US1 → `dedup` collapses near-dups across all transports (**MVP**).
4. + US3 → status counts + graceful migration + back-fill via Reprocess.
5. + Polish → no retrieval regression (SC-004), full quickstart green, H20 closed.

### Solo-Author Note

Per repo `CLAUDE.md`, Spec Kit work commits straight to `main` — no feature branch, no PR ceremony. Commit per task or logical group with Conventional Commits.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[US#]` maps a task to its spec user story; Setup/Foundational/Polish carry no label.
- Each story checkpoint is independently verifiable (US2: enumerate chunks; US1: query + parity; US3: status + graceful).
- The chunker, `FileReader`/`Embedder` interfaces, chunk/document identity, write-ACK ordering, and embedded text are all **unchanged** (FR-007, Constitution II/IV/V).
- `near_dup` is a non-identity sidecar (like `Poisoning`/`SectionContext`); collapse is opt-in and subtractive (default off).
- Verify the build/test gate stays green after every task; the repo is never left red.
