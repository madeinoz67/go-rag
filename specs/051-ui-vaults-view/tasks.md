# Tasks: Vaults Management View

**Input**: Design documents from `/specs/051-ui-vaults-view/`

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓.

**Tests**: INCLUDED. The engine fixes (Phase 1) carry engine tests; each user story carries UI tests (the repo's standing convention, mirroring spec 053).

## Format: `[ID] [P?] [Story?] Description (file path)`

---

## Phase 1: Setup (engine surface + UI skeleton)

The engine vault surface has two gaps found in research — close them before the UI. Then scaffold the UI file.

- [X] T001 Fix `Engine.ListVaults` to read the in-db registry: replace `vaultpkg.List()` + per-dir `Open` with `e.db.ListVaultNames()` + `countPrefix(e.db, ws, PrefixDocument)` for each name's ws (`internal/engine/config.go`).
- [X] T002 Add `Engine.CreateVault(ctx, name)`: `vaultpkg.ValidateName` → refuse if `e.db.VaultNameExists(name)` → `ws := e.db.ResolveVaultPrefix(name)` → `e.db.WriteVaultName(ws, name)` (`internal/engine/vault_lifecycle.go`).
- [X] T003 Add a `default`-vault delete guard: `Engine.DeleteVault` returns `ErrInvalid` when `vault == "default"` (before ClearVault) (`internal/engine/vault_lifecycle.go`).
- [X] T004 Engine tests for the fixes: `ListVaults` reflects in-db vaults (not dirs) after an implicit create; `CreateVault` registers an empty vault that `ListVaults` then lists, refuses duplicates/bad names; `DeleteVault("default")` is refused (`internal/engine/vault_create_test.go`).
- [X] T005 Create `internal/ui/vaults.go`: DTOs per [data-model.md](./data-model.md) — `vaultsListDTO{Vaults, Active}`, `vaultDTO{Name, Documents, Active}`, request bodies `createVaultRequest{Name}`, `renameVaultRequest{NewName}`; projection helper `toVaultsListDTO([]engine.VaultEntry, active string)`; empty handler stubs (`handleVaultsList`, `handleVaultCreate`, `handleVaultRename`, `handleVaultClear`, `handleVaultDelete`).

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean; `go test -race ./internal/engine/` green (ListVaults/CreateVault/delete-guard).

---

## Phase 2: Foundational (handlers + routes + shell picker)

- [X] T006 Implement handlers in `internal/ui/vaults.go`: `handleVaultsList` (`s.eng.ListVaults("")` → `vaultsListDTO`, active = `vaultFromRequest(r)`, 200); `handleVaultCreate` (decode `{name}` → `s.eng.CreateVault` → 201 + vaultDTO / 400 invalid / 409-or-400 duplicate); `handleVaultRename` (`{name}` path + `{new_name}` body → `s.eng.RenameVault` → 200 / 400 / 404); `handleVaultClear` (`s.eng.ClearVault` → 204 / 404); `handleVaultDelete` (`s.eng.DeleteVault` → 204 / 400 default / 404). Errors via `writeEngineErr`.
- [X] T007 Register routes in `internal/ui/ui.go::Server.Handler`: `GET /api/vaults`, `POST /api/vaults`, `POST /api/vaults/{name}/rename`, `POST /api/vaults/{name}/clear`, `DELETE /api/vaults/{name}` — all guarded (// spec 051).
- [X] T008 Populate the shell vault picker: on `mount()` + on Vaults view-entry, fetch `GET /api/vaults` → set `this.vaults` (names) from the list; keep `this.vault` valid (fall back to `default` if the active vault is no longer listed) (`internal/ui/web/static/js/app.js`).

**Checkpoint**: curl `GET /api/vaults` works (200, list with active marker); 401 without Bearer; the shell picker offers every vault.

---

## Phase 3: User Story 1 — List vaults + active marker (Priority: P1) 🎯 MVP

**Goal**: operator opens Vaults and sees every vault with name + document count + the active vault marked; the shell picker reflects the list.

**Independent Test**: [quickstart.md](./quickstart.md) §1 — rows + counts match `go-rag vault list`; the active row is marked.

- [X] T009 [US1] Alpine Vaults list view — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `components.css`): replace the Vaults placeholder with a real view; on view-entry fetch `/api/vaults`; render each vault (name, document count, active badge); a per-row active marker (distinct styling for `active===true`); healthy state when only `default` exists. Table follows the sortable-columns convention (name + documents sortable).
- [X] T010 [US1] US1 tests — `internal/ui/vaults_test.go`: (a) `GET /api/vaults` 200 + vaults with name/documents/active; (b) the active vault (per `X-Go-Rag-Vault`/`?vault=`) is marked active and no other is; (c) parity — the UI list matches `engine.ListVaults` (the in-db registry); (d) 401 without Bearer.

**Checkpoint**: US1 independently testable — browseable vault list (MVP).

---

## Phase 4: User Story 2 — Create a vault (Priority: P1)

**Goal**: operator creates a new named vault; it appears in the list + the picker.

**Independent Test**: [quickstart.md](./quickstart.md) §2 — create "archive"; it lists; `GET /api/vaults` includes it; a duplicate/bad name is refused.

- [X] T011 [US2] Alpine create dialog — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: a "Create vault" button → dialog (name input, validated client-side: lowercase alnum + hyphens, 1–64) → `POST /api/vaults` → on 201 refresh the list + picker; on 400 surface the reason (duplicate / invalid). Reuse the dialog pattern from the Documents add-dialog.
- [X] T012 [US2] US2 tests — `internal/ui/vaults_test.go`: (a) create valid → 201 + vault appears in `GET /api/vaults`; (b) duplicate name → 400; (c) invalid name (uppercase/space/too-long) → 400; (d) 401 without Bearer.

**Checkpoint**: US2 independently testable — creatable vaults.

---

## Phase 5: User Story 3 — Switch the active vault, live (Priority: P1)

**Goal**: operator switches the active vault; every view reflects it instantly (no restart); the choice persists.

**Independent Test**: [quickstart.md](./quickstart.md) §3 — switch A→B; Documents reflects B; switch back; A returns. No restart.

- [X] T013 [US3] Switch affordance — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: a per-row "Switch" action (for non-active vaults) → calls the existing `switchVault(name)` (sets the picker + `X-Go-Rag-Vault`, persists to localStorage, refreshes the current view); the active row shows "Switch" disabled/hidden. Confirm the shell picker change also drives `switchVault` (one path).
- [X] T014 [US3] US3 tests — `internal/ui/vaults_test.go`: (a) after switch, `GET /api/vaults?vault=B` marks B active; (b) a per-vault read (e.g. `GET /api/documents?vault=B`) returns B's data (isolation); (c) switching persists (the picker restores the last active on reload — assertible via the `?vault=` echo). (Live switch is client-side; the test pins the per-request isolation that makes it correct.)

**Checkpoint**: US3 independently testable — live vault switching.

---

## Phase 6: User Story 4 — Rename a vault (Priority: P2)

**Goal**: operator renames a vault; list + picker + active marker update; data identity preserved.

**Independent Test**: [quickstart.md](./quickstart.md) §4 — rename "scratch"→"drafts"; queries under "drafts" return "scratch"'s data.

- [X] T015 [US4] Rename action — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: per-row "Rename" → confirm dialog with a new-name input (validated) → `POST /api/vaults/{name}/rename` → refresh list + picker; if the renamed vault was active, the active marker follows the new name. Reuse the quarantine confirm-dialog pattern.
- [X] T016 [US4] US4 tests — `internal/ui/vaults_test.go`: (a) rename valid unused → 200 + new name lists; (b) rename to an existing name → 400; (c) rename unknown → 404; (d) data identity — after rename, `GET /api/documents?vault=<new>` returns the docs `<old>` held (the engine's metadata-only rename guarantees this); (e) 401 without Bearer.

**Checkpoint**: US4 independently testable — renamable vaults.

---

## Phase 7: User Story 5 — Clear + Delete (Priority: P2)

**Goal**: operator clears (empty, keep) or deletes (gone) a vault; default can't be deleted.

**Independent Test**: [quickstart.md](./quickstart.md) §5 — clear → count 0, still listed; delete → gone; delete default → refused.

- [X] T017 [US5] Clear + Delete actions — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: per-row "Clear" (confirm, danger) → `POST /api/vaults/{name}/clear`; per-row "Delete" (confirm, danger; disabled/hidden for `default`) → `DELETE /api/vaults/{name}`; on success refresh list + picker; if the active vault was deleted, fall back to `default` + a non-blocking notice. Reuse the confirm-dialog pattern.
- [X] T018 [US5] US5 tests — `internal/ui/vaults_test.go`: (a) clear → 204, vault still listed with documents 0; (b) delete non-default → 204, gone from list + (re)create confirms it's absent; (c) delete `default` → 400; (d) clear/delete unknown → 404; (e) 401 without Bearer.

**Checkpoint**: US5 independently testable — clearable/deletable vaults (default guarded).

---

## Phase 8: User Story 6 — Vault-aware, confirmed, shell-consistent (Priority: P2)

**Goal**: every op targets one vault; destructive ops confirmed; no Node; graceful 401; healthy single-default state.

**Independent Test**: [quickstart.md](./quickstart.md) §6 — every call targets a named vault; no destructive op without confirm; no Node artifacts.

- [X] T019 [US6] Invariant tests — `internal/ui/vaults_test.go`: (a) every write route 401s without Bearer (FR-010); (b) no write route mutates without the confirm gate being client-side (assert routes exist + are guarded); (c) repo-root scan finds no `package.json`/`node_modules` (re-assert the existing `TestNoNodeArtifacts` covers the new files); (d) single-default-vault state — `GET /api/vaults` on a fresh store returns exactly `default`, marked active; (e) session-expiry 401 → graceful (the shell re-locks). (FR-008, FR-009, FR-011, FR-012)

**Checkpoint**: US6 independently testable — invariants pinned.

---

## Phase 9: Polish & Cross-Cutting Concerns

- [X] T020 [P] Gate hygiene — `make lint` (0), `make vet`, `make test -race` clean.
- [X] T021 [P] quickstart validation — run [quickstart.md](./quickstart.md) §1–§6 on an isolated store: curl smoke + Interceptor browser verify (list/create/switch/rename/clear/delete; active marker; default-delete refusal).
- [X] T022 [P] Doc sync — update PROJECTS.md + MuninnDB memory; note the Vaults Management view shipped + the two engine fixes (stale ListVaults corrected; CreateVault added; default-delete guard).

---

## Dependencies & Execution Order

- **Setup (Phase 1)**: T001–T004 (engine) are independent of each other → parallelizable; T005 (UI skeleton) blocks T006.
- **Foundational (Phase 2)**: T006 (handlers) → T007 (routes) → T008 (picker). Depends on Phase 1.
- **US1 (Phase 3)**: depends on Foundational. MVP gate.
- **US2 (Phase 4)**: depends on US1 (create refreshes the list).
- **US3 (Phase 5)**: depends on US1 (switch acts on list rows).
- **US4 (Phase 6)**: depends on US1.
- **US5 (Phase 7)**: depends on US1.
- **US6 (Phase 8)**: depends on US1–US5.
- **Polish (Phase 9)**: depends on all stories.

**MVP: US1** (T001→T010). **Demo-complete: US1+US2+US3** (browse + create + switch).

## Implementation Strategy

Engine fixes first (Phase 1 — they unblock the UI + are independently testable) → UI handlers/routes/picker (Phase 2) → US1 (MVP) → US2+US3 (demo: create + switch) → US4+US5 (full management) → US6 (invariants) → Polish. Each checkpoint independently testable.
