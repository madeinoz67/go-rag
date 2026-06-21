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
| `internal/daemon` | detached daemon: start/stop/status, PID + Pebble-lock single-instance, per-transport addrs | §5 |
| `internal/engine` | unified operation facade shared by every transport (Query/Add/Status/…) | §4 |
| `internal/rest` | REST adapter (stdlib net/http), serves `GET /openapi.yaml` | spec 003 |
| `internal/grpc` | gRPC adapter (grpc-go) over the engine | spec 003 |
| `proto/` | protobuf schema (`gorag.proto`) + generated `proto/gen` (Gorag service) | spec 003 |

Every directory maps 1:1 to a PRD subsystem — when adding code, place it where the
PRD says it belongs.

**Multi-transport server (spec 003).** `go-rag start` runs a detached daemon
serving three transports in one process, each on its own loopback port — MCP
(`:7878`, HTTP/JSON-RPC), REST (`:7879`, stdlib `net/http`), gRPC (`:7880`,
grpc-go). All three are adapters over a single `internal/engine.Engine`, so a
query over REST/gRPC/MCP returns identical results (cross-transport parity,
FR-002/003). `--rest-addr`/`--grpc-addr` override the ports; empty disables that
transport. One Pebble writer; writes ACK on the durable store and embed async
(Principle IV, `engine.Close` drains).

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
- **Smoke-test the daemon on an isolated DB.** The default `dbPath` is the
  global vault (`~/.go-rag/vaults/default`), not a cwd-local path — so a bare
  `go-rag start`/`stop` targets the user's real running daemon. When scripting
  the daemon for tests/smoke, always pass `--db-path <tmp>` plus non-default
  `--mcp-addr`/`--rest-addr`/`--grpc-addr`, or you will collide with and stop a
  live instance.

## Code navigation

This repo is indexed by **tokensave**. Prefer `tokensave_context` (plain-English
queries) and `tokensave_search`/`tokensave_callers` over ad-hoc grep for
understanding structure. Re-run `tokensave init` after large structural changes.

## Out of scope for v1 (PRD §2.2)

Cloud/hosted service, multi-user auth, LLM inference, audio/video, web UI, plugin
system, embedding providers beyond Ollama. Don't build these without revisiting the PRD.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at specs/007-loopback-bind-default/plan.md
<!-- SPECKIT END -->
