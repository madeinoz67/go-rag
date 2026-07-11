# Implementation Plan: go-rag Management Console — Bridge Ops View (Slice 3)

**Branch**: `main` (single-author repo; commits straight to `main`) | **Date**: 2026-07-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/049-ui-bridge-ops/spec.md`

## Summary

Slice 3 of the go-rag management console: replace the **Bridge Ops** placeholder (view 4 of
the spec 046 shell) with the operational-health surface — a read-only view that tells an
operator whether the engine is healthy and making progress, what's drifted, which retrieval
subsystems are active, what's configured to be watched, and what ingest activity has happened
lately. It goes deeper than the Dashboard's corpus-count stat tiles.

The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp`
root, and spec 045 Bearer auth **unchanged**, and calls the engine **in-process**. The
operational tiles (US1/US3/US4 — backlog, drift detail, subsystem states, watch config) project
the existing `engine.StatusInfo` (which already carries `EmbedPending`/`EmbedFailed`, drift
baselines, poisoning summary, enrichment, caches, adaptive pool — specs 030/H11/H04/029/016/
024). The one genuinely-new surface is the **recent-activity feed** (US2): a thin read-only
`Engine.AuditRead` wrapper over the existing `audit.Read` (spec 021, already CLI-exposed via
`go-rag audit`), consumed in-process by the UI.

Net new surface: one small read-only engine method (`Engine.AuditRead`) + optionally surfacing
`WatchDirs` for the UI + the UI view itself. No new storage, no migration, no Node chain.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`); browser-side vendored Alpine.js 3.14
(already embedded from spec 046). No Node/Vite/Tailwind.

**Primary Dependencies**:
- stdlib `net/http` (the UI transport, unchanged from spec 046) — Go 1.22 pattern mux
- `internal/engine` — `Engine.Status()` (existing; returns `StatusInfo` with backlog/drift/
  poisoning/enrichment/cache/adaptive-pool fields), **new** thin `Engine.AuditRead` (this slice)
- `internal/audit` — `audit.Read(path, ReadOptions{Type, Tail, Since, All})` → `[]Event`,
  `audit.ReadOptions`, `audit.Event`, `audit.DefaultPath` (spec 021; already CLI-exposed)
- `internal/config` — `Config.WatchDirs` (the configured watch directories, spec 007)
- `internal/auth` — spec 045 `auth.Validate` guard (unchanged, via `Server.guard`)
- Vendored Alpine.js (already embedded)

**Storage**: None new. Reads existing Pebble state via `Engine.Status` + the audit JSONL log via
`audit.Read`. No new prefix, no migration, no `ExpectedVersion` bump.

**Testing**: `go test -race`; `curl -i` smoke for the new `/api/bridge-ops/*` routes (loopback
bypass + Bearer regimes); a cross-transport parity check that `/api/bridge-ops/stats` matches
`go-rag status` and `/api/bridge-ops/activity` matches `go-rag audit --type ingest`; Interceptor
browser verify of the health/activity/subsystem render. No JS test runner (vendored SPA).

**Target Platform**: Loopback HTTP (`127.0.0.1:7881`, the spec 046 UI transport), modern
browser SPA. Single-operator.

**Project Type**: Additive view on an existing web-service transport + one thin read-only engine
wrapper over an existing audit capability.

**Performance Goals**:
- Stats response ≤ existing `Engine.Status` latency (same call) + JSON marshal.
- Activity response bounded by the `tail` limit (default last 20 ingest events) — no unbounded
  log scan.

**Constraints** (hard):
- Read-only — no add/remove/reingest/scan-trigger or any mutation (spec FR-008).
- No Node/build chain (CLAUDE.md; single-binary).
- No new storage / no migration (constitution Storage Discipline).
- Activity feed bounded (tail-N, default 20) — never an unbounded log dump.
- UI calls the engine in-process; it is a 4th adapter, not a REST proxy.

**Scale/Scope**: ~4–6 edited/new files: `internal/engine/audit_read.go` (NEW, thin wrapper +
test), `internal/ui/bridgeops.go` (NEW +test), `internal/ui/ui.go` (EDIT — routes), and
`internal/ui/web/{static/js/app.js, static/css/components.css, templates/index.html}` (EDITs).
No REST / gRPC / MCP / CLI / proto changes for the audit read (pre-existing CLI audit already
covers that axis; cross-transport audit parity documented as a follow-up, see Constitution
Check).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The go-rag constitution (`.specify/memory/constitution.md`, v1.1.0) defines five Core
Principles. Slice 3 is evaluated against each:

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** | Loopback view in the existing binary; in-process engine call; reads a local log file; no cloud egress. |
| II | Content-Addressed Identity | **PASS** | Strictly read-only. No document/chunk identity, content hash, or ingest path is touched. Status + audit are read projections. |
| III | Pure Go — No CGo, No External Runtime | **PASS** | stdlib + engine + audit + config. No new Go dependency; vendored Alpine already embedded. |
| IV | Async-After-ACK Writes | **PASS** | Strictly read-only slice — no write path, so the <10ms write-ACK budget is unaffected. |
| V | Extension by Interface, MCP-First | **PASS (with note)** | The new `Engine.AuditRead` wraps an **existing** read capability (`audit.Read`, spec 021) that is **already CLI-exposed** (`go-rag audit`). The UI is a 4th adapter consuming it in-process. 049 introduces no new operation. **Pre-existing gap (not introduced here):** `go-rag audit` is CLI-only — it is not (yet) exposed on MCP/REST/gRPC. That gap originates in spec 021, predates 049, and is tracked as a follow-up (Complexity Tracking). Surfacing `WatchDirs` is an additive read field. |

**Storage discipline**: no on-disk layout change — no prefix added/retired, no value encoding
change. **No migration, no `ExpectedVersion` bump.** Affirmed: zero schema-version impact.

**Gate verdict: PASS.** One Complexity Tracking entry (the pre-existing audit cross-transport
parity gap, carried forward — not a 049 violation).

## Project Structure

### Documentation (this feature)

```text
specs/049-ui-bridge-ops/
├── plan.md              # This file
├── research.md          # Phase 0 output (R1–R9 decisions)
├── data-model.md        # Phase 1 output (DTOs + route table)
├── quickstart.md        # Phase 1 output (validation guide)
├── contracts/           # Phase 1 output (HTTP transport contract)
│   └── ui-bridge-ops.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT this command)
```

### Source Code (repository root)

```text
internal/engine/
  audit_read.go           # NEW (R3) — Engine.AuditRead(opts audit.ReadOptions) ([]audit.Event, error):
                          #   thin read-only wrapper; resolves path from cfg.AuditPath or
                          #   audit.DefaultPath(cfg.DBPath); delegates to audit.Read.
  audit_read_test.go      # NEW — wrapper: path resolution + tail/type filter + empty log
internal/ui/
  bridgeops.go            # NEW (R1,R2,R4) — handleBridgeOpsStats (StatusInfo operational
                          #   projection + subsystems + watch dirs) + handleBridgeOpsActivity
                          #   (Engine.AuditRead → activity DTOs); local DTOs
  bridgeops_test.go       # NEW — stats parity vs `go-rag status`; activity parity vs
                          #   `go-rag audit --type ingest`; read-only + guard + empty states
  ui.go                   # EDIT (R1) — register GET /api/bridge-ops/stats + GET /api/bridge-ops/activity (guarded)
  web/static/js/app.js    # EDIT — Bridge Ops view (Alpine): health tiles, drift detail,
                          #         subsystem tiles, watch-dirs, recent-activity feed, refresh
  web/static/css/components.css # EDIT — Bridge Ops layout (tiles + activity list)
  web/templates/index.html # EDIT — Bridge Ops template; sidebar active state; replaces the placeholder
```

**Structure decision**: No new package. One new engine file (`audit_read.go`) — a thin read-only
wrapper so the UI consumes audit via the engine, not the filesystem (mirrors how every UI view
calls the engine in-process). The view is a new file (`internal/ui/bridgeops.go`) inside the
existing UI transport, plus its Alpine/template/CSS edits — exactly how Dashboard, Documents,
and Query live in the package.

## Complexity Tracking

> **One entry — a pre-existing parity gap carried forward, not a 049 violation.**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| `go-rag audit` is CLI-only (no MCP/REST/gRPC) — spec 021 gap, surfaced by 049 consuming audit in the UI | 049 needs recent-activity in the browser; the audit log is the only durable source | Full cross-transport audit endpoints (MCP+REST+gRPC+proto) in this slice would balloon a UI view into a 5-transport change for a read the UI consumes in-process. Rejected: the UI is a peer adapter calling the engine directly; the capability already exists (audit.Read + CLI). Full audit cross-transport parity is a separate follow-up spec. |
