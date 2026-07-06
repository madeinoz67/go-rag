# Feature Specification: Authentication & Tokens

**Feature Branch**: `045-auth-tokens` *(single-author repo — work commits to `main`; slug identifies the spec)*
**Created**: 2026-07-06
**Status**: Draft
**Input**: go-rag's current auth is a single optional static shared secret (`mcp.token` + plain `==`, duplicated across REST/gRPC/MCP), inadequate for the network-exposed UI (spec 046). This slice is the prerequisite — the UI cannot ship before it. Technical design: [`docs/design/auth-tokens.md`](../../docs/design/auth-tokens.md) — MuninnDB-`internal/auth`-mirrored, with **Bearer sessions (not cookies)** for CSRF-freedom.

## User Scenarios & Testing

### User Story 1 — Multiple labelled API keys (Priority: P1) 🎯 MVP

An operator creates distinct API keys for distinct clients (the bridge, a script, a CI job), each labelled, scoped, and independently revocable. Today there is one shared secret in `mcp.token` — rotating it breaks every client at once.

**Why this priority**: the API-key store + CLI + validation middleware is the foundation every other story stands on.

**Independent Test**: `go-rag auth create --label bridge --mode write` prints a `gorag_…` token once; `auth list` shows it; a REST call with that Bearer succeeds; `auth revoke <id>` → the next call returns 401.

**Acceptance Scenarios**:
1. **Given** a vault, **When** `auth create --label bridge --mode write` runs, **Then** a `gorag_<id>.<secret>` is printed exactly once and only the SHA-256[:16] is persisted.
2. **Given** one or more keys exist, **When** `auth list` runs, **Then** a table shows id/label/mode/created/last-used/enabled (never the secret).
3. **Given** a key exists, **When** `auth revoke <id>` runs, **Then** subsequent Bearer-authenticated requests with that key return 401.

### User Story 2 — Unified validation across all transports (Priority: P1) 🎯 MVP

REST, gRPC, and MCP each currently duplicate a bearer check. After this spec, all three delegate to one `internal/auth.Validate`, and a key valid on one transport is valid on all three (and on the future UI).

**Independent Test**: create a key; call REST, gRPC, and MCP with it — all succeed; revoke — all three 401.

**Acceptance Scenarios**:
1. **Given** a valid key, **When** used against REST, gRPC, and MCP, **Then** all three accept it identically.
2. **Given** the codebase, **When** searched, **Then** `checkBearer`/`hasBearer`/`bearerInterceptor` are deleted and `internal/auth.Validate` is the sole auth entry point.
3. **Given** any authenticated request, **When** processed, **Then** validation completes before request body parsing.

### User Story 3 — UI login → Bearer session (no cookies) (Priority: P1) 🎯 MVP

The web UI (spec 046) needs a login. An admin logs in with username/password; the server mints a short-lived session token the SPA stores in `sessionStorage` and sends as `Authorization: Bearer gorags_…`. No cookies → no CSRF.

**Independent Test**: `POST /api/auth/login {admin, <password>}` → 200 `{token, expires_at}`; subsequent `GET /api/status` with that Bearer → 200; after `logout` or expiry → 401.

**Acceptance Scenarios**:
1. **Given** the admin user exists, **When** `POST /api/auth/login` is called with correct credentials, **Then** 200 returns an opaque `gorags_…` token + expiry.
2. **Given** a live session token, **When** `/api/*` is called with it as Bearer, **Then** the request is authorized as `admin`.
3. **Given** any auth response, **When** inspected, **Then** no `Set-Cookie` header is present.
4. **Given** a bad login, **When** processed, **Then** 401 + `AuthFailEvent('ui', …)` is audited.

### User Story 4 — `mcp.token` migration (Priority: P2)

An existing vault has `mcp.token` (the legacy shared secret). On first post-upgrade start it is imported as a labelled `legacy-mcp` API key (mode=`admin`) so existing scripts keep authenticating without reconfiguration.

**Independent Test**: a vault with `mcp.token`; start the daemon; `auth list` shows the `legacy-mcp` key; a request using the old token value authenticates; the file is flagged deprecated.

**Acceptance Scenarios**:
1. **Given** a vault with `mcp.token` and an empty key store, **When** the daemon starts, **Then** the token is imported as an API key (`label=legacy-mcp`, `mode=admin`).
2. **Given** the migrated key, **When** the original token value is presented as Bearer, **Then** it authenticates via the SHA-256 lookup path.
3. **Given** migration ran, **When** logs are inspected, **Then** a deprecation notice names the file and the removal release.

### User Story 5 — Loopback bypass preserves local UX (Priority: P2)

A local operator on their own machine with an empty token store gets auth-free access (as today). A non-loopback connection to the same instance is fail-closed.

**Independent Test**: fresh vault, no keys, loopback REST call → 200; the same instance reached via LAN IP → 401.

**Acceptance Scenarios**:
1. **Given** empty auth stores and a loopback connection, **When** any request arrives, **Then** it passes with `Principal.Source=bypass`.
2. **Given** a non-loopback connection, **When** any request arrives without a valid credential, **Then** 401 (fail-closed), even if stores are empty.

### User Story 6 — Admin bootstrap (Priority: P1) 🎯 MVP

First-run `go-rag init` creates the `admin` user. The password comes from `GORAG_ADMIN_PASSWORD` or is generated + printed once. No insecure hardcoded default ships.

**Independent Test**: `GORAG_ADMIN_PASSWORD=secret go-rag init` → admin login with `admin`/`secret` succeeds; `go-rag init` (no env) → a generated password is printed; login with it succeeds.

**Acceptance Scenarios**:
1. **Given** a fresh vault, **When** `go-rag init` runs, **Then** an `admin` user is created and no default password (`password`/`root`) is used.
2. **Given** `GORAG_ADMIN_PASSWORD` is set, **When** bootstrap runs, **Then** that password is applied (and can rotate an existing admin).

### Edge Cases

- Concurrent `auth create` — 32-byte random; PK conflict on `hash16` is astronomically unlikely → retry on collision.
- Expired session token presented — 401, `AuthFailEvent`.
- A `gorag_` API key sent to `/api/auth/login` — rejected (login is username/password, not API key).
- Migration when `mcp.token` exists **and** keys already exist — skip migration (store already authoritative), log.
- Bearer prefix spoofing (`gorag_` + garbage) — hash lookup misses → 401, no information leak (same response as absent).
- Two admin sessions from different browsers — both valid until logout/expiry; independent revocation.

## Requirements

### Functional Requirements

- **FR-001**: API keys are `gorag_<base64url(32 random bytes)>`; the raw secret is never persisted (SHA-256[:16] only).
- **FR-002**: `go-rag auth create/list/revoke` manage keys; `create` prints the secret exactly once.
- **FR-003**: Keys carry a mode ∈ {`read`, `write`, `admin`}; modes are enforced at the engine/handler layer.
- **FR-004**: A single `internal/auth.Validate` is the sole auth entry point; transport-local bearer functions (`checkBearer`/`hasBearer`/`bearerInterceptor`) are removed.
- **FR-005**: All three current transports (REST, gRPC, MCP) delegate to `Validate`; the future UI (spec 046) does too.
- **FR-006**: Validation occurs before request body parsing; bearer length is capped (4096 bytes).
- **FR-007**: Admin users are stored with bcrypt-hashed passwords; the default username is `admin`.
- **FR-008**: `POST /api/auth/login` validates credentials and returns an opaque `gorags_…` session token; `POST /api/auth/logout` invalidates it.
- **FR-009**: Sessions are server-side (Pebble session store), short-lived (default 12h, configurable), and revocable.
- **FR-010**: No `Set-Cookie` header is emitted on any auth path (Bearer-in-sessionStorage only).
- **FR-011**: First-run bootstrap creates the admin user (password from `GORAG_ADMIN_PASSWORD` or generated) and migrates `mcp.token` → `legacy-mcp` key.
- **FR-012**: Loopback + empty stores → bypass; non-loopback → fail-closed.
- **FR-013**: Every auth failure and every management operation emits an audit event (extending `AuthFailEvent`).

### Key Entities

- **APIKey** — `{ID, Label, Mode, CreatedAt, ExpiresAt, Enabled, StorageHash}`. Pebble: `PrefixAuthAPIKey`.
- **AdminUser** — `{Username, PassHash(bcrypt), CreatedAt}`. Pebble: `PrefixAuthAdmin`.
- **Session** — `{TokenHash, User, ExpiresAt, LastSeen, CreatedIP}`. Pebble: `PrefixAuthSession`.
- **Principal** — `{Subject, Mode, Source}` returned by `Validate`; carried in `context.Context`.

## Success Criteria

- **SC-001**: `go-rag auth create` produces a key that authenticates REST, gRPC, and MCP identically; `revoke` invalidates all three.
- **SC-002**: UI login returns a `gorags_…` session that authenticates `/api/*` for the configured TTL; after logout or expiry → 401.
- **SC-003**: No `Set-Cookie` header appears on any auth response (asserted in handler tests).
- **SC-004**: A pre-upgrade `mcp.token` authenticates post-migration as the `legacy-mcp` key.
- **SC-005**: Loopback + empty stores → 200; LAN IP → 401.
- **SC-006**: `go test -race` clean on the auth package; no secret-string-comparison path exists post-migration.
- **SC-007**: `make lint` clean (golangci-lint — the `ci.yml` gate).

## Assumptions

- Single-vault, single-operator (PRD §2.2 — no multi-user). The MuninnDB vault-index prefix is dropped.
- A reverse proxy terminates TLS; go-rag binds plain HTTP — no `internal/tlsutil`.
- Sessions are opaque + store-tracked (not JWT) — gives server-side revocation without a signing key.
- API keys use SHA-256 (not bcrypt) because 32-byte random secrets are high-entropy; bcrypt is reserved for the low-entropy admin password.
- Pebble prefix bytes are chosen from the free pool in `internal/storage` during T001 (spec 019's collision note is the gotcha — pick against the live constants, not by guess).
- Out of scope: OAuth/SSO, rate-limiting, IP allowlisting, per-vault public flags, multi-user, JWT.
