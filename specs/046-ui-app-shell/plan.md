# Implementation Plan: go-rag Management Console — Slice 0 (App Shell)

**Branch**: `046-ui-app-shell` | **Date**: 2026-07-07 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/046-ui-app-shell/spec.md`

## Summary

Slice 0 of the go-rag management console: a **fourth loopback transport**
(`internal/ui`, `--ui-addr` default `127.0.0.1:7881`) serving an **embedded,
vendored SPA** (Alpine.js + Chart.js + Cytoscape via `go:embed`, **no
Node/Vite/Tailwind build**), auth-gated by the **spec 045 Bearer-session system**
(opaque `gorags_` sessions, no cookies, loopback bypass on a bare vault). The
shell is an 8-view sidebar with **one real view (Dashboard)** surfacing
read-only corpus statistics off the existing engine status path, and seven
placeholder panels (the seam later view-specs replace). Hand-written 4-layer CSS
per `docs/style-guide.md`. Each subsequent view is its own spec; Memory & Graph +
half of Bridge Ops stay bridge-blocked.

The UI introduces **no new business logic** — it is a presentation adapter over
`internal/engine.Engine` and a consumer of `internal/auth`, exactly peer to the
REST/gRPC/MCP transports.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`). Browser-side: vendored
Alpine.js 3.14, Chart.js 4.4, Cytoscape.js 3.30 (+fcose), inline Lucide-style
SVG, Inter font. **No Node/Vite/Tailwind/PostCSS** — those are MuninnDB's build
tools (style-guide §2), dropped here per the no-Node CLAUDE.md rule.

**Primary Dependencies**:
- stdlib `net/http` + `embed` (transport + static asset serving)
- `internal/engine` (existing — the single business-logic surface; Dashboard reuses the status call path)
- `internal/auth` (existing — spec 045; `auth.Validate` gates routes, session store mints `gorags_`)
- Vendored front-end libs (all MIT, redistributable, pinned): Alpine.js 3.14, Chart.js 4.4, Cytoscape.js 3.30
- **NEEDS CLARIFICATION (Phase 0)**: exact transport-registration seam; `auth.Validate` middleware call shape + session-minting signature + loopback-bypass invariants; existing `go:embed` precedent; daemon flag-threading pattern for `--ui-addr`.

**Storage**: None new. Pebble KV via the engine; the UI is stateless
presentation (sessions live in the spec 045 session store, already on Pebble).

**Testing**: `go test -race` for the Go transport/handlers; `curl -i` smoke for
the HTTP auth + login + dashboard-stats endpoints (loopback bypass + Bearer
regimes); **Interceptor** (real Chrome) browser verification of the shell,
sidebar navigation, and Dashboard render against a real isolated DB. No JS test
runner (vendored SPA, no Node).

**Target Platform**: Loopback HTTP (`127.0.0.1:7881`), modern browser SPA.
Network-capable but localhost-default (matches the other three transports). A
reverse proxy terminates TLS for any remote access — no first-party TLS.

**Project Type**: Web-service transport — a fourth adapter over the engine,
peers with `internal/rest` / `internal/grpc` / `internal/mcp`. Plus an embedded
static SPA.

**Performance Goals**:
- Dashboard `/api/dashboard/stats` ≤ existing `go-rag status` latency (the same engine call).
- Static asset serve < 10 ms from `embed.FS` (in-memory, no disk I/O).
- Shell first paint < 500 ms on loopback.

**Constraints** (hard):
- No Node/Vite/Tailwind/npm build chain (CLAUDE.md; single-binary G2).
- No TLS, no cookies (CSRF-free; spec 045 Bearer-only).
- Single binary — the SPA is `go:embed`-ded, not a separate artifact.
- Auth-gated by spec 045 `auth.Validate`; the UI makes no independent auth decision.
- Pure Go (no CGo); vendored browser JS is a served static asset, not a Go dependency.
- Read-only in Slice 0 (Dashboard mutates nothing).

**Scale/Scope**: Single-operator. Slice 0 = 1 transport package + 1 real view
(Dashboard) + 7 placeholders + 4 CSS files + 1 Alpine root + 3 vendored libs.
~8–12 source files; one PR.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The go-rag constitution (`.specify/memory/constitution.md`, v1.1.0) defines five
Core Principles. Slice 0 is evaluated against each:

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** (with Complexity Tracking entry) | Loopback transport; SPA embedded via `go:embed` into the one binary; no cloud egress. Binary size is already over the <25 MB budget pre-existing from spec 032's bundled embedder — see Complexity Tracking. |
| II | Content-Addressed Identity | **PASS** | The UI is read-only presentation. Slice 0 (Dashboard) touches no document identity, content hash, or ingest path. |
| III | Pure Go — No CGo, No External Runtime | **PASS** | `net/http` + `embed`, pure Go. Vendored Alpine/Chart/Cytoscape are static assets *served* to the browser, not executed by the Go runtime; all MIT-licensed (permissive, per constitution). No new Go dependency. |
| IV | Async-After-ACK Writes | **PASS** | Slice 0 Dashboard is read-only; there is no write path in this slice, so the <10 ms write-ACK budget is unaffected. |
| V | Extension by Interface, MCP-First | **PASS** | The UI is an *additive* presentation transport alongside MCP; it removes no MCP tooling. The constitution's "every CLI op also exposed as MCP" is unchanged. |

**Gate verdict: PASS.** One Complexity Tracking entry (binary size, pre-existing
— not introduced by this slice). The PRD N7 carve-out is the product-level
scope change (already applied); the constitution's five principles are
respected without amendment (precedent: N2/N4/N9 carve-outs did not amend the
constitution either; it remains v1.1.0).

## Project Structure

### Documentation (this feature)

```text
specs/046-ui-app-shell/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (HTTP transport contracts)
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT this command)
```

### Source Code (repository root)

```text
internal/ui/                     # NEW — 4th transport package
├── ui.go                        # Transport: embed.FS, *http.ServeMux, auth middleware, /login
├── dashboard.go                 # Dashboard handler: engine status → /api/dashboard/stats JSON
├── placeholder.go               # Placeholder-panel handler (7 views)
├── web/                         # //go:embed web
│   ├── templates/
│   │   ├── index.html           # Shell + login (Alpine goragApp root)
│   │   └── _placeholder.html    # Placeholder partial (the seam later specs replace)
│   └── static/
│       ├── css/
│       │   ├── theme.css        # Design tokens (dark default + light opt-in) — style-guide §3
│       │   ├── base.css         # Resets, typography (Inter), layout primitives
│       │   ├── components.css   # Hand-written component catalog — style-guide §13
│       │   └── utilities.css    # ~40 primitives + named classes (.stat-grid) — replaces Tailwind
│       ├── js/
│       │   └── app.js           # Alpine goragApp root, sidebar nav, dashboard fetch
│       └── vendor/              # Pinned, vendored (MIT)
│           ├── alpine.min.js    # 3.14.x
│           ├── chart.min.js     # 4.4.x
│           └── cytoscape.min.js # 3.30.x
└── ui_test.go                   # Transport + auth + handler tests (go test -race)

internal/cli/start.go            # EDIT — add --ui-addr flag, thread to daemon (mirror --rest-addr)
internal/cli/serve.go            # EDIT — add --ui-addr flag, open UI listener (mirror REST block)
internal/daemon/pid.go           # EDIT — add UIAddr to Addrs struct
internal/daemon/lifecycle.go     # EDIT — append --ui-addr to serve argv when non-empty
```

**Structure decision**: Single new package `internal/ui` (peer to the existing
three transport packages), with four small edits to wire it in (the `--ui-addr`
flag on `start`+`serve`, `UIAddr` on the `Addrs` struct, the argv append, and
the listener block — all mirroring the REST transport exactly per Phase 0
research). No other packages change. This mirrors how spec 003 added REST/gRPC
and how spec 045 added auth — additive transport, no core rewrite.

## Complexity Tracking

> Filled because the Constitution Check has one entry to justify (binary size).

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Binary size > 25 MB budget (currently 48 MB) | The budget was already exceeded by spec 032's pure-Go bundled embedder (bge-small-en-v1.5 model). Spec 046's vendored JS (~750 KB) is marginal on top. | Refusing the UI transport does not recover the budget — the overage predates this slice. The <25 MB budget itself warrants a separate revision (out of scope for spec 046); flagged for a future constitution/perf-spec pass. |
