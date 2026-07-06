# Tasks: Authentication & Tokens

**Input**: Design documents in `specs/045-auth-tokens/` — plan.md, spec.md, research.md, data-model.md, contracts/auth.md, quickstart.md.

**Prerequisites**: plan.md (required), spec.md (required).

**Tests**: Included per story — go-rag's constitution mandates `go test ./...` passing on every change, so each entity/endpoint ships with its test.

**Organization**: Tasks grouped by user story, in dependency order (P1 stories first, ordered so each story's dependencies are built before it; P2 stories after).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US6 (maps to spec.md)
- Exact file paths in every description

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: package skeleton, dependency, prefix reservation.

- [x] T001 Reserve three free prefix bytes in `internal/storage/storage.go` — define `PrefixAuthAPIKey`, `PrefixAuthAdmin`, `PrefixAuthSession` (avoid `0x11` spec 019, `0x16` BL-011, `0xFF` reserved; pick against the live constants)
- [x] T002 [P] Create `internal/auth` package skeleton — `internal/auth/doc.go` (package doc) + `internal/auth/auth.go` with the `Principal` struct and `Validate`/`ValidateToken` signatures (stubs returning `error`)
- [x] T003 [P] Add `golang.org/x/crypto/bcrypt` dependency — `go get golang.org/x/crypto/bcrypt@latest && go mod tidy` (pure-Go, BSD-3 → Principle III)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Pebble store, schema migration v3, audit events — MUST complete before any story.

**⚠️ CRITICAL**: No user-story work can begin until this phase is complete.

- [x] T004 [P] Implement Pebble-backed `Store` in `internal/auth/store.go` — CRUD helpers over `storage.DB.SetWithPrefix`/`GetWithPrefix`/`PrefixScanByte` for the three auth prefixes (by-hash and by-username lookup)
- [x] T005 Add migration v3 — `internal/storage/migrate/v3_auth_prefixes.go` (`v3RegisterAuthPrefixes` idempotent `Up`) + register in `defaultMigrations` + bump `ExpectedVersion` 2→3 in `internal/storage/migrate/migrate.go`
- [x] T006 [P] Extend `internal/audit/event.go` — add `TokenMgmtEvent` + `LoginEvent` (success + failure) following the existing `AuthFailEvent` pattern
- [x] T007 Migration test — `internal/storage/migrate/migrate_test.go`: v3 idempotency (re-run is a no-op) + v2→v3 transform (constitution requires both)

**Checkpoint**: foundation ready — store, schema v3, audit, prefixes in place. User-story work can begin.

---

## Phase 3: User Story 1 — Multiple labelled API keys (Priority: P1) 🎯 MVP

**Goal**: `go-rag auth create/list/revoke` manages labelled, scoped, hashed, revocable keys.

**Independent Test**: quickstart.md §2 — create a key; it authenticates; revoke → 401; the secret is never persisted.

### Implementation

- [x] T008 [P] [US1] `APIKey` entity + storage in `internal/auth/apikey.go` — `CreateAPIKey` (32-byte random, `gorag_`+base64url, SHA-256[:16] storageHash, [:8] ID), `ValidateAPIKey`, `ListAPIKeys`, `RevokeAPIKey`
- [x] T009 [US1] `go-rag auth create/list/revoke` CLI in `internal/cli/auth.go` (cobra) — `create --label --mode read|write|admin [--expires]` prints `gorag_<id>.<secret>` once; `list` table (no secrets); `revoke <id>`
- [x] T010 [US1] Register the `auth` parent command in `cmd/go-rag/main.go`
- [x] T011 [US1] Tests in `internal/auth/apikey_test.go` — create→validate→list→revoke; secret absent from store; SHA-256 key correctness; expiry enforcement; `go test -race` clean

**Checkpoint**: API keys fully manageable via CLI (not yet enforced on transports — that is US2).

---

## Phase 4: User Story 6 — Admin bootstrap (Priority: P1) 🎯 MVP

**Goal**: first-run `go-rag init` creates the `admin` user; no insecure default.

**Independent Test**: quickstart.md §1 — `GORAG_ADMIN_PASSWORD=secret go-rag init` → admin login works; no env → generated password printed once.

### Implementation

- [x] T012 [P] [US6] `AdminUser` entity + bcrypt in `internal/auth/admin.go` — `CreateAdmin` (bcrypt cost 12), `VerifyPassword` (constant-time via bcrypt), `AdminExists`
- [x] T013 [US6] Bootstrap flow in `internal/cli/init.go` — on `init`/first `start`: if no admin, create `admin` (password from `GORAG_ADMIN_PASSWORD` or generated + printed once); idempotent; `GORAG_ADMIN_PASSWORD` rotates an existing admin
- [x] T014 [US6] Tests in `internal/auth/admin_test.go` — bcrypt round-trip; `AdminExists` flips; bootstrap idempotency; no `password`/`root` default ships

**Checkpoint**: `go-rag init` bootstraps the admin user.

---

## Phase 5: User Story 3 — UI login → Bearer session, no cookies (Priority: P1) 🎯 MVP

**Goal**: `POST /api/auth/login` mints an opaque `gorags_…` session; SPA stores in `sessionStorage`; no `Set-Cookie` ever.

**Independent Test**: quickstart.md §3 — login returns `gorags_…`; no `Set-Cookie`; logout → 401.

> **Note**: `/api/auth/*` ships on the REST transport (`:7879`) now; the UI (spec 046) will mount it on `:7881`. The no-cookie contract is identical either way.

### Implementation

- [x] T015 [P] [US3] `Session` entity in `internal/auth/session.go` — `MintSession` (32-byte random, `gorags_`+base64url, SHA-256[:16] key, opaque + store-tracked), `ValidateSession` (lookup + expiry + last-seen bump), `RevokeSession`, `ListSessions`; default TTL 12h
- [x] T016 [US3] HTTP handlers in `internal/auth/http.go` (or `internal/rest/auth.go`) — `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session` (admin), `DELETE /api/auth/session/<hash>` (admin); **never** emit `Set-Cookie`
- [x] T017 [US3] Wire `/api/auth/*` routes into the REST server in `internal/rest/server.go` (mount on `:7879`; 046 remounts on `:7881`)
- [x] T018 [US3] `go-rag auth session list/revoke` CLI in `internal/cli/auth.go`
- [x] T019 [US3] Tests — `internal/auth/session_test.go` (mint→validate→revoke; expiry) + login endpoint tests in `internal/rest/auth_test.go` (bcrypt login; **assert no `Set-Cookie`** in any response; logout invalidates; bad password → 401 + audit)

**Checkpoint**: login → Bearer session works end-to-end; no cookies emitted.

---

## Phase 6: User Story 2 — Unified validation across all transports (Priority: P1) 🎯 MVP

**Goal**: one `internal/auth.Validate`; delete the three duplicated bearer checks; a key works identically on REST/gRPC/MCP.

**Independent Test**: quickstart.md §2 — one key accepted on REST, gRPC, and MCP; revoke → all three 401.

### Implementation

- [x] T020 [US2] Implement `Validate(r)` + `ValidateToken(token)` in `internal/auth/auth.go` — prefix dispatch (`gorag_`→APIKey, `gorags_`→Session), length cap 4096, hash-lookup, `Enabled`/`ExpiresAt` checks, return `Principal`; emit `AuthFailEvent` on any failure
- [ ] T021 [US2] REST — delete `checkBearer` in `internal/rest/server.go`; route every `/api/*` through an `auth.Validate` middleware that puts `Principal` in `context.Context`
- [ ] T022 [US2] gRPC — delete `bearerInterceptor`+`hasBearer` in `internal/grpc/server.go`; replace with an interceptor calling `auth.ValidateToken`, propagating `Principal` via context (mirrors MuninnDB's context-key pattern)
- [ ] T023 [US2] MCP — delete `checkBearer` in `internal/mcp/http.go`; delegate to `auth.Validate`
- [ ] T024 [US2] Tests — `internal/auth/auth_test.go` (dispatch; reject absent/expired/disabled/garbage-prefix; audit fires) + update `TestHTTPBearerAuth`, `TestGRPC_Query_Bearer*_Rejected`, and the MCP bearer test to exercise `auth.Validate`

**Checkpoint**: all three transports delegate to the single validator; the bespoke checks are gone.

---

## Phase 7: User Story 4 — `mcp.token` migration (Priority: P2)

**Goal**: zero-break upgrade — existing `mcp.token` becomes a `legacy-mcp` API key on first post-upgrade open.

**Independent Test**: quickstart.md §4 — old token value still authenticates post-migration; `auth list` shows `legacy-mcp`.

### Implementation

- [x] T025 [US4] Migration import in `internal/auth/legacy.go` (called from `internal/cli/init.go`/store-open) — if `<vault>/mcp.token` exists and the key store is empty, import its value as an API key (`label=legacy-mcp`, `mode=admin`, `StorageHash=SHA-256(value)[:16]`); emit a deprecation log; skip when the store is non-empty
- [x] T026 [US4] Tests in `internal/auth/legacy_test.go` — `mcp.token` → `legacy-mcp` key; the old value authenticates via the SHA-256 path; skipped when keys already exist; idempotent on re-run

**Checkpoint**: existing scripts keep working through the upgrade.

---

## Phase 8: User Story 5 — Loopback bypass (Priority: P2)

**Goal**: loopback + empty stores → bypass (local "just works"); non-loopback → fail-closed.

**Independent Test**: quickstart.md §5 — loopback 200; LAN IP 401.

### Implementation

- [x] T027 [US5] Bypass logic in `internal/auth/bypass.go` — `isLoopback(r)`, `storesEmpty()`, return `Principal{Source:"bypass"}` when both hold; fail-closed otherwise
- [x] T028 [US5] Wire bypass into `auth.Validate` in `internal/auth/auth.go` — no Bearer + loopback + empty stores → bypass; no Bearer + non-loopback → 401 (even with empty stores)
- [x] T029 [US5] Tests in `internal/auth/bypass_test.go` — loopback+empty → `Source=bypass`; LAN IP → 401; non-empty stores disable bypass even on loopback

**Checkpoint**: local UX preserved; network exposure fail-closed.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: MCP-first exposure, PRD amendments, end-to-end validation.

- [ ] T030 [P] Expose MCP auth tools in `internal/mcp` — `auth.list`, `auth.create`, `auth.revoke`, `auth.session.list`, `auth.session.revoke` (admin-gated; `auth.bootstrap` stays CLI-only per research R6)
- [ ] T031 [P] Update `PRD_RAG_Database.md` §6.7 — add the three new prefix rows (Storage-discipline compliance)
- [ ] T032 [P] Amend `PRD_RAG_Database.md` §2.2 — distinguish single-operator auth (now in scope) from multi-user (still out), same pattern as spec 029/032
- [ ] T033 [P] Update `ISA.md` with the auth capability + schema-version note (ExpectedVersion now 3)
- [ ] T034 Run `quickstart.md` end-to-end (all six scenarios) on an isolated vault; capture outputs
- [ ] T035 `make lint && make vet && make test` clean (SC-006/007) — `golangci-lint` is the strict CI gate

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately.
- **Foundational (Phase 2)**: depends on Setup; **BLOCKS all stories**.
- **Stories (Phases 3–8)**: ordered by dependency (not strictly by P-number), because the validators stack:
  1. US1 (APIKey) — needs the Store.
  2. US6 (Admin) — needs the Store + bcrypt.
  3. US3 (Session/login) — needs the Store + Admin (login verifies the admin).
  4. US2 (Validate) — needs the APIKey + Session validators to dispatch to.
  5. US4 (migration) — needs the APIKey store + bootstrap path.
  6. US5 (bypass) — needs Validate + the empty-store check.
- **Polish (Phase 9)**: depends on all stories complete.

### Within Each User Story

Models/services before endpoints; core before integration; tests alongside each entity/endpoint; story independently testable before moving on.

### Parallel Opportunities

- Setup T002/T003 parallel with T001 (different files).
- Foundational T004/T006 parallel with each other (different files); T005/T007 are the migration + its test (sequential).
- Within US1: T008 (apikey.go) is the lone implementation file → T009 (CLI) follows it.
- US1, US6 begin after Foundational and can overlap (different packages) until US2 consolidates.
- Polish T030–T033 are all `[P]` (different files).

---

## Parallel Example: Foundational + US1

```bash
# Foundational — run together (different files):
Task: "T004 Pebble-backed Store in internal/auth/store.go"
Task: "T006 Audit events in internal/audit/event.go"

# US1 — APIKey entity, then the CLI that consumes it:
Task: "T008 APIKey entity in internal/auth/apikey.go"
# then:
Task: "T009 auth create/list/revoke CLI in internal/cli/auth.go"
```

---

## Implementation Strategy

### MVP scope (the four P1 stories)

1. Phase 1 (Setup) + Phase 2 (Foundational) — **CRITICAL**, blocks everything.
2. Phase 3 (US1 — API keys).
3. Phase 4 (US6 — bootstrap).
4. Phase 5 (US3 — login/session).
5. Phase 6 (US2 — unified validation).
6. **STOP and VALIDATE**: run quickstart §1–§3 + §6; confirm a key works across REST/gRPC/MCP and login→session is cookie-free.

At this point auth is functional and enforced. US4 (migration) + US5 (bypass) + Polish follow as hardening.

### Incremental Delivery

- After Phase 6: keys enforced on all transports; the old `mcp.token` static path is superseded (US4 restores zero-break for upgraders).
- After Phase 8: local-first UX preserved (bypass) + network fail-closed.
- Phase 9 closes out MCP-first exposure + the PRD/ISA documentation the constitution requires.

### Notes

- Commit after each task or logical group (Conventional Commits to `main`).
- The implementation PR **MUST** state the schema-version impact (migration v3 added, `ExpectedVersion` now 3) per the constitution's compliance rule.
- Never run `go-rag start`/`stop` against the global vault during development — use `--db-path <tmp>` + non-default `--rest-addr`/`--grpc-addr`/`--ui-addr`.
