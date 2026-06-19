# go-rag

> A single-binary local RAG (Retrieval-Augmented Generation) database for your
> documents — ingest, index, and query PDFs, Word files, Markdown, and images on
> your filesystem with zero external dependencies beyond a local Ollama instance.

**Status:** alpha — v1 implemented and working end-to-end. `init` → `add` → `query`
runs against text, Markdown, Word, and PDF (images are metadata-only; OCR is a
later version). Runs as a background **MCP daemon** (muninn-style) so AI agents can
query it directly. Full spec: [`PRD_RAG_Database.md`](./PRD_RAG_Database.md).

## Why

A local RAG database should be as frictionless as `git init; git add; git commit` —
no Docker, no API keys, no cloud services (PRD §1). Install the binary, run
`go-rag init`, and you have a working RAG system.

## Requirements

- **Go** 1.22+ (build from source)
- **Ollama** (runtime only — serves the embedding model via `/api/embed`)

## Quickstart

```bash
ollama pull nomic-embed-text            # one-time: fetch an embedding model
make build                              # build the static binary into ./bin
./bin/go-rag init                       # create ./.go-rag/ (config + data)
./bin/go-rag add ./my-docs/             # ingest a folder (PDF/Word/Markdown/text)
./bin/go-rag query "how does X work?"   # hybrid search, source-cited results
./bin/go-rag status                     # counts, storage, embedding health
./bin/go-rag files                      # list ingested files (dirs: per-directory)
```

## Commands

**Ingest & query**
| Command | Description |
|---------|-------------|
| `go-rag init` | Initialize a new RAG database (PRD §5.2) |
| `go-rag add <path>` | Add files or directories (idempotent; PRD §5.3) |
| `go-rag scan [--watch]` | Scan for changes (PRD §5.4) |
| `go-rag query "<q>"` | Hybrid semantic + keyword search (PRD §5.5) |
| `go-rag status` | Daemon + database statistics and health |
| `go-rag config [get\|set]` | View or change configuration (PRD §5.7) |

**Inspect & maintain**
| Command | Description |
|---------|-------------|
| `go-rag files [--json]` | List ingested file paths (type/status/chunks) |
| `go-rag dirs [--json]` | Per-directory file + chunk counts |
| `go-rag reprocess <path>` | Force re-ingest a directory — bypasses dedup so the current reader/embedder applies (use after a reader change, without wiping the DB) |
| `go-rag migrate` | Re-embed all documents to the current model (use after changing `ollama_model`) |

**Daemon (MCP-over-HTTP, muninn-style)**
| Command | Description |
|---------|-------------|
| `go-rag start` | Start the MCP daemon in the background (owns the DB, serves MCP on `:7878`) |
| `go-rag stop` | Stop the running daemon |
| `go-rag status` | Show daemon state (running/pid/addr) + DB counts |
| `go-rag mcp` | stdio→HTTP proxy — bridges a stdio MCP client (Claude Desktop) to the running daemon |

## MCP daemon

go-rag mirrors [muninndb](https://github.com/scrypster/muninndb)'s service model:
`start` re-execs a detached daemon (`Setsid`) that owns the Pebble database and
serves MCP over HTTP (`POST /mcp` + `GET /mcp/health`) on `:7878` (configurable via
`--mcp-addr` / `config.mcp_addr`; chosen to avoid muninn's 8475/8476/8750). Optional
bearer-token auth via `./.go-rag/mcp.token`.

**Ten MCP tools:** `go_rag_query`, `go_rag_status`, `go_rag_add`, `go_rag_init`,
`go_rag_scan`, `go_rag_config`, `go_rag_files`, `go_rag_dirs`, `go_rag_reprocess`,
`go_rag_migrate`.

Wire it into Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "go-rag": {
      "command": "/abs/path/to/go-rag",
      "args": ["mcp", "--db-path", "/abs/path/to/.go-rag"]
    }
  }
}
```

`go-rag mcp` is the stdio→HTTP proxy: Claude Desktop spawns it, it forwards to the
running daemon. (Start the daemon first with `go-rag start` in that database's
directory.)

## Obsidian support

The Markdown reader normalizes Obsidian syntax at ingest:
- `![[file.ext]]` image/file embeds → filename kept as a searchable token (brackets dropped).
- `[[wikilinks]]` → display text, alias (`[[a|b]]`) and heading (`[[a#s]]`) aware.
- `![[Note]]` transclusions → target recorded in `metadata["transcludes"]` (relationship captured, not inlined).

After a reader change, run `go-rag reprocess <vault>` to re-apply it to existing notes.

## Architecture

Layered: **CLI** → **orchestration pipeline** → **(readers / embedder /
change-detection)** → **core engine** → **(BM25 FTS + vector + metadata indexes)**
→ **embedded Pebble KV**. Retrieval fuses BM25 and vector results via Reciprocal
Rank Fusion (PRD §4.3). Writes acknowledge in <10ms; all indexing is asynchronous.
Full design: [`PRD_RAG_Database.md`](./PRD_RAG_Database.md).

## Project structure

```
cmd/go-rag/      # single binary entrypoint
internal/
  cli/           # cobra commands (init/add/scan/query/status/config/files/dirs/reprocess/migrate/start/stop/mcp)
  model/         # Source → Document → Chunk → Embedding data model
  reader/        # FileReader interface + registry (text/markdown/docx/pdf/image)
  embed/         # Embedder interface (Ollama /api/embed)
  storage/       # embedded Pebble KV + key-space prefixes
  index/         # BM25 full-text + vector indexes, RRF retrieval
  pipeline/      # ingest pipeline + async workers + reprocess/migrate
  watcher/       # 2-layer change detection (fsnotify + polling)
  chunk/         # text splitter
  config/        # persisted configuration (.go-rag/config.json)
  daemon/        # background daemon lifecycle (start/stop/status + pidfile)
  mcp/           # MCP server (stdio + HTTP) + tool dispatch
docs/            # mkdocs documentation
```

## Development

```bash
make test     # go test -race -cover ./...
make vet      # go vet ./...
make lint     # golangci-lint run
make vuln     # govulncheck ./...
make tidy     # go mod tidy
make help     # list all targets
```

The code graph is indexed by **tokensave** (`tokensave status`) for semantic
navigation; use `tokensave_context` for plain-English code queries.

## License

TBD (likely MIT — the dependency stack is permissively licensed, PRD §9.3).
