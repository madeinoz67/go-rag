# Feature Specification: Settings — API Keys (Slice 2a, spec 057)

**Feature Branch**: `main` (single-author repo — spec directory `057-ui-settings-api-keys`)

**Created**: 2026-07-16

**Status**: Draft

**Input**: Settings Slice 2a of the Auth & Credentials arc — manage labelled `gorag_` API keys (list / create / revoke) from the console. Sessions (list/revoke) + admin password reset are the follow-up (spec 058). This is the **first write surface** in the Settings view and is **security-sensitive** (credential handling).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - List API keys (Priority: P1)

The operator sees the existing API keys — id (`gorag_<id8>`), label, mode (read|write|admin), created, expires — to audit which programmatic clients hold access. **The raw secret is never included** in any list/view.

**Why this priority**: Visibility into existing credentials is the baseline for a credential-management surface.

**Independent test**: `GET /api/settings/auth/api-keys` returns keys with metadata only; the response has no `secret` field anywhere.

**Acceptance Scenarios**:

1. **Given** one or more API keys exist, **When** the operator opens the Auth panel, **Then** each key shows id/label/mode/created/expires and no secret.
2. **Given** no keys exist, **When** the operator lists, **Then** an empty list is shown (not an error).

---

### User Story 2 - Create an API key (Priority: P1)

The operator creates a key (label + mode `read`|`write`|`admin`); the **raw secret is shown exactly once** for copying; once dismissed it is **never recoverable** (only its SHA-256 hash is stored, per spec 045).

**Why this priority**: Create is the core write capability of the panel.

**Independent test**: `POST` returns `{id, label, mode, secret}` once; every subsequent `GET` (list) excludes `secret`.

**Acceptance Scenarios**:

1. **Given** a valid label + mode, **When** the operator creates a key, **Then** the response includes the id + the raw secret once.
2. **Given** the key was just created, **When** the operator lists keys, **Then** the secret is absent.
3. **Given** an invalid mode or missing label, **When** the operator submits, **Then** the request is rejected (400) and no key is created.

---

### User Story 3 - Revoke an API key (Priority: P2)

The operator revokes a key (confirmed client-side — irreversible); it **immediately** stops authenticating. The console operator is unaffected (they authenticate via a session, not the API key).

**Why this priority**: Lifecycle completeness; destructive, so it sits below the create/list baseline.

**Independent test**: after `DELETE`, the revoked bearer fails `ValidateAPIKey`; the key no longer appears as active.

**Acceptance Scenarios**:

1. **Given** an existing key, **When** the operator confirms revoke, **Then** the key is deleted/disabled and immediately fails auth.
2. **Given** the revoked bearer, **When** a client presents it, **Then** it is rejected.

---

### Edge Cases

- Invalid mode (not read|write|admin) → 400, no key created.
- Missing/empty label → 400.
- Revoking an unknown id → 404 (idempotent-safe: revoking an already-revoked id is 404, not an error that blocks the UI).
- The secret must not leak into: list responses, GET responses, the audit log, or any error message.
- The create dialog must not retain the secret client-side after dismissal (no localStorage, no re-show on re-render).
- Managing API keys cannot lock the console operator out — the UI authenticates via a `gorags_` session, not an API key.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `GET /api/settings/auth/api-keys` MUST return the keys' metadata (id, label, mode, created, expires) and MUST NOT include the raw secret.
- **FR-002**: `POST /api/settings/auth/api-keys` (label required; mode ∈ read|write|admin) MUST create a key and return `{id, label, mode, secret}` with the raw secret shown exactly once.
- **FR-003 (Anti)**: The raw secret MUST NEVER be persisted in plaintext, NEVER be re-displayable, and NEVER appear in list responses, GET responses, the audit log, or error messages.
- **FR-004**: `DELETE /api/settings/auth/api-keys/{id}` MUST revoke the key; revocation is immediate, irreversible, and the revoked bearer MUST fail `ValidateAPIKey`.
- **FR-005**: Invalid mode or missing label MUST be rejected with 400 (no key created).
- **FR-006**: Every route MUST be admin-gated via the spec 045 Bearer session.
- **FR-007**: Create + revoke MUST be confirmed client-side (the secret-once dismissal + the irreversible-revoke confirm — mirroring the 050/051 destructive-confirm pattern).
- **FR-008**: Managing API keys MUST NOT be able to lock the console operator out (the UI authenticates via a session, not an API key).
- **FR-009**: Scope is API keys ONLY; sessions + admin password reset are spec 058 (non-overlapping).

### Key Entities *(include if feature involves data)*

- **APIKeyView**: `{id, label, mode, created_at, expires_at}` — the metadata shape used in list/view. **Never** carries the secret. The create response is the one place the raw secret appears (transiently).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can list, create, and revoke API keys from the console without the `go-rag auth` CLI.
- **SC-002**: The raw secret appears in EXACTLY ONE place — the create response — and nowhere else (verified across list, GET, and the audit log).
- **SC-003**: A revoked key fails authentication immediately (proven via `ValidateAPIKey` on the revoked bearer).
- **SC-004**: No new on-disk layout (the auth store shipped in spec 045; this slice adds only a UI adapter over it).

## Assumptions

- Slice 2a = API keys only; sessions (list/revoke) + admin password reset are spec 058.
- Reuses the spec 045 auth store via the UI Server's existing `s.store` (`auth.CreateAPIKey`/`ListAPIKeys`/`RevokeAPIKey`) — **no engine/storage/migration change**; a 5th adapter over the auth surface, mirroring how 050/051 adapted engine writes.
- The secret-once + hash-only-storage guarantees are spec 045 invariants (`CreateAPIKey`); the UI layer preserves them (FR-003).
- **Constitution compliance**: a write surface but local-only (no egress); admin-gated (spec 045); no on-disk layout change (no migration, no `ExpectedVersion` bump). Principle V (UI adapter over existing interfaces) holds; Principle IV (async-after-ACK) is about ingest — auth writes are synchronous + small.
