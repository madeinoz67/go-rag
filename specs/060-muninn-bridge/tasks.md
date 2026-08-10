# Tasks: MuninnDB Bridge + Memory & Graph View

**Input**: Design documents from `specs/060-muninn-bridge/` (spec.md, plan.md, research.md, data-model.md, contracts/, quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅, quickstart.md ✅

**Tests**: INCLUDED — spec NFR-002 ("a verified property, not an aspiration") and the repo's verify-don't-assume doctrine require them. Tests are written first and RED-sanity-checked where they pin a property or a bug.

**Organization**: grouped by user story. US1 (P1, MVP) = the write path; US2 (P2) = auto-backfill; US3 (P3) = the view.

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[USx]**: the user story the task belongs to
- Exact file paths on every task

---

## Phase 1: Setup

**Purpose**: vendor the upstream proto stub + create the package skeleton.

- [x] T001 Vendor the `muninn_v1` gRPC client stub read-only under `proto/muninn/v1/` (generated stub from `scrypster/muninndb` @ `e4d6ad21`; outbound only — go-rag never serves MuninnDB RPCs). Verify `CGO_ENABLED=0 go build ./...` still passes and no transitive CGo enters via the proto package (Principle III). `proto/muninn/v1/`, `go.mod`
- [x] T002 Create the `internal/bridge/muninn/` package skeleton (doc comment + placeholder types; no behavior). `internal/bridge/muninn/doc.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: shared infrastructure every user story depends on. **⚠️ No story work begins until this phase is complete.**

- [x] T003 [P] Add flat `Bridge*` config fields + `EffectiveBridgeEnabled()` to `internal/config/config.go` (research.md R9: flat fields, not a nested object — mirror `Enrichment*`). Fields per data-model.md E1 (`BridgeEnabled`, `BridgeEndpoint`, `BridgeSourceVault`, `BridgeTargetVault`, `BridgeMaxInFlight`, `BridgeRatePerSec`, `BridgeWorkers`, `BridgeBatchSize`, `BridgeConnectTimeoutMs`, `BridgeRequestTimeoutMs`, `BridgeBackfillAutoOnEnable`). Defaults render the bridge inert. Add loopback validation in `Config.Validate` (`net.ParseIP`, beside the existing Ollama-URL check). `internal/config/config.go`, `internal/config/config_test.go`
- [x] T004 Define the `MuninnClient` interface + a test fake in `internal/bridge/muninn/client.go` (the transport-agnostic surface: `Hello`, `Write`, `BatchWrite`, `Read`, `Activate`, `Healthy` per contracts/muninn-grpc-client.md). The fake records calls + returns canned `Read` values so every story's tests run without a live MuninnDB. `internal/bridge/muninn/client.go`
- [x] T005 Implement the gRPC `MuninnClient` in `internal/bridge/muninn/grpc_client.go` — the repo's **first outbound gRPC client** (research.md R9). `grpc.NewClient` with: a loopback `WithContextDialer` (refuses non-loopback at dial — DNS-rebind defense), a `UnaryInterceptor` attaching `metadata.Pairs("authorization", "Bearer "+token)` from `GORAG_BRIDGE_TOKEN`, exponential backoff on `RESOURCE_EXHAUSTED`/transient, reconnect. Token read from env, never logged. `internal/bridge/muninn/grpc_client.go`
- [x] T006 Implement the `bridgeProc` decoupled worker pool in `internal/bridge/muninn/processor.go` (the `internal/embedproc.Processor` pattern, NOT enrich-inline — research.md R9). Buffered promotion queue + worker goroutines (`BridgeWorkers`), bounded by a `BridgeMaxInFlight` semaphore + `BridgeRatePerSec` token bucket (FR-013/NFR-006), a 3-state circuit breaker (mirror `internal/enrich/circuit.go`), and a **bounded** `Stop()` drain (`select { case <-done: case <-time.After(drainTimeout) }` — the spec 045 embedproc-drain lesson; abandon-in-flight is safe under the UPSERT no-op). `internal/bridge/muninn/processor.go`
- [x] T007 Implement the `Bridge` coordinator in `internal/bridge/muninn/bridge.go`: `Enqueue(ws, docID, vault, chunks, mode)` (non-blocking, sheds on full queue — FR-011), `Start(ctx)`/`Stop()`/`Pause()`/`Resume()`/`Status()`. Holds the in-memory `BackfillState` (data-model.md E6). Wire start into `Engine.pipeline` gated by `EffectiveBridgeEnabled()` and `Stop()` into `Engine.Close` between `bus.Close()` and `embedProc.Stop()` (research.md R9 drain-order). `internal/bridge/muninn/bridge.go`, `internal/engine/engine.go`
- [ ] T008 [P] Apply the PRD §2.2 carve-out — add the opt-in/loopback/never-core-egress note for the bridge alongside N4 (enrichment) and N7 (console). `docs/internals/PRD_RAG_Database.md`

**Checkpoint**: foundation ready — config, client (+fake), bridgeProc, coordinator wired into the engine. User-story implementation can begin.

---

## Phase 3: User Story 1 — Opt-in Promotion + Cognitive Hygiene (Priority: P1) 🎯 MVP

**Goal**: enable the bridge, add a document, its chunks promote to MuninnDB as content-addressed engrams; re-ingest is a strict no-op; MuninnDB-down never breaks ingest.

**Independent Test** (quickstart.md §US1): add a doc → chunks appear in MuninnDB vault `go-rag`; re-add the unchanged doc → `Read` shows `access_count`/`updated_at`/`last_access` unchanged; MuninnDB stopped → write still ACKs `<10ms`.

### Implementation

- [ ] T009 [P] [US1] Implement the chunk→`WriteRequest` mapper in `internal/bridge/muninn/mapper.go` per data-model.md E4 / contracts/muninn-grpc-client.md. Maintainer invariants are non-negotiable: `embedding=nil`, `stability=30.0`, `idempotent_id="chunk:"+chunkID`, `upsert_mode=true`, `confidence=1.0`, `vault=BridgeTargetVault`, tags `[go-rag, source_vault, file_type]` + `low-quality` when `extraction_quality<0.5`, wikilink `associations` (BL-004, weight 0.6–0.8). Assert `idempotent_id` non-empty whenever `upsert_mode=true` (MuninnDB rejects bare upsert fail-loud). `internal/bridge/muninn/mapper.go`, `internal/bridge/muninn/mapper_test.go`
- [ ] T010 [P] [US1] Implement the rule-based concept cascade in `internal/bridge/muninn/concept.go` (data-model.md E5): section_heading → doc title → filename+`[i/n]` → first-60-chars. Position suffix omitted for single-chunk docs / when the heading uniquely identifies. No LLM. `internal/bridge/muninn/concept.go`, `internal/bridge/muninn/concept_test.go`
- [ ] T011 [US1] Add the 2-line `processJob` enqueue hook in `internal/pipeline/workers.go` (the enrichment seam — `j.chunks` already in hand, research.md R9): nil-guarded `if p.bridge != nil { p.bridge.Enqueue(j.ws, j.docID, j.vault, j.chunks, ModeChangeEvent) }` after `enrichDocument`. Add a `SetBridge` setter on `Pipeline` (peer to `SetEnricher`), bound in `Engine.pipeline`. Non-blocking — never enters the `<10ms` ACK path. `internal/pipeline/workers.go`, `internal/pipeline/pipeline.go`, `internal/engine/engine.go`
- [ ] T012 [US1] NFR-002 cognitive-hygiene test in `internal/bridge/muninn/bridge_test.go` — using the fake `MuninnClient`: promote a chunk, capture `Read` `{access_count, updated_at, last_access}`; enqueue N re-promotions of the unchanged chunk; assert the three fields are byte-identical afterward (the UPSERT no-op). RED-sanity-check: a fake that bumps `access_count` on each Write must fail this test. `internal/bridge/muninn/bridge_test.go`
- [ ] T013 [US1] Graceful-degradation + circuit-breaker test in `internal/bridge/muninn/bridge_test.go` — fake `MuninnClient` returns errors / is unhealthy: the bridge trippes the breaker, surfaces `healthy=false`, and `Enqueue` never blocks a caller. Cross-check the pipeline write path is unaffected (a slow/unreachable MuninnDB doesn't stall `processJob`). `internal/bridge/muninn/bridge_test.go`
- [ ] T014 [US1] Implement `go-rag bridge muninn init` (non-interactive flags → flat config) and `go-rag bridge muninn status` (per data-model.md E6 + contracts/ui-rest.md status shape) in `internal/cli/bridge.go`. `init` reads `GORAG_BRIDGE_TOKEN` from env (never prompts inline, never logs). `internal/cli/bridge.go`, `cmd/go-rag/main.go`

**Checkpoint**: US1 functional and independently testable — promotion + no-op + degrade all verified.

---

## Phase 4: User Story 2 — Auto-Backfill, Storm-Limited, Pausable (Priority: P2)

**Goal**: enabling the bridge on a vault with an existing corpus auto-backfills it to MuninnDB, rate-bounded, operator-pausable/resumable.

**Independent Test** (quickstart.md §US2): enable on a vault with N existing docs → backfill auto-starts; foreground query stays in budget; `pause`/`resume` completes with zero duplicates.

**Depends on**: US1 (mapper + bridgeProc enqueue path).

### Implementation

- [ ] T015 [US2] Implement the backfill walker in `internal/bridge/muninn/backfill.go`: on `Start` (when `BridgeBackfillAutoOnEnable`), walk `storage.ListDocuments` + `GetDocumentChunks` for the source vault, enqueue `ModeBackfill` jobs at low priority. Park-checks the pause flag between pages. Reads chunks from disk by chunk ID (not from `j.chunks`), so it runs independently of the pipeline. `internal/bridge/muninn/backfill.go`
- [ ] T016 [US2] Implement pause/resume + status surfacing: in-memory pause flag on the coordinator, `go-rag bridge muninn pause|resume` CLI, and the backfill progress block in `status` (data-model.md E6: running/paused/cursor/promoted/skipped/failed). `internal/bridge/muninn/bridge.go`, `internal/cli/bridge.go`
- [ ] T017 [US2] Storm-limit test in `internal/bridge/muninn/backfill_test.go` — a large fake corpus backfill keeps concurrent `BatchWrite` calls ≤ `BridgeMaxInFlight` and rate ≤ `BridgeRatePerSec`; foreground query latency is unaffected (the caps hold). `internal/bridge/muninn/backfill_test.go`
- [ ] T018 [US2] Resume-no-duplicates test in `internal/bridge/muninn/backfill_test.go` — pause mid-backfill, resume ⇒ every chunk enqueued exactly once; the fake `MuninnClient` sees no duplicate `idempotent_id` (the UPSERT no-op would absorb dupes server-side, but the bridge must not rely on that for its own queue budget). `internal/bridge/muninn/backfill_test.go`

**Checkpoint**: US1 + US2 both independently functional.

---

## Phase 5: User Story 3 — Memory & Graph View (Priority: P3)

**Goal**: retire the last console placeholder; the 9th sidebar item renders the target MuninnDB vault's engrams + entity edges.

**Independent Test** (quickstart.md §US3): with ≥1 doc promoted, the Memory & Graph view renders browse + detail; MuninnDB-down ⇒ degraded state; no-Bearer ⇒ 401.

**Depends on**: Foundational `MuninnClient` read path; real data benefits from US1 but the view itself only needs the client.

### Implementation

- [ ] T019 [P] [US3] Retire the placeholder — delete `"memory-graph"` from `placeholderViews` in `internal/ui/placeholder.go` (after which `handlePlaceholder` 404s for it) and update `TestPlaceholder_Routes` in `internal/ui/ui_test.go` to reflect the empty placeholder set. `internal/ui/placeholder.go`, `internal/ui/ui_test.go`
- [ ] T020 [US3] Implement the guarded routes in `internal/ui/memory_graph.go` (named `memory_graph`, NOT `bridge*` — avoids the spec 049 `bridgeops` collision, research.md R9): `GET /api/memory-graph/browse`, `GET /api/memory-graph/engrams/{id}`, `GET /api/memory-graph/status`, `POST /api/memory-graph/backfill/{pause|resume}` per contracts/ui-rest.md. All through `s.guard(...)`. Register in `internal/ui/ui.go::Server.Handler`. `internal/ui/memory_graph.go`, `internal/ui/ui.go`
- [ ] T021 [US3] Build the Alpine view — replace the placeholder `<section>` (lines ~1288–1292) in `internal/ui/web/templates/index.html` with a real view bound to the new API (browse list + detail panel + degraded/empty state + pause/resume control). The 9th `nav-item` (line ~202) already targets `memory-graph`. `internal/ui/web/templates/index.html`
- [ ] T022 [US3] Auth + degraded-state tests in `internal/ui/memory_graph_test.go` — no-Bearer ⇒ 401 on every route; MuninnDB-unreachable (fake `Healthy()=false`) ⇒ `browse` returns `degraded:true` (200, not 5xx); `engrams/{unknown}` ⇒ 404. `internal/ui/memory_graph_test.go`

**Checkpoint**: all three stories independently functional; the last placeholder is retired.

---

## Phase 6: Polish & Cross-Cutting

- [ ] T023 [P] Add the `gorag_bridge_*` Prometheus instruments to the existing `:7881/metrics` endpoint (RFC §metrics: sync_lag, engrams_promoted/skipped/failed, muninn_healthy, batch_duration, rate_limit_total, hebbian_edges). `internal/bridge/muninn/processor.go`, `internal/observe/`
- [ ] T024 [P] Affirm the keyspace-registry stance — add a note in `docs/internals/keyspace-registry.md` that the v1 bridge is stateless (no `0x20–0x22` allocation; no migration, no `ExpectedVersion` bump) and that a future perf cache would reserve `0x20–0x22` with a numbered migration. `docs/internals/keyspace-registry.md`
- [ ] T025 Run the `quickstart.md` E2E on an isolated daemon (`--db-path /tmp/gorag-bridge`, non-default ports): US1 promotion + NFR-002 no-op + MuninnDB-down degrade; US2 auto-backfill + pause/resume; US3 view — **Interceptor-browser-verified** (mandatory for visual verification). `specs/060-muninn-bridge/quickstart.md`
- [ ] T026 Final gates — `make build && make vet && make test -race ./... && make lint` (0 issues, the `ci.yml` gate). RED-sanity-check every property/bug test (NFR-002, storm-limit, resume-no-dup) by temporarily reverting the fix and confirming the test fails. `Makefile`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup; **blocks all stories**.
- **US1 (Phase 3)**: depends on Foundational.
- **US2 (Phase 4)**: depends on Foundational **and US1** (shares the mapper + bridgeProc enqueue path).
- **US3 (Phase 5)**: depends on Foundational (the read path); real data benefits from US1.
- **Polish (Phase 6)**: after the stories.

### Within Each Story

Mapper/concept before the enqueue hook (US1). Walker before pause/resume (US2). Routes before the Alpine view (US3). Tests written alongside the code they pin; property tests RED-sanity-checked.

### Parallel Opportunities

- Setup T001/T002: parallel.
- Foundational T003, T004, T008 are [P] (different files). T005→T006→T007 are sequential (client → processor → coordinator wires them).
- US1 T009/T010 [P] (mapper + concept, different files); T011 after both.
- US2 T015→T016 (walker → pause/resume surfaces it).
- US3 T019 [P] can start immediately after Foundational; T020/T021 sequential.
- Polish T023/T024 [P].

---

## Implementation Strategy

### MVP First (US1 only)

1. Phase 1 (Setup) + Phase 2 (Foundational) → foundation ready.
2. Phase 3 (US1) → test independently (quickstart §US1).
3. **STOP and VALIDATE**: promotion + NFR-002 no-op + MuninnDB-down degrade all pass.
4. The bridge is useful end-to-end at this point (chunks in memory); backfill + view are refinements.

### Incremental Delivery

Add US2 (backfill) → validate. Add US3 (view, retires the last placeholder) → validate + Interceptor-browser-verify. Polish.

### Notes

- Commit after each task or logical group (Conventional Commits, straight to `main` — single-author repo).
- Every PR/task touching storage MUST affirm "no on-disk layout change" (v1 is stateless — the storage-discipline compliance rule).
- Restart the daemon after code changes (dev env): `make build && ./bin/go-rag stop && ./bin/go-rag start` on the default vault (standing instruction).
- The bridge is the repo's first outbound gRPC client and first egress surface — treat the loopback enforcement + Bearer interceptor + no-token-in-logs as hard requirements, not polish.
