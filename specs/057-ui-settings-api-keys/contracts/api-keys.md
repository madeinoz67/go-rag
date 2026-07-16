# Contract — Settings: API Keys (Slice 2a, spec 057)

> Three UI-only routes, Bearer-guarded (spec 045), vault-agnostic (API keys are
> process-wide). All admin-only. No other transport gains these in Slice 2a.

## `GET /api/settings/auth/api-keys`

- **Auth**: Bearer session (admin).
- **Response**: `200 application/json` → `[]apiKeyView` (id/label/mode/created_at/
  expires_at/enabled). **Never** a `secret` field. Empty list ⇒ `[]`.

## `POST /api/settings/auth/api-keys`

- **Auth**: Bearer session (admin).
- **Body**: `{label: string, mode: "read"|"write"|"admin"}`.
- **Response (201)**: `createAPIKeyResponse` = `apiKeyView` + `{secret}` — the full
  `gorag_<id8>.<secret>` display string. This is the **only** response that ever
  carries the secret; the operator must copy it now.
- **Errors**: `400` on missing label / invalid mode (no key created).

## `DELETE /api/settings/auth/api-keys/{id}`

- **Auth**: Bearer session (admin).
- **Response**: `204` on success (the key is disabled — `enabled=false` — and
  immediately fails `ValidateAPIKey`).
- **Errors**: `404` if the id is unknown (`ErrUnknownAPIKey`).

## Security invariants (binding)

- The raw secret appears in **exactly one** place: the `POST` response body. It is
  never in `GET`, never in the audit log, never in an error.
- All three routes require an admin Bearer session (the loopback bypass does NOT
  apply — an initialized vault has an admin).
- Revocation is irreversible from the UI (no re-enable route in Slice 2a).

## Out of contract (Slice 2a)

- No session management, no admin password reset (those are spec 058).
- No expiry-on-create (keys created via the UI are non-expiring; the CLI `--expires`
  remains the way to set expiry). `expires_at` is still returned in the list.
