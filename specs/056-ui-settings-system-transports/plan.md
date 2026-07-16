# Implementation Plan: Settings — System & Transports (Slice 1)

**Branch**: `main` (single-author repo) | **Date**: 2026-07-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/056-ui-settings-system-transports/spec.md`

## Summary

Slice 1 of the Settings arc — a read-only **System & Transports** panel plus an
operator-initiated **update-check**. Two new UI endpoints (`GET /api/settings/system`,
`POST /api/settings/updates/check`), Bearer-guarded, vault-agnostic. Reuses the
existing `daemon` (pidfile/addrs/bind posture), `migrate` (schema), `upgrade`
(release check), and `config` surfaces. Two small UI-layer additions: plumb the
binary version into the UI Server and record `startedAt` for uptime. **No
engine/storage/migration change** — mirrors spec 055 Slice 0.

## Technical Context

**Language/Version**: Go 1.26 (`CGO_ENABLED=0`); static vendored SPA (HTML/CSS/Alpine.js).

**Primary Dependencies**: cobra, Pebble, Alpine.js (vendored). **No new dependencies.** Reuses `internal/daemon`, `internal/storage/migrate`, `internal/upgrade`, `internal/config`.

**Storage**: N/A — read-only projection; no persisted entity, no schema change.

**Testing**: `go test -race ./...`; `golangci-lint run`; UI parity tests + Interceptor verification.

**Target Platform**: local single-operator loopback console (`127.0.0.1:7881`).

**Project Type**: single-binary local RAG database + embedded management console.

**Performance Goals**: render on par with the other read-only views.

**Constraints**: read-only except the explicit operator-initiated update-check (FR-005); Bearer-guarded (FR-007); no automatic egress (FR-004/SC-003); no on-disk layout change.

**Scale/Scope**: one new handler file (`system.go`) + tests, two routes, a `NewWithVersion` constructor + one `serve`-side caller wiring, a `startedAt` field, and one Alpine section + update-check button. UI-layer only.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|---|---|---|
| I. Local-First, Single-Binary | PASS | read-only; the update-check is the one operator-initiated egress (the documented `go-rag upgrade` exception), never automatic |
| II. Content-Addressed Identity | N/A | no ingest/identity path touched |
| III. Pure Go — No CGo | PASS | vendored SPA, no Node build; `CGO_ENABLED=0` |
| IV. Async-After-ACK | N/A | read-only + one synchronous operator check |
| V. Extension by Interface, MCP-First | PASS | UI-layer over existing `daemon`/`migrate`/`upgrade`/`config` surfaces; two small UI-local additions (version field + startedAt) |

**Storage discipline**: NO on-disk layout change — no new prefix, no migration;
`migrate.ExpectedVersion` is READ, not changed. (Affirmed: the slice reads live
state + the release endpoint; it writes nothing.)

**Violations**: none → Complexity Tracking table is empty.

## Project Structure

### Documentation (this feature)

```text
specs/056-ui-settings-system-transports/
├── spec.md              # /speckit-specify
├── plan.md              # this file (/speckit-plan)
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── system.md        # GET /api/settings/system + POST /api/settings/updates/check
└── tasks.md             # /speckit-tasks (next phase)
```

### Source Code (repository root)

```text
internal/
├── ui/
│   ├── system.go             # NEW  — handleSystem + handleUpdateCheck + systemStatusDTO/updateCheckDTO
│   ├── system_test.go        # NEW  — identity/transports parity + 401 + update-check (offline → unknown)
│   ├── ui.go                 # EDIT — NewWithVersion(eng,token,version) + startedAt; register 2 routes
│   └── (settings.go etc. unchanged)
├── cli/serve.go              # EDIT — pass version into NewWithVersion (one call site)
└── (daemon, storage/migrate, upgrade, config — UNCHANGED, read-only reuse)

internal/ui/web/
├── templates/index.html      # EDIT — System & Transports section (identity/transports/schema) + "Check for updates" button
└── static/js/app.js          # EDIT — loadSystem + checkUpdates; wire the section
```

**Structure Decision**: UI-layer only. No `engine`/`daemon`/`migrate`/`upgrade`/
`config` changes — read-only reuse per research R1–R4. The version is plumbed via a
`NewWithVersion` constructor that preserves `New(eng, token)` (test callers
unchanged); `startedAt` is a UI Server field. Mirrors spec 055 Slice 0's shape.

## Complexity Tracking

> None — Constitution Check passes with no violations.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
