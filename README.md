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
