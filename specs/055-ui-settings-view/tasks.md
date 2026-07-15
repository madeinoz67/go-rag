---

description: "Task list for spec 055 — Settings View: Effective Configuration (Slice 0)"
---

# Tasks: Settings View — Effective Configuration (Slice 0)

**Input**: Design documents from `/specs/055-ui-settings-view/` (spec.md, plan.md, research.md, data-model.md, contracts/settings.md, quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓

**Tests**: INCLUDED — the go-rag constitution mandates `go test ./...` green on every change and every console slice (046–054) shipped race-clean tests; this slice follows that pattern.

**Organization**: Tasks grouped by user story (US1/US2/US3 from spec.md) so each story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on incomplete tasks)
- **[Story]**: US1 / US2 / US3 (setup + foundational + polish have no story label)
- Exact file paths in every description

## Design refinements discovered during task grounding

1. **Config access (research follow-up).** `Engine` exposes no public `Config()`
   accessor, and `StatusInfo` does not carry raw knobs (`ChunkSize`, `ChunkOverlap`,
   `RRFK`, redaction, `NearDupHamming`, rerank candidates/retry). T002 adds a one-line
   read-only `Engine.Config()` getter so the handler can project them. This refines
   plan.md's "no new engine method" to "one trivial read-only getter" — no new
   capability/operation, no storage change, no migration, no `ExpectedVersion` bump.
   Constitution Principle V still holds.
2. **FR-002 refinement (research R2).** Default query depth/mode/threshold are not
   config keys; the retrieval section shows the actually-configurable knobs instead.

---

## Phase 1: Setup

**Purpose**: Confirm a green baseline on `main` before any change.

- [x] T001 Verify baseline green on `main`: run `make build && make vet && make test -race ./...` — all packages pass before starting (no file change; gate only).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The handler + route + accessor + Alpine shell that every story depends on.

**⚠️ CRITICAL**: No user-story work can begin until this phase is complete.

- [x] T002 Add a read-only config accessor `func (e *Engine) Config() config.Config { return e.cfg }` in `internal/engine/engine.go` — exposes the in-memory config so the UI can project effective knobs `StatusInfo` does not carry (ChunkSize/Overlap, RRFK, redaction, near-dup, rerank). Pure read; no storage change.
- [x] T003 Create `internal/ui/settings.go` — define `settingsDTO` (groups: `vault`, `retrieval`, `embeddings`, `cache`, `chunking`, `redaction` per `data-model.md`) and `Server.handleSettings(w, r)`: derive vault from `X-Go-Rag-Vault`/default (spec 052), call `s.eng.Status(vault)` + `s.eng.Config()`, project to the DTO with effective values (defaults where unset), compute `redaction.pattern_count` via `len(redact.DefaultPatterns(cfg.PIIPatterns))` (0 when disabled), `writeJSON`. Read-only — no mutation. Mirror `observability.go` handler style (`handleObservabilityMetrics`).
- [x] T004 Register `GET /api/settings` in `internal/ui/ui.go`, guarded by the same Bearer middleware as `/api/observability/*` (mirror that route registration; uses `Server.token` + `Server.store`). Vault header parsed identically to the other vault-aware handlers.
- [x] T005 [P] Add the Settings Alpine view shell to `internal/ui/web/static/js/app.js` (fetch `/api/settings` on view activation, hold the DTO in `x-data`, expose a `settings` view in `goragApp()`) and wire the Settings sidebar button in `internal/ui/web/templates/index.html` to call `switchView('settings')` rendering the real view instead of fetching `/api/placeholder/settings`. Section bodies are filled in T006/T008.

**Checkpoint**: Foundation ready — `/api/settings` returns the full DTO and the sidebar opens the Settings view shell.

---

## Phase 3: User Story 1 — Retrieval + Embedding sections (Priority: P1) 🎯 MVP

**Goal**: The operator sees the effective retrieval + embedding configuration, and the values match `go-rag status`.

**Independent test**: `GET /api/settings` `retrieval`+`embeddings` fields equal the status output; the UI renders both sections with real values.

- [x] T006 [P] [US1] Render the retrieval + embeddings sections of the Settings Alpine view (`app.js` / `index.html`) from the fetched DTO — retrieval: `rrf_k`, `pool_size`, `reranker`, `rerank_candidates`, `rerank_retry_on_failure`, `adaptive_depth_enabled`, `near_dup_hamming`; embeddings: `model`, `dimensions`, `prefix_mode`, `resolved_query_prefix`, `resolved_doc_prefix`, `stored_convention`, `ollama_url`. Match the card/tile styling of the Observability view.
- [x] T007 [US1] Add `internal/ui/settings_test.go` — `TestSettings_RetrievalAndEmbeddings`: authenticated `GET /api/settings` → 200; `retrieval` and `embeddings` fields equal `Engine.Status(vault)`/`Config()` effective values (SC-002 parity); cover the prefix-mode `"auto"` → resolved-convention edge case and the defaults-where-unset case.

**Checkpoint**: US1 fully functional and independently testable — retrieval + embedding configuration visible and correct.

---

## Phase 4: User Story 2 — Cache + Chunking + Redaction sections (Priority: P2)

**Goal**: The operator sees cache (caps/enabled/live stats), chunking policy, and redaction policy; values match status/config.

**Independent test**: `GET /api/settings` `cache`+`chunking`+`redaction` fields equal status/config; UI renders the three sections; cache-disabled renders zeroed stats without error.

- [x] T008 [P] [US2] Render the cache + chunking + redaction sections of the Settings Alpine view (`app.js` / `index.html`) — cache: global `enabled` + result/embedding `capacity`/`size`/`hits`/`misses`; chunking: `chunk_size`, `chunk_overlap`, fixed `boundary_mode` ("paragraph→sentence→word"), `section_context` (on); redaction: `enabled`, `pattern_count`, `custom_patterns_path`. Handle cache-disabled display cleanly.
- [x] T009 [US2] Add `TestSettings_CacheChunkingRedaction` to `internal/ui/settings_test.go` — `cache` caps/enabled/hit-stats, `chunking` size/overlap, `redaction` enabled/count/path equal `Status`/`Config`; include the cache-disabled zeroed-stats edge case and the defaults-where-unset case.

**Checkpoint**: US1 + US2 both independently functional — the full effective-configuration surface is visible and correct.

---

## Phase 5: User Story 3 — Placeholder retired, read-only, no regression (Priority: P3)

**Goal**: Settings is no longer a placeholder; Memory & Graph stays "blocked"; no built view regresses; the view is honestly read-only.

**Independent test**: placeholder map is exactly `{memory-graph}`; `/api/settings` without bearer → 401; calling it mutates nothing; browser shows a real panel.

- [x] T010 [US3] Edit `internal/ui/placeholder.go` — delete the `"settings": "planned"` entry from `placeholderViews` (keep `"memory-graph": "blocked"`).
- [x] T011 [US3] Edit `internal/ui/ui_test.go` `TestSidebar_ViewSet` — change `want` to `{"memory-graph": "blocked"}` only; leave the built-view-absence assertions unchanged so a regression fails the test.
- [x] T012 [US3] Add `TestSettings_401Unguarded` (no bearer → 401; mirror `TestObservabilityMetrics_401Unguarded`) and `TestSettings_ReadOnly` (calling `/api/settings` mutates nothing — document + chunk counts and embed-queue length unchanged before/after; FR-006) to `internal/ui/settings_test.go`.
- [x] T013 [US3] Browser-verify via the Interceptor skill on an isolated daemon (`quickstart.md`): Settings sidebar item opens a real panel (not the "planned" marker), all five sections render with real values, and Memory & Graph still shows "blocked".

**Checkpoint**: All three stories independently functional; boundary guarantees hold.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [x] T014 Run full gates — `make build && make vet && make test -race ./...` (all packages green, including new `internal/ui` settings + sidebar + placeholder tests) and `make lint` (golangci-lint, zero findings — the `ci.yml` gate).
- [x] T015 Run `specs/055-ui-settings-view/quickstart.md` end-to-end on an isolated daemon — parity vs `go-rag status` across all five sections, 401 unguarded, placeholder retired, then teardown (`lsof -ti` kill by port + `rm -rf` scratch vault).
- [x] T016 [P] Update `~/.claude/LIFEOS/USER/PROJECTS.md` go-rag entry — note Settings Slice 0 (spec 055, read-only Effective Configuration) shipped, with Slices 1–3 (System & Transports / Auth / Config Editing) still remaining.
- [x] T017 Commit to `main` per the repo's Conventional-Commits rhythm (CLAUDE.md standing instruction, single-author): `docs(spec055): specify + plan + tasks` for the `specs/055-ui-settings-view/` docs, then `feat(ui): settings view — effective configuration (spec 055)` for the implementation.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: none — start immediately.
- **Foundational (Phase 2)**: depends on Phase 1 — **BLOCKS all user stories**.
- **User stories (Phase 3–5)**: each depends on Phase 2; US1 → US2 → US3 in priority order (sequential recommended; the Alpine view is shared, so section rendering is sequential per file).
- **Polish (Phase 6)**: depends on all stories complete.

### Within each user story
- Render section (view) and its parity test may be written test-first (the test pins the DTO shape) — but both land together; the test gates the story.
- Story is complete only when its section renders AND its test is green.

### Parallel opportunities
- T005 (Alpine shell) is parallel with T002–T004 (Go handler stack) — different files.
- Within US1/US2, the view-render task ([P]) and the test task touch different files but the test asserts the handler the render depends on — sequence test after render within the story.
- T016 (PROJECTS.md) is parallel with the gates.

---

## Implementation Strategy

### MVP first (User Story 1 only)
1. Phase 1 baseline green → 2. Phase 2 foundation (handler + route + accessor + shell) → 3. Phase 3 US1 (retrieval + embeddings visible + correct) → **STOP and validate** (parity vs `go-rag status`). At this point the slice already delivers value; US2/US3 round it out.

### Incremental delivery
Foundation → +US1 (MVP) → +US2 (full config surface) → +US3 (boundary guarantees + browser) → Polish (gates + quickstart + commit). Each story adds value without breaking the previous.

---

## Notes

- [P] tasks = different files, no dependency on incomplete tasks.
- [Story] label maps each task to its spec user story for traceability.
- Constitution compliance is pre-checked (plan.md): read-only, UI-only (one trivial read-only `Engine.Config()` getter per T002), no on-disk layout change, no migration, no `ExpectedVersion` bump, vendored SPA (no Node build — `TestNoNodeArtifacts`).
- Every data display is read-only (FR-006); the endpoint is Bearer-guarded (FR-008); values are effective/parity-anchored (FR-007, SC-002).
- Commit after each logical group; stop at any checkpoint to validate a story independently.
