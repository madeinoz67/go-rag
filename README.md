# go-rag

> A single-binary local RAG (Retrieval-Augmented Generation) database for your
> documents — ingest, index, and query PDFs, Word files, Markdown, and images on
> your filesystem with zero external dependencies beyond a local Ollama instance.

**Status:** alpha — v1 implemented and working end-to-end. Multi-vault support,
cross-encoder reranking, muninn-style MCP daemon, and Obsidian-aware ingestion.
Full spec: [`PRD_RAG_Database.md`](./PRD_RAG_Database.md).

## Why

A local RAG database should be as frictionless as `git init; git add; git commit` —
no Docker, no API keys, no cloud services. Install the binary, run `go-rag init`,
and you have a working RAG system.

## Requirements

- **Go** 1.22+ (build from source)
- **Ollama** with an embedding model (`ollama pull mxbai-embed-large`)

## Quickstart

```bash
ollama pull mxbai-embed-large        # one-time: fetch an embedding model
make build                           # build the static binary into ./bin
./bin/go-rag init                    # create ./.go-rag/ (config + data)
./bin/go-rag add ./my-docs/          # ingest a folder (PDF/Word/Markdown/text)
./bin/go-rag query "how does X work?"  # hybrid search, source-cited results
./bin/go-rag                         # dashboard (daemon + DB status at a glance)
```

## Commands

**Ingest & query**
| Command | Description |
|---------|-------------|
| `go-rag init` | Initialize a new RAG database |
| `go-rag add <path>` | Add files or directories (idempotent; progress bar) |
| `go-rag scan [--watch]` | Scan for changes (2-layer: fsnotify + polling) |
| `go-rag query "<q>"` | Hybrid semantic + keyword search (optional `--no-rerank`) |
| `go-rag status` | Daemon + database statistics and health |
| `go-rag config [get\|set]` | View or change configuration |

**Inspect & maintain**
| Command | Description |
|---------|-------------|
| `go-rag files [--json]` | List ingested file paths |
| `go-rag dirs [--json]` | Per-directory file + chunk counts |
| `go-rag reprocess <path>` | Force re-ingest a directory (applies current reader/embedder; bypasses dedup) |
| `go-rag migrate` | Re-embed all documents to the current model |

**Vaults**
| Command | Description |
|---------|-------------|
| `go-rag vault create <name>` | Create a new isolated vault |
| `go-rag vault list` | List all vaults with doc counts, model, daemon status |
| `go-rag vault delete <name>` | Remove a vault (`--force` to skip confirm) |
| `go-rag vault clear <name>` | Clear data, preserve config |
| `go-rag vault clone <src> <dst>` | Clone a vault |
| `go-rag vault export <name>` | Export a vault as a tar archive (`--output file.tar`) |
| `go-rag vault import <name> --from <path>` | Import an existing database as a vault (no re-ingestion) |
| `go-rag --vault <name> <command>` | Run any command against a specific vault |

**Daemon (MCP-over-HTTP, muninn-style)**
| Command | Description |
|---------|-------------|
| `go-rag start` | Start the MCP daemon in the background (`:7878`) |
| `go-rag stop` | Stop the running daemon |
| `go-rag mcp` | stdio→HTTP proxy (bridges Claude Desktop to the daemon) |

## Document Vaults

Vaults are **isolated document corpora** — each vault is a separate Pebble database
with its own config, embedding model, and indexes. No cross-vault contamination.

```bash
# Create vaults with different embedding models
go-rag vault create cyber-notes --embedding_model mxbai-embed-large
go-rag vault create personal --embedding_model nomic-embed-text

# Import an existing database without re-ingesting
go-rag vault import obsidian --from ~/Documents/ObsidianVault/.go-rag

# Add docs to each vault independently
go-rag --vault cyber-notes add ~/Documents/ObsidianVault/
go-rag --vault personal add ~/Documents/Personal/

# Query each vault (results are isolated)
go-rag --vault cyber-notes query "honeypot deception"

# Start per-vault daemons (each vault can have its own mcp_addr)
go-rag --vault cyber-notes start

# List all vaults
go-rag vault list
```

Vault root: `~/.go-rag/vaults/` (override via `GO_RAG_VAULT_ROOT`). Vault names:
lowercase alphanumeric + hyphens, 1–64 chars.

## Cross-encoder reranking

After RRF retrieval returns top-20 candidates, an optional Ollama LLM scores each
query–chunk pair directly, cutting semantic noise (e.g., unrelated chunks with low
vector similarity). Enabled via config:

```bash
go-rag config set rerank_model phi3:latest   # or llama3.1, mistral, etc.
go-rag query "honeypot deception"            # reranked: noise scored 0.000
go-rag query "honeypot deception" --no-rerank  # skip reranking (faster)
```

## MCP daemon

`start` re-execs a detached daemon that owns the Pebble database and serves MCP
over HTTP (`:7878`). 10 MCP tools: `go_rag_query`, `go_rag_status`, `go_rag_add`,
`go_rag_init`, `go_rag_scan`, `go_rag_config`, `go_rag_files`, `go_rag_dirs`,
`go_rag_reprocess`, `go_rag_migrate`.

Wire into Claude Desktop:
```json
{"mcpServers": {
  "go-rag": {
    "command": "/abs/path/to/go-rag",
    "args": ["mcp", "--vault", "cyber-notes"]
  }
}}
```

## Obsidian support

The Markdown reader normalizes Obsidian syntax at ingest:
- `![[file.ext]]` image/file embeds → filename kept as a searchable token.
- `[[wikilinks]]` → display text (alias and heading aware).
- `![[Note]]` transclusions → target recorded in `metadata["transcludes"]`.

After a reader change: `go-rag reprocess <vault>` to re-apply it.

## Architecture

Layered: **CLI** → **ingest pipeline** (async-after-ACK, <10ms writes) →
**(readers / embedder / change-detection)** → **(BM25 FTS + vector + RRF retrieval)**
→ **embedded Pebble KV**. Optional cross-encoder reranking via Ollama LLM.
Full design: [`PRD_RAG_Database.md`](./PRD_RAG_Database.md).

## Project structure

```
cmd/go-rag/      # single binary entrypoint
internal/
  cli/           # commands (init/add/scan/query/status/config/files/dirs/reprocess/migrate/
                 #   start/stop/vault/mcp + dashboard)
  model/         # Source → Document → Chunk → Embedding
  reader/        # FileReader interface (text/markdown/docx/pdf/image) + Obsidian normalization
  embed/         # Embedder interface (Ollama /api/embed)
  rerank/        # Cross-encoder reranker (Ollama LLM scoring)
  storage/       # embedded Pebble KV + key-space prefixes
  index/         # BM25 FTS + vector + RRF retrieval + SearchWithRerank
  pipeline/      # ingest pipeline + async workers + reprocess/migrate
  watcher/       # 2-layer change detection (fsnotify + polling)
  chunk/         # text splitter
  config/        # persisted configuration
  vault/         # vault registry (create/list/delete/clear/ensure-default)
  daemon/        # background daemon lifecycle (start/stop/status + pidfile + HTTP client)
  mcp/           # MCP server (stdio + HTTP) + 10 tool dispatch
docs/            # mkdocs documentation
```

## Development

```bash
make test     # go test -race -cover ./...
make vet      # go vet ./...
make lint     # golangci-lint run
make vuln     # govulncheck ./...
make build    # CGO_ENABLED=0 static binary → ./bin/go-rag
make help     # list all targets
```

## License

TBD (likely MIT — the dependency stack is permissively licensed, PRD §9.3).
