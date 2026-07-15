# Implementation Plan: Settings View — Effective Configuration (Slice 0)

**Branch**: `main` (single-author repo) | **Date**: 2026-07-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/055-ui-settings-view/spec.md`

## Summary

A read-only "Effective Configuration" panel that replaces the Settings placeholder
(spec 055, Slice 0). It is a **UI-only 5th adapter** over surfaces that already
exist — `Engine.Status(vault)` (`StatusInfo`), `Config`, and
`redact.DefaultPatterns` — mirroring spec 054 (Observability). No new engine
capability, no new transport, no on-disk layout change. One new endpoint
`GET /api/settings` (Bearer-guarded, vault-aware), one new Alpine view, retirement
of the `settings` placeholder entry, and a plan-level refinement of FR-002
(research R2: query default depth/mode/threshold are not config keys).

## Technical Context

**Language/Version**: Go 1.26 (`CGO_ENABLED=0`); static vendored SPA (HTML/CSS/Alpine.js, no Node build).

**Primary Dependencies**: cobra (CLI), Pebble (KV), chromem-go (vectors), Alpine.js (vendored). **No new dependencies.**

**Storage**: N/A — read-only projection; no persisted entity, no schema change.

**Testing**: `go test -race ./...`; `golangci-lint run`; UI parity tests + Interceptor browser verification.

**Target Platform**: local single-operator loopback (4th transport, default `127.0.0.1:7881`).

**Project Type**: single-binary local RAG database + embedded management console.

**Performance Goals**: render on par with the other read-only views; `Engine.Status` already meets the query-latency budgets.

**Constraints**: read-only (FR-006); Bearer-guarded (FR-008); no on-disk layout change (constitution storage discipline); no Node build artifacts (`TestNoNodeArtifacts`).

**Scale/Scope**: one new handler file (`settings.go`) + tests, one route registration + one sidebar-test-map edit, one Alpine view + sidebar wiring (`index.html` / `app.js`), and one placeholder-map edit. UI-only.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|---|---|---|
| I. Local-First, Single-Binary | PASS | read-only, no egress; one binary, no new runtime dep |
| II. Content-Addressed Identity | N/A | no ingest / identity path touched |
| III. Pure Go — No CGo | PASS | vendored SPA, no Node build; `CGO_ENABLED=0` |
| IV. Async-After-ACK | N/A | read-only (no write path) |
| V. Extension by Interface, MCP-First | PASS | UI-only over existing `Engine.Status` / `Config` / `redact.DefaultPatterns`; no new engine method |

**Storage discipline**: NO on-disk layout change — no new prefix, no migration,
`migrate.ExpectedVersion` unchanged. (Affirmed: the view reads live state only.)

**Violations**: none → Complexity Tracking table is empty.

## Project Structure

### Documentation (this feature)

```text
specs/055-ui-settings-view/
├── spec.md              # /speckit-specify
├── plan.md              # this file (/speckit-plan)
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── settings.md      # GET /api/settings
└── tasks.md             # /speckit-tasks (next phase)
```

### Source Code (repository root)

```text
internal/
├── ui/
│   ├── settings.go          # NEW  — Server.handleSettings + SettingsDTO projection
│   ├── settings_test.go     # NEW  — 200 / parity / vault + 401-unguarded
│   ├── placeholder.go       # EDIT — drop "settings" from placeholderViews
│   ├── ui.go                # EDIT — register GET /api/settings (guarded)
│   └── ui_test.go           # EDIT — TestSidebar_ViewSet want → {memory-graph}
└── (engine, config, redact — UNCHANGED, read-only reuse)

internal/ui/web/
├── templates/index.html     # EDIT — Settings nav button → real view (not placeholder)
└── static/js/app.js         # EDIT — add settings Alpine view (read-only sections)
```

**Structure Decision**: UI-only. No `engine` / `config` / `redact` changes —
read-only reuse per research R1 / R3. The new handler + tests mirror
`observability.go` / `observability_test.go` (research R4).

## Complexity Tracking

> None — Constitution Check passes with no violations.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
