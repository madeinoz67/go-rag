# Implementation Plan: Settings — API Keys (Slice 2a, spec 057)

**Branch**: `main` (single-author repo) | **Date**: 2026-07-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/057-ui-settings-api-keys/spec.md`

## Summary

Slice 2a of the Settings/Auth arc — manage labelled `gorag_` API keys (list /
create / revoke) from the console. Three new admin-gated UI routes over the
**existing** `auth.Store` (`s.store`): `GET`/`POST`/`DELETE /api/settings/auth/api-keys`.
The first **write surface** in Settings and security-sensitive: the raw secret is
shown exactly once (the create response) and nowhere else. **No engine/storage/
migration change** — a 5th adapter over the spec 045 auth surface.

## Technical Context

**Language/Version**: Go 1.26 (`CGO_ENABLED=0`); static vendored SPA.

**Primary Dependencies**: cobra, Pebble, Alpine.js (vendored). **No new deps.** Reuses `internal/auth`.

**Storage**: N/A — reads/writes existing `PrefixAuthAPIKey` records via the store; no schema change.

**Testing**: `go test -race ./...`; `golangci-lint run`; UI parity tests + Interceptor verification.

**Target Platform**: local single-operator loopback console (`127.0.0.1:7881`).

**Constraints**: admin-gated (FR-006); secret-shown-once (FR-003, the load-bearing safety property); destructive-confirm on create-dismissal + revoke (FR-007); no on-disk layout change.

**Scale/Scope**: one new handler file (`apikeys.go`) + tests, three routes, one Alpine section (sortable keys table + create dialog + revoke confirm). UI-layer only.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|---|---|---|
| I. Local-First, Single-Binary | PASS | write surface but loopback-only + admin-gated; no egress; the secret transits loopback only (no TLS on loopback, spec 007) |
| II. Content-Addressed Identity | N/A | no ingest/identity path |
| III. Pure Go — No CGo | PASS | vendored SPA, no Node build; `CGO_ENABLED=0` |
| IV. Async-After-ACK | N/A | that budget is for INGEST; auth writes are synchronous + small |
| V. Extension by Interface, MCP-First | PASS | UI adapter over the existing `auth.Store`; no new auth/engine method |

**Storage discipline**: NO on-disk layout change — `PrefixAuthAPIKey` shipped in
spec 045; this slice reads/writes existing records via the store. No migration, no
`ExpectedVersion` bump.

**Security invariant**: the raw secret (`createAPIKeyResponse.secret`) appears in the
POST response body ONLY — never in `GET`, never in the audit log, never in an error.
This is enforced structurally (the secret is never persisted; only its SHA-256[:16]
hash is) + by the handler (FR-003).

**Violations**: none → Complexity Tracking table is empty.

## Project Structure

### Documentation (this feature)

```text
specs/057-ui-settings-api-keys/
├── spec.md              # /speckit-specify
├── plan.md              # this file (/speckit-plan)
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── api-keys.md      # GET/POST/DELETE /api/settings/auth/api-keys
└── tasks.md             # /speckit-tasks (next phase)
```

### Source Code (repository root)

```text
internal/
├── ui/
│   ├── apikeys.go           # NEW  — handleAPIKeysList/Create/Revoke + apiKeyView/createAPIKeyResponse
│   ├── apikeys_test.go      # NEW  — list/empty, create(secret-once), revoke(404+disabled), 400, 401
│   └── ui.go                # EDIT — register the 3 guarded routes
└── (auth — UNCHANGED, reuse CreateAPIKey/ListAPIKeys/RevokeAPIKey)

internal/ui/web/
├── templates/index.html     # EDIT — Settings → API Keys section (sortable table + create dialog + revoke confirm)
└── static/js/app.js         # EDIT — loadAPIKeys/createAPIKey/revokeAPIKey + the create-dialog/revoke-confirm state
```

**Structure Decision**: UI-layer only. `internal/auth` is unchanged (read/write
reuse per research R5). The API Keys table MUST be sortable (console-UI convention:
every data table is sortable). The create dialog + revoke confirm reuse the shared
`confirmDialog` pattern from 050/051. Mirrors spec 053 (Quarantine) for the
table + destructive-confirm shape.

## Complexity Tracking

> None — Constitution Check passes with no violations.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
