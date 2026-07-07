# Phase 0 Research — go-rag UI Console, Slice 0 (App Shell)

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md) · **Date**: 2026-07-07

Resolves every `NEEDS CLARIFICATION` in the plan's Technical Context. Findings are
graph-grounded (gortex symbol IDs + file:line) unless noted. Format per topic:
**Decision · Rationale · Alternatives rejected**.

---

## R1. Transport registration seam

**Decision.** Add a new `internal/ui` package that mirrors `internal/rest` exactly:
- Constructor `func New(eng *engine.Engine, token string) *Server` that builds `auth.NewStore(eng.DB())` internally (the same pattern as `rest.New`, `internal/rest/server.go:32`).
- Expose `func (s *Server) Handler() http.Handler` returning a `*http.ServeMux`.
- `internal/cli/serve.go` wraps it: `&http.Server{Addr: uiAddr, Handler: ui.New(eng, token).Handler()}`.

**Six touch points in `internal/cli/serve.go` (`newServeCmd` RunE):** (1) read `--ui-addr`; (2) add `UIAddr:` to **both** `daemon.Addrs{...}` literals (serve.go:53 and serve.go:230); (3) construct `uiSrv` only `if uiAddr != ""`; (4) drain in `stopOnce.Do`; (5) bound-logging line; (6) goroutine + bump `errCh` buffer 3→4.

**Three touch points in `internal/daemon`:** `pid.go::Addrs` += `UIAddr string`; `bind.go::enabledBinds` += one UI block (this single edit makes both `ValidateBind` and `ExternalBindWarning` cover UI automatically); `lifecycle.go::Start` argv += conditional `--ui-addr`.

**Cobra flags** on **both** `start.go` and `serve.go`: `cmd.Flags().String("ui-addr", "127.0.0.1:7881", "UI listen address (loopback); empty disables UI")`.

**Rationale.** REST is the closest analog (HTTP, optional, bearer-authed). There is no shared transport interface — each adapter has its own shape (HTTP / gRPC / MCP-JSON-RPC); cross-transport parity is enforced by `internal/engine/parity_test.go`, not by a type. The double-validation pattern (`start` pre-checks at start.go:21, `serve` re-validates at serve.go:53) is preserved by routing `--ui-addr` through `enabledBinds`.

**Alternatives rejected.**
- *Shared `Transport` interface.* Over-abstraction for four adapters with genuinely different I/O models.
- *MCP always-on pattern.* UI must be disable-able (`--ui-addr ""`); only MCP is mandatory.
- *Construct transports in `internal/daemon`.* Rejected — `internal/daemon` owns lifecycle/re-exec only; all construction lives in `serve`'s RunE.

---

## R2. Dashboard data source — no new engine methods

**Decision.** The Dashboard calls `engine.Status()` directly (`go-rag/internal/engine/status.go::Engine.Status`, signature `func (e *Engine) Status() (*StatusInfo, error)`) and projects a `DashboardDTO`. **No new engine method, no REST proxy.**

`StatusInfo` (`internal/engine/types.go::StatusInfo`) already carries every field the Dashboard needs. The existing REST `GET /v1/status` (`internal/rest/engine_adapter.go::Server.handleStatus` → `statusResponse`) confirms the projection pattern but is a **subset**; the UI projects its own superset DTO.

**DashboardDTO fields** (all already in `StatusInfo`): `documents`, `chunks`, `embeddings`, `dimensions`, `embedding_model`, `reranker`, `ollama_url`, `embeddings_complete` (the index-health boolean — `docs==0 || embs>=chunks`), `drift_verdict` (`clean|hard-drift|version-warning|unknown|n/a`), `hard_drift`, `embed_pending`, `embed_failed`, `enriched_docs`, `enrichment_enabled`, plus a derived `vault` string.

**Active vault** is not surfaced by any engine method. **Decision:** derive server-side in the handler from `eng`'s config DBPath (`filepath.Base` relative to `vault.Root()`) — the engine already holds its config. No engine change.

**Rationale.** The UI is a peer transport with direct engine access; a cross-transport hop through REST would add a dependency and latency for nothing. `embeddings_complete` is the natural "index healthy" flag; `drift_verdict` carries the readiness story.

**Alternatives rejected.**
- *Proxy `/api/dashboard/stats` to REST `/v1/status`.* Unnecessary cross-transport hop; couples UI→REST.
- *Add `Vault` to `Engine.Status()`.* Leakage — vault is a config/path concern, not a corpus stat; derive it at the edge.
- *Reuse the CLI `gatherStats` path.* That path (cli/status.go:103) is daemon-stopped-only and produces a different struct; the Dashboard always talks to a running daemon → `Engine.Status()`.

---

## R3. Auth integration — UI makes zero auth decisions

**Decision.** `internal/ui` builds `auth.NewStore(eng.DB())` (same as REST) and funnels every gate through the spec 045 primitives:
- **App routes** (`/`, `/api/dashboard/stats`, placeholders): guard middleware calls `store.Validate(r *http.Request) (Principal, error)` — bypass-enabled, reads `Authorization: Bearer`, derives loopback from `r.RemoteAddr`. Inject `Principal` into request context.
- **`POST /login`** (public): decode `{username, password}` → `auth.VerifyPassword(store, user, pass)` (bcrypt cost 12, timing-neutral, collapse `ErrUnknownAdmin`/`ErrBadPassword` to one 401) → `auth.MintSession(store, user, ip, auth.DefaultSessionTTL)` → return `{token, expires_at}`. **Never `Set-Cookie`.**
- **Admin surface** (later slices — session list/revoke): `store.ValidateStrict(r)` + `p.Mode == auth.ModeAdmin` gate; 401 (not 403) on non-admin.

The patterns are lifted (≈25 lines) from `internal/rest/auth.go::handleLogin` + `server.go::guard` into `internal/ui` — the contract is identical and pinned by the same spec 045 tests.

**Loopback-bypass invariants (load-bearing — do not re-derive):** bypass fires iff `token==""` AND loopback AND no API-key record AND no admin record. `CreateAdmin` runs at every `init`, so every runnable vault has an admin → bypass narrows to a bare pre-init vault only. Fail-closed on Pebble read error and on missing `RemoteAddr`. Regression-pinned by `internal/rest/bypass_guard_test.go::TestBypassGuard_BareVaultBypasses_InitializedVaultDoesNot`.

**Rationale.** spec 045 was designed transport-agnostic (`Validate`/`ValidateTokenOrBypass`/`ValidateStrict`). `internal/ui` depending only on `internal/auth` + `internal/engine` keeps it a true peer transport.

**Alternatives rejected.**
- *Import `internal/rest` and reuse `Server.handleLogin`/`mountAuth`.* Couples the UI transport to the REST transport package; a peer transport should share the engine + auth primitives, not another transport's server type.
- *Reimplement auth logic.* Explicitly forbidden — the UI makes no independent auth decision.

---

## R4. Static asset serving — first `embed.FS` use

**Decision.** `//go:embed web` over the `internal/ui/web` tree → `var webFS embed.FS`. Serve `/static/` via `http.FileServer(http.FS(fs.Sub(webFS, "web/static")))`; serve `/` from `web/templates/index.html`. MIME types come from net/http's `DetectContentType` path (no custom mime code).

**Rationale.** This is the codebase's first `embed.FS` use. The only existing `//go:embed` is `internal/rest/openapi.go` (byte-slice pattern: `var openapiYAML []byte`), which establishes the blank `_ "embed"` import + directive placement but not the FS pattern. Single-binary (G2) + local-first (constitution I) require the assets to live inside the binary.

**Alternatives rejected.**
- *Serve assets from an on-disk directory.* Breaks single-binary; reintroduces a runtime path dependency.
- *Fetch vendored libs from a CDN at runtime.* Violates local-first + offline operation; a hard no.

---

## R5. Vendored front-end libraries (MIT, pinned)

**Decision.** Vendor (into `internal/ui/web/static/vendor/`), pin, and `go:embed`:
- **Alpine.js 3.14.x** (MIT) — reactivity; single `goragApp` root on `<body>`.
- **Chart.js 4.4.x** (MIT) — vendored now, used by the Observability view (Slice 5).
- **Cytoscape.js 3.30.x** + fcose layout (MIT) — vendored now, used by Memory & Graph (Slice 7, bridge-blocked).

Versions match `docs/style-guide.md §2`. Slice 0 ships Alpine (the shell needs it); Chart/Cytoscape are vendored now so later slices add no rewiring.

**Rationale.** All three are MIT (permissive — satisfies constitution III's license rule for dependencies, and they are static assets, not Go-runtime deps). Vendoring (not building) respects the no-Node CLAUDE.md rule.

**Alternatives rejected.**
- *Tailwind + Vite build (the MuninnDB stack).* Rejected — requires Node; CLAUDE.md forbids it; single-binary.
- *Defer Chart/Cytoscape vendoring until their slices.* Rejected — vendoring once in Slice 0 avoids a binary-shape change and a re-audit later.

---

## R6. CSS — hand-written, 4-layer, style-guide-derived

**Decision.** Four hand-written CSS files in `web/static/css/`:
- `theme.css` — the `docs/style-guide.md §3` token system verbatim (dark default at `:root`, light via a `light` class). Rename the localStorage key `muninnTheme` → `goragTheme`; mechanism identical.
- `base.css` — resets, Inter typography stack, layout primitives.
- `components.css` — the hand-written component catalog (style-guide §13).
- `utilities.css` — ≈40 primitives + named classes (`.stat-grid`, `.card`, `.sidebar`, …) replacing the Tailwind utility layer MuninnDB used.

**Rationale.** The no-Node rule removes Tailwind; the style guide's tokens and components are framework-agnostic and copy directly. Dark-first (principle §1) matches the operator-console intent.

**Alternatives rejected.**
- *Tailwind via CDN play.* Runtime fetch (not offline) and still pulls the Tailwind runtime; rejected on local-first grounds.
- *Copy MuninnDB's compiled `app.css`.* Inheriting a build artifact creates a silent build-tooling dependency and blocks future sync; hand-writing from the token spec is honest.

---

## R7. `--ui-addr` empty-disables semantics

**Decision.** Mirror REST/gRPC: cobra default `127.0.0.1:7881`; `--ui-addr ""` disables. Construction guarded by `if uiAddr != ""`; every downstream site nil-checks the `*http.Server` var (not the addr string); `enabledBinds` contributes a `BindEntry` only when `UIAddr != ""` (auto-exempts disabled UI from `ValidateBind`/`ExternalBindWarning`).

**Rationale.** Agent findings: the empty-disables contract is enforced at three layers (construction guard, nil-check downstream, `enabledBinds`). MCP is the lone always-on exception; UI follows the optional pattern.

**Alternatives rejected.** Always-on UI (rejected — operators must be able to run the binary exactly as before, UI off).

---

## Open items deferred to `/speckit-tasks`

- Exact Alpine/Chart/Cytoscape patch versions + SRI/integrity hashes (download + verify step).
- Dashboard poll cadence (on-load vs interval) — lean: on-load + manual refresh in Slice 0.
- Login as a separate HTML route vs an Alpine auth-gate state inside `index.html` — lean: single `index.html` with an Alpine-driven auth gate (one file, no route split).
- Binary-size budget (48 MB vs constitution <25 MB) — pre-existing; flagged for a separate perf/constitution pass, not this slice.
