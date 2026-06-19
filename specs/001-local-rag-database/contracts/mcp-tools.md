# MCP Tool Contracts: go-rag v1

**Phase**: 1 (Design) | **Date**: 2026-06-19 | **Source**: PRD G7; research Q14

go-rag exposes its operations as Model Context Protocol tools so AI coding agents
(Claude, Cursor, Copilot) can query the database directly. The v1 set is minimal —
six tools mirroring the CLI (research decision Q14), not a 35-tool surface.

**Transport**: stdio MCP server (JSON-RPC). **Auth**: none (local, single-user —
PRD non-goal N2). **Errors**: JSON-RPC error objects with a message and code.

## Tools

### `go_rag_query`
Semantic/keyword search.
- **Input**: `{ query: string, k?: int=5, mode?: "hybrid"|"semantic"|"keyword"="hybrid", source_glob?: string, threshold?: float=0.0 }`
- **Output**: `{ results: [{ chunk_text, source_path, page_number, score }] }`
- **Errors**: database-not-initialized; embedding-service-unreachable.

### `go_rag_add`
Ingest a path.
- **Input**: `{ path: string, recursive?: bool=true, glob?: string, dry_run?: bool=false }`
- **Output**: `{ new: int, skipped: int, errors: int, embedding_queued: int }`
- **Note**: returns immediately after the <10ms write ACK; embedding continues async.

### `go_rag_status`
Database health + counts.
- **Input**: `{}`
- **Output**: `{ sources, documents, chunks, embedded_pct, storage_bytes, embedding_model, dimensions, health: "ok"|"degraded", last_ingested_at, last_queried_at }`

### `go_rag_init`
Initialize a database.
- **Input**: `{ ollama_url?: string, model?: string, watch_dir?: string, chunk_size?: int, chunk_overlap?: int }`
- **Output**: `{ db_path, model, initialized: true }`

### `go_rag_scan`
Scan once (non-blocking variant of the CLI watch).
- **Input**: `{ poll_interval?: int=60 }`
- **Output**: `{ added: [...paths], modified: [...paths], deleted: [...paths] }`

### `go_rag_config`
Get/set configuration.
- **Input (get)**: `{ action: "get", key?: string }` → `{ values: {...} }`
- **Input (set)**: `{ action: "set", key: string, value: string }` → `{ ok: true }`
- **Errors**: invalid-value (rejected, previous retained).

## Design Notes

- **Parity**: each tool's semantics match its CLI counterpart (see
  [cli-commands.md](./cli-commands.md)); one implementation, two surfaces.
- **Streaming**: `go_rag_query` MAY stream ranked results as they are scored (PRD US4);
  clients that don't consume the stream receive the full ranked batch.
- **Read-only agent safety**: tools that mutate (`add`, `init`, `config set`,
  `scan`) are clearly distinct from read-only tools (`query`, `status`) so agents can
  be scoped to read-only access if desired.
