# Data Model: Document Vaults

**Phase**: 1 | **Date**: 2026-06-20

A vault is a **directory** — not a struct in a shared database. Each vault is a self-contained go-rag database instance.

## Vault Entity (logical)

| Field | Type | Notes |
|-------|------|-------|
| `Name` | string | Lowercase alphanumeric + hyphens, 1–64 chars. Unique within the vault root. |
| `Path` | string | Absolute path: `<vaultRoot>/<name>/`. |
| `Config` | config.Config | The vault's own `config.json` (embedding_model, rerank_model, chunk_size, mcp_addr, etc.). |
| `DocumentCount` | int | Derived — scanned from Pebble 0x02 prefix. |
| `StorageBytes` | int64 | Derived — directory size. |
| `DaemonRunning` | bool | Derived — pidfile + process check. |

## Physical layout (per vault)

```
<vaultRoot>/<name>/
  config.json          # standard go-rag config (embedding_model, rerank_model, ...)
  data/                # Pebble KV (isolated — no cross-vault keyspace)
  daemon.pid           # created by `start`, read by `stop`/`status`
  daemon.log           # daemon stderr
  daemon.addrs         # bound MCP address
  mcp.token            # optional bearer token for MCP auth (future RBAC)
```

## Vault root resolution

```
GO_RAG_VAULT_ROOT env var  →  ~/.go-rag/vaults/  (default)
```

## --vault flag resolution

```
--vault cyber-notes  →  dbPath = <vaultRoot>/cyber-notes/
--db-path ./my-db    →  dbPath = ./my-db/  (low-level override, no vault)
(no flags)           →  dbPath = ./.go-rag/  (backward compat)
```

Precedence: `--db-path` > `--vault` > default.

## Data invariants

- Each vault's Pebble instance is **independent** — no shared keyspace, no prefix partitioning.
- Cross-vault queries are **impossible** by construction (each command opens one vault's Pebble).
- Vault deletion removes the entire directory — no orphaned keys in a shared DB.
- The vault root (`~/.go-rag/vaults/`) contains only vault directories — no global state, no shared indexes.
