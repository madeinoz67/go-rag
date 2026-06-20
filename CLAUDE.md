# CLAUDE.md — go-rag

Project-scoped guidance for Claude Code working in this repository.

## What this is

`go-rag` is a single-binary local RAG database in Go. **`PRD_RAG_Database.md` is the
product specification** — the authoritative source for behavior, data model, and
architecture. `ISA.md` is the project's done-condition / system of record. When the
two conflict on *what to build*, the PRD wins; on *whether it's done*, the ISA wins.

## Module & toolchain

- Module path: `github.com/madeinoz67/go-rag`
- Go 1.22+ required (PRD §10.4); pure Go, **no CGo** — everything builds with
  `CGO_ENABLED=0` (PRD §9.5).
- Single binary entrypoint: `cmd/go-rag/main.go`. Do not add other `main` packages.

## Commands

```bash
make build        # CGO_ENABLED=0 go build → ./bin/go-rag
make test         # go test -race -cover ./...
make vet          # go vet ./...
make lint         # golangci-lint run
make tidy         # go mod tidy
```

Keep `go build ./...`, `go vet ./...`, and `go test ./...` green at all times.

## Architecture map (directory → PRD section)

| Directory | Responsibility | PRD |
|-----------|---------------|-----|
| `cmd/go-rag` | binary entrypoint | §1, §5 |
| `internal/cli` | cobra commands (6) | §5 |
| `internal/model` | Source/Document/Chunk/Embedding | §6.2–6.5 |
| `internal/reader` | `FileReader` interface + registry | §8 |
| `internal/embed` | `Embedder` interface (Ollama) | §4 |
| `internal/storage` | Pebble KV + key-space prefixes | §6.7, §4.2 |
| `internal/index` | BM25 FTS + vector (chromem-go) | §6.6 |
| `internal/pipeline` | ingest pipeline | §4.4 |
| `internal/watcher` | fsnotify + polling change detection | §7 |
| `internal/chunk` | text splitter | §4.4 |
| `internal/config` | `.go-rag/config.json` | §5.7 |

Every directory maps 1:1 to a PRD subsystem — when adding code, place it where the
PRD says it belongs.

## Constraints

- **Pure Go only.** Never introduce CGo or C dependencies (PRD §9.5).
- **Single Pebble instance**, prefix-partitioned key space — see `internal/storage`
  for the fixed prefix constants before adding new key types (PRD §6.7).
- **Extension by interface.** New file types implement `reader.FileReader` and
  self-register; new embedding providers implement `embed.Embedder` (PRD §8.1, §4.2.5).
- **Idempotent ingestion** via SHA-256 content-addressed IDs (PRD §7.2) — identity
  and change-detection hashes are distinct.
- **No Bun/Python/Node artifacts.** This is a Go project — do not create
  `package.json`, `pyproject.toml`, `tsconfig.json`, or a `src/` directory.

## Code navigation

This repo is indexed by **tokensave**. Prefer `tokensave_context` (plain-English
queries) and `tokensave_search`/`tokensave_callers` over ad-hoc grep for
understanding structure. Re-run `tokensave init` after large structural changes.

## Out of scope for v1 (PRD §2.2)

Cloud/hosted service, multi-user auth, LLM inference, audio/video, web UI, plugin
system, embedding providers beyond Ollama. Don't build these without revisiting the PRD.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan:
`specs/002-document-vaults/plan.md`
<!-- SPECKIT END -->
