# Tasks: Observability View

**Input**: Design documents from `/specs/054-ui-observability-view/`

**Prerequisites**: spec.md ✓, plan.md ✓ (data-model + contracts + quickstart inlined into plan.md;
no separate research.md/data-model.md/contracts/ quickstart.md for this slice).

**Tests**: INCLUDED. UI-only slice — telemetry read via `prometheus.DefaultGatherer().Gather()`
(no engine change); audit via the existing read-only `Engine.AuditRead(vault, opts)`.

**Constitution**: UI-only — no storage key-space change (no migration, `ExpectedVersion`
unchanged); pure-Go / no Node (III); local-first / zero egress (I, the posture note);
async-after-ACK untouched (IV). Compliant by construction.

## Format: `[ID] [P?] [Story?] Description (file path)`

---

## Phase 1: Setup (UI skeleton)

- [X] T001 Create `internal/ui/observability.go`: DTO structs per plan.md §Data model — `telemetryResponse{ProcessWide, Operations []opStat, Cache, ErrorRate, Posture, FreshAt}`, `opStat{Op, Count, P50, P95, P99, Errors}`, `cacheStat{Result, Embedding hitMiss}`, `postureNote{MetricsLocal, AuditLocal, QueryHashed, RetentionBytes}`, `auditPageResponse{Events, Type, Since, Limit, Offset, Truncated, Enabled}`, `auditEventDTO` (query→hash/mode/k/hits/status/ts; ingest→path/counts/ts; auth-fail→transport/ts); projection helpers `gatherTelemetry()`, `toAuditEventDTO(audit.Event)`; empty handler stubs (`handleObservabilityMetrics`, `handleObservabilityAudit`).

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean.

---

## Phase 2: Foundational (handlers + routes + placeholder cleanup)

- [X] T002 Implement handlers in `internal/ui/observability.go`: `handleObservabilityMetrics` (`prometheus.DefaultGatherer().Gather()` → project the `gorag_*` families → `telemetryResponse`; `ProcessWide:true`; zero families → healthy zero state, not error) + `percentileFromBuckets(cumulative []bucket, total uint64, q float64) float64` helper (returns `-1` for <2 samples / all-in-one-bucket); `handleObservabilityAudit` (`s.eng.AuditRead(vault, opts)` → `auditPageResponse`; query rows carry `QueryHash` only — never raw text; `Enabled` from `cfg.EffectiveAuditLogEnabled()`; vault from the shell resolver mirroring 053/049). Errors via `writeEngineErr`.
- [X] T003 Register routes in `internal/ui/ui.go::Server.Handler`: `GET /api/observability/metrics`, `GET /api/observability/audit` — both guarded `// spec 054`. Clean `internal/ui/placeholder.go`: delete the `"observability"` entry from `placeholderViews` and correct the stale comment + spec numbers (documents=047, query=048, operations=049, vaults=051, observability=054 now built; settings + memory-graph remain placeholders).

**Checkpoint**: `curl GET /api/observability/metrics` → 200 (zero-valued cold or projected); `GET /api/observability/audit` → 200; both 401 without Bearer.

---

## Phase 3: User Story 1 — Live telemetry in-browser (Priority: P1) 🎯 MVP

**Goal**: operator opens Observability and sees op counts, latency percentiles, error rate, cache hit/miss — agreeing with `/metrics`.

**Independent Test**: plan.md §Quickstart — run a mixed workload; telemetry tiles present + plausible; UI == `/metrics` scrape.

- [X] T004 [US1] Alpine telemetry panel — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `internal/ui/web/static/css/components.css`): on Observability view-entry fetch `/api/observability/metrics`; render tiles (query/ingest counts, p50/p95/p99, error rate, cache hit/miss) + a per-operation **sortable** table (`setSort`/`sortedOps` mirroring `setDocSort`); refresh button (+ optional auto-poll toggle, off by default); "process-wide (all vaults)" label; cold/zero → healthy "no data yet" tiles, never an error.
- [X] T005 [US1] US1 tests — `internal/ui/observability_test.go`: (a) `percentileFromBuckets` unit test — known histogram → known quantile; <2 samples → `-1`; all-in-one-bucket → `-1`; (b) `GET /api/observability/metrics` 200 + op fields present after a recorded query/ingest; (c) **telemetry parity** — UI gather == `/metrics` scrape for the same `gorag_*` families (counts + bucket counts); (d) cold daemon → zero-valued fields, status 200 (not error); (e) 401 without Bearer.

**Checkpoint**: US1 independently testable — in-browser telemetry matching `/metrics` (MVP).

---

## Phase 4: User Story 2 — Filterable audit-log browser (Priority: P1)

**Goal**: browse the full audit trail filtered by type + time window; query rows hash-only.

**Independent Test**: plan.md §Quickstart — generate one event of each type; filter works; plaintext absent from DOM.

- [X] T006 [US2] Alpine audit panel — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ components.css): fetch `/api/observability/audit?type=&since=&limit=&offset=`; type filter (all / query / ingest / auth-fail) + time-window picker; bounded **sortable** table, newest first (50/page + older/newer); query row = hash (monospace) + mode + top-k + hits + status + ts; ingest row = path + outcome counts (no content); auth-fail row = transport + ts (no credential); `enabled:false` → "audit logging is off" state; empty/missing → healthy empty state.
- [X] T007 [US2] US2 tests — `internal/ui/observability_audit_test.go`: (a) filter by type returns only matching rows; (b) filter by time window; (c) **hash-only / no-plaintext** — the query `auditEventDTO` has no raw-query field and the marshalled JSON contains no query text; (d) `audit_log_enabled=false` → `enabled:false`, healthy (not error); (e) missing/empty log → healthy empty (`[]`); (f) vault isolation — audit reads the active vault's path; (g) **audit parity** — UI response == `Engine.AuditRead` direct call for the same opts; (h) 401 without Bearer.

**Checkpoint**: US2 independently testable — filterable, plaintext-free audit browser.

---

## Phase 5: User Story 3 — Posture + retention context (Priority: P2)

**Goal**: trust label — local-only metrics, local append-only audit, hashed queries, retention cap.

**Independent Test**: posture renders + retention matches `EffectiveAuditLogMaxBytes`.

- [X] T008 [US3] Alpine posture footer — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: render `Posture` from the metrics response — "metrics local-only (zero egress)", "audit log local + append-only", "query text hashed (never stored)", and the retention cap (human-readable bytes from `RetentionBytes`).
- [X] T009 [US3] US3 test — `internal/ui/observability_test.go`: the `Posture` block renders with `MetricsLocal:true`, `AuditLocal:true`, `QueryHashed:true`, and `RetentionBytes == cfg.EffectiveAuditLogMaxBytes()`.

**Checkpoint**: US3 independently testable — legible, accurate posture label.

---

## Phase 6: User Story 4 — Vault-aware, guarded, no Node (Priority: P2)

**Goal**: hard invariants — vault param flows to audit; Bearer-guarded; single binary; edge states degrade.

- [X] T010 [US4] Invariant tests — `internal/ui/observability_test.go`: (a) vault param flows — switching the shell vault changes the audit trail returned (per-vault path); (b) telemetry is labelled `process_wide:true` (not per-vault — no misleading scoping); (c) repo-root scan finds no `package.json` / `node_modules` introduced; (d) no `/api/observability/*` route reachable without Bearer; (e) session-expiry 401 → graceful (no crash). (FR-005, FR-006, FR-007, FR-010)

**Checkpoint**: US4 independently testable — invariants pinned.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T011 [P] Gate hygiene — `make lint` (0), `make vet`, `make test -race` clean.
- [X] T012 [P] Quickstart validation — run plan.md §Quickstart on an isolated store (`--db-path <tmp>` + non-default addrs): add a doc, run a few queries, trigger an auth-fail; curl smoke + **Interceptor browser-verify** US1 telemetry (values match `/metrics`) and US2 filtered audit (incl. a DOM-grep asserting query plaintext is absent).
- [X] T013 [P] Doc sync — update `PROJECTS.md` + MuninnDB memory: Observability view (spec 054) shipped; remaining console placeholders = Settings (unblocked) + Memory & Graph (blocked on the MuninnDB bridge); move the spec 054 entry from planned → shipped.

---

## Dependencies & Execution Order

- **Setup (Phase 1)**: T001 blocks T002 (DTOs before handlers).
- **Foundational (Phase 2)**: T002 → T003 (routes need handler symbols; placeholder cleanup pairs with route registration).
- **US1 (Phase 3)**: depends on Foundational. **MVP gate.**
- **US2 (Phase 4)**: depends on Foundational (independent of US1 — audit ≠ telemetry).
- **US3 (Phase 5)**: depends on US1 (posture rides on the metrics response).
- **US4 (Phase 6)**: depends on US1–US3.
- **Polish (Phase 7)**: depends on all stories.

**MVP: US1** (T001→T005). **Demo-complete: US1 + US2** (telemetry + filterable audit).

## Parallel Opportunities

- **US1 ↔ US2** are independent after Foundational (T002/T003): the telemetry panel (T004/T005) and the audit panel (T006/T007) touch different fetch endpoints and can be built in parallel (distinct code regions in `app.js`, distinct test files).
- **T011 / T012 / T013** (Polish) are mutually parallel once the stories land.

## Implementation Strategy

Setup → Foundational → US1 (MVP) → US2 (parallel-safe) → US3 → US4 → Polish. Each checkpoint independently testable; each phase lands `build + vet + test -race` green.
