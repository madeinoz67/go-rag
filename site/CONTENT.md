# site/CONTENT.md — verified content facts for the landing page

> T002 output. Every value below was verified against the shipping binary
> (`bin/go-rag`, v0.3.3-102-g...) or the source on 2026-08-11. The page MUST be
> built from these, NOT from the mockup's placeholders. When this file and the
> mockup disagree, this file wins (FR-003 / SC-006).

## Identity
- **Name**: go-rag
- **Tagline / thesis**: A RAG database that lives in one binary. (Single-binary,
  local-first, pure-Go. Offline by default.)
- **Status badge**: `Alpha · v0.3.3` (latest released tag; the working tree is
  102 commits past it, unreleased). The mockup's "v1 working end-to-end" is wrong.
- **License**: **MIT** (principal-confirmed 2026-08-11). ⚠️ NOTE: the repo's
  `LICENSE` file currently contains the Apache-2.0 text and `release.yml`'s
  docker label says `Apache-2.0` — both are stale/scaffold leftovers and must be
  reconciled to MIT for consistency (the `LICENSE` file replaced with MIT text;
  the docker label updated). The website says MIT per Stephen's decision.
- **Author**: Stephen Eaton.
- **Repo**: https://github.com/madeinoz67/go-rag
- **PRD**: docs/internals/PRD_RAG_Database.md

## Transport ports (loopback by default; `--bind-external` opts into network)
- MCP: `127.0.0.1:7878`
- REST: `127.0.0.1:7879`
- gRPC: `127.0.0.1:7880`
- Management console (UI): `127.0.0.1:7881`  ← the mockup omits this 4th transport

## CLI commands (from `go-rag --help`; 29 visible + 1 hidden)
**Daemon**: `start` (MCP+REST+gRPC+UI in one process), `stop`, `health`, `mcp`
(hidden — stdio→HTTP MCP proxy bridging Claude Desktop to the daemon),
`upgrade` (self-upgrade to latest release).
**Ingest & query**: `init`, `add`, `scan` (`--watch`), `query`, `reprocess`,
`delete` (index-only; source preserved), `migrate` (re-embed to current model).
**Inspect**: `files`, `dirs`, `documents` (cursor + status + pagination),
`chunk` (fetch one by ID), `status`, `audit` (structured event log).
**Quality & safety**: `eval` / `eval-gen` (retrieval-quality measurement),
`poison` (list/release/reset injection-flagged chunks), `threat` (manage
instruction-phrase sources), `enrich` (re-run doc auto-tag + summary).
**Config & vaults**: `config` (get/set), `vault` (create/list/delete/clear/
clone/export/import), `model` (manage the bundled embedder), `auth` (API keys,
sessions, admin user — spec 045), `bridge` (MuninnDB bridge — spec 060),
`version`.

### query flags (the retrieval surface — `go-rag query --help`)
`--mode` hybrid|semantic|keyword · `--k` (default 5) · `--threshold` ·
`--no-rerank` · `--pool-size` (reranker candidate pool, default 60) ·
`--rrf-k` (RRF constant, default 60) · `--context-window` · `--dedup` ·
`--source`/`--type`/`--tags` filters · `--include-quarantined` · `--no-cache` ·
`--format` text|json.

## MCP tools — 30 total (the mockup's "10 tools" is stale)
Served by the daemon over HTTP (`:7878`); `go-rag mcp` bridges stdio clients
(Claude Desktop) to it. Grouped:
- **Query/status**: `go_rag_query`, `go_rag_status`, `go_rag_guide`
- **Ingest/maintain**: `go_rag_add`, `go_rag_init`, `go_rag_scan`,
  `go_rag_reprocess`, `go_rag_migrate`, `go_rag_migrate_plan`,
  `go_rag_delete_document`, `go_rag_model_install`
- **Inspect**: `go_rag_files`, `go_rag_dirs`, `go_rag_list_documents`,
  `go_rag_list_chunks`, `go_rag_get_chunk`, `go_rag_get_chunk_context`,
  `go_rag_batch_get_chunks`
- **Poisoning triage**: `go_rag_poison_list`, `go_rag_poison_release`,
  `go_rag_poison_reset`, `go_rag_poison_rescan`
- **Vaults/config/eval**: `go_rag_vault_list`, `go_rag_config`, `go_rag_eval`
- **Auth (admin-gated, spec 045)**: `go_rag_auth_list`, `go_rag_auth_create`,
  `go_rag_auth_revoke`, `go_rag_auth_session_list`, `go_rag_auth_session_revoke`

The Claude-Desktop config snippet (the mockup's is correct in shape):
```json
{"mcpServers": {
  "go-rag": {
    "command": "/abs/path/to/go-rag",
    "args": ["mcp", "--vault", "cyber-notes"]
  }
}}
```

## Architecture (confirmed, unchanged from mockup)
CLI → Ingest pipeline (async-after-ACK, <10ms writes) → Readers/Embedder/Change
detection → Retrieval (BM25 + vector + RRF, optional rerank) → Embedded Pebble KV
(key-space prefixes, per-vault isolation). RRF formula: `score(d) = Σ 1/(k + rank)`, k = 60.

## Embeddings
A bundled **pure-Go embedding model** ships in the binary (spec 032,
`bge-small-en-v1.5` int8 ONNX) — ingest and query work fully offline from first
run. A local Ollama is **optional** for alternative embedding models and
cross-encoder reranking, never required. (`--model install` / `go-rag model`.)

## Install paths to surface (release.yml already publishes all of these)
1. **Primary (macOS/Linux)**: `curl -fsSL https://madeinoz67.github.io/go-rag/install.sh | sh`
2. **Homebrew** (when the tap is live): `brew install madeinoz67/tap/go-rag`
3. **From source** (Go 1.22+): `go install github.com/madeinoz67/go-rag/cmd/go-rag@latest`
4. **Windows**: download the `.zip` from the latest release manually.
5. **Docker**: `docker pull ghcr.io/madeinoz67/go-rag` (multi-arch linux/amd64+arm64).

## Benchmarks
The mockup's SciFact / MS MARCO numbers are **illustrative placeholders**, not
verified this session. For v1: either reproduce with `go-rag eval` and cite, or
drop the section and replace with a "measure it yourself: `go-rag eval`" pointer.
Do NOT ship the mockup's specific figures unverified.

## Voice reminders (style guide §6)
Person's side of the terminal ("you get source-cited results"). Name things by
what they do ("your document vault", not "the Pebble-backed corpus"). Plain,
active, unhurried. No exclamation points. No "supercharge/unlock/seamless".

## Advanced surfaces (the site's "Advanced" section — verified commands)
Each command below was verified against `bin/go-rag --help` on 2026-08-12. If a
command/flag changes, the matching Advanced block on `site/index.html` must move
(this is what the code-reviewer's "Public website" drift bullet checks).

- **Management console** — 4th loopback transport, `127.0.0.1:7881`, Bearer-guarded
  (spec 045/046). `go-rag start` serves it.
- **Docker** — image `ghcr.io/madeinoz67/go-rag` (linux/amd64 + linux/arm64,
  distroless/static, nonroot, spec 033). Container CMD = foreground `serve`
  against `/data`; map `-p 127.0.0.1:7878:7878` to keep host loopback-only.
- **Vaults** — `go-rag vault create|list|delete|clear|clone|export|import`; per
  command `--vault <name>` (specs 002/052). One daemon, one Pebble store,
  key-space isolation.
- **Embeddings & enrichment** — `go-rag model install` (bundled pure-Go model,
  spec 032); `go-rag enrich` (back-fill tags + summaries, opt-in, local model,
  spec 029); embedding-drift monitor (pins profile, fails loudly). Ollama via
  `go-rag init --ollama-url` / config (don't ship an unverified `config set` key).
- **MuninnDB bridge** — `go-rag bridge muninn init` + `go-rag bridge muninn status`
  (spec 060; note the nested `muninn` subcommand). Token = env `GORAG_BRIDGE_TOKEN`.
  Opt-in, loopback-only, never blocks a core op, degrades safe when MuninnDB down.
