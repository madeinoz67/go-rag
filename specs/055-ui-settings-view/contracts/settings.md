# Contract — Settings View (Slice 0, spec 055)

> The Settings view's single backend contract. UI-only — no other transport
> (CLI/REST/gRPC/MCP) gains a settings endpoint in Slice 0 (the `status` command
> and MCP `go_rag_status` already expose the underlying values).

## Endpoint

`GET /api/settings`

- **Auth**: Bearer session — the same guard as every `/api/*` console route
  (spec 045). Unauthenticated ⇒ `401` (proven by a `TestSettings_401Unguarded`
  mirroring `TestObservabilityMetrics_401Unguarded`).
- **Vault**: active vault derived from the `X-Go-Rag-Vault` header / default
  (spec 052); vault-sensitive values (embedding model/dim majority) reflect the
  requested vault.
- **Response**: `200 application/json` — the `SettingsDTO` from
  [data-model.md](../data-model.md).

## Response shape

Grouped read-only object: `retrieval`, `embeddings`, `cache`, `chunking`,
`redaction`, plus `vault`. The authoritative field list is
[data-model.md](../data-model.md); this contract fixes the endpoint, auth, and
vault semantics.

## Out of contract (Slice 0)

No `POST`/`PUT`/`DELETE` on `/api/settings` (read-only — FR-006).
Auth-credential routes (API keys / sessions / admin) and config-editing routes
arrive in later slices (Slice 2 and Slice 3 respectively).
