---

description: "Task list for H01 — Cached loaded index (no per-query rebuild)"
---

# Tasks: Cached Loaded Index — No Per-Query Rebuild (H01)

**Input**: Design documents from `/specs/011-index-cache/` — plan.md, spec.md, research.md, data-model.md, contracts/query-cache.md, quickstart.md

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: The constitution mandates `go test ./...` green on every change, and a stale or racy cache is a correctness regression worse than the slow rebuild it replaces — so tests are included per FR/acceptance scenario, run under `-race`.

**Organization**: Tasks grouped by user story. The Foundational phase builds the shared-index mechanism (the whole implementation); the user-story phases are the verification matrix (US1=latency, US2=freshness, US3=concurrency) proving different facets of that one mechanism.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story (US1, US2, US3) — present only on user-story-phase tasks
- Exact file paths in every description

## Path Conventions

Go single-module repo. This feature lives in `internal/engine` (core) and `internal/pipeline` + `internal/watcher` (cache-aware delete). Tests alongside source. Paths are repository-relative.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm a green starting point.

- [x] T001 Confirm green starting state: run `make build && make vet && make test` from repo root (capture any pre-existing failures so regressions are unambiguous)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared live index — Engine owns one `(FTS, Vector)` pair, seeded once and reused by Query; the pipeline/watcher/migrate mutate it; delete is cache-aware. Every user story depends on this mechanism.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete and `make test` is green.

- [x] T002 Add the shared index to the Engine in `internal/engine/engine.go`: new fields `idxMu sync.Mutex` + `idxFts *index.FTS` + `idxVec *index.Vector`, and a method `func (e *Engine) indexes() (*index.FTS, *index.Vector, error)` that lazily runs `pipeline.LoadIndex(e.db)` exactly once (seed the full corpus) under `idxMu` and returns the shared pointers on every later call. Lock ordering note: `pipeline()` acquires `pipeMu` then `idxMu` (via `indexes()`); `Query()` acquires only `idxMu` — no inversion
- [x] T003 Wire the pipeline to the shared index in `internal/engine/engine.go`: in `e.pipeline()`, call `e.indexes()` and pass the returned shared `*FTS`/`*Vector` to `pipeline.New(...)` instead of the current fresh `index.NewFTS()`/`index.NewVector()`. Depends on T002
- [x] T004 [P] Stop the per-query rebuild in `internal/engine/query.go`: replace `fts, vec, err := pipeline.LoadIndex(e.db)` with `fts, vec, err := e.indexes()` (the rest of `Query` is unchanged — it already builds a `Retrieval` over the fts/vec it receives). Depends on T002
- [x] T005 [P] Make delete cache-aware in `internal/pipeline/delete.go`: convert the package-level `func DeleteDoc(db *storage.DB, docID string) error` into a method `func (p *Pipeline) DeleteDoc(docID string) error` that does everything the current function does (DB deletes) AND, for each deleted chunk ID, calls `p.fts.Delete(cid)` and `p.vec.Delete(cid)` (both already exported) so the shared in-memory index stays fresh. Independent of T002–T004 (different package)
- [x] T006 Update the 4 `DeleteDoc` callers to the method form: `internal/pipeline/reprocess.go` (×2: `DeleteDoc(p.db, …)` → `p.DeleteDoc(…)`) and `internal/watcher/watcher.go` (×2: `pipeline.DeleteDoc(cd.db, …)` → `cd.pl.DeleteDoc(…)` — the `ChangeDetector` already holds the engine's `*Pipeline`). Depends on T005

**Checkpoint**: Foundation ready — `Engine.Query` reads a shared seeded index (no per-query `LoadIndex`); ingest/watcher/migrate mutate it live; delete clears it. `make build && make vet && make test` green (existing tests still pass — results are identical, only the load path changed).

---

## Phase 3: User Story 1 — Repeated queries are fast; cache reused (Priority: P1) 🎯 MVP

**Goal**: Back-to-back queries reuse the shared index — identical results, and the seed runs once not per query. (spec US1, FR-001/FR-008, SC-001/SC-003)

**Independent Test**: Query twice; assert identical hits AND that the shared index was seeded once (not rebuilt per query).

### Tests for User Story 1

- [x] T007 [US1] Add `TestQuery_ReusesSharedIndex` in `internal/engine/index_cache_test.go` (use the existing `openEngine`/`fastFakeOllama` harness): ingest a small corpus, issue the same query twice, assert (a) the two result sets are identical (FR-008), and (b) the shared index was seeded once — e.g. `e.indexes()` returns the same `*FTS`/`*Vector` pointers across queries, and a load-counter or pointer-identity check shows `LoadIndex` ran once for N queries (SC-001 structural proof). Depends on T004

**Checkpoint**: User Story 1 functional — the per-query rebuild is gone; the latency win is structurally proven.

---

## Phase 4: User Story 2 — The cached index stays correct (read-after-write) (Priority: P1)

**Goal**: Ingest/delete/migrate are reflected by the very next query — no restart, no flush, no phantom hits. (spec US2, FR-002/FR-003/FR-004, SC-002)

**Independent Test**: Ingest → query (see it); delete → query (don't see it); migrate → query (new embeddings).

### Tests for User Story 2

- [x] T008 [US2] Add `TestQuery_ReadAfterWrite_Ingest` in `internal/engine/index_cache_test.go`: ingest a doc, poll `Status.EmbeddingsComplete` (async-after-ACK), then query for its content and assert it appears (FR-003/FR-004 — the live index reflects embedding completion). Depends on T004
- [x] T009 [US2] Add `TestQuery_AfterDelete_NoPhantomHits` in `internal/pipeline/delete_test.go` (or `internal/engine`): ingest + embed a doc, delete it via the (now method) `DeleteDoc`, then query and assert none of its chunks appear (FR-003 — cache-aware delete removed them from the shared index). Depends on T005, T006
- [x] T010 [US2] Add `TestQuery_AfterMigrate_ReflectsNewEmbeddings` in `internal/engine/index_cache_test.go`: ingest under one embedding, migrate (re-embed), then query and assert results reflect the new embeddings (FR-002 — migrate flows through reprocess → cache-aware delete + re-ingest). Depends on T004

**Checkpoint**: Stories 1 AND 2 both hold — the cache is fast AND fresh.

---

## Phase 5: User Story 3 — Safe under concurrency; no thundering herd (Priority: P2)

**Goal**: Concurrent queries (+ background writes) never crash, tear, or double-build the index. (spec US3, FR-005/FR-006, SC-004)

**Independent Test**: Parallel queries while ingesting; N concurrent cold queries seed once.

### Tests for User Story 3

- [x] T011 [US3] Add `TestQuery_ConcurrentSafe_UnderBackgroundIngest` in `internal/engine/index_cache_test.go`: run many concurrent queries while docs ingest in the background, under `-race`; assert no errors/panics and every query returns a self-consistent result set (FR-005). Depends on T004
- [x] T012 [US3] Add `TestIndexes_SeedsOnce_NoThunderingHerd` in `internal/engine/index_cache_test.go`: fire N concurrent first-time `e.indexes()` calls against a cold cache; assert the seed (`LoadIndex`) ran exactly once and all callers got the same pointers (FR-006). Depends on T002

**Checkpoint**: All user stories independently functional; the cache is correct under load.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Regression anchors, merge gate, and backlog hygiene.

- [x] T013 [P] Confirm the results-identical guarantee (FR-008): the existing cross-transport parity tests (spec 003, `internal/engine/parity_test.go`) and the eval harness (spec 004, `make test-eval`) pass **unchanged** — the cache changes latency, not results. This is the regression anchor; if any drifts, the cache is wrong
- [x] T014 Run `make build && make vet && make test && make test-eval`; run `golangci-lint run` and `govulncheck ./...` (note: both may be unavailable in this env — `golangci-lint` config-version-incompatible, `govulncheck` uninstalled — record as pre-existing env issues, not code regressions, as on prior specs)
- [x] T015 Run the hermetic `quickstart.md` scenarios 1–7 (deterministic embedder, no real Ollama); scenario 8 (real daemon latency) is optional
- [x] T016 [P] Fix backlog staleness in `RAG_BOOK_AUDIT_BACKLOG.md`: mark **H08** (spec 009) and **H12** (spec 010) complete — both shipped in prior sessions but left unchecked (`- [ ]` → `- [x]`, with a `✅ COMPLETE (spec NNN): …` annotation summarizing each, matching the H03/H07/H09/H13 row style). Independent of the 011 implementation — these are already shipped
- [x] T017 Mark **H01** complete in `RAG_BOOK_AUDIT_BACKLOG.md`: change the H01 row `- [ ]` → `- [x]` and append a `✅ COMPLETE (spec 011): …` annotation summarizing the shared-live-index design + the gates passed (matching the prior rows' style). Depends on T007–T012 green and T013/T014 passing
- [x] T018 Commit to `main` with Conventional Commits (e.g. `perf(engine): cache the loaded index, no per-query rebuild (H01)`) including the backlog updates, and push

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: T002 → T003 (same file, `engine.go`); T004 [P] depends only on T002; T005 [P] independent; T006 depends on T005. **BLOCKS all user stories.**
- **US1 (Phase 3)**: depends on T004.
- **US2 (Phase 4)**: T008/T010 depend on T004; T009 depends on T005/T006.
- **US3 (Phase 5)**: T011 depends on T004; T012 depends on T002.
- **Polish (Phase 6)**: T013/T015 depend on all stories; T016 [P] is independent (already-shipped items); T017 depends on green stories + gate; T018 is last.

### Critical path

`T001 → T002 → T004 → (US1/US2/US3 tests) → T014 → T017 → T018`. T005→T006 (the delete refactor) is a parallel sub-chain that must also land (FR-003).

### Parallel Opportunities

- Phase 2: T005 (`pipeline/delete.go`) is fully independent of T002–T004 (`engine.go`/`query.go`) → parallel. T004 (`query.go`) is parallel to T003 (`engine.go`) once T002 lands.
- Phase 6: T013, T015, T016 are mutually independent (different concerns/files) → parallel.
- US tests: each writes an independent test function; they share `index_cache_test.go` so are not `[P]` by the same-file rule, but are logically independent.

---

## Parallel Example: Phase 2 (the mechanism)

```bash
# Two independent sub-chains, runnable together:
# Sub-chain A (engine core):
Task: "shared index + indexes() seed-once in internal/engine/engine.go"   # T002
Task: "pipeline() passes shared fts/vec in internal/engine/engine.go"     # T003 (after T002)
Task: "Query uses e.indexes() in internal/engine/query.go"                # T004 (after T002)

# Sub-chain B (cache-aware delete — different package):
Task: "DeleteDoc → method in internal/pipeline/delete.go"                 # T005
Task: "update 4 callers in reprocess.go + watcher.go"                    # T006 (after T005)
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (Setup) — green baseline.
2. Phase 2 (Foundational) — shared index + cache-aware delete; existing tests green (results unchanged).
3. Phase 3 (US1) — cache reuse proven (identical results, seed-once).
4. **STOP and VALIDATE** — run `TestQuery_ReusesSharedIndex`; run quickstart scenarios 1–2.
5. US1 alone delivers the headline latency win — but **do not ship without US2/US3**: a stale or racy cache is a regression.

### Incremental Delivery

1. Setup + Foundational → cache exists, results identical.
2. + US1 → latency win (MVP).
3. + US2 → freshness (read-after-write, delete, migrate).
4. + US3 → concurrency safety + seed-once.
5. Polish → gate green, backlog marked (H01 + the stale H08/H12), committed to `main`.

---

## Notes

- Same-file tasks are never marked `[P]` (T002/T003 in `engine.go`; the US tests in `index_cache_test.go`).
- **`pipeline.New` needs no signature change** — it already accepts `fts`/`vec`; the engine just passes its shared pair instead of fresh ones.
- **No transport/CLI/config/proto change** — `Engine.Query`'s contract is unchanged (FR-008). Any task that touches a transport adapter is scope drift.
- **Backlog hygiene**: T016 fixes H08/H12 (shipped specs 009/010, left unchecked — a process oversight caught during this planning step). T017 marks H01 after 011 ships. The user explicitly requested the "mark complete when finished" task.
- `golangci-lint`/`govulncheck` may be unavailable in this env (same as specs 006–010) — record as an env note, do not block. `go vet` + `-race` are the active gates.
