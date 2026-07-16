---

description: "Task list for spec 057 — Settings: API Keys (Slice 2a)"
---

# Tasks: Settings — API Keys (Slice 2a, spec 057)

**Input**: Design docs from `/specs/057-ui-settings-api-keys/` (spec.md, plan.md, research.md, data-model.md, contracts/api-keys.md, quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓

**Tests**: INCLUDED — go-rag constitution mandates `go test ./...` green; a credential surface especially needs airtight tests (secret-once, revoke-immediate).

**Organization**: Tasks grouped by user story (US1/US2/US3 from spec.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on incomplete tasks)
- **[Story]**: US1 / US2 / US3 (setup + foundational + polish have no story label)
- Exact file paths in every description

## Design refinements (from research.md)

1. **Secret-once is structural** — the raw secret is never persisted (only SHA-256[:16]); `CreateAPIKey` returns it as a display string in the create response ONLY. List/GET/audit physically cannot carry it (FR-003).
2. **Direct adapter over `s.store`** — the UI Server already holds the auth store; handlers call `auth.CreateAPIKey`/`ListAPIKeys`/`RevokeAPIKey` directly. No engine/storage/migration change.
3. **Revoked keys stay visible** — `RevokeAPIKey` sets `enabled=false` (doesn't delete); the list shows them as revoked (audit trail) + they immediately fail `ValidateAPIKey`.
4. **Expiry-on-create deferred** — UI creates non-expiring keys (Slice 2a); `expires_at` is still displayed in the list.

---

## Phase 1: Setup

**Purpose**: Confirm a green baseline on `main`.

- [x] T001 Verify baseline green on `main`: run `make build && go test ./...` (gate only).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The handler + routes + Alpine shell that every story depends on.

**⚠️ CRITICAL**: No user-story work can begin until this phase is complete.

- [x] T002 Create `internal/ui/apikeys.go` — define `apiKeyView` (id/label/mode/created_at/expires_at/enabled) + `createAPIKeyResponse` (apiKeyView + secret), and three handlers: `handleAPIKeysList` (`auth.ListAPIKeys(s.store)` → `[]apiKeyView`, no secret), `handleAPIKeyCreate` (validate label non-empty + mode ∈ read|write|admin → `auth.CreateAPIKey(s.store, label, mode, nil)` → `{view, secret}`; 400 on invalid), `handleAPIKeyRevoke` (`auth.RevokeAPIKey(s.store, id)` → 204; 404 on `auth.ErrUnknownAPIKey`). Read `s.store` directly (the UI Server holds it).
- [x] T003 Register `GET /api/settings/auth/api-keys`, `POST /api/settings/auth/api-keys`, `DELETE /api/settings/auth/api-keys/{id}` in `internal/ui/ui.go` (all guarded, admin-only).
- [x] T004 [P] Add the API Keys Alpine section to `internal/ui/web/static/js/app.js` (`loadAPIKeys` GET) + a Settings → API Keys card in `internal/ui/web/templates/index.html` (sortable table scaffold + create-dialog/revoke-confirm stubs).

**Checkpoint**: the three routes return correct shapes; the Settings view shows the API Keys shell.

---

## Phase 3: User Story 1 — List API keys (Priority: P1) 🎯 MVP

**Goal**: Operator sees existing keys (metadata only, never the secret).

**Independent test**: `GET` returns `[]apiKeyView`; no `secret` field anywhere.

- [x] T005 [P] [US1] Render the API Keys table in the Alpine section — columns id / label / mode / created / expires / enabled / actions. **Sortable** (console-UI convention: every data table is sortable), mirroring the Documents/Quarantine tables.
- [x] T006 [US1] Add `internal/ui/apikeys_test.go` — `TestAPIKeys_List`: authenticated `GET` → 200; empty vault ⇒ `[]`; **no `secret` field** in any element (FR-001/003).

**Checkpoint**: US1 functional — keys list, no secrets leak.

---

## Phase 4: User Story 2 — Create an API key (Priority: P1)

**Goal**: Operator creates a key; the secret shows once + never again.

**Independent test**: `POST` returns `{view, secret}`; subsequent `GET` excludes `secret`; invalid mode/label ⇒ 400.

- [x] T007 [P] [US2] Create dialog (label input + mode select read|write|admin) → POST → display the secret ONCE with a copy affordance + an "I've copied it" dismissal. The secret is NOT stored client-side beyond the dialog (no localStorage, no re-show on re-render). After dismissal, refresh the list.
- [x] T008 [US2] Add `TestAPIKeys_Create` to `apikeys_test.go` — `POST {label, mode}` → 201 with `{id,label,mode,created_at,expires_at,enabled,secret}`; **then** `GET` list excludes `secret` (FR-003 structural proof); invalid mode (`foo`) ⇒ 400; missing label ⇒ 400 (no key created).

**Checkpoint**: US2 functional — create works, secret shown once, never re-displayable.

---

## Phase 5: User Story 3 — Revoke an API key (Priority: P2)

**Goal**: Operator revokes a key (confirmed); it immediately stops working.

**Independent test**: `DELETE` → 204; the revoked bearer fails `ValidateAPIKey`; unknown id ⇒ 404.

- [x] T009 [P] [US3] Revoke action per row + a destructive-confirm dialog (reuse the shared `confirmDialog` from 050/051) → DELETE → refresh the list (the revoked row shows `enabled:false`, distinct styling).
- [x] T010 [US3] Add `TestAPIKeys_Revoke` to `apikeys_test.go` — `DELETE {id}` → 204; the revoked bearer then fails auth (401 via `ValidateAPIKey`); unknown id ⇒ 404; the list still returns the key with `enabled:false` (audit trail).

**Checkpoint**: US3 functional — revoke is immediate + irreversible + auditable.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [x] T011 Add `TestAPIKeys_401Unguarded` to `apikeys_test.go` — all three routes 401 without a bearer (admin-gated; bypass does not apply on an initialized vault).
- [x] T012 Run full gates — `make build && go vet && go test -race ./...` + `make lint` (golangci-lint, zero findings).
- [x] T013 Run `specs/057-ui-settings-api-keys/quickstart.md` end-to-end on an isolated daemon — create→secret-once, list→no-secret, revoke→revoked-bearer-401, invalid-mode→400, no-bearer→401; teardown.
- [x] T014 Browser-verify via Interceptor on an isolated daemon: API Keys sortable table + create dialog (secret shown once, copyable) + revoke confirm; confirm the secret appears only in the create dialog (not the table).
- [x] T015 [P] Update `~/.claude/LIFEOS/USER/PROJECTS/PROJECTS.md` go-rag entry — Settings Slice 2a (spec 057, API keys) shipped; sessions + admin reset (spec 058) remain.
- [x] T016 Commit to `main` (Conventional Commits): `docs(spec057): specify + plan + tasks`, then `feat(ui): settings — API keys (spec 057)`.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: none.
- **Foundational (Phase 2)**: BLOCKS all user stories.
- **User stories (Phase 3–5)**: US1 → US2 → US3 (shared Alpine section → sequential rendering).
- **Polish (Phase 6)**: after all stories.

### Parallel opportunities
- T004 (Alpine shell) ∥ T002–T003 (Go stack).
- T015 (PROJECTS.md) ∥ the gates.

---

## Implementation Strategy

### MVP first (US1)
Phase 1 → Phase 2 → US1 (list visible, no secrets) → **validate**. The slice already delivers value; US2/US3 round it out.

### Incremental delivery
Foundation → +US1 (list) → +US2 (create + secret-once) → +US3 (revoke) → Polish.

---

## Notes

- [P] tasks = different files, no dependency on incomplete tasks.
- Constitution compliance pre-checked (plan.md): write surface, local-only (no egress), admin-gated, **no on-disk layout change** (auth prefix shipped spec 045). Principle V (UI adapter).
- **Security-critical**: the secret-once property is structural (FR-003) — the raw secret is never persisted, so it can only ever appear in the create response. Tests T006/T008 prove it never reaches the list/GET.
- The API Keys table is sortable (console-UI convention).
- Commit after each logical group; stop at any checkpoint to validate a story independently.
