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
  live instance. To clean up orphaned test daemons — which `pkill -f go-rag.*<dbpath>`
  misses (go-rag detaches + re-execs without `--db-path` in argv, leaving a stale
  binary holding the port → phantom route 404s) — kill by port:
  `for p in 7878 7879 7880 7881; do lsof -ti :$p | xargs kill -9; done`.
- **Lint before push.** Run `make lint` (golangci-lint) before `git push` — it is
  the `ci.yml` gate and strictly stricter than `go vet`/`go test` (catches
  built-in shadowing like `min`/`max`, gofmt nits, errcheck, staticcheck). A
  committed pre-push hook in `githooks/` enforces it once enabled
  (`git config core.hooksPath githooks`); bypass one push with `git push --no-verify`.

## Console UI conventions

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

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at specs/056-ui-settings-system-transports/plan.md
<!-- SPECKIT END -->

<!-- gortex:communities:start -->
## Codebase Overview (generated by Gortex)

- **Languages:** go (primary), , bash, contract, dockerfile, gitignore, json, makefile, markdown, mcp_config, powershell, protobuf, text, toml, yaml
- **Entry points:** `cmd/go-rag/main.go`
- **Most-referenced symbols:** `string` (1027 usages), `len` (826 usages), `Fatalf` (367 usages), `int` (363 usages), `error` (331 usages), `Background` (330 usages), `append` (226 usages), `Errorf` (208 usages), `Join` (183 usages), `make` (172 usages)
- **Graph size:** 23583 nodes, 103314 edges
- **Breakdown:** 24 builtins, 298 closures, 1 config_keys, 156 constants, 111 contracts, 3545 docs, 10 enum_members, 1327 fields, 672 files, 9 fixtures, 1334 functions, 3 generic_params, 4 images, 1472 imports, 14 interfaces, 6086 locals, 846 methods, 174 modules, 1833 params, 2 resources, 200 strings, 5 todos, 362 types, 5095 variables

## MANDATORY: Use Gortex MCP tools instead of Read/Grep/Glob

Gortex is running as an MCP server. You **MUST** prefer graph queries over file reads on every task in this repo — `search_symbols`, `find_usages`, `get_symbol_source`, `get_editing_context`, `smart_context`, `edit_symbol` / `edit_file` / `rename_symbol` / `batch_edit`. PreToolUse hooks deny `Read` / `Grep` / `Glob` against indexed source; the deny message names the right tool. The full per-tool catalog loads via `tools/list` — not restated here.

### Calibration: the graph narrows scope, source confirms behavior

The mandate above stands — but graph queries *narrow scope*, they do not *replace reading the implementation*. The graph tells you **where** the logic lives and **what** connects to it; the source tells you **how** it behaves. For the symbol you are about to change or depend on, read its full body with `get_symbol_source` — do not act on a one-line summary alone.

Be especially deliberate with **behavior-critical code** — database migrations, retry / fallback / error-recovery paths, compatibility shims, concurrency-sensitive sections, and the tests that pin them. For these, call `get_symbol_source` and read the real implementation; never pass `compress_bodies:true`, which elides exactly the branches that carry the risk. Reserve compressed bodies and graph summaries for breadth (surveying many symbols); use full source for the few you are about to commit to.

## Required workflow (every task on this repo)

These are not suggestions — run each step at the trigger.

1. Confirm the daemon is up with `index_health` (cheap liveness + scope). Call `graph_stats` only when you actually need node/edge counts or `per_repo` orientation — it returns a large payload and can block during warmup.
2. If `total_nodes` is 0, **call** `index_repository` with `"."` before anything else.
3. In multi-repo mode, **call** `get_active_project` to check scope; use `set_active_project` to switch.
4. Open a non-trivial task with `smart_context` for orientation. For a single known symbol or file, go straight to `search_symbols` / `get_symbol_source` — don't front-load `smart_context` before every read.
5. Before editing a file, **call** `get_editing_context` on it first.
6. Before changing any function signature, **call** `verify_change` to catch broken callers and interface implementors (cross-repo).
7. For any refactor, **call** `get_edit_plan` then `batch_edit` to apply atomically.
8. Verify with the project's real build/test. Reserve `check_guards` for guard-relevant changes and `get_test_targets` to find the tests covering a substantive change — not mechanically after every edit.

<!-- gortex:skills:start -->
## Community Skills

| Area | Description | Skill |
|------|-------------|-------|
| Engine 19 Dirs | 1268 symbols | `/gortex-engine-19-dirs` |
| Engine 12 Dirs | 977 symbols | `/gortex-engine-12-dirs` |
| Reader 19 Dirs | 617 symbols | `/gortex-reader-19-dirs` |
| Daemon 15 Dirs | 515 symbols | `/gortex-daemon-15-dirs` |
| Engine 13 Dirs | 469 symbols | `/gortex-engine-13-dirs` |
| Grpc 8 Dirs | 462 symbols | `/gortex-grpc-8-dirs` |
| Cli 13 Dirs | 418 symbols | `/gortex-cli-13-dirs` |
| Reader 8 Dirs | 371 symbols | `/gortex-reader-8-dirs` |
| Index 1 Dirs Search | 344 symbols | `/gortex-index-1-dirs-search` |
| Cli 7 Dirs | 335 symbols | `/gortex-cli-7-dirs` |
| Engine 7 Dirs | 318 symbols | `/gortex-engine-7-dirs` |
| Eval 6 Dirs | 312 symbols | `/gortex-eval-6-dirs` |
| Protobuf Runtime 2 Dirs | 267 symbols | `/gortex-protobuf-runtime-2-dirs` |
| Reader 7 Dirs | 232 symbols | `/gortex-reader-7-dirs` |
| Engine 3 Dirs | 217 symbols | `/gortex-engine-3-dirs` |
| Pipeline 4 Dirs | 211 symbols | `/gortex-pipeline-4-dirs` |
| Rest 2 Dirs | 199 symbols | `/gortex-rest-2-dirs` |
| Grpc 3 Dirs | 187 symbols | `/gortex-grpc-3-dirs` |
| Redact 1 Dirs Applywithedits | 171 symbols | `/gortex-redact-1-dirs-applywithedits` |
| Pipeline 2 Dirs | 164 symbols | `/gortex-pipeline-2-dirs` |
<!-- gortex:skills:end -->

<!-- gortex:communities:end -->
