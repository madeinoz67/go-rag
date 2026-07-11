# Tasks: go-rag Management Console — Query View (Slice 2)

**Input**: Design documents from `/specs/048-ui-query-view/` — [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/ui-query.md](./contracts/ui-query.md), [quickstart.md](./quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓.

**Tests**: INCLUDED — go-rag is test-gated (`make test -race`, `make lint(0)`) and the constitution enforces "Spec/Test/Evals First". Every story ships a test task. `Engine.Query` already exists and is parity-proven, so — unlike spec 047 — **there is no engine/REST/gRPC/MCP/CLI/proto phase**. Every task is in the UI transport.

**Organization**: Tasks grouped by user story. Research decision tags (R1–R12) cross-link to [research.md](./research.md); FR/SC tags cross-link to [spec.md](./spec.md).

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[USx]**: user-story phase tag (Setup/Foundational/Polish tasks carry none)
- Every task names its exact file path + the symbol/seam it touches

## Path conventions

New files: `internal/ui/query.go`, `internal/ui/query_test.go`. Edits: `internal/ui/ui.go` (route registration in `Server.Handler`), `internal/ui/web/static/js/app.js`, `internal/ui/web/templates/index.html`. **No engine / REST / gRPC / MCP / CLI / proto changes** — `Engine.Query` already exists with proven cross-transport parity.

---

## Phase 1: Setup (UI skeleton)

**Purpose**: Land the `internal/ui/query.go` DTO + stub skeleton so everything downstream compiles before logic lands.

- [X] T001 Create `internal/ui/query.go`: package comment + Slice-2 scope note; define DTO structs field-parallel to the REST contract (`internal/rest/engine_adapter.go::toQueryHits` + `queryRequest`) — `queryRequestDTO`, `queryResponseDTO` (`Hits, RerankFailed, EffectiveK, EffectivePool, EffectiveMode`), `queryHitDTO` (`ChunkID, DocumentID, Score, Content, FilePath, Page, ChunkIndex, SectionContext, SectionDepth, Poisoning, NearDup, Wikilinks, Summary, EnrichmentStatus, ExtractionMethod, ExtractionQuality, Context`), `poisonVerdictDTO`, `nearDupDTO`, `contextChunkDTO` — per [data-model.md](./data-model.md); projection helpers `toQueryHitDTO(engine.QueryHit)` and `toQueryResponseDTO(*engine.QueryResult)`; empty `handleQuery` stub. (R4)

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean; DTOs compile.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the one backend route every story uses — `POST /api/query` calling `Engine.Query` in-process.

**⚠️ CRITICAL**: No user story can function until this route is live.

- [X] T002 Implement `handleQuery` — `internal/ui/query.go`: decode `queryRequestDTO` body → validate (empty/whitespace `query` → 400 `empty query`; `mode` ∉ {`hybrid`,`semantic`,`keyword`} → 400 `invalid mode`; R11) → compose `engine.QueryRequest` (compose filter via `engine.NewFilter(source,type,tags)`) → `s.eng.Query(ctx, req)` → project via `toQueryResponseDTO` → `writeJSON` 200. Engine errors flow through the existing `writeEngineErr` helper (already in `internal/ui/documents.go`, same package — reuse, do not duplicate); map embedder-unreachable and embedding-mismatch to plain guidance (R10). Mirror the REST `handleQuery` mapping 1:1 (`internal/rest/engine_adapter.go::handleQuery`). (R1, R3, R10, R11; [contracts/ui-query.md](./contracts/ui-query.md))
- [X] T003 Register the route — `internal/ui/ui.go::Server.Handler`: add `mux.HandleFunc("POST /api/query", s.guard(s.handleQuery)) // spec 048` alongside the existing guarded `/api/*` registrations. (R3)

**Checkpoint**: `curl -X POST …/api/query` works — 200 happy path, 400 empty/invalid-mode, 401 without Bearer, 503 embedder-down. `make build && make vet` clean.

---

## Phase 3: User Story 1 — Run a query and see ranked results (Priority: P1) 🎯 MVP

**Goal**: An authenticated operator opens Query, types a query, submits, and sees ranked hits with score, citation, and section breadcrumb.

**Independent Test**: [quickstart.md](./quickstart.md) §1 + §2 — hits non-empty with score/citation/section_context; `effective_mode=="hybrid"`; byte-identical to `go-rag query` and REST `/v1/query`; 400 empty; 401 unauth.

### Implementation

- [X] T004 [US1] Alpine Query view — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: on Query view-entry render a query input + Submit (Enter or click); `POST /api/query` with `{query,k:5,mode:"hybrid"}`; render ranked hits — each row shows `score`, citation (`file_path` / `page` / `chunk_index`), `section_context` breadcrumb + `section_depth`, and a `content` preview (expandable to full); sidebar "Query" active (replaces the placeholder); no full-page reload; **explicit submit only** — controls do not auto-fire (R9).
- [X] T005 [US1] US1 tests — `internal/ui/query_test.go`: (a) `POST /api/query` 200 + `hits[]` non-empty with `score`/`file_path`/`chunk_index`/`section_context` present; (b) empty/whitespace `query` → 400; (c) invalid `mode` → 400; (d) 401 without Bearer on an initialized vault; (e) `k` bounds the hit count. (R11, FR-013)

**Checkpoint**: US1 independently testable — runnable retrieval from the browser (MVP).

---

## Phase 4: User Story 2 — Inspect a result's detail, context, and provenance (Priority: P1)

**Goal**: Click a hit → full text, section path/depth, sibling context, document summary/enrichment, and provenance signals.

**Independent Test**: [quickstart.md](./quickstart.md) §1 (detail) — full `content` + `section_context` + `context[]` + provenance render; un-enriched doc shows an empty state, not an error.

### Implementation

- [X] T006 [US2] Alpine hit detail — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: click a hit → detail pane rendering the **already-fetched** payload (no extra round-trip — R7): full `content`; `section_context`/`section_depth`; `context[]` sibling chunks labelled by `is_before`; `summary` + `enrichment_status` (or a clear empty state when un-enriched); provenance — `extraction_method`/`extraction_quality`, `near_dup`, `wikilinks`, and `poisoning` verdict when present. Each provenance field omitted cleanly when absent.
- [X] T007 [US2] US2 tests — `internal/ui/query_test.go`: (a) `queryHitDTO` carries `content` + `section_context` + `context` + all provenance fields per [data-model.md](./data-model.md); (b) `context_window > 0` populates `context[]`; (c) un-enriched source → empty `summary`/`enrichment_status`, not an error; (d) with `include_quarantined=true`, a flagged hit carries a non-nil `poisoning` verdict. (FR-003, SC-002)

**Checkpoint**: US2 independently testable — inspectable hits.

---

## Phase 5: User Story 3 — Control retrieval and see what the engine used (Priority: P2)

**Goal**: Tune mode/k/threshold/rerank/filters/context/dedup/cache, opt into flagged chunks, and see effective mode/k/pool + rerank status.

**Independent Test**: [quickstart.md](./quickstart.md) §4 — threshold trims; tag filter narrows; `effective_mode/k` echo; opt-in reveals flagged chunks with verdicts; the opt-in does not persist.

### Implementation

- [X] T008 [US3] Alpine controls + transparency — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: a controls form (mode select; `k` number clamped [1,50] default 5, R7; `threshold`; `no_rerank`; `source`/`type`/`tags`; `context_window`; `dedup`; `no_cache`; `include_quarantined` toggle **default off**) serialized into the `POST /api/query` body; after results render, show `effective_mode`/`effective_k`/`effective_pool`, with a short note when `effective_k` ≠ requested `k` (classifier acted, R6); a `rerank_failed` banner when true (R2); `include_quarantined` **resets to false on each new query** (R8); controls never auto-fire (R9).
- [X] T009 [US3] US3 tests — `internal/ui/query_test.go`: (a) `threshold` trims — every returned hit has `score ≥ threshold`; (b) `tags`/`source`/`type` narrow (intersection); (c) `effective_mode`/`effective_k`/`effective_pool` echo the engine result; (d) `rerank_failed` surfaces on the response; (e) quarantine-by-default — flagged chunk absent at `include_quarantined=false`, present with verdict at `=true`. (FR-004, FR-005, FR-006, FR-008, R8)

**Checkpoint**: US3 independently testable — controllable, transparent retrieval.

---

## Phase 6: User Story 4 — Read-only, quarantine-by-default, shell-consistent (Priority: P2)

**Goal**: Prove the slice writes nothing, ships no Node chain, degrades gracefully, quarantines by default, and holds cross-transport parity.

**Independent Test**: [quickstart.md](./quickstart.md) §3 + §5 — parity across UI/REST/CLI; quarantine-by-default; no Node artifacts; embedder-down/mismatch surface plainly.

### Implementation / Verification

- [X] T010 [US4] Cross-transport parity test — `internal/ui/query_test.go::TestUIQuery_Parity` (pattern of the Documents view's parity test + `internal/engine/parity_test.go`): against one engine, assert `POST /api/query` returns byte-identical `hits`/order/`score` to REST `POST /v1/query` and the engine direct call for identical input. (R12, FR-013, SC-003)
- [X] T011 [US4] No-write + no-Node + quarantine invariants — `internal/ui/query_test.go`: (a) a query leaves document/chunk/embedding counts unchanged (snapshot `engine.Status()` before/after — no mutation via the query path); (b) repo-root scan finds no `package.json`/`node_modules`/`vite.config.*`/`tailwind.config.*`; (c) quarantine-by-default confirmed (flagged chunk excluded unless opted in). (FR-009, FR-011, FR-007, SC-005, SC-006, SC-007)
- [X] T012 [US4] Empty/error-state rendering — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: deliberate states for empty corpus, no-results (hint to lower threshold / broaden filters), in-flight loading, embedder-unreachable (suggest keyword mode, R10), embedding-mismatch (suggest re-embed / switch model), and session-expired 401 → login. Never a silent failure or crash. (FR-012, SC-001)

**Checkpoint**: US4 independently testable — the invariants are pinned.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T013 [P] Gate hygiene — `make lint` (0 findings), `make vet`, `make test -race` clean across `internal/ui` (and any touched package). Independently re-run by the parent DA: build/vet/lint `0 issues`, `make test -race` 31/31 `ok` (internal/ui 179.5s, 67.2% cov).
- [X] T014 [P] quickstart validation — run [quickstart.md](./quickstart.md) §1–§6 on an isolated DB + non-default ports: `curl` smoke for §1–§5 + **Interceptor** browser verify for §6 (mandatory per CLAUDE.md — `curl` 200 alone is not "the view works"). Smoke: 400 empty / 400 invalid-mode / 401 / 200 happy-path / UI==REST byte-parity all confirmed. Interceptor (real Chrome): login → Query view renders REAL (all controls + transparency bar, R8/R9/R11 inline labels) → query → ranked hits with score/rank/section/poisoning badge → `/api/query` 200, zero console errors / 404 / 500. (Screenshot failed on an Interceptor SVG-rasterize bug — not a go-rag defect; a11y tree + text + network log are the evidence.)
- [X] T015 [P] Doc sync — update spec 046's Slice Decomposition row (048 status); update `PROJECTS.md` go-rag entry + MuninnDB memory to reflect Slice 2 shipped. Spec 046 row → Done/shipped; data-model.md `contextChunkDTO` corrected (`chunk_index` → `chunk_id`, faithful to `engine.ContextChunk`).

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: no deps — start immediately. T001 (skeleton) blocks T002 (handler logic).
- **Foundational (Phase 2)**: depends on Setup; **blocks US1–US3** (they all call `POST /api/query`). T002 → T003.
- **US1 (Phase 3)**: depends on Foundational. MVP gate.
- **US2 (Phase 4)**: depends on Foundational + US1 (detail opens from a result row; reuses US1's hit rendering).
- **US3 (Phase 5)**: depends on Foundational + US1 (controls augment the US1 query form).
- **US4 (Phase 6)**: depends on US1–US3 (verifies them).
- **Polish (Phase 7)**: depends on all stories complete.

### User-story independence
- US1 is the MVP gate — testable alone once Foundational lands (curl + a minimal results UI).
- US2 is client-side (the hit payload already carries full text + context + provenance — no new route).
- US3 reuses US1's query/results view; adds the controls form + transparency indicators.
- US4 is cross-cutting verification of US1–US3.

### Parallel opportunities
- Phase 1: T001 is the single skeleton task (sequential — everything builds on it).
- Phase 2: T002 → T003 sequential (route registration needs the handler symbol).
- Story test tasks (T005, T007, T009) can run alongside their UI implementation tasks where files differ (test file vs template/js).
- US2 (T006) and US3 (T008) both edit `app.js`/`index.html` — sequence them (US2 before US3) to avoid file conflicts.

---

## Parallel Example: US1 (handler-already-live → UI + test)

```bash
# Once Phase 2 lands (POST /api/query live), US1's two tasks can overlap where files differ:
Task: "Alpine Query view (input + results) in internal/ui/web/static/js/app.js + templates/index.html"   # T004
Task: "US1 handler tests in internal/ui/query_test.go"                                                     # T005
```

---

## Implementation Strategy

### MVP First
1. Complete Phase 1 (Setup) + Phase 2 (Foundational) — `POST /api/query` live, curl-proven.
2. Complete Phase 3 (US1 — run query + results). **STOP and VALIDATE**: hits match `go-rag query` byte-for-byte (quickstart §2). This is the **MVP gate** — retrieval from the browser.
3. Complete Phase 4 (US2) — the **demo-complete** point: query → inspect a hit's full text, context, and provenance. The euphoric-surprise moment.
4. Phase 5 (US3) + Phase 6 (US4) + Phase 7 (Polish) add controls/transparency + harden + verify.

### Incremental delivery
- Setup → Foundational → US1 (MVP) → US2 (demo) → US3 → US4 → Polish.
- Each checkpoint is independently testable per its Independent Test.

### Single-author note
This repo commits straight to `main` (CLAUDE.md). Commit after each task or logical group; run `make lint && make test -race` before push.

---

## Notes

- All retrieval funnels through `Engine.Query` — the UI makes no independent retrieval decision (R1). Cross-transport parity is the proof (FR-013, pinned by T010).
- **No new engine capability, no proto, no migration** — `Engine.Query` already exists across all transports. This is the simplest console slice so far (contrast spec 047's 5-transport `ListChunks` work).
- The hit DTO carries full text + sibling context + provenance in one payload, so US2 detail is **client-side** — no detail round-trip (R7).
- Per-stage score breakdown (BM25/vector/rerank) is intentionally absent — the engine returns one fused `Score`; deferred to a future engine spec (R2, OQ1).
- Vendoring (not building) remains the constraint: no Node/Vite/Tailwind, single binary (FR-011).
