# Tasks: go-rag Management Console — Bridge Ops View (Slice 3)

**Input**: Design documents from `/specs/049-ui-bridge-ops/` — [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/ui-bridge-ops.md](./contracts/ui-bridge-ops.md), [quickstart.md](./quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓.

**Tests**: INCLUDED — go-rag is test-gated (`make test -race`, `make lint(0)`) and the constitution enforces "Spec/Test/Evals First". Every story ships a test task. The operational tiles reuse `engine.StatusInfo` (no new accessor); the one new engine surface is the thin read-only `Engine.AuditRead` wrapper.

**Organization**: Tasks grouped by user story. Research decision tags (R1–R9) cross-link to [research.md](./research.md); FR/SC tags cross-link to [spec.md](./spec.md).

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[USx]**: user-story phase tag (Setup/Foundational/Polish tasks carry none)
- Every task names its exact file path + the symbol/seam it touches

## Path conventions

New files: `internal/engine/audit_read.go` (+ test), `internal/ui/bridgeops.go` (+ test). Edits: `internal/ui/ui.go` (route registration in `Server.Handler`), `internal/ui/web/static/js/app.js`, `internal/ui/web/static/css/components.css`, `internal/ui/web/templates/index.html`. **No REST / gRPC / MCP / CLI / proto changes** — audit-read is CLI-exposed already; cross-transport audit parity is a pre-existing gap deferred to a follow-up.

---

## Phase 1: Setup (UI skeleton)

**Purpose**: Land the `internal/ui/bridgeops.go` DTO + stub skeleton so everything downstream compiles before logic lands.

- [ ] T001 Create `internal/ui/bridgeops.go`: package comment + Slice-3 scope note; define DTO structs per [data-model.md](./data-model.md) — `bridgeOpsStatsDTO` (vault, last_activity, backlog, drift{...}, subsystems{poisoning,enrichment,caches,adaptive}, watch), `cacheStatsDTO`, `activityEventDTO`, `activityResponseDTO`; projection helpers `toBridgeOpsStats(*engine.StatusInfo, watchDirs []string)` and `toActivityEvents([]audit.Event)`; empty `handleBridgeOpsStats` + `handleBridgeOpsActivity` stubs. (R1, R2)

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean; DTOs compile.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the engine audit wrapper + the two backend routes every story renders against.

**⚠️ CRITICAL**: No user story can function until these are live.

- [ ] T002 Implement `Engine.AuditRead` — `internal/engine/audit_read.go`: `func (e *Engine) AuditRead(opts audit.ReadOptions) ([]audit.Event, error)`, a thin read-only wrapper that resolves the audit path (`e.cfg.AuditPath` or `audit.DefaultPath(e.cfg.DBPath)`) and delegates to `audit.Read(path, opts)`. Read-only — opens no DB write. (R3)
- [ ] T003 `Engine.AuditRead` tests — `internal/engine/audit_read_test.go`: (a) path resolution (configured vs default); (b) tail + type filter pass-through matches `audit.Read` directly; (c) missing/disabled audit log → empty slice, no error; (d) read-only (no DB mutation). (R3)
- [ ] T004 Register routes — `internal/ui/ui.go::Server.Handler`: add `mux.HandleFunc("GET /api/bridge-ops/stats", s.guard(s.handleBridgeOpsStats))` and `mux.HandleFunc("GET /api/bridge-ops/activity", s.guard(s.handleBridgeOpsActivity))` (// spec 049). (R1)
- [ ] T005 Implement `handleBridgeOpsStats` — `internal/ui/bridgeops.go`: `s.eng.Status()` → `toBridgeOpsStats(...)`, including surfacing the configured `WatchDirs` (read from the engine config; `watch.scan_driven = true`); 200 always. Engine error → `writeEngineErr` (existing helper, same package). (R1, R2, R5)
- [ ] T006 Implement `handleBridgeOpsActivity` — `internal/ui/bridgeops.go`: parse `tail` (clamp [0,100], default 20) + `type` (default `ingest`; validate `ingest|query|auth-fail`, else 400 `invalid type`) → `s.eng.AuditRead(audit.ReadOptions{Type, Tail})` → `toActivityEvents(...)` → 200 `{events, count}`. Missing/disabled log → `{events:[], count:0}` (healthy empty). (R1, R4)

**Checkpoint**: `curl /api/bridge-ops/stats` + `/api/bridge-ops/activity` work — 200 happy path, 400 invalid-type, 401 without Bearer. `make build && make vet` clean.

---

## Phase 3: User Story 1 — Pipeline health at a glance (Priority: P1) 🎯 MVP

**Goal**: An operator opens Bridge Ops and sees backlog (pending/failed), completion, drift verdict + cause, and last activity — the "is it healthy and progressing" view.

**Independent Test**: [quickstart.md](./quickstart.md) §1 — backlog + drift + subsystem tiles render values matching `go-rag status`; counts match.

### Implementation

- [ ] T007 [US1] Alpine health view — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `components.css`): on Bridge Ops view-entry fetch `/api/bridge-ops/stats`; render the backlog tile (pending / failed / completion flag), the drift verdict + one-line cause, and last-activity timestamp; sidebar "Bridge Ops" active (replaces the placeholder); explicit refresh button (R8 — no auto-poll/streaming); no full-page reload.
- [ ] T008 [US1] US1 tests — `internal/ui/bridgeops_test.go`: (a) `GET /api/bridge-ops/stats` 200 + backlog/drift/last_activity fields present; (b) values match `go-rag status` for the same vault (parity); (c) 401 without Bearer; (d) fresh/empty vault → zero backlog, `drift.verdict` present, no error. (FR-012, SC-002)

**Checkpoint**: US1 independently testable — operational health from the browser (MVP).

---

## Phase 4: User Story 2 — Recent ingest / reingest activity (Priority: P1)

**Goal**: A bounded recent-activity feed of ingest events with outcomes + timestamps; failures distinguishable.

**Independent Test**: [quickstart.md](./quickstart.md) §2 — activity shows recent ingest events matching `go-rag audit --type ingest`; failures distinguishable; empty vault → `{events:[],count:0}`.

### Implementation

- [ ] T009 [US2] Alpine activity feed — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: fetch `/api/bridge-ops/activity?tail=20&type=ingest`; render a reverse-chron list (type, timestamp, summary, outcome) with failures visually distinct from successes; healthy empty state when no events; a control to broaden `type` (ingest/query/auth-fail) and adjust `tail`. Re-renders on refresh.
- [ ] T010 [US2] US2 tests — `internal/ui/bridgeops_test.go`: (a) `GET /api/bridge-ops/activity` 200 + events most-recent-first; (b) parity with `go-rag audit --type ingest --tail 20` for the same vault; (c) `tail` clamp + `type` validation (400 on bogus type); (d) missing/disabled audit log → `{events:[],count:0}`; (e) 401 without Bearer. (R3, R4, FR-004, FR-005)

**Checkpoint**: US2 independently testable — a findable recent-activity feed.

---

## Phase 5: User Story 3 — Subsystem states (Priority: P2)

**Goal**: Tiles for poisoning / enrichment / caches / adaptive retrieval (on/off + the numbers that matter) + expandable drift detail.

**Independent Test**: [quickstart.md](./quickstart.md) §1 (subsystem tiles) — each tile matches `go-rag status`; drift detail expands to the baseline-vs-live breakdown.

### Implementation

- [ ] T011 [US3] Alpine subsystem tiles + drift detail — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `components.css`): render a subsystem tile grid (poisoning: enabled/flagged/sources/phrases; enrichment: enabled/captioning/enriched_docs; caches: result + embedding enabled/size/hits/misses; adaptive: pool_size/enabled/utilization/near_dup_chunks) from `/api/bridge-ops/stats`; make the drift tile expandable to the full baseline-vs-live breakdown (`drift.baseline`, `live_ollama_ver`, drift counts). Tiles show "off/disabled" cleanly when a subsystem is off. (R6, R7)
- [ ] T012 [US3] US3 tests — `internal/ui/bridgeops_test.go`: (a) subsystems DTO carries poisoning/enrichment/caches/adaptive with the `StatusInfo`-sourced values; (b) drift detail (baseline + cause) present and matches `StatusInfo`; (c) all-subsystems-off (default) renders cleanly, not as error. (FR-006, SC-002)

**Checkpoint**: US3 independently testable — subsystem + drift-detail visibility.

---

## Phase 6: User Story 4 — Read-only, shell-consistent, honest watcher (Priority: P2)

**Goal**: Prove the slice writes nothing, ships no Node chain, degrades gracefully, and is honest about the scan-driven watcher.

**Independent Test**: [quickstart.md](./quickstart.md) §3 + §4 — read-only/guard/no-Node invariants; empty/embedder-down edge states; watch config reflects scan-driven.

### Implementation / Verification

- [ ] T013 [US4] No-write + no-Node + guard invariants — `internal/ui/bridgeops_test.go`: (a) snapshot `engine.Status()` counts before/after a stats+activity fetch → identical (no mutation via the Bridge Ops path); (b) repo-root scan finds no `package.json`/`node_modules`/`vite.config.*`/`tailwind.config.*`; (c) every `/api/bridge-ops/*` route is `GET` (no write verb registered; POST → 405); (d) 401 without Bearer on both routes. (FR-008, FR-010, FR-011, SC-005, SC-006)
- [ ] T014 [US4] Empty + embedder-down + watch-honesty rendering — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: deliberate states for fresh/empty vault (zero backlog, "no recent activity", drift `n/a`), embedder unreachable (drift/version signals degrade to "unknown" plainly), and the watch section reflecting `scan_driven: true` (no live-watcher claim). Never a silent failure or crash. (FR-007, FR-012)

**Checkpoint**: US4 independently testable — the invariants are pinned.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T015 [P] Gate hygiene — `make lint` (0 findings), `make vet`, `make test -race` clean across `internal/engine` + `internal/ui` (and any touched package).
- [ ] T016 [P] quickstart validation — run [quickstart.md](./quickstart.md) §1–§4 on an isolated DB via `serve --db-path <tmp>` (non-default ports): `curl` smoke for §1–§3 + **Interceptor** browser verify for §4 (mandatory per CLAUDE.md). Note: prefer `serve` over `start` for the isolated smoke (the `start --db-path <tmp>` isolation quirk).
- [ ] T017 [P] Doc sync — update spec 046's Slice Decomposition row (049 status); update `PROJECTS.md` go-rag entry + MuninnDB memory to reflect Slice 3 shipped; note the pre-existing audit cross-transport-parity follow-up.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: no deps. T001 (skeleton) blocks T005/T006 (handlers).
- **Foundational (Phase 2)**: depends on Setup; **blocks US1–US3**. T002→T003 (wrapper+test); T004 (routes) needs the handler symbols from T005/T006 — so T005/T006 may land before or alongside T004 (all in Phase 2). T006 (activity handler) depends on T002 (`Engine.AuditRead`).
- **US1 (Phase 3)**: depends on Foundational (renders `/api/bridge-ops/stats`). MVP gate.
- **US2 (Phase 4)**: depends on Foundational (renders `/api/bridge-ops/activity` + the `Engine.AuditRead` wrapper).
- **US3 (Phase 5)**: depends on Foundational + US1 (subsystem tiles + drift detail augment the stats view US1 renders).
- **US4 (Phase 6)**: depends on US1–US3 (verifies them).
- **Polish (Phase 7)**: depends on all stories complete.

### User-story independence
- US1 is the MVP gate — testable alone once Foundational lands (stats route + a minimal health UI).
- US2 binds to the activity route + `Engine.AuditRead`; testable independently of US1/US3.
- US3 reuses US1's stats fetch; adds subsystem tiles + drift-detail expansion.
- US4 is cross-cutting verification of US1–US3.

### Parallel opportunities
- Phase 2: T002 (engine wrapper) is independent of T005 (stats handler) — parallel different files. T003 (wrapper test) follows T002.
- Story test tasks (T008, T010, T012) can run alongside their UI implementation tasks where files differ (test file vs template/js).
- US1 (T007) and US2 (T009) both edit `app.js`/`index.html` — sequence them; US3 (T011) after US1 (same files).

---

## Parallel Example: Phase 2 (backend)

```bash
Task: "Engine.AuditRead wrapper in internal/engine/audit_read.go"        # T002
Task: "handleBridgeOpsStats (StatusInfo -> DTO) in internal/ui/bridgeops.go"  # T005  (different package/file; no dep on T002)
```

---

## Implementation Strategy

### MVP First
1. Complete Phase 1 (Setup) + Phase 2 (Foundational) — both routes live, `Engine.AuditRead` shipped, curl-proven.
2. Complete Phase 3 (US1 — health). **STOP and VALIDATE**: backlog/drift/last-activity match `go-rag status` (quickstart §1). This is the **MVP gate** — operational health from the browser.
3. Complete Phase 4 (US2) — the **demo-complete** point: health + a live recent-activity feed. The euphoric-surprise moment.
4. Phase 5 (US3) + Phase 6 (US4) + Phase 7 (Polish) add subsystems + harden + verify.

### Incremental delivery
- Setup → Foundational → US1 (MVP) → US2 (demo) → US3 → US4 → Polish.
- Each checkpoint is independently testable per its Independent Test.

### Single-author note
This repo commits straight to `main` (CLAUDE.md). Commit after each task or logical group; run `make lint && make test -race` before push.

---

## Notes

- All operational data funnels through `engine.StatusInfo` + `engine.AuditRead` — the UI makes no independent data decision (R1, R2). Cross-transport parity is the proof for stats (matches `go-rag status`); activity matches `go-rag audit`.
- `Engine.AuditRead` is a **thin read-only wrapper** over the existing `audit.Read` (spec 021, CLI-exposed) — not a new operation. Full audit cross-transport parity (MCP/REST/gRPC) is a **pre-existing spec 021 gap**, deferred to a follow-up (Constitution Check + Complexity Tracking).
- The daemon runs **no persistent watcher** — `watch.scan_driven = true` is the honest framing; an always-on watcher would add a live tile here in a future slice (R5, spec US4).
- Vendoring (not building) remains the constraint: no Node/Vite/Tailwind, single binary (FR-010).
- Refresh is manual (on view-entry + button); no SSE/streaming this slice (R8).
