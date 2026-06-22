# Tasks: Query Caching — Result + Query-Embedding LRU (H06)

**Input**: Design documents from `/specs/016-query-cache/` — [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/query-cache-contract.md](contracts/query-cache-contract.md), [quickstart.md](quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅

**Tests**: INCLUDED — go-rag is test-gated by the constitution (`go test -race -cover ./...` + `make test-eval` must stay green). Every story carries its own `_test.go` tasks.

**Organization**: Phases map to the spec's 5 user stories. Shared primitives (LRU, index epoch, config, pipeline onChange) live in Phase 2 Foundational because every story depends on them.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 (user-story phases only)
- Exact file paths in every description

---

## Phase 1: Setup

**Purpose**: Confirm a clean baseline before touching anything.

- [x] T001 Record clean baseline: run `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...` — all green; run `make test-eval` and note the recall@10 baseline (must not regress, SC-006). Note the result so the Polish phase can diff against it.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared primitives EVERY user story depends on — the bounded LRU, the index epoch, the config keys, engine ownership, and the pipeline `onChange` seam that bumps the epoch at all three mutation sites.

**⚠️ CRITICAL**: No user-story work can begin until this phase is complete. The epoch-bump coverage (T007/T008) is the highest-risk correctness item in the whole feature — missing the async `processJob` site returns stale results.

- [x] T002 [P] Implement the generic bounded LRU in `internal/engine/cache.go`: `LRU[K, V]]` using `sync.RWMutex` + `container/list` + `map[string]*list.Element`; `atomic.Uint64` hits/misses; methods `Get(key)(V,bool)`, `Put(key,V)` (LRU-evict at capacity), `Flush()`, `Stats() CacheStats`; `max<=0` ⇒ disabled (Get always misses, Put no-op). Cover with `internal/engine/cache_test.go` (Get hit/miss, eviction order, disabled no-op, Flush).
- [x] T003 [P] Implement the engine-owned index epoch in `internal/engine/epoch.go`: `atomic.Uint64` field on `*Engine`, `markIndexChanged()` (`atomic.Add`), `epoch() uint64` (`atomic.Load`).
- [x] T004 [P] Add cache config keys in `internal/config/config.go`: fields `QueryCacheEnabled bool` (json `query_cache_enabled`), `QueryCacheResults int` (`query_cache_results`), `QueryCacheEmbeddings int` (`query_cache_embeddings`); `Default()` → `true / 256 / 512`; `Validate()` rejects negative capacities; wire `Get()`/`Set()` for the three keys; append them to `engine.knownConfigKeys` in `internal/engine/config.go`.
- [x] T005 Wire engine ownership in `internal/engine/engine.go`: add `resultCache LRU[string,*QueryResult]`, `embedCache LRU[string,[][]float32]`, and the epoch to `Engine`; construct them in `NewWithDB`/`NewWithEmbedder` from config (empty/disabled when capacity 0 or `QueryCacheEnabled=false`); `Close()` flushes both caches (alongside dropping the index at engine.go:156).
- [x] T006 Add the pipeline `onChange` seam in `internal/pipeline/pipeline.go`: `pipeline.New` gains an `onChange func()` param (stored on `*Pipeline`); call it in `storeDocument` (pipeline.go:246) after the synchronous FTS add (the comment block at :266–267); and pass `e.markIndexChanged` from `engine.pipeline()` (engine.go:132) as that callback.
- [x] T007 [P] Call `onChange` in `internal/pipeline/workers.go` `processJob` immediately after `p.vec.Add` (workers.go:55) — the **asynchronous** vector landing; guard nil-onChange. (Different file from T006 ⇒ parallel after T006 lands the field.)
- [x] T008 [P] Call `onChange` in `internal/pipeline/delete.go` `DeleteDoc` (delete.go:16) after the FTS+Vector removal; guard nil-onChange. (Parallel with T007.)

**Checkpoint**: Foundation ready — LRU primitive, atomic epoch, config keys, engine-owned caches, and the epoch bumps at all three mutation sites (sync FTS-add, async vector-add, delete). User-story wiring can begin.

---

## Phase 3: User Story 1 — Repeated query is a result-cache hit (Priority: P1) 🎯 MVP

**Goal**: A second identical query returns the cached `QueryResult` — no embed round-trip, no retrieval work, byte-identical to cold.

**Independent Test**: quickstart Scenario 1 — query twice; second call is a hit (status `Hits` incremented, wall-time drops), results identical.

### Implementation for User Story 1

- [x] T009 [P] [US1] Add the `cacheKey` struct + deterministic `hash() string` (FNV-1a) in `internal/engine/cache.go` covering every D3 component: normalized query, mode, clamped k, threshold, effective RRFK, filter(source/type/tags — sorted), contextWindow, rerank{enabled,model}, and the epoch. (`req.NoCache` is NOT part of the key.)
- [x] T010 [P] [US1] Add `NoCache bool` field to `QueryRequest` in `internal/engine/types.go` (engine-level bypass; transport exposure is US5).
- [x] T011 [US1] Wire the result-cache **check** into `Engine.Query` in `internal/engine/query.go`: after K-clamp (query.go:41), before `indexes()` (query.go:47) — build the `cacheKey` with the current epoch; if enabled and `!req.NoCache`, `Get`; on hit return the cached `*QueryResult`.
- [x] T012 [US1] Wire the result-cache **store** in `internal/engine/query.go`: after building the `QueryResult` (query.go:151), `Put` iff enabled and `!rerankFailed` and `err==nil` (FR-009 — never cache degraded/erroring results).
- [x] T013 [US1] Tests in `internal/engine/cache_test.go`: (a) second identical query is a hit and byte-identical to cold (transparency, FR-008/SC-004); (b) changing any single key component (k/mode/threshold/rrf_k/filter/context_window) is a miss; (c) capacity eviction is LRU; (d) `req.NoCache=true` bypasses serving (still stores); (e) `RerankFailed=true` result is not stored.
- [x] T014 [US1] Test in `internal/engine/cache_test.go`: cache disabled (`QueryCacheEnabled=false` / capacity 0) ⇒ every query is a miss and nothing is stored (the kill-switch path).

**Checkpoint**: US1 fully functional and independently testable — repeated queries are served from the result cache.

---

## Phase 4: User Story 2 — A corpus change evicts stale results (Priority: P1)

**Goal**: After any ingest/delete/migrate, a previously-cached query returns a result consistent with the NEW corpus — never a stale entry.

**Independent Test**: quickstart Scenario 2 (ingest invalidates) + Scenario 5 (Migrate flushes both caches).

*Depends on*: Phase 2 (epoch) + US1 (the cache being invalidated). Independently testable once US1 exists.

### Implementation for User Story 2

- [x] T015 [US2] Flush both caches at the start of `Migrate` in `internal/engine/ingest.go` (ingest.go:92) — result + embedding cache `Flush()`. (The ongoing `ReprocessAll`→`processJob` vector-adds bump the epoch via T007, so the result cache stays invalid during async re-embedding.)
- [x] T016 [US2] Test in `internal/engine/cache_epoch_test.go`: cached query reflects a newly-ingested document after `Add` + wait-for-embed (epoch advanced ⇒ stale entry not served).
- [x] T017 [US2] Test in `internal/engine/cache_epoch_test.go`: cached query reflects a deletion via the `DeleteDoc` path (epoch advanced).
- [x] T018 [US2] **CRITICAL async-epoch regression test** in `internal/engine/cache_epoch_test.go`: ingest → query (caches at epoch E) → wait for the background `processJob` vector-add (epoch E+1) → query again → assert the result reflects the now-landed vector and is NOT the stale E entry. (This is the bug a write-ACK-only epoch would cause; the spec's central correctness gate.)
- [x] T019 [US2] Test in `internal/engine/cache_epoch_test.go`: after `Migrate`, both caches are empty (SC-003) and the next query is a cold miss under the new profile.

**Checkpoint**: US1 + US2 together — a correct, never-stale result cache.

---

## Phase 5: User Story 3 — Identical queries skip the embedding round-trip (Priority: P2)

**Goal**: A repeated query string under the same embedding profile reuses its vector without an Ollama call, even when the result cache misses.

**Independent Test**: quickstart Scenario 4 — force a result miss (change `k`); the query is not re-embedded (embedding cache hit, observable via Ollama logs / `status`).

### Implementation for User Story 3

- [x] T020 [US3] Wrap the `queryEmbed` closure in `internal/engine/query.go` (query.go:61–63) with the embedding cache: key = `profileFingerprint(model|dim|convention)` + `"\x00"` + prefixed query text; `Get` before `em.Embed`, `Put` after. (The mismatch-probe embed at query.go:232 is intentionally NOT cached.)
- [x] T021 [US3] Tests in `internal/engine/cache_embed_test.go`: (a) a result-cache miss with identical query text does not re-embed (recording embedder asserts one call); (b) ingest does NOT flush the embedding cache; (c) a profile change (model/convention) produces a different key ⇒ re-embed; (d) `Migrate` flushes the embedding cache.

**Checkpoint**: US1–US3 — the two-layer cache (result + embedding) fully working.

---

## Phase 6: User Story 4 — Cache is visible, bounded, and safe (Priority: P2)

**Goal**: `status` reports cache state (enabled/size/capacity/hits/misses); caches are bounded and concurrency-safe; empty on restart.

**Independent Test**: `go-rag status` shows a non-zero hit count after repeat queries and a bounded size; `go test -race` clean; a reopened DB starts cold.

### Implementation for User Story 4

- [ ] T022 [P] [US4] Add `CacheStats` struct + `ResultCache`/`EmbeddingCache` fields to `StatusInfo` in `internal/engine/types.go`.
- [ ] T023 [US4] Populate both `CacheStats` in `Engine.Status` in `internal/engine/status.go` (status.go:42 return block).
- [ ] T024 [US4] Render a "Cache" section in the CLI status command `internal/cli/status.go` (result + embedding: enabled/size/capacity/hits/misses).
- [ ] T025 [P] [US4] Add cache-stats fields to the REST status response in `internal/rest/types.go` and render them in the REST status handler.
- [ ] T026 [P] [US4] Add cache-stats fields to the gRPC status response — edit `proto/gorag.proto` `StatusResponse` + regen `proto/gen` + map in `internal/grpc/` adapter. **Batch this proto edit + regen with T031** (the `no_cache` QueryRequest field) to do one regen.
- [ ] T027 [P] [US4] Add cache-stats to the MCP status tool output in `internal/mcp/server.go`.
- [ ] T028 [US4] Concurrency test in `internal/engine/cache_concurrency_test.go`: parallel queries + a concurrent ingest under `go test -race`; assert no race/corruption (a stale-epoch read simply misses).
- [ ] T029 [US4] Restart-empty test in `internal/engine/cache_test.go`: reopen the DB / construct a fresh Engine ⇒ first query is a cold miss (cache is in-process, FR-007).

**Checkpoint**: US1–US4 — a visible, bounded, race-safe, restart-cold cache.

---

## Phase 7: User Story 5 — Cross-transport `nocache` override + parity (Priority: P2)

**Goal**: `nocache` is exposed identically on CLI/REST/gRPC/MCP and yields identical fresh results across transports.

**Independent Test**: quickstart Scenario 3 — `--no-cache` (and the equivalent REST/gRPC/MCP field) forces a fresh result; parity test confirms identical results across transports.

### Implementation for User Story 5

- [ ] T030 [P] [US5] Add the `--no-cache` bool flag in `internal/cli/query.go` (mirror `--no-rerank` at query.go:92) → `QueryRequest.NoCache`.
- [ ] T031 [P] [US5] Add `no_cache` to the REST request in `internal/rest/types.go` (`json:"no_cache,omitempty"`) + passthrough in `internal/rest/engine_adapter.go`.
- [ ] T032 [US5] Add `bool no_cache = 11;` to `message QueryRequest` in `proto/gorag.proto`; regen `proto/gen`; map it in `internal/grpc/engine_adapter.go`. (**Single batched regen with T026.**)
- [ ] T033 [P] [US5] Add `no_cache` to the MCP query tool input schema + passthrough in `internal/mcp/server.go` (no result-rendering change — cache is transparent).
- [ ] T034 [US5] Extend the cross-transport parity test with `nocache=true` in `internal/engine/parity_test.go` — identical fresh results over CLI/REST/gRPC/MCP (FR-010/SC-007).

**Checkpoint**: All five stories complete; `nocache` is a first-class, parity-guaranteed override.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Eval-harness integration, end-to-end validation, docs, gates, and backlog closure.

- [ ] T035 [P] Make the H02 eval harness cold: in `internal/eval/`, construct the eval engine with `QueryCacheEnabled=false` (D8) so every measurement is pure retrieval (SC-006) — confirm `make test-eval` recall@10 is unchanged from the T001 baseline.
- [ ] T036 Run every quickstart.md scenario (1–7) on an isolated DB with non-default transport ports (per CLAUDE.md §Constraints — never bare `go-rag start` against the live vault); capture the observed hit rates + that all scenarios pass.
- [ ] T037 [P] Update `README.md` if it documents query flags/config — add `--no-cache` and the three `query_cache_*` config keys (match the existing flag/config documentation style).
- [ ] T038 Final gates all green: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test -race -cover ./...`, and `make test-eval`. Record coverage.
- [ ] T039 Mark H06 complete in `RAG_BOOK_AUDIT_BACKLOG.md`: change the H06 line checkbox `[ ] → [x]` and append a `✅ COMPLETE (spec 016)` note — following the exact format of the neighbouring H01/H15 completion entries — summarising what shipped (result + query-embedding LRU, engine-owned index-epoch invalidation incl. the async `processJob` vector-add, `Migrate` flush, `nocache` on all four transports, `status` cache stats), the gates passed, and any caveat (e.g. `golangci-lint` skipped if the env still lacks a compatible config). This is the explicit "make the backlog item complete when finished" task requested for this spec.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately.
- **Foundational (Phase 2)**: depends on Phase 1 — **BLOCKS all user stories**.
- **US1 (Phase 3)**: depends on Phase 2.
- **US2 (Phase 4)**: depends on Phase 2 **+ US1** (invalidation is meaningless without a cache to invalidate; honestly coupled).
- **US3 (Phase 5)**: depends on Phase 2 (shares the LRU primitive + Migrate flush from US2's T015).
- **US4 (Phase 6)**: depends on Phase 2 (stats read the caches US1/US3 populate; rendering is independent).
- **US5 (Phase 7)**: depends on US1 (engine-level `NoCache` field from T010); exposes it to humans.
- **Polish (Phase 8)**: depends on all stories complete.

### Within Each User Story

- Types/structs before the logic that reads them.
- Engine `Query` check (T011) before store (T012) — same file, sequential.
- Implementation before its tests where a test needs the code to exist; where TDD is preferred (the cache is pure logic), the `cache_test.go` cases may be written first.

### Parallel Opportunities

- **Phase 2**: T002, T003, T004 are independent (different files) → parallel; T007/T008 parallel after T006 lands the `onChange` field.
- **Phase 3**: T009, T010 independent → parallel; then T011 → T012 (same file).
- **Phase 6**: T022, T025, T026, T027 independent across files → parallel (T026 batches its proto regen with T032).
- **Phase 7**: T030, T031, T033 independent → parallel; T032 is the proto+regen (batch with T026).
- **Phase 8**: T035, T037 independent → parallel.

### Cross-phase coordination note

- **One proto regen** covers both T026 (StatusResponse cache fields) and T032 (QueryRequest `no_cache = 11`) — do them together to avoid a second generate step.

---

## Implementation Strategy

### MVP First (User Story 1 + the epoch it needs)

1. Phase 1 (baseline) → Phase 2 (foundation: LRU, epoch, config, pipeline bump).
2. Phase 3 (US1: result cache).
3. **STOP and VALIDATE**: quickstart Scenario 1 passes (repeated query is a transparent hit).
4. Ship/demo if desired — the headline latency win is live.

### Incremental Delivery

1. Foundation → US1 (MVP: result-cache hits) → validate.
2. + US2 (correctness: never-stale, incl. the async-epoch gate) → validate Scenarios 2 & 5.
3. + US3 (embedding cache) → validate Scenario 4.
4. + US4 (visible/bounded/safe) → validate status + `-race`.
5. + US5 (cross-transport `nocache`) → validate Scenario 3 + parity.
6. Polish → T035–T038 green → T039 closes H06 in the backlog.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[USx]` labels map tasks to spec user stories for traceability.
- Every story is independently testable at its checkpoint.
- Commit after each task or logical group (Conventional Commits, straight to `main`).
- Highest-risk item: **T007 + T018** — the async `processJob` epoch bump and its regression test. If a stale result ever appears after an ingest, look here first.
