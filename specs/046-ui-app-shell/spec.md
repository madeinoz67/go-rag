# Feature Specification: go-rag Management Console — Slice 0 (App Shell)

**Feature Branch**: `046-ui-app-shell`

**Created**: 2026-07-07

**Status**: Draft

**Input**: User description (Stephen, 2026-07-06 brainstorm): *"the UI … will be managing all
aspects of go-rag, including bridge ops dashboard, also include go-rag retrieval UI."*
The unified go-rag management console — a MuninnDB-aesthetic web app **embedded in the
go-rag binary**, operator-facing, covering retrieval + bridge ops + all-aspects admin.
This spec covers **Slice 0 only**: the application shell, the transport that serves it,
the auth gate in front of it, and the one real view (Dashboard) that proves the shell
wires end-to-end to the engine. Each subsequent view is its own spec → plan → tasks.

> **Scope dependency (must acknowledge):** The PRD lists "Web UI" as a v1 non-goal (N7).
> This feature revises that posture **for a single-operator, embedded, vendored-SPA
> management console only** — served on loopback as a fourth transport, auth-gated by the
> already-shipped spec 045 Bearer-session system, with **no Node/Vite/Tailwind build
> chain** (assets are vendored and served via `go:embed`). The PRD N7 revision is a
> prerequisite tracked as a dependency; the constitution (local-first, pure-Go,
> single-binary, content-addressed identity, no query-time generation) is honoured. A
> general multi-user web app and a TUI remain out of scope. This is the same carve-out
> pattern as spec 029 (enrichment) / spec 032 (bundled embedder) / spec 045 (auth).

---

## Context & Background

go-rag already exposes one engine over three transports — MCP (`:7878`), REST (`:7879`),
gRPC (`:7880`) — all adapters over `internal/engine.Engine`, with cross-transport parity
(FR-002/003). Spec 045 added single-operator auth across all three: labelled `gorag_` API
keys, a bcrypt admin user, opaque `gorags_` Bearer sessions, one unified `auth.Validate`,
and a narrow loopback bypass that fires only on a bare pre-init vault.

The console is the **fourth transport** (`internal/ui`): it serves an HTML/JS/CSS SPA that
calls the **same engine** the other transports do — it introduces no new business logic,
only a new presentation surface. The aesthetic and stack are derived entirely from
MuninnDB's web UI and captured in `docs/style-guide.md` (dark-first operator console,
Alpine.js + Chart.js + Cytoscape, 13-component catalog).

**Why Slice 0 first:** the shell (layout, sidebar, auth flow, embed serving, CSS layers,
Alpine root) is the load-bearing skeleton every subsequent view depends on. Building it
once — with one real view proving the engine wiring — de-risks all later views.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - An operator opens the console and authenticates (Priority: P1)

An operator (Stephen, single-operator) wants to manage go-rag from a browser instead of
the CLI. They start the daemon (`go-rag start`), open `http://127.0.0.1:7881`, and reach
the console. If a credential has been minted (spec 045 admin user exists), they see a
login screen; on submitting the admin password they receive an opaque `gorags_` Bearer
session and the app shell loads. If **no** credential has been minted and the vault is
bare/local, the spec 045 loopback bypass admits them directly (local "just works"). The
session is a Bearer header — no cookies, therefore no CSRF surface.

**Why this priority**: The gate in front of every other view. Without auth-wired serving,
nothing else in the console can ship.

**Independent Test**: Start the daemon on an isolated DB with an admin user; `curl -i
http://127.0.0.1:7881/` returns the login page (or a redirect to it) without a Bearer
header; with a valid `gorags_` Bearer it returns the shell. Repeat on a bare vault: the
shell loads with no credential.

**Acceptance Scenarios**:
1. **Given** the daemon is running with `--ui-addr 127.0.0.1:7881`, **When** an unauthenticated
   request hits `/` on an initialized vault, **Then** the response is the login screen (or a
   401/redirect to it) — the shell HTML is not served.
2. **Given** a valid admin login, **When** the operator submits credentials, **Then** a
   `gorags_` session is minted and subsequent requests bearing it return the full shell.
3. **Given** a bare vault with no minted credential, **When** the operator hits `/` over
   loopback, **Then** the spec 045 bypass admits them — no login screen.
4. **Given** the auth flow, **When** inspected, **Then** no `Set-Cookie` is ever issued — the
   session is a Bearer header only.

---

### User Story 2 - The shell presents an 8-view sidebar and navigates between views (Priority: P1)

Once authenticated, the operator sees the application shell: a persistent left sidebar
with eight items — **Dashboard, Documents, Query, Bridge Ops, Vaults, Observability,
Settings, Memory & Graph** — and a main content area. Clicking a sidebar item swaps the
main content. In Slice 0, only **Dashboard** is a real view; the other seven render a
consistent placeholder panel ("planned — see spec 04N"). Navigation is client-side
(Alpine), with no full page reloads after the initial shell load.

**Why this priority**: The shell + sidebar is the skeleton. The placeholder contract makes
it cheap to slot in each real view as its own later spec without touching the shell again.

**Independent Test**: Load the shell; confirm all eight sidebar items render and are
clickable; confirm clicking Dashboard renders real content and clicking each other item
renders the placeholder panel; confirm no network round-trip replaces the shell HTML.

**Acceptance Scenarios**:
1. **Given** the authenticated shell, **When** rendered, **Then** all eight sidebar items are
   present, labelled exactly: Dashboard, Documents, Query, Bridge Ops, Vaults,
   Observability, Settings, Memory & Graph.
2. **Given** the shell, **When** the operator clicks Dashboard, **Then** the main area shows
   real corpus statistics (see US3) without a full page reload.
3. **Given** the shell, **When** the operator clicks any non-Dashboard item, **Then** the main
   area shows the standard placeholder panel naming the future spec.
4. **Given** client-side navigation, **When** navigating between views, **Then** the shell
   chrome (sidebar, header) does not reload.

---

### User Story 3 - The Dashboard shows real corpus statistics from the engine (Priority: P1)

The Dashboard is Slice 0's proof that the shell wires end-to-end to the engine. It
surfaces live, read-only corpus statistics: document count, chunk count, embedding count,
index health/status, and the active vault — the same data `go-rag status` reports, over
the same engine calls (no new logic). It uses **only go-rag-native data** — no MuninnDB /
bridge calls (those are bridge-blocked, see Slice Decomposition). Stats refresh on load
(and may poll; polling cadence is an implementation detail).

**Why this priority**: The one real view that validates the entire transport + auth +
embed + engine wiring. If the Dashboard renders real numbers, every later view is
mechanically derivable.

**Independent Test**: Ingest a known small corpus on an isolated DB; open the Dashboard;
confirm the document/chunk/embedding counts match `go-rag status`; confirm the vault name
matches; confirm no bridge/MuninnDB dependency is invoked (grep the served JS / network
tab).

**Acceptance Scenarios**:
1. **Given** a corpus of N documents, **When** the Dashboard loads, **Then** it shows N
   documents and chunk/embedding counts consistent with `go-rag status`.
2. **Given** the Dashboard, **When** the engine reports index health, **Then** the Dashboard
   surfaces the same status string the CLI does.
3. **Given** the Dashboard's read-only nature, **When** the operator interacts with it, **Then**
   it mutates no state and triggers no bridge/MuninnDB network call.

---

### User Story 4 - The console ships as a vendored SPA with no Node/Vite/Tailwind build (Priority: P2)

The console respects go-rag's hard "no Node/npm" constraint (CLAUDE.md). It is a
**vendored** SPA: hand-written CSS (per `docs/style-guide.md`, four layers:
theme/base/components/utilities) plus vendored copies of Alpine.js, Chart.js, and
Cytoscape, all distributed inside the binary via `go:embed` and served as static files.
There is no `package.json`, no `node_modules`, no build step — `make build` produces a
binary that serves the console with zero front-end tooling.

**Why this priority**: Not P1 because the shell works functionally before this is proven,
but it is a hard constraint (a Node chain would violate CLAUDE.md and the single-binary
goal). Proven once in Slice 0 so no later view can accidentally introduce it.

**Independent Test**: Confirm no `package.json`/`node_modules`/`vite.config` exist in the
repo; confirm `make build` produces a binary; confirm the served HTML references
vendored `/static/vendor/*.js` paths, not CDN URLs; confirm the CSS is the hand-written
4-layer set, not a Tailwind build artifact.

**Acceptance Scenarios**:
1. **Given** the repo, **When** checked, **Then** no `package.json`, `node_modules`,
   `vite.config.*`, or `tailwind.config.*` exists anywhere.
2. **Given** `make build`, **When** it completes, **Then** the single binary serves the
   console at `--ui-addr` with no external build tooling.
3. **Given** the served HTML, **When** assets are inspected, **Then** all JS/CSS is served
   from the embedded `embed.FS` (vendored), not from any CDN or network origin.
4. **Given** the CSS, **When** inspected, **Then** it is the hand-written 4-layer set
   (theme/base/components/utilities) per `docs/style-guide.md`.

---

### Edge Cases

- **Bare vault, no credential minted**: the spec 045 loopback bypass admits loopback
  requests — the shell loads with no login. On an *initialized* vault (admin present),
  the bypass is disabled and login is required. The UI must behave correctly in both
  regimes (it only renders what the transport admits; it does not make its own auth
  decisions).
- **UI disabled (`--ui-addr ""`)**: the transport is not started; `:7881` is not bound;
  the other three transports are unaffected. The binary behaves exactly as before for
  users who do not opt in.
- **Session expiry mid-session**: a `gorags_` session that expires returns the operator to
  the login screen on the next engine call (401); the shell handles the 401 by routing
  back to login (no crash, no silent failure).
- **Engine not yet warm / empty corpus**: the Dashboard renders zero counts and a healthy
  "empty" state, not an error.
- **Reverse proxy in front**: because the UI is path-rooted and Bearer-authed (no
  host-bound cookies), it works behind a reverse proxy terminating TLS without
  configuration; the spec takes no position on the proxy.
- **Vendored library license/version drift**: each vendored library is pinned to a
  specific version in Dependencies; the build does not fetch them at runtime.

---

## Technical Design

### Transport: `internal/ui` (4th transport)

A new package `internal/ui` peer to `internal/rest` / `internal/grpc` / `internal/mcp`,
registered by the daemon alongside the others. It is an adapter over
`internal/engine.Engine` — it adds no business logic.

- **Flag**: `--ui-addr` (default `127.0.0.1:7881`; empty string disables the transport).
- **Server**: stdlib `net/http`, one `*http.ServeMux`.
- **Auth middleware**: wraps every route except the login endpoint; delegates to spec 045
  `auth.Validate` (the same unified validator REST/gRPC/MCP use). On 401, returns the
  login screen (HTML) or 401 JSON depending on `Accept` header.
- **Login endpoint**: `POST /login` accepts admin credentials, mints an opaque
  `gorags_` session via the spec 045 session store, returns the token in the response
  body (to be held in memory by the SPA and sent as `Authorization: Bearer gorags_…`).
  **No `Set-Cookie`.**
- **Loopback bypass**: inherited from spec 045 verbatim — the UI makes no independent auth
  decision; it is gated by whatever `auth.Validate` admits.

### Static assets via `go:embed`

```
internal/ui/
  web/
    templates/
      index.html          # shell + login
    static/
      css/
        theme.css         # design tokens (dark-first + light opt-in)
        base.css          # resets, typography, layout primitives
        components.css    # hand-written component catalog (style-guide §13)
        utilities.css     # ~40 primitives + named classes (.stat-grid etc.)
      js/
        app.js            # Alpine goragApp root
      vendor/
        alpine.min.js     # pinned version (see Dependencies)
        chart.min.js
        cytoscape.min.js
  ui.go                   # transport: embed.FS, mux, auth middleware, handlers
  dashboard.go            # Dashboard view handlers (engine stats → JSON)
```

`//go:embed web` embeds the whole tree; the mux serves `/static/` from the embed FS and
`/` from `templates/index.html`.

### Naming (no `muninn_*`)

- Alpine root component: `goragApp` (parallel to MuninnDB's `muninnApp`).
- CSS class prefixes / IDs: `gorag-*`.
- Env / flag: `GORAG_UI_ADDR`.
- Session/API-key prefixes: `gorags_` / `gorag_` (already spec 045).

### CSS: hand-written, 4-layer, style-guide-derived

Per `docs/style-guide.md`. **No Tailwind** (MuninnDB used Tailwind for grid utilities;
go-rag replaces those with a hand-written `utilities.css` of ~40 primitives + named
classes like `.stat-grid`, per style-guide §13). Dark-first; light is an opt-in via a
`data-theme` attribute, identical token system.

### Dashboard view (the one real view)

Reads engine status (the same call path as `go-rag status`) and renders: document count,
chunk count, embedding count, index health/status, active vault. Implementation detail:
fetch `/api/dashboard/stats` (JSON, engine-sourced) on load; render via Alpine +
`stat-grid`. **No Chart.js/Cytoscape in Slice 0** — those are vendored now so later views
(Documents graph, Observability charts, Memory & Graph) need no new wiring, but Slice 0
Dashboard is plain stat tiles.

### Placeholder panel contract

A single partial `templates/_placeholder.html` taking `{{.ViewName}}` and
`{{.FutureSpec}}`; every non-Dashboard sidebar item renders it. This is the seam later
specs replace.

---

## Slice Decomposition

| Slice | Scope | Status | Spec |
|-------|-------|--------|------|
| **0 (this spec)** | App shell: `internal/ui` transport, embed serving, auth middleware, login, Alpine `goragApp`, 4-layer CSS, 8-item sidebar, **Dashboard** real, 7 placeholders | This spec | 046 |
| 1 | Documents view (list, status, summaries from spec 029) | Done/shipped | 047 |
| 2 | Query view (retrieval UI — the "go-rag retrieval UI" Stephen named) | Done/shipped | 048 |
| 3 | Bridge Ops view (go-rag-native half: ingest/watcher status) | Done/shipped | 049 |
| 4 | Documents write-actions (first write surface; reuses cross-transport Add/Reprocess + ships new cross-transport Delete) | Done/shipped | 050 |
| 5 | Vaults view (moved from slot 050 — `Engine.ListVaults` already exists) | Planned | 054 |
| 5 | Observability view (charts — Chart.js) | Planned | 051 |
| 6 | Settings view | Planned | 052 |
| 7 | Memory & Graph view (Cytoscape) — **bridge-blocked** | Blocked | 053 |
| 7' | Bridge Ops (MuninnDB-health half) — **bridge-blocked** | Blocked | 049 cont. |

**Unblocked now (go-rag-native):** Dashboard (Slice 0), Documents, Query, Vaults,
Observability, Settings, and the go-rag-native half of Bridge Ops.
**Bridge-blocked:** Memory & Graph + the MuninnDB-health half of Bridge Ops (depend on the
MuninnDB bridge: their issues #560 → #556 still open).

---

## Dependencies

- **spec 045 (auth & tokens)** — shipped v0.3.3. Provides `auth.Validate`, the admin user,
  `gorags_` sessions, the loopback bypass. **Hard dependency.**
- **`docs/style-guide.md`** — canonical CSS reference (538 lines, MuninnDB-derived).
- **Vendored front-end libraries** (all MIT/Apache, redistributable, pinned):
  - Alpine.js v3.x (MIT) — reactivity.
  - Chart.js v4.x (MIT) — Observability charts (vendored now, used Slice 5).
  - Cytoscape.js v3.x (MIT) — Memory & Graph (vendored now, used Slice 7).
- **`internal/engine.Engine`** — the single business-logic surface; the UI calls it
  exactly as the other transports do (no new engine methods required for Slice 0;
  Dashboard reuses the status call path).

---

## Out of Scope (this slice)

- **Real views beyond Dashboard** — Documents, Query, Bridge Ops, Vaults, Observability,
  Settings, Memory & Graph are placeholder panels in Slice 0. Each gets its own spec.
- **Any bridge / MuninnDB integration** — Memory & Graph and the MuninnDB-health half of
  Bridge Ops are blocked on the bridge (their issues #560 → #556).
- **Chart.js / Cytoscape usage** — vendored in Slice 0 so later slices need no rewiring,
  but no chart/graph is rendered in Slice 0.
- **TLS termination** — out of scope; a reverse proxy terminates TLS. No `internal/tlsutil`.
- **Multi-user accounts / RBAC** — remains out of scope (PRD N2). The console is
  single-operator, gated by the spec 045 admin login.
- **Mobile-responsive design** — operator console, desktop-first; mobile is not a Slice 0
  target (style-guide is desktop-first).
- **Query-time LLM generation** — out of scope (PRD N4); the Query view (Slice 2) is
  retrieval-only, no answer synthesis.
- **TUI (bubbletea)** — remains out of scope (PRD N7).

---

## Open Questions (to resolve in plan/tasks)

- Dashboard polling cadence (on-load vs interval) — defer to plan.
- Whether the login screen is a separate HTML route or a 401-driven Alpine state inside
  `index.html` — defer to plan (lean: single `index.html` with Alpine auth gate).
- Exact vendored versions + SRI/integrity — pin in tasks.
