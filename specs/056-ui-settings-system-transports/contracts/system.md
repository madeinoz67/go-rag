# Contract — Settings: System & Transports (Slice 1, spec 056)

> Two new UI-only endpoints, Bearer-guarded (spec 045), vault-agnostic (system
> identity is process-wide, like 054 Observability). No other transport gains
> these surfaces in Slice 1.

## `GET /api/settings/system`

- **Auth**: Bearer session — same guard as every `/api/*` console route.
- **Egress**: NONE — fully local (pidfile + config + migrate + plumbed version).
- **Response**: `200 application/json` — the `systemStatusDTO` from
  [data-model.md](../data-model.md) (version, pid, uptime, schema, transports[],
  bind_warning).

## `POST /api/settings/updates/check`

- **Auth**: Bearer session.
- **Egress**: YES — operator-initiated only. Fetches the latest release (spec 034
  machinery). MUST NOT be called automatically on view load (Constitution I).
- **Response**: `200 application/json` — the `updateCheckDTO` from
  [data-model.md](../data-model.md). Unreachable source ⇒ `latest="unknown"`,
  `newer_available=false` (200, not an error).

## Out of contract (Slice 1)

- No mutation of config, credentials, or storage (read-only + one read-action).
- No upgrade *execution* — only the check. Applying an upgrade stays with the
  `go-rag upgrade` CLI (spec 034); a UI upgrade-action is a later slice.
- Auth-credential management (Slice 2) and config editing (Slice 3) are not in
  this contract.
