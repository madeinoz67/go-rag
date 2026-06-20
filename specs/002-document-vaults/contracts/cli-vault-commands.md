# CLI Vault Commands

**Phase**: 1 | **Date**: 2026-06-20

## `go-rag vault create <name>`

Create a new vault with default config.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--embedding_model` | string | `nomic-embed-text` | Embedding model for this vault |
| `--ollama-url` | string | `http://localhost:11434` | Ollama server URL |
| `--mcp-addr` | string | `:7878` | MCP listen address for this vault's daemon |

Creates `<vaultRoot>/<name>/config.json` + `data/`. Errors if the vault already exists.

## `go-rag vault list`

List all vaults with summary info.

```
VAULT         DOCS   MODEL                DAEMON     STORAGE
default       2457   mxbai-embed-large    stopped    24.6 MiB
cyber-notes   180    nomic-embed-text     running    3.2 MiB
personal      0      nomic-embed-text     stopped    1.0 KiB
```

`--json` for machine-readable output.

## `go-rag vault delete <name>`

Remove a vault and all its data. Requires `--force` to skip confirmation (or pipe `yes`).

## `go-rag vault clear <name>`

Remove all data (Pebble contents) but preserve the vault's `config.json`. The vault becomes empty but retains its model/settings.

## `go-rag --vault <name> <command>`

The `--vault` global flag selects the active vault for any command:

```bash
go-rag --vault cyber-notes add ./docs/
go-rag --vault cyber-notes query "threat model"
go-rag --vault cyber-notes start
go-rag --vault cyber-notes status
go-rag --vault cyber-notes reprocess .
```

Resolves `dbPath` to `<vaultRoot>/<name>/`. Can be combined with `--db-path` (db-path wins as low-level override).
