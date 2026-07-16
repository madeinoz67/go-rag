---

description: "Task list for spec 056 — Settings: System & Transports (Slice 1)"
---

# Tasks: Settings — System & Transports (Slice 1)

**Input**: Design documents from `/specs/056-ui-settings-system-transports/` (spec.md, plan.md, research.md, data-model.md, contracts/system.md, quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓

**Tests**: INCLUDED — go-rag constitution mandates `go test ./...` green on every change; every console slice shipped race-clean tests.

**Organization**: Tasks grouped by user story (US1/US2/US3 from spec.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on incomplete tasks)
- **[Story]**: US1 / US2 / US3 (setup + foundational + polish have no story label)
- Exact file paths in every description

## Design refinements (from research.md)

1. **Version plumbing** — the binary version (`main.version`) is not visible to `internal/ui`. T002 adds a `NewWithVersion(eng, token, version)` constructor + `version`/`startedAt` Server fields; `New(eng, token)` is preserved (test callers unaffected). No package-level mutable state.
2. **Update-check is operator-initiated** (US3) — `POST /api/settings/updates/check` runs ONLY on explicit click (Constitution I; never on view load). Offline ⇒ `latest="unknown"`.
3. **Read-only reuse** — daemon/migrate/upgrade/config are READ, not modified. No engine/storage/migration change.

---

## Phase 1: Setup

**Purpose**: Confirm a green baseline on `main`.

- [x] T001 Verify baseline green on `main`: run `make build && go test ./...` — all packages pass before starting (gate only).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Version/uptime plumbing + the system handler + route that every story depends on.

**⚠️ CRITICAL**: No user-story work can begin until this phase is complete.

- [x] T002 Add `NewWithVersion(eng *engine.Engine, token, version string) *Server` to `internal/ui/ui.go`, plus `version string` and `startedAt time.Time` fields on `Server`. `New(eng, token)` delegates to `NewWithVersion(eng, token, "unknown")` with `startedAt: time.Now()` (test callers unchanged).
- [x] T003 Wire `internal/cli/serve.go` to build the UI via `ui.NewWithVersion(eng, token, version)` instead of `New(...)` — pass the binary version that `serve` already receives (one call site).
- [x] T004 Create `internal/ui/system.go` — define `systemStatusDTO` (`version`, `pid`, `uptime_seconds`, `schema{on_disk,expected,unified_store}`, `transports[]{kind,address,loopback,state}`, `bind_warning`) and `Server.handleSystem(w, r)`: project PID+addrs via `daemon.Status`/`ReadPID`/`ReadAddrs`, loopback posture via `daemon.IsLoopbackBind`/`NonLoopbackBinds`/`ExternalBindWarning`, schema via `migrate.readVersion(db)`+`migrate.ExpectedVersion`, transports from `eng.Config()`, version from `s.version`, uptime from `s.startedAt`. Read-only, no egress. Mirror `settings.go` handler style.
- [x] T005 Register `GET /api/settings/system` in `internal/ui/ui.go`, guarded by the same Bearer middleware (next to the `GET /api/settings` route from spec 055).
- [x] T006 [P] Add the System & Transports Alpine section to `internal/ui/web/static/js/app.js` (`loadSystem` fetches `/api/settings/system`) and a `<section>` block under the Settings view in `internal/ui/web/templates/index.html` (below the 055 Effective Configuration cards). Section bodies filled in T007/T009.

**Checkpoint**: `/api/settings/system` returns the full DTO; the Settings view shows the System & Transports section shell.

---

## Phase 3: User Story 1 — System identity (Priority: P1) 🎯 MVP

**Goal**: Operator sees version / PID / uptime / schema; values match `go-rag version` + the pidfile + migrate.

**Independent test**: `GET /api/settings/system` identity fields equal the binary version, the running PID, and the on-disk schema version.

- [x] T007 [P] [US1] Render the identity block in the System & Transports Alpine section (`app.js` / `index.html`) — version, PID, uptime (humanized), schema on_disk/expected + unified-store posture.
- [x] T008 [US1] Add `internal/ui/system_test.go` — `TestSystem_Identity`: authenticated `GET /api/settings/system` → 200; `version` equals the `NewWithVersion`-supplied version; `schema.on_disk == migrate.readVersion(db)`; `schema.expected == migrate.ExpectedVersion`; `schema.unified_store == true`.

**Checkpoint**: US1 functional — system identity visible + correct.

---

## Phase 4: User Story 2 — Transport posture (Priority: P1)

**Goal**: Operator sees the 4 transport binds + loopback/external status; security-relevant, matches the listening ports.

**Independent test**: `transports[]` addresses match `eng.Config()`; loopback=true for loopback binds; `bind_warning` empty when all loopback.

- [x] T009 [P] [US2] Render the transports block in the Alpine section — the 4 transports (MCP/REST/gRPC/UI) with address + loopback/disabled state, plus `bind_warning` flagged when any bind is non-loopback.
- [x] T010 [US2] Add `TestSystem_Transports` to `internal/ui/system_test.go` — 4 transports present (or "disabled" for empty addrs); addresses match `cfg` listen addrs; loopback flag correct; `bind_warning` empty when all loopback, non-empty when `--bind-external`.

**Checkpoint**: US1 + US2 functional — identity + transports visible + correct.

---

## Phase 5: User Story 3 — Update availability (Priority: P2, operator-initiated)

**Goal**: Operator can check for a newer release; current version always local, the check is explicit + air-gap-preserving.

**Independent test**: `POST /api/settings/updates/check` returns `{current, latest, newer_available, checked_at}`; offline ⇒ `latest="unknown"`; never auto-fires.

- [x] T011 [US3] Add `Server.handleUpdateCheck` + `updateCheckDTO` to `internal/ui/system.go`: call `upgrade.LatestVersion()` (resilient — short timeout, offline ⇒ "unknown") + `upgrade.NewerVersionAvailable(current, latest)`; return `{current: s.version, latest, newer_available, checked_at: now}`. Register `POST /api/settings/updates/check` in `internal/ui/ui.go` (guarded).
- [x] T012 [US3] Add `TestSystem_UpdateCheck` to `internal/ui/system_test.go` — 200; `current == s.version`; offline/parse-fail path ⇒ `latest="unknown"`, `newer_available=false` (use the dev-skip / default path or a short timeout so the test does no real network call); confirm the route is POST-only (never invoked by `GET /api/settings/system`).
- [x] T013 [US3] Add a "Check for updates" button to the System & Transports Alpine section (`app.js` / `index.html`) that calls `POST /api/settings/updates/check` ONLY on click, shows current/latest/newer_available with a loading state. Browser-verify via Interceptor that opening the Settings view triggers NO network call to the release source (egress is click-only — SC-003).

**Checkpoint**: All three stories functional; the update-check is honestly operator-initiated.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [x] T014 Run full gates — `make build && go vet && go test -race ./...` (all packages green, including new `internal/ui` system tests) and `make lint` (golangci-lint, zero findings — the `ci.yml` gate).
- [x] T015 Run `specs/056-ui-settings-system-transports/quickstart.md` end-to-end on an isolated daemon — identity matches `go-rag version` + pidfile + migrate; transports match `lsof`; update-check returns current; `GET /api/settings/system` does NO egress; 401 unguarded; teardown.
- [x] T016 [P] Update `~/.claude/LIFEOS/USER/PROJECTS/PROJECTS.md` go-rag entry — note Settings Slice 1 (spec 056, System & Transports) shipped; Slices 2 (Auth) + 3 (Config Editing) remain.
- [x] T017 Commit to `main` per the repo's Conventional-Commits rhythm: `docs(spec056): specify + plan + tasks` for the `specs/056-…/` docs, then `feat(ui): settings — system & transports (spec 056)` for the implementation.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: none.
- **Foundational (Phase 2)**: depends on Phase 1 — **BLOCKS all user stories**.
- **User stories (Phase 3–5)**: each depends on Phase 2; US1 → US2 → US3 in priority order (shared Alpine section → sequential rendering).
- **Polish (Phase 6)**: depends on all stories complete.

### Within each user story
- Render (view) + parity test land together; the test gates the story.

### Parallel opportunities
- T006 (Alpine shell) ∥ T002–T005 (Go stack).
- T016 (PROJECTS.md) ∥ the gates.

---

## Implementation Strategy

### MVP first (US1)
Phase 1 → Phase 2 foundation → Phase 3 US1 (system identity visible + correct) → **validate**. The slice already delivers value; US2/US3 round it out.

### Incremental delivery
Foundation → +US1 (MVP) → +US2 (transports) → +US3 (update-check) → Polish (gates + quickstart + commit).

---

## Notes

- [P] tasks = different files, no dependency on incomplete tasks.
- Constitution compliance pre-checked (plan.md): read-only (Principle IV N/A); UI-layer only (Principle V); no on-disk layout change (no migration, no `ExpectedVersion` bump — it is READ); the update-check's opt-in egress is the documented `go-rag upgrade` operator-utility exception (Principle I).
- Non-overlap with 049 (Bridge Ops) + 054 (Observability) enforced in FR-009.
- Commit after each logical group; stop at any checkpoint to validate a story independently.
