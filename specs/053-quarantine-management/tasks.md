# Tasks: Quarantine Management View

**Input**: Design documents from `/specs/053-quarantine-management/`

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓.

**Tests**: INCLUDED. UI-only slice — engine surface already exists (ListPoisoned/ReleaseChunk/ResetChunk/RescanPoisoning, all vault-aware).

## Format: `[ID] [P?] [Story?] Description (file path)`

---

## Phase 1: Setup (UI skeleton)

- [X] T001 Create `internal/ui/quarantine.go`: DTO structs per [data-model.md](./data-model.md) — `quarantineListDTO{Chunks, Count}`, `poisonedChunkDTO{ChunkID, DocumentID, Preview, Verdict{Level, Score, Signals, MatchedPhrases}}`; projection helper `toQuarantineListDTO([]engine.PoisonedChunk)`; empty handler stubs (`handleQuarantineList`, `handleQuarantineRelease`, `handleQuarantineReset`, `handleQuarantineRescan`).

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean.

---

## Phase 2: Foundational (routes + handlers)

- [X] T002 Implement handlers in `internal/ui/quarantine.go`: `handleQuarantineList` (`s.eng.ListPoisoned(vault)` → `quarantineListDTO`, 200 always); `handleQuarantineRelease` (`s.eng.ReleaseChunk(vault, chunkID)` → 204 / 404); `handleQuarantineReset` (`s.eng.ResetChunk(vault, chunkID)` → 204 / 404); `handleQuarantineRescan` (`s.eng.RescanPoisoning(vault)` → 204). Vault from `?vault=` query param (default "default"). Errors via `writeEngineErr`.
- [X] T003 Register routes in `internal/ui/ui.go::Server.Handler`: `GET /api/quarantine/list`, `POST /api/quarantine/{id}/release`, `POST /api/quarantine/{id}/reset`, `POST /api/quarantine/rescan` — all guarded (// spec 053).

**Checkpoint**: curl `GET /api/quarantine/list` works (200, list or empty); 401 without Bearer.

---

## Phase 3: User Story 1 — Browse flagged chunks (Priority: P1) 🎯 MVP

**Goal**: operator opens Quarantine and sees every flagged chunk with verdict + score.

**Independent Test**: [quickstart.md](./quickstart.md) §1 — flagged chunk appears; count matches `go-rag poison list`.

- [X] T004 [US1] Alpine Quarantine list view — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `components.css`): on Quarantine view-entry fetch `/api/quarantine/list?vault=...`; render each chunk (document name, preview, verdict level badge, composite score); sidebar "Quarantine" item (new, expanding the sidebar); healthy empty state when zero flagged.
- [X] T005 [US1] US1 tests — `internal/ui/quarantine_test.go`: (a) `GET /api/quarantine/list` 200 + chunks with verdict fields; (b) parity with `go-rag poison list`; (c) 401 without Bearer; (d) empty vault → `{chunks:[], count:0}`.

**Checkpoint**: US1 independently testable — browseable quarantine (MVP).

---

## Phase 4: User Story 2 — Inspect verdict detail (Priority: P1)

**Goal**: click a flagged chunk → full text with matched phrases highlighted + signal breakdown.

**Independent Test**: [quickstart.md](./quickstart.md) §2 — matched phrases highlighted; signal scores + thresholds visible.

- [X] T006 [US2] Alpine detail view — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `components.css`): click a flagged chunk → fetch `GetChunk(vault, chunkID)` for the full text; overlay the PoisonVerdict's `MatchedPhrases` as highlighted `<mark>` spans (repetition=amber, stuffing=red, instruction=purple); show the per-signal score breakdown (Repetition/Stuffing/Instruction + the thresholds from the Operations view's PoisonThresholdSus/Qua); document name + section breadcrumb.
- [X] T007 [US2] US2 tests — `internal/ui/quarantine_test.go`: (a) detail carries full content + verdict; (b) matched phrases present in the verdict; (c) signals breakdown (Repetition/Stuffing/Instruction) present.

**Checkpoint**: US2 independently testable — inspectable verdicts with highlighting.

---

## Phase 5: User Story 3 — Release / Reset / Rescan (Priority: P2)

**Goal**: release false positives, reset verdicts, trigger vault rescan — all confirmed.

**Independent Test**: [quickstart.md](./quickstart.md) §3 — release → chunk gone, count decremented, queryable.

- [X] T008 [US3] Alpine actions — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: per-chunk **Release** button → confirm dialog → `POST /api/quarantine/{id}/release`; **Reset** button → confirm → `POST /api/quarantine/{id}/reset`; vault-wide **Rescan** button → confirm → `POST /api/quarantine/rescan` + "scanning..." state + refresh. Tooltips explain Release (permanent false-positive override) vs Reset (force re-scan).
- [X] T009 [US3] US3 tests — `internal/ui/quarantine_test.go`: (a) release → 204, chunk gone from list, count decremented; (b) reset → 204; (c) rescan → 204; (d) 404 unknown chunk; (e) 401 without Bearer.

**Checkpoint**: US3 independently testable — actionable quarantine.

---

## Phase 6: User Story 4 — Vault-aware, confirmed, shell-consistent (Priority: P2)

**Goal**: vault picker works; destructive ops confirmed; no Node; edge states degrade.

- [X] T010 [US4] Vault-aware + confirmed + edge-state tests — `internal/ui/quarantine_test.go`: (a) vault param flows (list reflects selected vault); (b) repo-root scan finds no `package.json`/`node_modules`; (c) no write route reachable without Bearer; (d) session-expiry 401 → graceful. (FR-005, FR-006, FR-008, FR-009)

**Checkpoint**: US4 independently testable — invariants pinned.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T011 [P] Gate hygiene — `make lint` (0), `make vet`, `make test -race` clean.
- [X] T012 [P] quickstart validation — run [quickstart.md](./quickstart.md) §1–§4 on an isolated store: curl smoke + Interceptor browser verify.
- [X] T013 [P] Doc sync — update PROJECTS.md + MuninnDB memory; note the Quarantine Management view shipped (the standing preference fulfilled).

---

## Dependencies & Execution Order

- **Setup (Phase 1)**: T001 blocks T002.
- **Foundational (Phase 2)**: T002 → T003 (routes need handler symbols).
- **US1 (Phase 3)**: depends on Foundational. MVP gate.
- **US2 (Phase 4)**: depends on Foundational + US1 (detail opens from a list row).
- **US3 (Phase 5)**: depends on US1 (actions on list rows).
- **US4 (Phase 6)**: depends on US1–US3.
- **Polish (Phase 7)**: depends on all stories.

**MVP: US1** (T001→T005). **Demo-complete: US2** (browse + inspect verdicts with highlighting).

## Implementation Strategy

Setup → Foundational → US1 (MVP) → US2 (demo) → US3 → US4 → Polish. Each checkpoint independently testable.
