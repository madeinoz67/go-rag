# Tasks: go-rag Management Console — Slice 0 (App Shell)

**Input**: Design documents from `/specs/046-ui-app-shell/` — [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/ui-transport.md](./contracts/ui-transport.md), [quickstart.md](./quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓.

**Tests**: INCLUDED — go-rag is test-gated (`make test -race`, `make lint(0)`) and the constitution enforces "Spec/Test/Evals First". Every story ships a test task.

**Organization**: Tasks grouped by user story. Research decision tags (R1–R7) cross-link to [research.md](./research.md) for grounding.

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[USx]**: user-story phase tag (Setup/Foundational/Polish tasks carry none)
- Every task names its exact file path + the symbol/seam it touches

## Path conventions

New package: `internal/ui/`. Edits: `internal/cli/{start,serve}.go`, `internal/daemon/{pid,bind,lifecycle}.go`. Embed tree: `internal/ui/web/`. Tests: `internal/ui/ui_test.go`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the `internal/ui` package skeleton + the embed tree + acquire vendored libs.

- [ ] T001 Create `internal/ui` package skeleton — `internal/ui/doc.go` (package comment + Slice-0 scope note), plus empty-package-declaration stubs `internal/ui/ui.go`, `internal/ui/dashboard.go`, `internal/ui/placeholder.go` so the package compiles before logic lands.
- [ ] T002 [P] Create the embed tree directories — `internal/ui/web/templates/`, `internal/ui/web/static/css/`, `internal/ui/web/static/js/`, `internal/ui/web/static/vendor/` (the `//go:embed web` root).
- [ ] T003 [P] Acquire + pin vendored front-end libs — download Alpine.js 3.14.x, Chart.js 4.4.x, Cytoscape.js 3.30.x (+fcose) into `internal/ui/web/static/vendor/{alpine,chart,cytoscape}.min.js`; record exact version + SHA-256 in `internal/ui/web/static/vendor/VERSIONS.txt`. All MIT. (R5)

**Checkpoint**: package compiles (`CGO_ENABLED=0 go build ./internal/ui/`); embed tree exists with vendored assets.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The transport, embed serving, daemon wiring, CSS layers, and shell scaffold. **MUST be complete before any user story.**

- [ ] T004 Implement `ui.Server` + constructor — `internal/ui/ui.go`: `type Server struct { eng *engine.Engine; store *auth.Store }` and `func New(eng *engine.Engine, token string) *Server` that builds `auth.NewStore(eng.DB())` internally (mirror `internal/rest/server.go::New`). (R1, R3)
- [ ] T005 Implement `embed.FS` + `Handler()` mux — `internal/ui/ui.go`: `//go:embed web` → `var webFS embed.FS`; `func (s *Server) Handler() http.Handler` returns a `*http.ServeMux` serving `/static/` via `http.FileServer(http.FS(fs.Sub(webFS, "web/static")))` and `/` from `web/templates/index.html`. MIME via net/http `DetectContentType`. (R4)
- [ ] T006 Add `UIAddr` to `daemon.Addrs` — `internal/daemon/pid.go`: field `UIAddr string \`json:"ui_addr,omitempty"\``. (R1)
- [ ] T007 Register UI in `enabledBinds` — `internal/daemon/bind.go::enabledBinds`: `if addrs.UIAddr != "" { out = append(out, BindEntry{Name: "UI", Addr: addrs.UIAddr}) }` (single edit covers `ValidateBind` + `ExternalBindWarning`). (R1)
- [ ] T008 Forward `--ui-addr` in re-exec argv — `internal/daemon/lifecycle.go::Start`: append `if addrs.UIAddr != "" { args = append(args, "--ui-addr", addrs.UIAddr) }` beside the REST/gRPC appends. (R1)
- [ ] T009 Declare `--ui-addr` on `start` — `internal/cli/start.go`: `cmd.Flags().String("ui-addr", "127.0.0.1:7881", "UI listen address (loopback); empty disables UI")`; read in RunE; set `addrs.UIAddr`; include in `daemon.ValidateBind`. (R1, R7)
- [ ] T010 Declare `--ui-addr` on `serve` — `internal/cli/serve.go`: same flag declaration; read in RunE; add `UIAddr:` to **both** `daemon.Addrs{...}` literals (serve.go:53 and serve.go:230); include in both `ValidateBind` calls. (R1, R7)
- [ ] T011 Wire the UI listener in `serve` — `internal/cli/serve.go::newServeCmd` RunE: construct `uiSrv = &http.Server{Addr: uiAddr, Handler: ui.New(eng, token).Handler()}` only `if uiAddr != ""`; drain (`uiSrv.Shutdown`) in `stopOnce.Do`; add the bound-logging line; start goroutine; bump `errCh` buffer 3→4. (R1)
- [ ] T012 [P] Write `theme.css` — `internal/ui/web/static/css/theme.css`: the `docs/style-guide.md §3` token system verbatim (dark `:root` default + light via `.light` class); FOUC-prevention script reads `localStorage['goragTheme']`. (R6)
- [ ] T013 [P] Write `base.css` — `internal/ui/web/static/css/base.css`: resets, Inter font stack, layout primitives. (R6)
- [ ] T014 [P] Write `utilities.css` — `internal/ui/web/static/css/utilities.css`: ≈40 primitives + named classes (`.stat-grid`, `.card`, `.sidebar`, `.nav-item`, …) replacing the Tailwind utility layer. (R6)
- [ ] T015 [P] Write `components.css` — `internal/ui/web/static/css/components.css`: hand-written component catalog per style-guide §13 (buttons, badges, inputs, tabs, tiles, toasts). (R6)
- [ ] T016 [P] Write `index.html` shell scaffold — `internal/ui/web/templates/index.html`: Alpine `goragApp` root on `<body>`, layout grid (sidebar + main), `<link>` the 4 CSS layers, `<script>` the 3 vendored libs + `app.js`. No auth-gate logic yet (US1). (R6)
- [ ] T017 [P] Write `_placeholder.html` partial — `internal/ui/web/templates/_placeholder.html`: takes `{{.ViewName}}` / `{{.FutureSpec}}` — the seam later view-specs replace. (R6)

**Checkpoint**: `go-rag start --ui-addr 127.0.0.1:7881 ...` binds the UI port and serves a bare shell at `/` (unauthenticated scaffold); `make build` + `make vet` clean.

---

## Phase 3: User Story 1 — Authenticate (Priority: P1) 🎯 MVP gate

**Goal**: An operator authenticates (or is bypass-admitted on a bare vault) and reaches the shell behind spec 045 Bearer sessions.

**Independent Test**: [quickstart.md](./quickstart.md) §2–3 — bare-vault bypass returns 200; on an initialized vault, `POST /login` mints `gorags_…` (no `Set-Cookie`); guarded routes 401 without Bearer, 200 with it; wrong password → identical 401.

### Implementation

- [ ] T018 [US1] Implement the auth guard middleware — `internal/ui/ui.go`: `func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc` calling `s.store.Validate(r)`, injecting `auth.Principal` into the request context (`principalCtxKey{}`), returning 401 `{"error":"unauthorized"}` + `audit.Log` on failure. Mirror `internal/rest/server.go::guard`. (R3)
- [ ] T019 [US1] Implement `POST /login` — `internal/ui/ui.go::handleLogin`: decode `loginRequest{Username,Password}`; default empty username to `auth.DefaultAdminUsername`; `auth.VerifyPassword`; `auth.MintSession(s.store, user, peerIP(r), auth.DefaultSessionTTL)`; return `loginResponse{Token, ExpiresAt}`; `audit.Log`. **Never `Set-Cookie`.** Lift the body from `internal/rest/auth.go::handleLogin`. (R3)
- [ ] T020 [US1] Implement `POST /logout` — `internal/ui/ui.go::handleLogout`: guard (any credential); drop the calling session via the spec 045 session store; 204. (R3)
- [ ] T021 [US1] Register routes + apply guard — `internal/ui/ui.go::Handler`: `POST /login` + `POST /logout` public/guarded; `GET /` and `/api/*` wrapped in `s.guard`. (R3, contracts/ui-transport.md)
- [ ] T022 [US1] Implement the Alpine auth-gate — `internal/ui/web/templates/index.html` + `internal/ui/web/static/js/app.js`: login-form state when no token held; on submit `POST /login`, hold the `gorags_…` token in memory (`localStorage` optional), send `Authorization: Bearer` on every fetch; on 401, return to login. No cookies read/written client-side.
- [ ] T023 [US1] Auth tests — `internal/ui/ui_test.go`: (a) bare vault + loopback + no Bearer → 200 (bypass); (b) after `auth.CreateAdmin` + loopback + no Bearer → 401 (bypass disabled — the `TestBypassGuard_BareVaultBypasses_InitializedVaultDoesNot` invariant, re-derived for the UI transport); (c) `POST /login` good creds → 200 + `gorags_` token + **no `Set-Cookie`**; (d) bad password → 401 with byte-identical body to missing user; (e) guarded route 401 without Bearer, 200 with. (R3)

**Checkpoint**: US1 independently testable — the auth gate works in both regimes.

---

## Phase 4: User Story 2 — 8-view sidebar navigation (Priority: P1)

**Goal**: Authenticated shell shows the 8-item sidebar; clicking swaps the main view client-side; non-Dashboard views render the placeholder panel.

**Independent Test**: [quickstart.md](./quickstart.md) §4 steps 3–6 — 8 sidebar items labelled exactly; Dashboard renders real content; each other item renders the placeholder; shell chrome does not reload on nav.

### Implementation

- [ ] T024 [US2] Implement sidebar markup + client-side nav — `internal/ui/web/templates/index.html` + `internal/ui/web/static/js/app.js`: exactly 8 items — Dashboard, Documents, Query, Bridge Ops, Vaults, Observability, Settings, Memory & Graph; Alpine-driven `currentView` swap in the main area; no full-page reload.
- [ ] T025 [US2] Implement the placeholder handler + route — `internal/ui/placeholder.go::handlePlaceholder`: validate `view` ∈ the 7 non-dashboard names; render `_placeholder.html` with `{ViewName, FutureSpec}`; `GET /api/placeholder/{view}` guarded. 404 on unknown view.
- [ ] T026 [US2] Sidebar/placeholder tests — `internal/ui/ui_test.go`: enumerate the 8 view names; `GET /api/placeholder/{each non-dashboard}` → 200 with the planned marker; unknown view → 404.

**Checkpoint**: US2 independently testable — the navigable shell.

---

## Phase 5: User Story 3 — Dashboard with real corpus stats (Priority: P1)

**Goal**: The Dashboard surfaces live, read-only corpus statistics off `engine.Status()` — the proof the shell wires end-to-end to the engine.

**Independent Test**: [quickstart.md](./quickstart.md) §4 step 4 + §2 — Dashboard counts match `go-rag status` exactly; index-health badge reflects `embeddings_complete` + `hard_drift`.

### Implementation

- [ ] T027 [US3] Implement `handleDashboardStats` + DashboardDTO — `internal/ui/dashboard.go`: call `s.eng.Status()`; project `DashboardDTO` (documents, chunks, embeddings, dimensions, embedding_model, reranker, ollama_url, embeddings_complete, drift_verdict, hard_drift, embed_pending, embed_failed, enriched_docs, enrichment_enabled, derived `vault` from the engine's config DBPath via `filepath.Base` relative to `vault.Root()`); `GET /api/dashboard/stats` guarded; 500 `{"error":"internal"}` on engine failure. (R2, data-model.md)
- [ ] T028 [US3] Render Dashboard tiles — `internal/ui/web/templates/index.html` (dashboard view) + `internal/ui/web/static/js/app.js`: fetch `/api/dashboard/stats` on view entry; render `.stat-grid` tiles (docs/chunks/embeddings/dimensions/model) + an index-health badge (`embeddings_complete && !hard_drift` → healthy); show the vault name.
- [ ] T029 [US3] Cross-transport parity test — `internal/ui/ui_test.go`: against one engine, assert `DashboardDTO.{documents,chunks,embeddings}` == `GET /v1/status` body == MCP `go_rag_status` counts (same engine, same prefix scans — pattern of `internal/engine/parity_test.go`). (R2)

**Checkpoint**: US3 independently testable — the Dashboard is real.

---

## Phase 6: User Story 4 — Vendored SPA, no Node build (Priority: P2)

**Goal**: Prove the console ships with zero front-end build chain — vendored libs via `go:embed`, hand-written CSS, no Node.

**Independent Test**: [quickstart.md](./quickstart.md) §5 — no `package.json`/`node_modules`/`vite.config`/`tailwind.config`; all assets served from `/static/*`; CSS is the hand-written 4-layer set.

### Implementation / Verification

- [ ] T030 [US4] Vendored-lib pin verification — `internal/ui/web/static/vendor/`: confirm `alpine.min.js`/`chart.min.js`/`cytoscape.min.js` present with the pinned versions + SHA from T003's `VERSIONS.txt`; `index.html` references `/static/vendor/*` (not CDN). (R5)
- [ ] T031 [US4] No-Node + embed assertion tests — `internal/ui/ui_test.go` (or `internal/ui/embed_test.go`): repo-root scan finds no `package.json`/`node_modules`/`vite.config.*`/`tailwind.config.*`; `GET /static/css/theme.css` etc. return 200 from the embed FS (not 404); `GET /static/vendor/alpine.min.js` MIME is `application/javascript`. (R4, R5)

**Checkpoint**: US4 independently testable — the no-Node invariant is pinned.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T032 [P] Run [quickstart.md](./quickstart.md) end-to-end on an isolated DB (`--db-path /tmp/gorag-ui-smoke`, non-default ports): bypass + auth curl regimes + Interceptor browser verify; record results in the ISA/commit.
- [ ] T033 [P] Gate hygiene — `make lint` (0 findings), `make vet`, `make test -race` all clean; `golangci-lint` passes on the new package + edited files.
- [ ] T034 [P] Doc sync — affirm PRD §6.7 unchanged (no storage prefix added); update `~/.claude` MuninnDB memory + `PROJECTS.md` go-rag entry to reflect Slice 0 implemented.
- [ ] T035 Architecture snapshots — regenerate `docs/architecture/*.mermaid` (gortex reindex drift) for the new `internal/ui` package.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: no deps — start immediately.
- **Foundational (Phase 2)**: depends on Setup; **blocks all user stories**.
- **US1 (Phase 3)**: depends on Foundational.
- **US2 (Phase 4)**: depends on Foundational + US1 (needs the auth-gated shell to navigate).
- **US3 (Phase 5)**: depends on Foundational + US1 (needs the guarded route); independent of US2.
- **US4 (Phase 6)**: depends on Foundational (vendoring is in T003; this phase verifies it).
- **Polish (Phase 7)**: depends on all stories complete.

### User-story independence
- US1, US3, US4 each bind to distinct handlers/files once Foundational lands — testable in isolation.
- US2 depends on US1's auth-gate (you navigate *inside* the authenticated shell).

### Parallel opportunities
- Phase 1: T002, T003 parallel with T001.
- Phase 2: T012–T017 (the 4 CSS files + index.html + placeholder partial) are all `[P]` — fully parallel, different files.
- Phase 2 daemon edits (T006–T011) are sequential (same wiring thread) but each is a small distinct edit.
- Story test tasks can run alongside their implementation tasks (different files).

---

## Parallel Example: Phase 2 (CSS + templates)

```bash
Task: "Write theme.css       in internal/ui/web/static/css/theme.css"        # T012
Task: "Write base.css        in internal/ui/web/static/css/base.css"         # T013
Task: "Write utilities.css   in internal/ui/web/static/css/utilities.css"    # T014
Task: "Write components.css  in internal/ui/web/static/css/components.css"   # T015
Task: "Write index.html      in internal/ui/web/templates/index.html"        # T016
Task: "Write _placeholder    in internal/ui/web/templates/_placeholder.html" # T017
```

---

## Implementation Strategy

### MVP First

1. Complete Phase 1 (Setup) + Phase 2 (Foundational) — the transport serves a bare shell.
2. Complete Phase 3 (US1 — auth). **STOP and VALIDATE**: authenticated + bypass regimes both pass the curl smoke (quickstart §2–3). This is the **MVP gate** — a working auth-gated console.
3. Complete Phase 4 (US2) + Phase 5 (US3) — the **demo-complete** point: an authenticated, navigable shell with a real Dashboard. This is the euphoric-surprise moment.
4. Phase 6 (US4) + Phase 7 (Polish) harden + verify.

### Incremental delivery
- Setup → Foundational → US1 (MVP) → US2 → US3 (demo) → US4 → Polish.
- Each checkpoint is independently testable per its Independent Test.

### Single-author note
This repo commits straight to `main` (CLAUDE.md). Commit after each task or logical group; run `make lint && make test -race` before push.

---

## Notes

- All auth decisions funnel through `internal/auth` — the UI never decides (R3). Re-derive the bypass invariant, do not copy a status code (T023).
- `Engine.Status()` is the single data source for the Dashboard — no new engine method, no REST proxy (R2).
- Vendoring (not building) is the constraint that satisfies both no-Node (CLAUDE.md) and single-binary (constitution I) (R4, R5).
- Binary size is already over the <25 MB budget pre-existing (spec 032); Slice 046's ~750 KB vendored JS is marginal and tracked in plan.md Complexity Tracking — do not block on it here.
