# go-rag

> A single-binary local RAG (Retrieval-Augmented Generation) database for your
> documents — ingest, index, and query PDFs, Word files, Markdown, and images on
> your filesystem. Embeddings work out of the box via a bundled pure-Go model
> (spec 032); a local Ollama is optional for alternative models.

**Status:** alpha — v1 implemented and working end-to-end. Multi-vault support,
cross-encoder reranking, muninn-style MCP daemon, and Obsidian-aware ingestion.
Full spec: [`PRD_RAG_Database.md`](./PRD_RAG_Database.md).

## Why

A local RAG database should be as frictionless as `git init; git add; git commit` —
no Docker, no API keys, no cloud services. Install the binary, run `go-rag init`,
and you have a working RAG system.

## Requirements

- **Go** 1.22+ (build from source)
- _(Optional)_ **Ollama** + an embedding model — only if you prefer Ollama embeddings over the bundled pure-Go default

## Installation

**Homebrew** (macOS/Linux — recommended):

```bash
brew install madeinoz67/tap/go-rag
go-rag version
```

**Build from source** (needs Go 1.22+):

```bash
make build   # → ./bin/go-rag
```

Prebuilt binaries for every release: [github.com/madeinoz67/go-rag/releases](https://github.com/madeinoz67/go-rag/releases).


## Quickstart

```bash
ollama pull mxbai-embed-large        # OPTIONAL — only for Ollama embeddings (the default pure-Go model needs no pull)
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
| `go-rag start` | Start the daemon in the background (loopback `127.0.0.1:7878`; `--bind-external` to bind a non-loopback address) |
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

## Structured PDF ingestion

`go-rag add` extracts the full structure of a PDF (not just flattened text):

- **Metadata** — title, author, subject, keywords from the PDF Info dictionary → `Document.Metadata`. Keywords flow into the `--tags` filter.
- **Document hierarchy** — headings from DOCX (Word styles), PDF (bookmark outline or font-size heuristics), and text (pattern heuristics). Threaded into each chunk's `section_context` breadcrumb (spec 025 parity).
- **Tables** — grid-detected tables rendered as searchable Markdown in the chunk content. Cross-page tables get continuation markers.
- **Image/chart captions** — embedded images extracted and captioned by a local vision model (opt-in). The caption text becomes a searchable chunk so image content is retrievable.

### Enabling image captioning

Captioning is **opt-in** (default off). Set in `.go-rag/config.json`:

```json
{
  "captioning_enabled": true,
  "captioning_model": "minicpm-v4.6:latest"
}
```

Tested models (chart-caption quality, June 2026):

| Model | Speed | Quality |
|-------|-------|---------|
| `minicpm-v4.6:latest` | 4.5s | ✅ Recommended — correct, concise, fastest |
| `glm-ocr:latest` | 137s | Accurate but too slow for ingest |
| `llava:latest` | 6.5s | Miscounts (not recommended) |

### Provider abstraction (model backends)

Every model-using component (embeddings, enrichment, captioning, reranking) can use Ollama (default) or any OpenAI-compatible endpoint (OpenAI, Azure, vLLM, LM Studio). Per-capability config:

```json
{
  "captioning_provider": "openai",
  "captioning_endpoint": "https://api.openai.com/v1",
  "captioning_api_key": "sk-...",
  "embedding_provider": "ollama"
}
```

The same `provider`/`endpoint`/`api_key` pattern applies to `embedding_*`, `enrichment_*`, `captioning_*`, and `rerank_*`. Embeddings default to `native` (the bundled pure-Go model, spec 032 — no service required); enrichment/captioning/rerank default to Ollama at `ollama_url`.


## Hybrid retrieval (RRF)

Hybrid mode fuses the BM25 (keyword) and vector (semantic) ranked lists with
**Reciprocal Rank Fusion** using a single symmetric constant `k` (default **60**,
the retrieval book's canonical value):

```
score(d) = Σ 1/(k + rank)        # rank is 1-based; summed over the lists d appears in
```

The same `k` applies to both lists — the prior asymmetric per-list constants have
been removed so the fusion is reviewable and matches the standard formula. You can
tune `k` per corpus (a larger `k` flattens rank dominance):

```bash
go-rag config set rrf_k 120              # persist a non-default constant
go-rag query "..." --rrf-k 30            # one-off override for a single query
```

`rrf_k` is honoured identically by the CLI, REST, gRPC, and MCP surfaces, and is
inert in `keyword`/`semantic` mode (those use a single list, so there is nothing
to fuse). An absent `rrf_k` (or `0`) means "use the default 60".

## Cross-encoder reranking

After RRF retrieval returns top-20 candidates, an optional Ollama LLM scores each
query–chunk pair directly, cutting semantic noise (e.g., unrelated chunks with low
vector similarity). Enabled via config:

```bash
go-rag config set rerank_model phi3:latest   # or llama3.1, mistral, etc.
go-rag query "honeypot deception"            # reranked: noise scored 0.000
go-rag query "honeypot deception" --no-rerank  # skip reranking (faster)
```

## Embedding instruction prefixes

Instruction-tuned embedding models (the default `nomic-embed-text`, plus E5 and
BGE families) expect **asymmetric** prefixes: a retrieval query and an indexed
document are different roles. go-rag applies the right prefix automatically so
each text reaches the model in its trained role (`search_query:` for queries,
`search_document:` for documents on nomic; `query:`/`passage:` on E5; a query
instruction on BGE). Models that don't use prefixes (e.g. `mxbai-embed-large`,
`all-MinLM-L6-v2`) are left untouched.

This is on by default (`auto`) and requires no configuration for the common
case. Override per vault:

```bash
go-rag config set embedding_prefix off              # disable prefixing
go-rag config set embedding_query_prefix "query: " # explicit per-role overrides
go-rag config set embedding_doc_prefix "passage: "
```

## Query caching

The daemon caches query results and query embeddings in-process so a repeated
query returns instantly — no Ollama round-trip, no retrieve/fuse/rerank work.
Caching is **on by default**, transparent (a cached hit is byte-identical to a
cold result), and never goes stale: an internal index epoch bumps on every
ingest/delete/migrate (including the asynchronous vector landing), and `migrate`
flushes both caches. It only helps the long-lived **daemon** (one-shot `go-rag
query` calls each start cold).

```bash
go-rag query "..." --no-cache          # force a fresh result this once (still caches it)
go-rag config set query_cache_enabled false   # global kill-switch
go-rag config set query_cache_results 512     # result-cache capacity (0 = off)
go-rag config set query_cache_embeddings 1024 # query-embedding-cache capacity (0 = off)
go-rag status                            # shows: cache: result 2/256 (3 hits, 1 misses), …
```


The prefix never alters stored document content or document identity, so changing
the convention is a re-embed (`go-rag reprocess`), not a re-ingest. `go-rag
status` reports the active convention; a query whose convention differs from the
corpus is refused with a re-embed hint rather than scored across a
half-prefixed corpus.

### Retrieval-quality benchmark (BEIR, manual)

The committed golden dataset (`testdata/golden/`) is a small **regression gate**
— it saturates at recall 1.0, so it catches breakage but can't show a quality
delta. To actually measure a retrieval change (e.g. the effect of instruction
prefixes), run a real BEIR benchmark — opt-in and slow (it fully ingests the
corpus with the real model), **not** in CI:

```bash
go-rag eval --benchmark scifact \
  --embedder ollama --embedding-model nomic-embed-text \
  --embedding-prefix off  --no-rerank --mode semantic   # baseline
go-rag eval --benchmark scifact \
  --embedder ollama --embedding-model nomic-embed-text \
  --embedding-prefix auto --no-rerank --mode semantic   # prefixes on
```

The dataset is fetched once (cached under `~/.go-rag/benchmarks/`). For large
datasets, MS MARCO is supported via subsampling — its full corpus is ~8.8M
passages, so `--benchmark msmarco` streams it once and keeps a tractable,
reproducible subset (sampled dev queries + their relevant passages + a stride
sample of distractors):

```bash
go-rag eval --benchmark msmarco \
  --embedder ollama --embedding-model nomic-embed-text \
  --embedding-prefix auto --no-rerank --mode semantic \
  --benchmark-queries 200 --benchmark-distractors 8000
```

`--benchmark-distractors` sizes the distractor pool (larger = harder/more
realistic but slower to ingest). **Measured results** (`nomic-embed-text`,
semantic-only):

| dataset | asymmetry | prefix effect (off → on) |
|---------|-----------|--------------------------|
| **SciFact** (300 q) | low (claims ~ abstracts) | near-neutral: recall@5 0.735→0.744, recall@10 0.813→0.807 |
| **MS MARCO** (200 q, 8k pool) | high (short query / long passage) | **positive on every metric**: recall@5 0.988→0.998, MRR 0.954→0.971, NDCG 0.964→0.977 |

The contrast is the point: instruction prefixes help where query/passage
asymmetry is high (MS MARCO) and help least where it's low (SciFact). The MS
MARCO magnitude here is capped by the easy ~8k subsample (recall near-saturated);
a larger pool (`--benchmark-distractors 50000+`) would show a bigger effect but
ingests slowly (~1000 passages/min — cross-document embed batching is audit H12).

**Attribution:** BEIR (Thakur et al., 2021, https://arxiv.org/abs/2104.08663);
SciFact is CC BY-NC. Benchmark data is fetched at runtime and is **not**
committed to this repo.

## Embedding drift monitoring & version pinning

An Ollama-server or model update can **silently change** the vectors a given
model produces (a pooling/quantization change), tanking recall with no error.
go-rag pins the embedding profile the corpus was built under and checks it at
boot, so this drift is caught before a query — not noticed after retrieval
quality collapses.

A **corpus baseline** `{model, dim, convention, ollama-version}` is persisted per
vault, written on first embed, refreshed on `migrate`, and backfilled on first
boot for older vaults. At daemon startup (and in `status`) the live config +
live Ollama version are compared against it:

- **model / dim / convention mismatch** → **hard drift**: the daemon starts
  **degraded** — `/health` reports `ready:false` (liveness `ok:true`, so the
  process stays up and `status`/`migrate` still work) and mismatched queries are
  refused. Run `go-rag migrate` to re-embed under the current model.
- **Ollama-version change** (same model) → **soft drift**: a warning; queries
  still serve, but re-indexing is recommended.

```bash
go-rag status        # shows: baseline: model=… dim=… conv=… ollama=…/live=…, drift: <verdict>
curl localhost:7879/health   # {"ok":true,"ready":false,"drift_verdict":"hard-drift",…}
go-rag migrate       # remediate: re-embed under the current model + refresh the baseline
```

This layers on the existing query-time mismatch guard (which refuses a mismatched
query) — the boot check makes drift visible *before* the first query and catches
Ollama-server drift that the model/dim guard cannot.

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

## Network binding & security

go-rag is **loopback-only by default**. Every transport (MCP `:7878`, REST
`:7879`, gRPC `:7880`) binds to `127.0.0.1` and is unreachable from other
machines unless you explicitly opt in:

```bash
# Refused — exits before opening any listener:
go-rag start --mcp-addr 0.0.0.0:7878

# Allowed, with a prominent exposure warning printed once at boot:
go-rag start --mcp-addr 0.0.0.0:7878 --bind-external
```

`--bind-external` authorizes non-loopback binding for whichever transports you
configured externally. go-rag ships **no TLS** — external binding is plaintext at
your explicit risk, and access control is your responsibility. If you expose
go-rag beyond loopback, front it with a TLS-terminating reverse proxy or tunnel.
The default-close posture is deliberate: a frictionless local database should
never silently expose your document vault to the network.

## Deployment (Docker)

go-rag ships as a static, CGO-free binary in a minimal distroless image, with a
`docker-compose.yml` for one-command local deployment. The bundled pure-Go
embedder means **zero external services by default**.

### Quick start

```bash
docker compose up -d                                # healthy daemon on host loopback
docker compose exec go-rag go-rag add /ingest       # ingest the ./docs bind mount
docker compose exec go-rag go-rag query "<term>"    # query
```

- **Image**: `ghcr.io/madeinoz67/go-rag:latest` (multi-arch `linux/amd64` +
  `linux/arm64`, built by the release workflow). Set `build: .` in
  `docker-compose.yml` to build locally instead of pulling.
- **Ports** (host **loopback** by default): MCP `127.0.0.1:7878`, REST `:7879`,
  gRPC `:7880`. See `docker-compose.yml` for the commented LAN-exposure variant
  (no TLS — trusted networks only).
- **Vault**: named volume `go-rag-data` at `/data` (persists across `down`/`up`).
- **Ingestion**: host `./docs` is bind-mounted **read-only** at `/ingest`.
  One-shot via `go-rag add /ingest`; continuous via `GO_RAG_WATCH_DIRS=/ingest`.

### Configuration — `GO_RAG_*` environment variables

go-rag config is **hybrid**: the file base (`.go-rag/config.json` in the vault)
is overridden by `GO_RAG_*` env vars set in `docker-compose.yml` (an env var wins
only when set + non-empty). Container-priority vars include `GO_RAG_MCP_ADDR`,
`GO_RAG_MCP_TOKEN`, `GO_RAG_OLLAMA_URL`, `GO_RAG_EMBEDDING_MODEL`,
`GO_RAG_WATCH_DIRS`, `GO_RAG_ENRICHMENT_ENABLED`. Full mapping table:
[`specs/033-docker-deployment/contracts/interface-contracts.md`](specs/033-docker-deployment/contracts/interface-contracts.md).

### Rules

- **Single-writer**: exactly one go-rag process may own the vault volume — never
  set `deploy.replicas > 1` or mount `go-rag-data` read-write into a second
  container.
- **Loopback-by-default** (spec 007): the container binds `0.0.0.0` and passes
  `--bind-external` (required for port-forwarding to work); host exposure stays
  loopback by default.
- **Optional Ollama** (escape hatch): `docker compose --profile ollama up -d`
  enables a sidecar; set `GO_RAG_EMBEDDING_MODEL` on `go-rag` to switch off the
  bundled embedder.

Runnable validation scenarios:
[`specs/033-docker-deployment/quickstart.md`](specs/033-docker-deployment/quickstart.md).

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
  reader/        # FileReader interface (text/markdown/docx/pdf/image)
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
