# HTTP Transport Contract — `internal/ui` (Slice 0)

**Spec**: [../spec.md](../spec.md) · **Data model**: [../data-model.md](../data-model.md)

The UI is a fourth loopback transport over `internal/engine.Engine`, peer to
REST/gRPC/MCP. It speaks HTTP/1.1 on `--ui-addr` (default `127.0.0.1:7881`).
Every route funnels through the spec 045 auth primitives — the UI makes **no
independent auth decision**.

## Auth model

- **Credential:** `Authorization: Bearer <token>` header only. **No cookies,
  ever** (CSRF-free; pinned by spec 045 `TestAuth_NoSetCookieEver`). The UI's
  `/login` contract is identical to `internal/rest`'s and must pass the same
  invariant (`TestAuth_Login_RoundTrip_NoSetCookie`).
- **Guard middleware (app routes):** `auth.Store.Validate(r)` — bypass-enabled.
  On failure: HTTP 401 `{"error":"unauthorized"}` + `audit.Log`. All auth
  failures collapse to an identical 401 (no status-code oracle, no body-shape
  oracle).
- **Loopback bypass:** fires iff `token==""` AND loopback AND no API-key AND no
  admin record. Because `init` always creates an admin, this narrows to a bare
  pre-init vault only. Fail-closed on read error / missing `RemoteAddr`.
- **Principal in context:** on success the guard injects `auth.Principal` under
  `principalCtxKey{}` (same key/convention as REST).

## Routes

### `POST /login` — public

Authenticate the admin user and mint a session.

- **Auth:** none (public).
- **Request body:** `{"username": string, "password": string}`. Empty `username`
  defaults to `auth.DefaultAdminUsername`.
- **Response 200:** `{"token": "gorags_<opaque>", "expires_at": "<RFC3339 UTC>"}`
  — the opaque session token (returned exactly once; the client holds it in
  memory and sends it as a Bearer header).
- **Response 401:** `{"error":"unauthorized"}` — wrong username, wrong password,
  or missing user all collapse to this (timing-neutral via `auth.VerifyPassword`'s
  dummy-bcrypt path). No `Set-Cookie` on any path.
- **Audit:** `audit.Log(audit.AuthEvent("login", …))` on success; `AuthFailEvent`
  on failure.

### `POST /logout` — any credential

Drop the calling session.

- **Auth:** guard (any valid credential — API key or session).
- **Response 204** on success.

### `GET /` — shell

Serve the application shell.

- **Auth:** **public** (the shell *is* the login page — no data lives in the
  HTML). Serving it unauthenticated is intentional: guarding it would return a
  401 before the browser could load the login form. The Alpine gate renders
  login vs. app client-side based on whether a `gorags_` token is held; all
  sensitive data is behind the guarded `/api/*` routes (which 401 until
  authenticated). The shell returns 200 on any vault, bare or initialized.
- **Response 200:** `text/html; charset=utf-8` — `templates/index.html`
  (Alpine `goragApp` root, 8-view sidebar, auth-gate state).

### `GET /static/*` — vendored static assets

- **Auth:** public (the login page must be able to load its CSS/JS before auth).
- **Response 200:** served from the embedded `web/static` tree via
  `http.FileServer(http.FS(fs.Sub(webFS, "web/static")))`. MIME by net/http
  `DetectContentType`.
- **Includes:** `css/{theme,base,components,utilities}.css`, `js/app.js`,
  `vendor/{alpine,chart,cytoscape}.min.js`.

### `GET /api/dashboard/stats` — Dashboard DTO

Read-only corpus statistics.

- **Auth:** guard (bypass-enabled).
- **Response 200:** `application/json` — the `DashboardDTO` (see
  [data-model.md](../data-model.md)). Projection of `engine.Status()` + derived
  `vault`. Computed per-request (no caching in Slice 0).
- **Response 401:** standard guard failure.
- **Error contract:** `engine.Status()` failure → HTTP 500
  `{"error":"internal"}` (no detail leakage).

### `GET /api/placeholder/{view}` — placeholder panel

- **Auth:** guard.
- **Path param:** `view` ∈ `{documents, query, bridge-ops, vaults, observability, settings, memory-graph}`.
- **Response 200:** `application/json` — `{"view": "<view>", "status": "planned", "future_spec": "<NNN>"}`.
  This is the seam later view-specs replace with real data.

## Error contract (all routes)

- **401:** `{"error":"unauthorized"}` — identical body for missing, malformed,
  expired, or unknown credentials. No `Set-Cookie`.
- **404:** `{"error":"not found"}` — unknown route / unknown placeholder view.
- **405:** `{"error":"method not allowed"}` — wrong method on a known path.
- **500:** `{"error":"internal"}` — engine failure; no stack/detail leakage.

## Cross-transport parity

The UI calls the same `engine.Status()` the REST `GET /v1/status` and the MCP
`go_rag_status` tool call. The Dashboard DTO is a **superset** projection; the
`documents`/`chunks`/`embeddings` counts it returns MUST match `go-rag status`
and `GET /v1/status` byte-for-byte (same engine, same prefix scans). Pinned in
Slice 0's test plan by a parity assertion.

## Non-goals this slice

No write endpoints. No `/api/auth/session` (list) or `/api/auth/session/{hash}`
(revoke) — those admin-surface routes ship with the Settings view (later slice),
gated by `auth.ValidateStrict` + `Mode==admin`. No WebSocket/SSE (Observability
live stream is Slice 5). No TLS (reverse proxy terminates).
