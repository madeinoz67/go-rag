# Data Model — Settings: API Keys (Slice 2a, spec 057)

> No new persisted entity. The auth key record shipped in spec 045 (`APIKey` under
> `PrefixAuthAPIKey`). This slice defines the UI transfer objects + reuses the
> `auth.Store` methods.

## Source symbols (read/write reuse — no schema change)

| Source | Location | Provides |
|---|---|---|
| `auth.CreateAPIKey(s, label, mode, expiresAt)` | internal/auth/apikey.go:70 | create → `(display, APIKey, error)` |
| `auth.ListAPIKeys(s)` | internal/auth/apikey.go:162 | list → `[]APIKey` (no secret) |
| `auth.RevokeAPIKey(s, id)` | internal/auth/apikey.go:183 | disable (`Enabled=false`) |
| `s.store` (UI Server) | internal/ui/ui.go | the auth store handle |

## Transfer objects

### `apiKeyView` — list element + the metadata half of the create response

```
apiKeyView {
  id:          string     // gorag_<id8>
  label:       string
  mode:        string     // read | write | admin
  created_at:  string     // RFC3339
  expires_at:  string     // RFC3339, "" when non-expiring
  enabled:     bool       // false after revoke
}
```

### `createAPIKeyResponse` — POST response (the ONE place the secret appears)

```
createAPIKeyResponse {
  apiKeyView          // id/label/mode/created_at/expires_at/enabled
  secret:    string   // the full display string gorag_<id8>.<secret> — shown once, never again
}
```

### Request shapes

- `POST /api/settings/auth/api-keys` body: `{label: string, mode: "read"|"write"|"admin"}` (no expiry in Slice 2a).
- `DELETE /api/settings/auth/api-keys/{id}`: no body.

## Validation rules (from spec)

- `label` non-empty; `mode ∈ {read, write, admin}` — else 400, no key created (FR-005).
- `id` path segment must match a stored key — else 404 (FR-004 / ErrUnknownAPIKey).
- The raw secret (`createAPIKeyResponse.secret`) appears in the POST response ONLY — never in `apiKeyView`, never in the audit log, never in errors (FR-003).
- Revocation sets `enabled=false`; the list still returns the record (audit visibility).
