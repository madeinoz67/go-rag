---

description: "Task list for H13 — Loopback Bind by Default"
---

# Tasks: Loopback Bind by Default (H13)

**Input**: Design documents from `specs/007-loopback-bind-default/` (plan.md, spec.md, research.md, data-model.md, contracts/bind-policy.md, quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅

**Tests**: INCLUDED — the go-rag constitution mandates `go test ./...` green on every change ("Spec/test/evals first" founding principle). quickstart.md defines the validation scenarios each story must pass.

**Organization**: Tasks grouped by user story. Each story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable — different files, no dependency on an incomplete task
- **[Story]**: US1/US2/US3 (maps to spec.md user stories)
- Exact file paths in every description

## Path Conventions

Pure-Go single-binary repo. Source under `internal/`, entrypoint `cmd/go-rag`. No `src/`, no `tests/` — Go tests live alongside source as `*_test.go`.

---

## Phase 1: Setup

**Purpose**: Baseline + branch (no `before_*` git hook created a branch in this repo).

- [X] T001 Create feature branch `007-loopback-bind-default` from current default branch
- [X] T002 [P] Confirm baseline green before any change: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared loopback primitives every user story depends on.

**⚠️ CRITICAL**: No user-story wiring can begin until `IsLoopbackBind` + `ValidateBind` exist.

- [X] T003 [P] Add `BindExternal bool` field with tag `json:"bind_external,omitempty"` to the `Addrs` struct in `internal/daemon/pid.go`
- [X] T004 [P] Implement `IsLoopbackBind(addr string) bool` in NEW file `internal/daemon/bind.go` per research.md D2: `net.SplitHostPort`; empty host / `0.0.0.0` / `::` → false; `localhost` → true; `net.ParseIP` non-nil → `IsLoopback()`; hostname → `net.LookupIP` (true if any loopback, false on resolve error); unparseable → false
- [X] T005 Implement `ValidateBind(addrs Addrs, allowExternal bool) error` in `internal/daemon/bind.go` (depends T004): skip empty/disabled addrs (FR-008); collect non-loopback offenders; when `!allowExternal` and offenders exist return an actionable error naming each `TRANSPORT addr`; return nil otherwise
- [X] T006 [P] Unit-test the primitives in `internal/daemon/bind_test.go` (FR-006): `IsLoopbackBind` table — `127.0.0.1`, `127.5.6.7`, `::1`, `localhost` → true; `""`, `0.0.0.0`, `::`, `:7878`, `192.168.1.10`, unresolvable host → false. `ValidateBind` — all-loopback→nil; external+no-opt-in→error listing offenders; external+allowExternal→nil; disabled transport ignored

**Checkpoint**: Pure bind-validation primitives ready and tested. Story wiring can begin.

---

## Phase 3: User Story 1 — Never exposed by accident (Priority: P1) 🎯 MVP

**Goal**: Default `start`/`serve` binds every transport to loopback; any non-loopback bind without opt-in is refused at boot before any listener opens.

**Independent Test** (quickstart.md Scenario 1 & 2): `start --db-path <tmp> --mcp-addr 127.0.0.1:17878` → listener on `127.0.0.1` only; `start --db-path <tmp> --mcp-addr 0.0.0.0:17878` → non-zero exit, zero listeners.

### Implementation for User Story 1

- [X] T007 [US1] Register `--bind-external` flag (bool, default false) on the `serve` command in `internal/cli/serve.go`; read it into a local `allowExternal`
- [X] T008 [US1] Call `daemon.ValidateBind(daemon.Addrs{MCPAddr:mcpAddr,RESTAddr:restAddr,GRPCAddr:grpcAddr}, allowExternal)` at the top of `serve`'s `RunE` in `internal/cli/serve.go`, after reading the addr flags and before `openDB`/listener setup; return its error to refuse startup (FR-001, FR-003) (depends T005, T007)
- [X] T009 [US1] Integration test `internal/cli/serve_bind_test.go`: (a) external addr without `--bind-external` → `serve` returns a non-nil error naming the offending address, no listener created; (b) default loopback addrs → no error (FR-001/003, SC-001/002)

**Checkpoint**: Default path is loopback-only; accidental exposure is impossible. MVP shippable.

---

## Phase 4: User Story 2 — Deliberate external bind with informed consent (Priority: P2)

**Goal**: `--bind-external` on `start` authorizes external binding (forwarded to `serve`) and `serve` prints a prominent exposure warning when something is actually exposed.

**Independent Test** (quickstart.md Scenario 3 & 5): `start --mcp-addr 0.0.0.0:17878 --bind-external` → starts + warning present once; same without `--bind-external` → rejected; `--bind-external` with all-loopback addrs → starts, no warning.

### Implementation for User Story 2

- [X] T010 [P] [US2] Register `--bind-external` flag (bool, default false) on the `start` command in `internal/cli/start.go`; set `addrs.BindExternal` from it (FR-004)
- [X] T011 [US2] Forward the opt-in through the daemon: in `internal/daemon/lifecycle.go` `Start`, append `"--bind-external"` to the spawned `serve` arg list when `addrs.BindExternal` is true (FR-004) (depends T003, T010)
- [X] T012 [US2] Emit the exposure warning in `internal/cli/serve.go` when `allowExternal &&` any configured addr is non-loopback: print one stderr line immediately after the existing `"go-rag daemon serving …"` line, text per research.md D6 (exposed vault + transports, no TLS, user-owned access control, "allowed by --bind-external") (FR-005) (depends T008)
- [X] T013 [US2] Integration test `internal/cli/serve_bind_test.go`: (a) external addr + `--bind-external` → starts, warning present exactly once; (b) all-loopback addrs + `--bind-external` → starts, no warning (FR-004/005, SC-003, edge case)

**Checkpoint**: External binding is an explicit, eyes-open choice with a loud warning. Stories 1 & 2 both independently functional.

---

## Phase 5: User Story 3 — Fail closed on unsafe config (Priority: P3)

**Goal**: The persisted default is loopback and cannot silently regress to all-interfaces; the boot gate is source-agnostic so no config source can bypass it.

**Note**: `serve` binds from flags (loopback default, per spec 003), not from `config.MCPAddr` — so US3's code surface is the defense-in-depth default fix plus a regression guard. The source-agnostic reject is already delivered by the serve gate (T008/T009), which validates whatever addrs `serve` resolves.

**Independent Test** (quickstart.md Scenario 4 + FR-006): `config.Default().MCPAddr` classifies as loopback via `daemon.IsLoopbackBind`; the gate rejects an external addr regardless of how it was supplied.

### Implementation for User Story 3

- [X] T014 [P] [US3] Change `config.Default().MCPAddr` from `":7878"` to `"127.0.0.1:7878"` in `internal/config/config.go` (FR-001, research.md D4)
- [X] T015 [US3] Regression test in `internal/config/config_test.go`: assert `config.Default().MCPAddr` is loopback (`daemon.IsLoopbackBind(...)` → true), locking out a silent revert to `":7878"` (the exact audit finding) (depends T004)

**Checkpoint**: No config source can reintroduce an all-interfaces default. All three stories independently functional.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation and the final quality gate.

- [X] T016 [P] Document loopback-by-default, `--bind-external`, and the explicit no-TLS warning in `README.md` (FR-007)
- [X] T017 [P] Update flag help text in `internal/cli/start.go` and `internal/cli/serve.go` so `--bind-external` and the loopback default are discoverable in `--help` (FR-007)
- [X] T018 Run all five `specs/007-loopback-bind-default/quickstart.md` scenarios on an isolated tmp DB with non-default ports; record pass/fail
- [X] T019 Final gate: `make build && make vet && make test` green; run `make lint` (golangci-lint) if installed in the environment

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately.
- **Foundational (Phase 2)**: after Setup — **BLOCKS all user stories**.
- **User Stories (Phases 3–5)**: each depends on Foundational; can proceed in priority order (US1 → US2 → US3) or in parallel if staffed.
- **Polish (Phase 6)**: after the desired stories are complete.

### User Story Dependencies

- **US1 (P1)**: depends on Foundational only (T004/T005). No cross-story deps.
- **US2 (P2)**: depends on Foundational + US1's serve gate (T012 emits the warning beside the gate; T011 forwards the flag `start`→`serve`). Independently testable once T008 exists.
- **US3 (P3)**: depends on Foundational's `IsLoopbackBind` (T004) only for its regression test. Fully independent of US1/US2 wiring.

### Within Each User Story

- Tests written alongside / before impl (constitution: "Spec/test/evals first").
- Primitive (Foundational) before wiring (story).
- Gate wiring (T008) before warning (T012).
- `go build ./... && go vet ./... && go test ./...` green after each task.

### Parallel Opportunities

- **Foundational**: T003, T004, T006 run in parallel (distinct files / test-only); T005 after T004.
- **US2**: T010 (`start` flag) parallels T012 prep; T011 after T003+T010; T012 after T008.
- **US3**: T014 + T015 are independent of US1/US2 and can run anytime after Foundational.
- **Polish**: T016, T017 parallel (distinct files).

---

## Parallel Example: Foundational + US3

```bash
# Distinct files, no shared incomplete dependency — launch together:
Task: "T003 add BindExternal to Addrs in internal/daemon/pid.go"
Task: "T004 implement IsLoopbackBind in internal/daemon/bind.go"
Task: "T014 fix config.Default().MCPAddr in internal/config/config.go"
# Then the dependents:
Task: "T005 ValidateBind (needs T004)"
Task: "T006 primitive tests (needs T004/T005)"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 Setup (T001–T002) → baseline green.
2. Phase 2 Foundational (T003–T006) → primitives + tests.
3. Phase 3 US1 (T007–T009) → serve gate: loopback default, external rejected.
4. **STOP and VALIDATE**: run quickstart.md Scenario 1 & 2. Default path is air-gapped; accidental exposure impossible. Shippable on its own.

### Incremental Delivery

1. Setup + Foundational → primitives ready.
2. + US1 → MVP (default safe, external refused).
3. + US2 → power users can opt into LAN exposure with a warning.
4. + US3 → default can't regress; source-agnostic guarantee locked.
5. + Polish → documented, quickstart-validated, gate green.

### Single-developer (this repo's mode)

Sequential in priority order. No cross-story merge conflicts — each story touches a distinct concern (gate / opt-in+warning / default-fix+regression).

---

## Notes

- `[P]` = distinct file, no dependency on an incomplete task.
- Story labels map tasks to spec.md user stories for traceability.
- Every story is independently completable and testable per its quickstart scenario(s).
- Smoke rule (CLAUDE.md): any daemon run in tests/quickstart MUST use `--db-path <tmp>` + non-default ports to avoid colliding with the live default-vault daemon.
- Commit after each task or logical group; Conventional Commits (`feat:`/`fix:`/`chore:`).
