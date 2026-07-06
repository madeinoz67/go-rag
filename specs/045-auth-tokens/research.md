# Phase 0 Research: Authentication & Tokens

All Technical Context fields are resolved (no `NEEDS CLARIFICATION`). Open design questions settled below; each is a Decision / Rationale / Alternatives record.

## R1 — Free Pebble prefix bytes

- **Decision**: `PrefixAuthAPIKey`, `PrefixAuthAdmin`, `PrefixAuthSession` assigned three free bytes from `internal/storage/storage.go`, selected at T001 against the live constants.
- **Rationale**: the constitution's Storage discipline requires collision-free prefixes. The documented data range is `0x01–0x15`; spec 019 occupies `0x11` (poison quarantine) and BL-011 reserves `0x16` (webhook registry) — both avoided. `0xFF` is reserved for store metadata.
- **Alternatives**: a single auth prefix with sub-prefixing (rejected — breaks independent `PrefixScan` per PRD §6.7).

## R2 — bcrypt cost factor

- **Decision**: bcrypt cost 12 (≈250 ms verify on modern hardware).
- **Rationale**: balances security vs login latency. Login is one-time per session (not per-request), so 250 ms is acceptable and below human "lag" perception. Per-request validation uses the hash-lookup path, never bcrypt.
- **Alternatives**: cost 14 (slower, more DoS-resistant, but painful on login); argon2id (stronger KDF for passwords, available in `golang.org/x/crypto/argon2`, pure Go — acceptable, but bcrypt is simpler and sufficient for a single-operator tool).

## R3 — Session token transport (cookie vs Bearer)

- **Decision**: opaque session token stored in the browser's `sessionStorage`, sent as `Authorization: Bearer gorags_…`. **No cookies.**
- **Rationale**: cookies auto-attach on every request → CSRF-vulnerable; a Bearer token in `sessionStorage` is opt-in per-request → **CSRF-free**. XSS risk (sessionStorage is JS-readable) is mitigated by strict CSP, short session TTL, and the operator-only surface. Stephen directed this explicitly ("session model over cookie model as more secure").
- **Alternatives**: MuninnDB's signed-cookie model (rejected — CSRF surface + signing-key management); JWT (rejected — no server-side revocation without a denylist).

## R4 — Disambiguating API keys from session tokens

- **Decision**: prefix dispatch — `gorag_` → API-key validator; `gorags_` → session validator.
- **Rationale**: a single prefix check routes the request without a DB round-trip to discover the credential type.
- **Alternatives**: a unified store with a type discriminator field (rejected — extra lookup); separate auth headers (rejected — breaks the single-Bearer convention).

## R5 — gRPC integration of the unified validator

- **Decision**: replace `bearerInterceptor`/`hasBearer` with a unary (and stream) interceptor that calls `auth.Validate`, propagating the `Principal` via `context.Context` using the same context-key pattern MuninnDB uses (`auth_vault`/`auth_mode`/`auth_apikey` → here `auth_principal`).
- **Rationale**: preserves gRPC's interceptor auth model; one validator shared across REST/gRPC/MCP.
- **Alternatives**: per-method auth checks (rejected — re-fragments the validator we are consolidating).

## R6 — MCP-first exposure of `auth` commands (Principle V)

- **Decision**: expose `auth.list` and `auth.session.list` as read-only MCP tools (admin-gated); expose `auth.create` / `auth.revoke` / `auth.session.revoke` as MCP tools too (admin-gated). The bootstrap path (`go-rag init` / `auth bootstrap`) is CLI-only — it needs local filesystem access to seed the first admin and is the chicken-and-egg escape hatch.
- **Rationale**: Principle V requires every CLI operation to also be an MCP tool. The first-admin bootstrap cannot itself require MCP auth, so it stays CLI-bound; everything else is MCP-exposable.
- **Alternatives**: auth CLI-only (rejected — violates V); ungated MCP auth tools (rejected — security).

## R7 — `mcp.token` migration semantics

- **Decision**: on first post-upgrade store open, if `<vault>/mcp.token` exists and the API-key store is empty, import the file's value as an API key (`label=legacy-mcp`, `mode=admin`, `StorageHash=SHA-256(value)[:16]`). Retain the file for one release with a deprecation log; remove in the release after.
- **Rationale**: zero-break upgrade — existing scripts keep authenticating via the same SHA-256 lookup path. Skipping when the store is non-empty avoids double-import.
- **Alternatives**: delete `mcp.token` on sight (rejected — breaks existing clients); retain it as a parallel static-token path (rejected — defeats the unified validator and leaves the constant-time gap).
