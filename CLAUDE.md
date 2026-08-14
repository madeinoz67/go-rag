# CLAUDE.md — go-rag

This file governs how AI agents (Claude Code and others) work in this repository. It
auto-loads for anyone using Claude Code here. It is the **index and the non-negotiables**:
what go-rag *is*, the principles every change is measured against, and how to work and
review in the repo. The deep references live in `docs/internals/` and `.specify/memory/constitution.md`.

## Memory (project vault)

> **go-rag dev memory goes to the `go-rag` vault via the `muninndb-gorag` MCP server** —
> use the `mcp__muninndb-gorag__*` tools (no `vault` arg; the connection key is scoped to
> `go-rag`). Do **not** use the global `muninndb` server for go-rag memory — that is the
> default/LifeOS vault, and writing there leaves duplicates in the wrong place. The preferred
> path is the ledger + drain (`node .claude/hooks/memory-propose.mjs` →
> `.claude/memory-proposals.jsonl`, auto-flushed on PreCompact/SessionEnd/Stop); the
> `muninndb-gorag` tools are the immediate path. Full bar + wiring in
> [`.claude/memory-protocol.md`](.claude/memory-protocol.md).

## What this is

`go-rag` is a **single-binary, local-first, pure-Go RAG database** — retrieval-only and
air-gapped. Its one-line promise, from the PRD, is load-bearing — every change is measured
against it:

> **As frictionless as `git init; git add; git commit`.**

Three documents share authority, in this order of precedence on conflict:

- **`docs/internals/PRD_RAG_Database.md`** — the product specification (what to build: behavior, data model, architecture).
- **`ISA.md`** — the project's done-condition / system of record (whether it's done).
- **`.specify/memory/constitution.md`** — the non-negotiable engineering principles every build MUST respect.

When the PRD and ISA conflict: the PRD wins on *what to build*; the ISA wins on *whether
it's done*. When anything conflicts with the constitution: the constitution wins on
principles and constraints.

## Core principles (the lens for every change)

Condensed from `.specify/memory/constitution.md` (v1.1.0) — that file is authoritative; this
is the working summary. Every change is measured against these five:

1. **Local-First, Single-Binary** — all data and processing on the user's machine; one
   `CGO_ENABLED=0` binary, no cloud/accounts/egress for any core operation.
2. **Content-Addressed Identity** — a document's identity is SHA-256 over content plus a
   canonicalized metadata map; identity and change-detection hashes stay distinct.
3. **Pure Go — No CGo, No External Runtime** — every dependency pure Go, permissively
   licensed; never CGo/C libraries.
4. **Async-After-ACK Writes** — writes validate, fsync-commit, and ACK in <10ms; all
   embedding/indexing happens on background workers *after* the ACK.
5. **Extension by Interface, MCP-First** — new formats implement `FileReader`; new providers
   implement `Embedder`; every CLI operation is also an MCP tool.

See the constitution for the full rationale, the performance/reliability budgets, and the
storage-discipline rule (any key-space layout change requires a numbered migration + an
`ExpectedVersion` bump + a PRD §6.7 update).

## How we work

**Verify, don't assume.** For any non-trivial change:

1. **Confirm the commit you're on.** The working checkout can be on a stale branch. Run
   `git branch --show-current` / `git log` before asserting what the code does.
2. **Build and test the actual change**, not just the diff: `make build && make vet && make test`
   plus the relevant package tests. Use `-race` for anything touching `internal/storage`,
   auth, migration, or concurrency.
3. **RED-sanity-check bug fixes.** A test for a fixed bug or closed race must be shown to
   *fail without the fix*. A test that passes both ways proves nothing.
4. **Verify claims independently.** If a description says "closes the race" / "all green" /
   "no behavior change," confirm it yourself — never take it on faith.

**The docs can drift.** When a doc's claim disagrees with the live code, **live code wins** —
say so and fix the doc; don't enforce the stale claim. The Pebble keyspace registry
(`docs/internals/keyspace-registry.md`) declares `internal/storage/storage.go` as its source
of truth on exactly this basis.

**The code-reviewer agent.** `.claude/agents/code-reviewer.md` is the repo's resident
reviewer — correctness, the core invariants, and cross-surface drift, with its own
verify-build-test protocol. Use it (or the global `/code-review` skill) when reviewing a
change, and proactively before opening a PR. It routes by what the diff touches and cites
`docs/internals/`.

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


## Architecture map (directory → PRD section)

| Directory | Responsibility | PRD |
|-----------|---------------|-----|
| `cmd/go-rag` | binary entrypoint | §1, §5 |
| `internal/cli` | cobra commands | §5 |
| `internal/model` | Source/Document/Chunk/Embedding | §6.2–6.5 |
| `internal/reader` | `FileReader` interface + registry | §8 |
| `internal/embed` | `Embedder` interface (Ollama) | §4 |
| `internal/storage` | Pebble KV + key-space prefixes | §6.7, §4.2 |
| `internal/storage/migrate` | on-open schema-migration runner (spec 034) | §6.7 |
| `internal/upgrade` | binary self-upgrade: release resolve + checksum verify + atomic self-replace (spec 034) | §5 |
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

- **Spec Kit work commits to `main` directly.** This is a single-author repo:
  all Spec Kit changes (`/speckit-specify`, `-plan`, `-tasks`, `-implement`) and
  their code land on `main` — **no feature branches, no PRs, no merge ceremony.**
  Commit with Conventional Commits straight to `main` and push. (Standing
  instruction until further notice; revisit if the repo ever takes outside
  contributors.)
- **Pure Go only.** Never introduce CGo or C dependencies (PRD §9.5).
- **Single Pebble instance**, prefix-partitioned key space — see `internal/storage`
  for the fixed prefix constants and `docs/internals/keyspace-registry.md` for the
  full prefix map before adding new key types (PRD §6.7).
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
  live instance. To clean up orphaned test daemons — which `pkill -f go-rag.*<dbpath>`
  misses (go-rag detaches + re-execs without `--db-path` in argv, leaving a stale
  binary holding the port → phantom route 404s) — kill by port:
  `for p in 7878 7879 7880 7881; do lsof -ti :$p | xargs kill -9; done`.
- **Lint before push.** Run `make lint` (golangci-lint) before `git push` — it is
  the `ci.yml` gate and strictly stricter than `go vet`/`go test` (catches
  built-in shadowing like `min`/`max`, gofmt nits, errcheck, staticcheck). A
  committed pre-push hook in `githooks/` enforces it once enabled
  (`git config core.hooksPath githooks`); bypass one push with `git push --no-verify`.

- **Restart the daemon after code changes (dev environment).** Rebuild (`make build`) + `./bin/go-rag stop` + `./bin/go-rag start` to serve the new binary on the default vault — no need to confirm first (Stephen's standing instruction, 2026-07-16: "always restart when needed, it's a dev environment"). A clean stop/start preserves the vault data AND the admin password — do NOT set `GORAG_ADMIN_PASSWORD` on a default-vault restart (it rotates the real admin password; only use it on isolated `/tmp` test daemons).

## Console UI conventions

The console's design system — color tokens, typography, spacing, component catalog, motion, z-index — is documented in `docs/internals/ui-style-guide.md`. It is the canonical reference for any console UI change; the CSS in `internal/ui/web/static/css/` is the executable source of truth (when guide and CSS disagree, CSS wins). The rules below are go-rag-specific additions on top of that design system.

- **Every data table is sortable.** All data tables in the management console —
  old and new, current and future — MUST have sortable column headers on their
  meaningful columns. Mirror the Documents table's pattern: `<th class="sortable"
  :class="{ active: sortKey==='col' }" @click="setSort('col')">` with a
  `sort-arrow`, backed by page-local `sortKey`/`sortDir` state + a `sortedX()`
  method the `x-for` reads (see `setDocSort`/`sortedDocs`, `setQuarSort`/
  `sortedQuar`). Non-meaningful columns (Tags, Actions, long-form Preview) may
  stay non-sortable. No new table ships without sort.
- **Static assets are served `Cache-Control: no-cache`.** The embedded SPA assets
  change with each binary but live at stable URLs; without the no-cache header the
  browser serves a stale copy after a daemon restart (hide-the-rule bugs). Don't
  strip the `noCache` wrapper on `/static/` + the shell.

## Out of scope for v1 (PRD §2.2)

Cloud/hosted service, multi-user auth, LLM inference, audio/video, plugin
system, embedding providers beyond Ollama. **Web UI: a single-operator management
console IS in scope as of spec 046** (embedded vendored SPA, loopback 4th transport,
spec 045 auth) — see PRD §2.2 N7. Don't build the rest without revisiting the PRD.

## Internal references

| Reference | What it holds |
|-----------|---------------|
| `docs/internals/keyspace-registry.md` | the Pebble prefix map — every allocated byte, vault-scoped vs global, reserved/free bytes, and the live reviewer hazards. Read before touching `internal/storage`. |
| `docs/internals/ui-style-guide.md` | the console UI design system — color tokens, typography, spacing, component catalog, motion, z-index. The canonical reference for any console UI change; the CSS in `internal/ui/web/static/css/` is the executable source of truth (when guide and CSS disagree, CSS wins). |
| `.specify/memory/constitution.md` | the five core principles, performance/reliability budgets, storage-discipline + schema-evolution rules. |
| `docs/internals/PRD_RAG_Database.md` | product specification (what to build). |
| `ISA.md` | project done-condition / system of record. |

(Future `docs/internals/` pages — invariants, decision-record — slot in here as they are written.)

## Attribution

Do not add "Generated with Claude" / Anthropic attribution to any PR body, commit message,
issue, or code comment.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at specs/061-public-website/plan.md
<!-- SPECKIT END -->

