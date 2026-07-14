# Data Model — Vaults Management View

**Feature**: specs/051-ui-vaults-view | **Date**: 2026-07-14

## Route table (new — UI transport)

| Method | Path | Auth | Body/Query | Returns | Maps to |
|--------|------|------|------------|---------|---------|
| GET | `/api/vaults` | `Server.guard` | — | `vaultsListDTO` | `Engine.ListVaults` |
| POST | `/api/vaults` | `Server.guard` | `{name}` | `201` + `vaultDTO` | `Engine.CreateVault` |
| POST | `/api/vaults/{name}/rename` | `Server.guard` | `{new_name}` | `200` + `vaultDTO` | `Engine.RenameVault` |
| POST | `/api/vaults/{name}/clear` | `Server.guard` | — | `204` | `Engine.ClearVault` |
| DELETE | `/api/vaults/{name}` | `Server.guard` | — | `204` | `Engine.DeleteVault` |

All guarded (spec 045). **Switch carries no route** — it is a client-side state change (the shell
sets the `X-Go-Rag-Vault` header); the only server confirmation is that subsequent reads target the
new vault. Clear/delete are confirmed client-side (R4 / the quarantine confirm-dialog pattern).

**Route precedence** (Go 1.22 mux): `GET /api/vaults` (literal) vs `POST /api/vaults/{name}/rename`
(4 segments) vs `DELETE /api/vaults/{name}` (3 segments) — distinct segment counts + methods, no
conflict.

## DTOs

**vaultsListDTO** (GET /api/vaults):
```json
{
  "vaults": [
    { "name": "default", "documents": 42, "active": true },
    { "name": "archive", "documents": 0, "active": false }
  ],
  "active": "default"
}
```

**vaultDTO** (POST create / rename response):
```json
{ "name": "archive", "documents": 0, "active": false }
```

**createRequest / renameRequest** (request bodies):
```json
{ "name": "archive" }
{ "new_name": "drafts" }
```

The `active` flag is computed server-side (`active == the request's vault` — i.e. the vault the
caller's `X-Go-Rag-Vault` header names, read via `vaultFromRequest`). This lets the UI mark the
current vault without client guessing.

## Engine surface (after the two Phase-0 fixes)

```go
// FIXED (config.go) — now reads the in-db registry, not filesystem dirs:
func (e *Engine) ListVaults(_ string) ([]VaultEntry, error)
//   VaultEntry{ Name string, Documents int }  (unchanged shape)

// ADDED (vault_lifecycle.go):
func (e *Engine) CreateVault(ctx context.Context, name string) error
//   validates (vaultpkg.ValidateName), refuses if exists, registers via WriteVaultName.

// EXISTING (vault_lifecycle.go) — already on the in-db registry:
func (e *Engine) RenameVault(ctx context.Context, oldName, newName string) error
func (e *Engine) ClearVault(ctx context.Context, vault string) error
func (e *Engine) DeleteVault(ctx context.Context, vault string) error
//   + NEW default-vault guard: DeleteVault("default") → ErrInvalid.
```

## Error mapping (UI → HTTP, via the existing `writeEngineErr`)

- `ErrInvalid` (bad name, duplicate, rename collision, default-delete) → **400** "invalid".
- Unknown vault (rename/clear/delete a name not registered) → engine returns a not-found error →
  **404** "not found" (the lifecycle methods wrap storage errors; `writeEngineErr` maps the
  `ErrNotFound` sentinel).
- Unauthorized → **401** (the guard).

## State (client, app.js)

- `vaults: []` (populated from `GET /api/vaults`; replaces the hardcoded `['default']`).
- `vault` (the active vault; the header value) — unchanged; `switchVault(v)` already persists it.
- `vaultsLoading`, `vaultsError`, plus a create dialog + the reused `confirmDialog` for
  rename/clear/delete.
