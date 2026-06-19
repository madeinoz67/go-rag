# go-rag

> A single-binary local RAG (Retrieval-Augmented Generation) database for your
> documents — ingest, index, and query PDFs, Word files, markdown, and images on
> your filesystem with zero external dependencies beyond a local Ollama instance.

**Status:** scaffold / pre-alpha. The CLI surface, project structure, and tooling
are in place; ingestion and retrieval logic are implemented against the spec in
[`PRD_RAG_Database.md`](./PRD_RAG_Database.md).

## Why

A local RAG database should be as frictionless as `git init; git add; git commit` —
no Docker, no API keys, no cloud services (PRD §1). Install the binary, run
`go-rag init`, and you have a working RAG system.

## Requirements

- **Go** 1.22+ (build from source)
- **Ollama** (runtime only — serves the embedding model via `/api/embed`)

## Quickstart

```bash
make build                       # build the static binary into ./bin
./bin/go-rag --help              # see all commands
./bin/go-rag version
```

## Commands

| Command | Description |
|---------|-------------|
| `go-rag init` | Initialize a new RAG database (PRD §5.2) |
| `go-rag add <path>` | Add files or directories (PRD §5.3) |
| `go-rag scan [--watch]` | Scan for changes (PRD §5.4) |
| `go-rag query "<q>"` | Hybrid semantic + keyword search (PRD §5.5) |
| `go-rag status` | Database statistics and health (PRD §5.6) |
| `go-rag config` | View or change configuration (PRD §5.7) |

## Architecture

Layered: **CLI** → **orchestration pipeline** → **(readers / embedder /
change-detection)** → **core engine** → **(BM25 FTS + vector + metadata indexes)**
→ **embedded Pebble KV**. Retrieval fuses BM25 and vector results via Reciprocal
Rank Fusion (PRD §4.3). Full design: [`PRD_RAG_Database.md`](./PRD_RAG_Database.md).

## Project structure

```
cmd/go-rag/      # the single binary entrypoint
internal/
  cli/           # cobra commands (init/add/scan/query/status/config)
  model/         # Source → Document → Chunk → Embedding data model
  reader/        # FileReader extension interface + registry
  embed/         # Embedder interface (Ollama /api/embed)
  storage/       # embedded Pebble KV + key-space prefixes
  index/         # BM25 full-text + vector (chromem-go) indexes
  pipeline/      # ingest pipeline (Read→Split→Hash→Dedup→Embed→Store)
  watcher/       # 2-layer change detection (fsnotify + polling)
  chunk/         # text splitter
  config/        # persisted configuration (.go-rag/config.json)
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
