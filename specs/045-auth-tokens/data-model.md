# Data Model: Authentication & Tokens

## Entities

### APIKey

| Field | Type | Notes |
|---|---|---|
| `ID` | `string` | `base64url(sha256(secret)[:8])`; displayed as `gorag_<id8>` |
| `Label` | `string` | operator-supplied free text |
| `Mode` | `string` | `read` / `write` / `admin` |
| `CreatedAt` | `time.Time` | |
| `ExpiresAt` | `*time.Time` | `nil` = never expires |
| `Enabled` | `bool` | set `false` on revoke |
| `StorageHash` | `[]byte` | `sha256(secret)[:16]` — the Pebble key |

**Pebble key**: `PrefixAuthAPIKey(0xNN) || StorageHash(16B)` → `APIKey JSON`. **The raw secret is never persisted** (shown once at `create`).

**Validation**: presented bearer `gorag_<b64(32B)>` → `sha256` → `Get(StorageHash)` → if present and `Enabled` and not expired → valid.

**Lifecycle**: `absent → enabled → revoked` (or `→ expired`, lazily swept).

### AdminUser

| Field | Type | Notes |
|---|---|---|
| `Username` | `string` | default `admin` |
| `PassHash` | `[]byte` | bcrypt(cost 12) |
| `CreatedAt` | `time.Time` | |

**Pebble key**: `PrefixAuthAdmin(0xNN) || Username` → `AdminUser JSON`.

**Lifecycle**: `absent → created` (single admin in v1). Password rotatable via `GORAG_ADMIN_PASSWORD` env on bootstrap, or a future CLI command.

### Session

| Field | Type | Notes |
|---|---|---|
| `TokenHash` | `[]byte` | `sha256(opaqueToken)[:16]` — the Pebble key |
| `User` | `string` | the admin username |
| `ExpiresAt` | `time.Time` | `now + TTL` (default 12h, configurable) |
| `LastSeen` | `time.Time` | bumped on each validated request |
| `CreatedIP` | `string` | connecting peer (audit trail) |

**Pebble key**: `PrefixAuthSession(0xNN) || TokenHash(16B)` → `Session JSON`. The opaque token `gorags_<base64url(32B)>` is returned to the client exactly once at login.

**Lifecycle**: `absent → live → revoked` (logout/admin-revoke) or `→ expired` (TTL passed, lazily swept).

### Principal (in-memory only — not persisted)

```go
type Principal struct {
    Subject string  // APIKey.ID or AdminUser.Username
    Mode    string  // read | write | admin
    Source  string  // apikey | session | bypass
}
```

Carried in `context.Context` by `auth.Validate`; consumed by handlers/middleware to enforce mode.

## State Transitions

```
APIKey:    absent --create--> enabled --revoke--> revoked
                              enabled --expiry--> expired
Session:   absent --login--> live --logout/revoke--> revoked
                          live --TTL--> expired
AdminUser: absent --bootstrap--> created --env/CLI--> password-rotated
```

## Key-space layout (migration v3)

Three new single-byte prefixes in `internal/storage/storage.go`, registered by an idempotent migration:

- `PrefixAuthAPIKey` (0xNN)
- `PrefixAuthAdmin` (0xNN)
- `PrefixAuthSession` (0xNN)

`v3RegisterAuthPrefixes.Up` is a data no-op (new prefixes hold only new records) but bumps the schema version and reserves the bytes — required by the constitution's Storage discipline. `migrate.ExpectedVersion` goes 2 → 3; PRD §6.7 gains three rows.
