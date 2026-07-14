# Contract: UI Vaults Transport

**Feature**: specs/051-ui-vaults-view | **Date**: 2026-07-14

## `GET /api/vaults`
Lists every vault the daemon serves (the in-db registry). **200**: `{vaults: [...], active: <name>}`,
each vault `{name, documents, active}`. **401**: unauthorized. The `active` vault is the one named by
the caller's `X-Go-Rag-Vault` header (or `?vault=`).

## `POST /api/vaults`
Creates a new empty vault (registers it in the unified store). Body `{name}`. **201**: the new
`{name, documents:0, active:false}`. **400**: invalid name (bad characters / length / duplicate /
reserved). **401**: unauthorized. (Client-side: confirmation dialog before sending.)

## `POST /api/vaults/{name}/rename`
Renames a vault (metadata-only; data identity preserved). Body `{new_name}`. **200**: the renamed
`{name: new_name, documents, active}`. **400**: invalid new_name or new_name already exists.
**404**: `{name}` not registered. **401**: unauthorized.

## `POST /api/vaults/{name}/clear`
Empties a vault's contents (documents/chunks/embeddings); the vault stays registered + writable.
**204**: cleared. **404**: not registered. **401**: unauthorized. (Confirmed client-side.)

## `DELETE /api/vaults/{name}`
Deletes a vault entirely (clear + unregister). **204**: deleted. **400**: `{name}` is `default`
(the default vault is always present). **404**: not registered. **401**: unauthorized. (Confirmed
client-side.)

## Switching (no route)
Switching the active vault is a client-side state change: the shell sets its `vault` (sent as the
`X-Go-Rag-Vault` header on every `/api/*` call) + persists it. The server confirms a switch only
implicitly — subsequent reads target the new vault. If the active vault was deleted from another
session, the next fetch 404s and the shell falls back to `default` + a non-blocking notice.

## Non-goals (this slice)
No clone/import/export (CLI has them), no per-vault model/storage-size columns (name + count + active
now; enrichment can follow), no REST/MCP/gRPC parity for the lifecycle ops (pre-existing gap, noted
in the plan's constitution check).
