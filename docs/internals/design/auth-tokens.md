# Authentication & Tokens — Technical Design

**Status:** Draft (brainstorm-approved 2026-07-06) · **Spec:** [`specs/045-auth-tokens/spec.md`](../../specs/045-auth-tokens/spec.md)
**Reference architecture:** MuninnDB `internal/auth` (commit `e17df9…`) — mirrored structurally, diverged where go-rag's single-operator ethos and CSRF-freedom justify it.

## 1. Problem

go-rag's current auth is a single optional static shared secret:

- `internal/daemon/pid.go::ReadToken` reads one token from `<vault>/mcp.token` (`""` if absent → **auth disabled entirely**).
- Each transport duplicates its own check — `internal/rest/server.go::checkBearer`, `internal/grpc/server.go::bearerInterceptor`+`hasBearer`, `internal/mcp/http.go::checkBearer` — all doing a plain `strings.TrimSpace(...) == token` (non-constant-time, three copies).
- No issuance, no management, no login, no sessions, no scopes, no expiry, plaintext file, no audit on the bypass path.

Adequate for a CLI-only local tool; inadequate for a network-exposed operator UI (spec 046). This slice is the prerequisite — the UI cannot ship before it.

## 2. Goals

- Multiple labelled, scoped, hashed, revocable **API keys** per vault.
- Username/password **admin login** for the UI, minting short-lived **sessions**.
- Sessions carried via **Bearer header (sessionStorage), not cookies** — CSRF-free.
- One **validation middleware** used by all three transports + the future UI.
- First-run **bootstrap** (admin user + migrate `mcp.token`).
- **Loopback bypass** preserves the local "just works" UX; non-loopback always authenticated.
- Audited via the existing `internal/audit.AuthFailEvent` + new management events.

## 3. Non-goals (out of scope)

OAuth/SSO; multi-user (PRD §2.2); rate-limiting; IP allowlisting; TLS (a reverse proxy terminates it — go-rag binds plain HTTP, no `internal/tlsutil`); JWT signing (opaque store-tracked sessions instead); per-vault public/private flags (single-vault).

## 4. Model

### 4.1 API keys — programmatic clients (CLI/MCP/REST/gRPC)

- **Format:** `gorag_<base64url(32 random bytes)>`. The `gorag_` prefix disambiguates from session tokens (`gorags_`).
- **Storage:** `sha256(raw32)`; `[:16]` is the Pebble key suffix, `[:8]` (base64url) is the display ID (`gorag_<id8>`). The record (not the secret) is JSON-marshalled under the hashed key.
- **Fields:** `ID`, `Label`, `Mode` (`read`/`write`/`admin`), `CreatedAt`, `ExpiresAt`, `Enabled`.
- **Modes:** `read` (queries only), `write` (ingest + queries), `admin` (full, incl. token management). Sessions minted by admin login carry `admin`.
- **Issuance:** `go-rag auth create` — the secret is printed **once** and never persisted.

### 4.2 Admin users — UI login

- `AdminUser{Username, PassHash, CreatedAt}`. Default username **`admin`** (not MuninnDB's `root`).
- Password hashed with **bcrypt** (cost ≥ 12). Passwords are low-entropy → they need a slow KDF. (API keys do not — they are 32-byte random, so SHA-256 suffices.)
- Created at bootstrap. Password source: `GORAG_ADMIN_PASSWORD` env, or a generated random password printed once. **No hardcoded `password` default.**

### 4.3 Sessions — UI auth, Bearer not cookie

- **Login:** `POST /api/auth/login {username, password}` → bcrypt-validate → mint opaque session token → `200 {token, expires_at}`.
- **Token:** 32 random bytes, base64url, prefix `gorags_` (disambiguates from API keys). **Opaque** (not signed) — stored server-side.
- **Store:** `PrefixAuthSession` Pebble records — `token-hash → {user, expires_at, last_seen, created_ip}`. Validation = lookup + expiry check; update `last_seen`.
- **Transport:** client stores the token in browser `sessionStorage`; sends `Authorization: Bearer gorags_…`. **No `Set-Cookie` header is ever emitted.**
- **Lifecycle:** default TTL 12h (configurable). `POST /api/auth/logout` deletes the record. Admin can list/revoke active sessions.

**Why Bearer-session over MuninnDB's cookie-session.** Cookies are auto-attached by the browser on every request → CSRF-vulnerable (needs CSRF tokens). A Bearer token in `sessionStorage` is opt-in per-request → **CSRF-free**. Trade-off: XSS could read `sessionStorage` — mitigated by strict CSP, short TTL, and the operator-only surface. Net: Bearer-session is the more secure default for a SPA-over-API operator console, which is why we diverge from MuninnDB here.

### 4.4 Validation middleware

New `internal/auth` package. `Validate(r *http.Request) (Principal, error)`:

1. Parse `Authorization: Bearer <token>`. Length-cap at 4096 bytes (DoS guard, per MuninnDB).
2. Disambiguate by prefix: `gorag_` → API-key path; `gorags_` → session path.
3. Hash + Pebbble `Get` by hash. **No string compare of the secret** — a hit *is* the match (hash collision negligible). This removes MuninnDB's `ValidateStaticToken`/constant-time-compare path entirely; loopback bypass replaces their open-server mode.
4. Check `Enabled` + `ExpiresAt`; for sessions, check store presence + expiry; bump `last_seen`.
5. Return `Principal{Subject, Mode, Source}` (`source ∈ {apikey, session, bypass}`), carried in `context.Context`.
6. On any failure → `audit.AuthFailEvent(transport, detail)` + 401.

All three transports **delete** their bespoke `checkBearer`/`hasBearer`/`bearerInterceptor` and delegate to `auth.Validate`. Validation runs **before any body parse** (MuninnDB's DoS discipline).

### 4.5 Bootstrap (`go-rag init` / first `start`)

- If no admin user exists: create `admin` with password = `GORAG_ADMIN_PASSWORD`, else generate a random password and print it once (persist only the bcrypt hash).
- If `mcp.token` exists and the key store is empty: import it as a labelled `legacy-mcp` key (mode=`admin`) so existing scripts keep authenticating. The file is retained for one release (with a deprecation log), then removed.
- Result: every post-upgrade vault has an admin user + the legacy key migrated; nothing breaks on upgrade.

### 4.6 Bypass — loopback local-only

- Loopback connection **and** both stores empty (no admin, no keys) → auth bypassed, `Principal.Source=bypass`. Preserves today's "just works locally" UX for first-time users.
- Non-loopback connections **always** require a valid credential, even with empty stores (fail-closed).
- Configurable: `auth.required` (default `auto` = bypass-when-empty-on-loopback; can be set to `always`).

## 5. MuninnDB mapping

| Aspect | MuninnDB | go-rag | Divergence reason |
|---|---|---|---|
| Package | `internal/auth` | `internal/auth` | mirror |
| API-key format | `mk_<b64(32)>` | `gorag_<b64(32)>` | project rename |
| Key hashing | SHA-256[:16] | SHA-256[:16] | mirror |
| Modes | full / observe / write | read / write / admin | conventional rename |
| Admin login | username + password | same | mirror |
| Session transport | **cookie** (`muninn_session`) | **Bearer** (`gorags_…` in sessionStorage) | **diverge — CSRF-free** |
| Session integrity | signed cookie (`auth_secret`) | opaque store-tracked (no secret) | diverge — simpler + revocable |
| Per-vault `Public` flag | yes | no | single-vault, n/a |
| Bypass | per-vault Public | loopback + empty-store | diverge |
| Bootstrap admin | `root` / `password` | `admin` / env-or-generated | security hardening |
| Pebble prefixes | 0x11–0x14 | new free bytes (TBD) | go-rag prefix space differs |

## 6. Security considerations

- **CSRF:** eliminated (no cookies).
- **XSS:** session token in `sessionStorage` is JS-readable → mitigate via strict CSP, short TTL, operator-only surface.
- **Password storage:** bcrypt (cost ≥ 12); never logged.
- **API-key storage:** SHA-256 (32-byte random secrets are high-entropy; bcrypt is wasted cost here).
- **Timing:** all credentials validated by hash-keyed `Get` — no secret comparison occurs; the legacy static-compare path is gone entirely post-migration.
- **DoS:** bearer length capped (4096); validate before body parse.
- **Audit:** every auth failure + every mgmt op (create / revoke / login / logout) → audit log.

## 7. Pebble layout (bytes chosen from the free pool in `internal/storage` during T001)

- `PrefixAuthAPIKey` (0xNN) — `<hash16> → APIKey JSON`
- `PrefixAuthAdmin` (0xNN) — `<username> → AdminUser JSON`
- `PrefixAuthSession` (0xNN) — `<hash16> → Session JSON`

*(MuninnDB's vault-index prefix is dropped — go-rag is single-vault. If multi-vault ever lands, add it then.)*

> **Gotcha:** spec 019 recorded a prefix collision (`0x11` ↔ `0x16`). The exact bytes are picked in T001 against the live `internal/storage` prefix-constant block, not by guess.

## 8. CLI (`go-rag auth …`)

- `auth create --label L --mode read|write|admin [--expires D]` → prints `gorag_<id>.<secret>` once.
- `auth list` → table (id, label, mode, created, last-used, enabled).
- `auth revoke <id>` → disable + delete.
- `auth bootstrap` → run the §4.5 flow explicitly.
- `auth session list / revoke` → manage UI sessions.

## 9. Decisions (approved 2026-07-06)

1. **`mcp.token` migration** — import as `legacy-mcp` key on first post-upgrade start; deprecate the file. ✅
2. **UI auth surface** — admin-login → Bearer session (not cookie, not raw-API-key). ✅ *(revised twice during brainstorm; final = session-over-cookie for CSRF-freedom)*
3. **Bypass** — loopback + empty stores → bypass; non-loopback → fail-closed. ✅
4. **Hashing** — API keys: SHA-256 (random secrets); admin passwords: bcrypt (low-entropy). ✅
5. **Default admin username** — `admin` (not `root`). ✅
6. **Naming** — every `muninn_*` → `gorag_*` project-wide: token prefix `gorag_`, session prefix `gorags_`, env `GORAG_*`. *(The cookie `gorag_session` is now n/a — the final model uses no cookie.)* ✅

## 10. Open / future

- Exact Pebble prefix bytes — T001.
- Session TTL default (propose 12h; make configurable).
- Login rate-limiting (future hardening, out of scope here).
- If multi-vault ever lands, reintroduce the vault-index prefix + per-vault config.
