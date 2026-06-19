# CLI Command Contracts: go-rag v1

**Phase**: 1 (Design) | **Date**: 2026-06-19 | **Source**: PRD §5

go-rag exposes six commands via cobra. Global flags apply to all commands. Every
command also exists as an MCP tool (see [mcp-tools.md](./mcp-tools.md)).

**Global flags**:
- `--db-path <path>` (default `./.go-rag`) — database directory
- `-v, --verbose` — verbose logging
- `--help` / `--version`

**Exit codes**: `0` success; `1` runtime error; `2` usage error.

**Output**: every command supports `--format text` (default, human-readable) or
`--format json` (machine-readable) where noted.

## `go-rag init`

Initialize a new RAG database in the current directory.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--db-path` | string | `./.go-rag` | |
| `--ollama-url` | string | `http://localhost:11434` | |
| `--model` | string | (auto-detect) | prompts user to pick if omitted |
| `--watch-dir` | string | `.` | |
| `--chunk-size` | int | 512 | tokens |
| `--chunk-overlap` | int | 50 | tokens |

**Behavior**: creates `.go-rag/` (+ `config.json`, `data/`), probes Ollama for
embedding-capable models, initializes Pebble + key prefixes, prints next steps.
**Exit 1** if Ollama unreachable and no model given.

## `go-rag add [path]`

Add files or directories.

| Flag | Type | Default |
|------|------|---------|
| `--recursive` | bool | true |
| `--glob` | string | (none) | e.g. `*.pdf` |
| `--dry-run` | bool | false |

**Args**: exactly one path. **Output**: per-file status
(`NEW`/`SKIPPED`/`ERROR` with chunk counts), then a summary line and an async
embedding notice. Write ACKs in <10ms; embedding/indexing is background.

## `go-rag scan`

Scan watched directories for changes.

| Flag | Type | Default |
|------|------|---------|
| `--watch` | bool | false | run continuously |
| `--poll-interval` | int | 60 (seconds) |
| `--once` | bool | true | scan once and exit |

**Output (watch)**: timestamped `[ADDED]` / `[MODIFIED]` / `[DELETED]` lines.
Graceful shutdown on SIGINT/SIGTERM.

## `go-rag query [query]`

Search the database.

| Flag | Type | Default |
|------|------|---------|
| `--k` | int | 5 | number of results |
| `--mode` | enum | `hybrid` | `hybrid` \| `semantic` \| `keyword` |
| `-f, --format` | enum | `text` | `text` \| `json` |
| `--source` | string | (none) | filter by source file glob |
| `--threshold` | float | 0.0 | minimum relevance score |

**Args**: ≥1 (the query string). **Output**: ranked results — each with chunk text,
source file path, page number (PDFs), relevance score. Hybrid fuses vector + BM25
via Reciprocal Rank Fusion.

## `go-rag status`

Database statistics and health.

| Flag | Type | Default |
|------|------|---------|
| `--json` | bool | false | JSON output |

**Output**: counts (sources, files, chunks, embedded %), storage size, Pebble health,
embedding model + dimensions + provider, last ingested/queried timestamps, health
indicator (`OK` / `degraded`).

## `go-rag config`

View or change configuration. Subcommands: `get [key]`, `set [key] [value]`.

**Configurable keys**: `ollama_url`, `ollama_model`, `watch_dirs`, `chunk_size`,
`chunk_overlap`, `db_path`, `file_glob`, `poll_interval_secs`. Invalid values (malformed
URL, non-positive int) are rejected with a clear error; the previous value is retained.
