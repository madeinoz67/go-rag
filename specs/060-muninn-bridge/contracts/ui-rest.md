# Contract: UI REST endpoints (Memory & Graph view + bridge status)

**Surface**: the management console's 4th loopback transport (`internal/ui`, `127.0.0.1:7881`, embedded vendored SPA — no Node/build chain), spec 045 Bearer-guarded. The Memory & Graph view is a **5th adapter** over the bridge's read path (UI-only; no new engine capability beyond what the bridge exposes).

## Auth

Every route below goes through `s.guard(...)` → `auth.Validate` (spec 045 opaque `gorags_` Bearer session). Bypassing `guard` is the spec 045 red-team finding-A pattern — never omit it.

## Sidebar graduation

Retiring the last placeholder (research.md R9):
- `internal/ui/placeholder.go` — delete the `"memory-graph"` entry from `placeholderViews`. After deletion `handlePlaceholder` 404s for it (the view is graduated).
- `internal/ui/web/templates/index.html` — replace the placeholder `<section>` (lines ~1288–1292) with a real Alpine view bound to the new API. The 9th `nav-item` (line ~202) already targets `memory-graph` — unchanged.
- Handler file is named `internal/ui/memory_graph.go` (**not** `bridge*` — `bridgeops.go` is the spec 049 Operations view; the collision is real).

## `GET /api/memory-graph/browse?q=<phrases>&limit=<n>`

Activate-driven browse of the target MuninnDB vault (Q3 = live target-vault graph).

- **Auth**: Bearer session.
- **Query**: `q` — context phrases (space-separated); `limit` — max engrams (default 25, cap 100).
- **Server**: proxies `MuninnClient.Activate(BridgeTargetVault, phrases)`, projects each `ActivationResponse` to a row.
- **200** response:
  ```json
  { "vault": "go-rag", "rows": [
    { "engram_id": "...", "concept": "...", "score": 0.83, "last_access": "2026-...", "tags": ["go-rag","default","markdown"] }
  ], "degraded": false }
  ```
- **`degraded: true`** when MuninnDB is unreachable / bridge disabled — the client renders an empty/degraded state, never a crash (FR-015 acceptance 2).

## `GET /api/memory-graph/engrams/{id}`

Per-engram detail.

- **Auth**: Bearer session.
- **Server**: `MuninnClient.Read(BridgeTargetVault, id)`.
- **200**: `{ id, concept, content, tags, access_count, stability, state, created_at, updated_at, associations: [{target_id, rel_type, weight}] }`.
- **404** if MuninnDB returns not-found; **503** with `degraded: true` if MuninnDB is unreachable.

## `GET /api/memory-graph/status`

Bridge health + promotion/backfill status (FR-017). Surfaced in the console alongside Operations/Observability.

- **Auth**: Bearer session.
- **200**:
  ```json
  {
    "enabled": true,
    "healthy": true,
    "endpoint": "127.0.0.1:8477",
    "source_vault": "default",
    "target_vault": "go-rag",
    "promoted_total": 1284, "skipped_total": 56, "failed_total": 0,
    "backfill": { "running": false, "paused": false, "cursor": "doc-abc", "promoted": 1200, "started_at": "..." },
    "circuit": "closed"
  }
  ```
- When disabled: `{ "enabled": false, "degraded": true }`.

## `POST /api/memory-graph/backfill/{action}`

`action` ∈ `{pause, resume}` — the operator control for FR-014. Sets the in-memory pause flag on the bridge coordinator; the backfill worker park-checks it between pages.

- **Auth**: Bearer session.
- **200**: the updated `status` object.
- **409** if `action=pause` but no backfill is running.

## Cross-cutting

- **No plaintext secrets in responses**: the `mk_` key never appears in any payload (it lives only in the gRPC interceptor). The status endpoint shows `endpoint` + `vault names`, never the token.
- **Cache-Control: no-cache** on the static assets (the console convention — a daemon restart must serve the fresh SPA, not a stale copy).
- **All routes registered** in `internal/ui/ui.go::Server.Handler` via `mux.HandleFunc("METHOD /api/memory-graph/...", s.guard(s.handleX))` — the Go 1.22 pattern-mux shape used by spec 054/055/056.
