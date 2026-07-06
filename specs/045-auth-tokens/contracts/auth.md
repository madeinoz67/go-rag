# Auth Contracts

This feature exposes four surfaces. Each contract is authoritative for its surface; the implementation MUST conform.

## CLI — `go-rag auth …`

| Command | Args | Output | Auth |
|---|---|---|---|
| `auth create` | `--label <s> --mode read\|write\|admin [--expires <duration>]` | prints `gorag_<id8>.<secret>` once | local (CLI on the vault) |
| `auth list` | — | table: id, label, mode, created, last-used, enabled (never the secret) | local |
| `auth revoke` | `<id>` | status line | local |
| `auth bootstrap` | — | creates `admin` + migrates `mcp.token` | local FS |
| `auth session list` | — | active sessions: user, expires, last-seen, ip | admin |
| `auth session revoke` | `<hash>` | status line | admin |

**Exit codes**: `0` success · `1` runtime error · `2` not-found · `3` auth-required.

## HTTP — `/api/auth/*`

| Method + Path | Body | Response | Notes |
|---|---|---|---|
| `POST /api/auth/login` | `{username, password}` | `200 {token, expires_at}` / `401` | mints `gorags_…` session; bcrypt verify |
| `POST /api/auth/logout` | — | `204` | deletes the calling session |
| `GET /api/auth/session` | — | `200 [{user, expires_at, last_seen, ip}]` | admin only |
| `DELETE /api/auth/session/<hash>` | — | `204` / `404` | admin only |

**Invariants**: no `Set-Cookie` header is ever emitted on any auth path (Bearer-in-header only). Every other `/api/*` route requires a valid Bearer (API key or session). Validation runs before body parse.

## MCP — auth tools

| Tool | Input | Returns | Mode |
|---|---|---|---|
| `auth.list` | — | `[APIKey]` (no secrets) | admin |
| `auth.create` | `{label, mode, expires_at?}` | `{id}` (secret printed once by the CLI) | admin |
| `auth.revoke` | `{id}` | `{ok}` | admin |
| `auth.session.list` | — | `[Session]` | admin |
| `auth.session.revoke` | `{hash}` | `{ok}` | admin |

`auth.bootstrap` is **CLI-only** (not an MCP tool) — it requires local filesystem access to seed the first admin (the chicken-and-egg escape hatch, per research R6).

## Go interface — `internal/auth`

```go
// Principal is the authenticated caller. Carried in context.Context.
type Principal struct {
    Subject string  // APIKey.ID or AdminUser.Username
    Mode    string  // read | write | admin
    Source  string  // apikey | session | bypass
}

// Validate parses the Bearer credential on r and returns the Principal.
// It performs NO body parsing (DoS guard). On failure the caller MUST
// emit audit.AuthFailEvent(transport, detail) and return 401.
func Validate(r *http.Request) (Principal, error)

// ValidateToken is the transport-agnostic core (gRPC interceptor calls this
// with the bearer extracted from metadata).
func ValidateToken(token string) (Principal, error)
```

**Prefix dispatch**: `gorag_` → API-key validator · `gorags_` → session validator. Bearer length capped at 4096 bytes. **Bypass**: loopback peer + empty stores → `Principal{Source: "bypass"}` (non-loopback is fail-closed even when stores are empty).
