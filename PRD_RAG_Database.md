# Product Requirements Document: go-rag

> **Version**: 1.0  
> **Date**: 2026-06-19  
> **Status**: Draft  
> **Author**: Hermes Researcher (synthesized from 3 parent research tasks)

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Goals & Non-Goals](#2-goals--non-goals)
3. [User Stories](#3-user-stories)
4. [System Architecture](#4-system-architecture)
5. [CLI Commands](#5-cli-commands)
6. [Data Model](#6-data-model)
7. [Document Change Detection](#7-document-change-detection)
8. [File Type Support](#8-file-type-support)
9. [Recommended Go Libraries](#9-recommended-go-libraries)
10. [Non-Functional Requirements](#10-non-functional-requirements)
11. [Open Questions & Risks](#11-open-questions--risks)
12. [Glossary](#appendix-a-glossary)
13. [Comparison with Existing Systems](#appendix-b-comparison-with-existing-systems)

---

## 1. Executive Summary

**go-rag** is a Go CLI local RAG (Retrieval-Augmented Generation) database — a single-binary tool that ingests, indexes, and queries documents on your filesystem with zero external dependencies. It brings RAG capabilities to the terminal: point it at a directory of PDFs, Word documents, images, and markdown files, and it builds a searchable vector database that answers questions grounded in your local content.

**The core thesis**: A local RAG database should be as frictionless to set up and use as `git init; git add; git commit`. No Docker, no API keys, no cloud services. Install the binary, run `go-rag init`, and you have a working RAG system.

**Key design decisions**:
- **Single binary** — statically linked Go, embedded storage (Pebble LSM-tree), no CGo, no runtime dependencies. Inspired by MuninnDB.
- **SHA-256 content-addressed IDs** — idempotent ingestion: add the same file twice, it's only processed once. Pattern adopted from Haystack/LlamaIndex.
- **2-layer change detection** — fsnotify for real-time watching + periodic polling with SHA-256 comparison for restart safety.
- **Pure Go stack** — pdfcpu for PDF extraction, chromem-go for embedded vector storage, fsnotify for file watching, Ollama HTTP API for embeddings.
- **Async-after-ACK write model** — writes complete in <10ms; all indexing, embedding, and dedup work happens asynchronously. Inspired by MuninnDB.
- **MCP-first design** — expose tools via Model Context Protocol so AI coding agents (Claude, Cursor, Copilot) can query the RAG database directly.

**Target audience**: Developers, researchers, and knowledge workers who want local, private RAG over their documents without managed services.

---

## 2. Goals & Non-Goals

### 2.1 Goals (7)

| # | Goal | Description |
|---|------|-------------|
| G1 | **Local-first RAG** | All data stays on the user's machine. No cloud dependency for core operations. |
| G2 | **Single-binary deployment** | One `go-rag` binary. No Docker, no Python venv, no npm install. |
| G3 | **Idempotent ingestion** | SHA-256 content-addressed IDs ensure adding the same file twice is a no-op. |
| G4 | **Automatic change detection** | Detect added, modified, and deleted files in watched directories via fsnotify + polling. |
| G5 | **Multi-format ingestion** | PDF, plain text, Markdown, Word (.docx), JPEG, PNG. Extensible via file-type interface. |
| G6 | **Semantic + keyword search** | Hybrid retrieval combining vector similarity (via Ollama embeddings) and BM25 full-text search. |
| G7 | **MCP integration** | Expose query, status, and management operations as MCP tools for AI agent consumption. |

### 2.2 Non-Goals (9)

| # | Non-Goal | Rationale |
|---|----------|-----------|
| N1 | Cloud/hosted service | Local-only. A separate product could add sync later. |
| N2 | Multi-user / auth | Single-user local tool. No login, no RBAC. |
| N3 | Real-time collaboration | Single-process. Concurrency is for background workers, not multiple users. |
| N4 | LLM inference (except background local enrichment) | go-rag stores and retrieves documents and does **not** run models at query time (no answer synthesis / generation on the retrieval path) and does **not** call cloud LLMs. Narrow exception: a background, opt-in, local-only **document enrichment** step (spec 029) may call the bundled Ollama for per-document auto-tagging + summary, strictly after the durable ingest ACK and off the query path. Query-time generation and cloud providers remain out of scope. |
| N5 | Audio/video ingestion | Out of scope for v1. Extension interface supports future addition. |
| N6 | Distributed clustering | Single-node. MuninnDB's Cortex/Lobe model is reference for future. |
| N7 | Web UI | CLI only for v1. TUI (bubbletea) can be added later. |
| N8 | Plugin system | Extension interface for file types is sufficient for v1. Plugin architecture (like MuninnDB's 3-tier system) is v2. |
| N9 | Custom embedding models beyond Ollama | Ollama is the only supported embedding provider for v1. The client interface is abstracted for future providers. |

---

## 3. User Stories (6)

### US1: First-Time Setup
**As a** developer new to RAG,  
**I want to** run `go-rag init` and have it auto-detect my Ollama instance and create a working database,  
**So that** I can start ingesting documents in under 30 seconds.

**Acceptance criteria**:
- `go-rag init` creates `.go-rag/` directory with default config
- Auto-detects Ollama at `http://localhost:11434`
- Prompts user to select an embedding model from available Ollama models
- Creates the Pebble database and initializes indexes
- Prints success message with next-step suggestions

### US2: Ingest Documents
**As a** researcher with a folder of PDF papers,  
**I want to** run `go-rag add ./papers/` and have all PDFs ingested, chunked, and embedded,  
**So that** I can query across my entire paper collection.

**Acceptance criteria**:
- `go-rag add <path>` recursively discovers all supported files
- Shows progress (files processed, chunks created, embeddings generated)
- Skips already-ingested files (content-addressed ID match)
- Reports: total files, new, skipped, errors
- Runs embedding generation asynchronously (write ACK <10ms for metadata)

### US3: Continuous Watching
**As a** knowledge worker who constantly adds files to a research directory,  
**I want to** run `go-rag scan --watch` and have new/modified/deleted files automatically reflected,  
**So that** my RAG database stays current without manual re-ingestion.

**Acceptance criteria**:
- `go-rag scan --watch` starts a long-lived process watching the configured directory
- Detects file creation, modification, deletion via fsnotify
- Also runs periodic SHA-256 polling (every 60s) as safety net
- Logs changes to stdout: `[ADDED] paper.pdf`, `[MODIFIED] notes.md`, `[DELETED] old.txt`
- Graceful shutdown on SIGINT/SIGTERM

### US4: Query Documents
**As a** developer onboarding to a large codebase with markdown docs,  
**I want to** run `go-rag query "how does the auth system work"` and get relevant document chunks with source attribution,  
**So that** I can find answers without reading every document.

**Acceptance criteria**:
- `go-rag query "<question>"` returns top-K ranked results (default K=5)
- Each result shows: chunk text, source file path, page number (for PDFs), relevance score
- Supports `--k N` flag to control result count
- Supports `--mode` flag: `hybrid` (default), `semantic`, `keyword`
- Results stream as they're scored (not batched at end)

### US5: Database Status
**As a** user who ingested files last week,  
**I want to** run `go-rag status` and see what's in my database,  
**So that** I know what's searchable and what might need updating.

**Acceptance criteria**:
- `go-rag status` shows: total sources, total documents, total chunks, total embeddings
- Storage size on disk
- Last ingestion timestamp
- Watched directories
- Embedding model in use
- Health indicator (OK / degraded)

### US6: Configuration
**As a** user with a non-standard Ollama setup,  
**I want to** run `go-rag config` to view and change settings,  
**So that** I can point go-rag at a remote Ollama instance.

**Acceptance criteria**:
- `go-rag config` prints current configuration
- `go-rag config set <key> <value>` updates settings
- Configurable: `ollama_url`, `ollama_model`, `watch_dir`, `chunk_size`, `chunk_overlap`, `db_path`
- Configuration stored as JSON in `.go-rag/config.json`
- Validates values on set (e.g., URL format, positive integers)

---

## 4. System Architecture

### 4.1 Architecture Diagram

```
                          ┌──────────────────────────┐
                          │      CLI Layer            │
                          │  cobra commands + huh     │
                          │  init | add | scan |      │
                          │  query | status | config  │
                          └────────────┬─────────────┘
                                       │
                          ┌────────────▼─────────────┐
                          │    Orchestration Layer    │
                          │  Pipeline:                │
                          │  Read → Split → Hash →    │
                          │  Dedup → Embed → Store    │
                          └────────────┬─────────────┘
                                       │
        ┌──────────────────────────────┼──────────────────────────────┐
        │                              │                              │
┌───────▼────────┐  ┌─────────────────▼──────┐  ┌─────────────────────▼──┐
│  File Readers   │  │   Embedding Client     │  │    Change Detection    │
│  ┌───────────┐  │  │   ┌────────────────┐   │  │    ┌───────────────┐   │
│  │  pdfcpu    │  │  │   │  Ollama HTTP   │   │  │    │   fsnotify    │   │
│  │  .txt      │  │  │   │  /api/embed    │   │  │    │  (real-time)  │   │
│  │  .md       │  │  │   └────────────────┘   │  │    └───────────────┘   │
│  │  .docx     │  │  │                        │  │    ┌───────────────┐   │
│  │  .jpg/.png │  │  │  Interface:            │  │    │  Polling       │   │
│  │  (extensible)│ │  │  Embedder interface    │  │    │  (SHA-256 cmp) │   │
│  └───────────┘  │  │                        │  │    └───────────────┘   │
└───────┬────────┘  └────────────┬───────────┘  └────────────┬──────────┘
        │                        │                           │
        └────────────────────────┼───────────────────────────┘
                                 │
                    ┌────────────▼───────────────┐
                    │      Core Engine            │
                    │  ┌───────────────────────┐  │
                    │  │  Write path (<10ms)   │  │
                    │  │  Async index workers  │  │
                    │  │  Dedup engine         │  │
                    │  └───────────────────────┘  │
                    └────────────┬───────────────┘
                                 │
        ┌────────────────────────┼────────────────────────┐
        │                        │                        │
┌───────▼──────────┐  ┌─────────▼────────┐  ┌────────────▼──────────┐
│  BM25 FTS Index   │  │  Vector Index     │  │  Metadata Index        │
│  (inverted,       │  │  (chromem-go,     │  │  (source ID → docs,    │
│   field-weighted)  │  │   embedded,       │  │   file path → hash)    │
│                   │  │   in-memory+disk)  │  │                        │
└───────┬──────────┘  └─────────┬────────┘  └────────────┬──────────┘
        │                        │                        │
        └────────────────────────┼────────────────────────┘
                                 │
                    ┌────────────▼───────────────┐
                    │    Pebble KV Store          │
                    │    (embedded LSM-tree)      │
                    │    Single data directory    │
                    │    ~/.go-rag/data/          │
                    └────────────────────────────┘
```

### 4.2 Architectural Principles

1. **Async-after-ACK writes** — Writes validate, commit to Pebble, then ACK in <10ms. All indexing (FTS, vector, metadata) happens asynchronously via buffered channels. Inspired by MuninnDB's write path.
2. **Single Pebble instance** — All data (documents, chunks, embeddings, indexes) lives in one embedded Pebble database. No external services.
3. **Prefix-partitioned key space** — Single-byte prefixes separate logical data types within Pebble. Enables efficient prefix scans and independent index rebuilds.
4. **Content-addressed identity** — SHA-256 hash of content + metadata is the canonical document ID. Idempotent by construction.
5. **Extension interface for file types** — File readers implement a `FileReader` interface. Adding a new format requires one new file + registration.
6. **MCP-first tool exposure** — Every CLI operation is also available as an MCP tool for AI agent consumption. Design for both human and machine consumers from day one.

### 4.3 Retrieval Flow

```
User Query
    │
    ▼
┌─────────────────────────────────────────┐
│ 1. Embed query (Ollama /api/embed)       │
└───────────────┬─────────────────────────┘
                │
    ┌───────────┴───────────┐
    │                       │
    ▼                       ▼
┌──────────────┐    ┌──────────────┐
│ 2a. Vector    │    │ 2b. BM25 FTS │
│ search        │    │ search       │
│ (chromem-go)  │    │ (inverted    │
│ top-K=60      │    │  index)      │
│               │    │ top-K=60     │
└──────┬───────┘    └──────┬───────┘
       │                   │
       └─────────┬─────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│ 3. Reciprocal Rank Fusion (RRF)          │
│    score(d) = Σ 1/(k + rank(d, list_i)) │
│    K_vector = 40, K_fts = 60            │
└───────────────┬─────────────────────────┘
                │
                ▼
┌─────────────────────────────────────────┐
│ 4. Results: top-K chunks with metadata   │
│    score, source file, page, chunk_idx   │
└─────────────────────────────────────────┘
```

### 4.4 Write / Ingestion Flow

```
File added/scanned
    │
    ▼
┌──────────────────────────────────────────┐
│ 1. Read file (pdfcpu / text / docx / etc)│
└───────────────┬──────────────────────────┘
                │
                ▼
┌──────────────────────────────────────────┐
│ 2. Generate SHA-256 content hash          │
│    hash = SHA256(content + metadata)      │
└───────────────┬──────────────────────────┘
                │
                ▼
┌──────────────────────────────────────────┐
│ 3. Dedup check: hash exists?              │
│    Yes → SKIP (idempotent)                │
│    No  → continue                         │
└───────────────┬──────────────────────────┘
                │
                ▼
┌──────────────────────────────────────────┐
│ 4. Split into chunks                      │
│    Default: 512 tokens, 50 token overlap  │
│    Paragraph → sentence → word cascade    │
│    Min chunk: 50 tokens                   │
└───────────────┬──────────────────────────┘
                │
                ▼
┌──────────────────────────────────────────┐
│ 5. Write Source + Document + Chunks        │
│    to Pebble (SYNC, <10ms)                │
└───────────────┬──────────────────────────┘
                │
                ▼
┌──────────────────────────────────────────┐
│ 6. ACK to user                            │
└───────────────┬──────────────────────────┘
                │
                ▼ (async, non-blocking channels)
┌──────────────────────────────────────────┐
│ 7. Background workers:                    │
│    • Embedding worker → Ollama /api/embed │
│    • FTS index worker → BM25 inverted     │
│    • Vector index worker → chromem-go add │
└──────────────────────────────────────────┘
```

---

## 5. CLI Commands

### 5.1 Command Overview

```
go-rag [command] [flags]

Commands:
  init        Initialize a new RAG database
  add         Add files or directories to the database
  scan        Scan for changes (with optional --watch)
  query       Search the database
  status      Show database statistics
  config      View or change configuration

Flags (global):
  --db-path   Path to database directory (default: ./.go-rag)
  --verbose   Enable verbose logging
  --help      Show help
```

### 5.2 `go-rag init`

Initialize a new RAG database in the current directory.

```
go-rag init [flags]

Flags:
  --db-path string       Database path (default: ./.go-rag)
  --ollama-url string    Ollama server URL (default: http://localhost:11434)
  --model string         Embedding model name (auto-detected if omitted)
  --watch-dir string     Directory to watch (default: current directory)
  --chunk-size int       Chunk size in tokens (default: 512)
  --chunk-overlap int    Overlap in tokens (default: 50)

Examples:
  go-rag init
  go-rag init --model nomic-embed-text --watch-dir ~/Documents/papers
  go-rag init --ollama-url http://192.168.1.100:11434
```

**Behavior**:
1. Creates `.go-rag/` directory with `config.json` and `data/` subdirectory
2. Probes Ollama at `--ollama-url` for available models
3. If `--model` not specified, lists embedding-capable models and prompts user to pick one
4. Initializes Pebble database and creates key-space prefixes
5. Prints success message with next steps: `go-rag add <dir>` and `go-rag scan --watch`

### 5.3 `go-rag add`

Add files or directories to the RAG database.

```
go-rag add <path> [flags]

Flags:
  --recursive            Recurse into subdirectories (default: true)
  --glob string          File pattern filter (e.g., "*.pdf")
  --dry-run              Show what would be added without ingesting

Examples:
  go-rag add ./papers/
  go-rag add ~/Documents/ --glob "*.md"
  go-rag add report.pdf
  go-rag add . --dry-run
```

**Behavior**:
1. Walks the path, collecting files matching `--glob` and supported types
2. For each file: computes SHA-256 hash, checks dedup, reads content via appropriate reader
3. Splits text into chunks (512 tokens, 50 overlap)
4. Creates Source → Document → Chunk records in Pebble
5. Queues chunks for async embedding and indexing
6. Reports: `Processed 42 files: 15 new, 24 skipped, 3 errors`

**Progress output**:
```
[1/42] paper1.pdf ................... NEW (12 chunks)
[2/42] paper2.pdf ................... SKIPPED (unchanged)
[3/42] notes.md ..................... NEW (3 chunks)
...
Done. 15 new, 24 skipped, 3 errors.
Embedding 180 chunks in background...
```

### 5.4 `go-rag scan`

Scan watched directories for changes. With `--watch`, runs continuously.

```
go-rag scan [flags]

Flags:
  --watch                Watch for changes continuously
  --poll-interval int    Polling interval in seconds (default: 60)
  --once                 Scan once and exit (default behavior)

Examples:
  go-rag scan
  go-rag scan --watch
  go-rag scan --watch --poll-interval 30
```

**Behavior (once mode)**:
1. Reads the list of tracked files from the database
2. For each file: stat + SHA-256 hash → compare with stored hash
3. Reports added, modified, and deleted files
4. Modified files are re-ingested (old chunks deleted, new chunks embedded)

**Behavior (watch mode)**:
1. Starts fsnotify watcher on configured directories
2. On filesystem events: debounce 500ms, then check SHA-256
3. Periodic polling (every `--poll-interval` seconds) as safety net
4. Runs until SIGINT/SIGTERM

**Output**:
```
[ADDED]   2026-06-19 14:32:01  new-paper.pdf (12 chunks)
[MODIFIED] 2026-06-19 14:35:17  notes.md (3 chunks re-indexed)
[DELETED] 2026-06-19 14:40:55  old-draft.txt
```

### 5.5 `go-rag query`

Search the RAG database.

```
go-rag query <query> [flags]

Flags:
  --k int                Number of results (default: 5)
  --mode string          Retrieval mode: hybrid|semantic|keyword (default: hybrid)
  --format string        Output format: text|json (default: text)
  --source string        Filter by source file glob
  --threshold float      Minimum relevance score (default: 0.0)

Examples:
  go-rag query "how does authentication work"
  go-rag query "transformer architecture" --k 10 --mode semantic
  go-rag query "error handling" --source "*.go" --format json
```

**Behavior**:
1. Embeds the query string via Ollama `/api/embed`
2. Runs parallel retrieval: vector search (chromem-go) + BM25 FTS
3. Fuses results via Reciprocal Rank Fusion (K_vector=40, K_fts=60)
4. Returns top-K chunks with source attribution

**Output (text format)**:
```
Found 5 results for "how does authentication work":

[1] auth.md (score: 0.87)
    The authentication system uses JWT tokens with a 24-hour expiry.
    Refresh tokens are stored in an HTTP-only cookie...

[2] api-design.md (score: 0.72)
    All API endpoints require a valid Bearer token in the Authorization
    header. The middleware validates tokens on every request...

[3] setup-guide.pdf (page 12) (score: 0.65)
    To configure authentication, set the AUTH_SECRET environment variable
    and run the migration to create the users table...
...
```

### 5.6 `go-rag status`

Show database statistics and health.

```
go-rag status [flags]

Flags:
  --json                 Output as JSON

Examples:
  go-rag status
  go-rag status --json
```

**Output**:
```
go-rag database: .go-rag/

  Database
    Path:       /home/user/project/.go-rag/data
    Size:       245 MB
    Pebble:     healthy

  Sources
    Total:      5 directories watched
    Files:      142 tracked

  Documents
    Total:      142 (text: 98, PDF: 32, markdown: 10, image: 2)
    Chunks:     2,840
    Embedded:   2,550 (89.8%)

  Embeddings
    Model:      nomic-embed-text (Ollama)
    Dimension:  768
    Provider:   http://localhost:11434

  Last activity
    Ingested:   2026-06-19 14:32:01
    Queried:    2026-06-19 14:45:22
```

### 5.7 `go-rag config`

View or change configuration.

```
go-rag config [flags]
go-rag config set <key> <value>
go-rag config get <key>

Examples:
  go-rag config
  go-rag config set ollama_model mxbai-embed-large
  go-rag config set chunk_size 1024
  go-rag config get ollama_url
```

**Configurable keys**:

| Key | Default | Description |
|-----|---------|-------------|
| `ollama_url` | `http://localhost:11434` | Ollama server URL |
| `ollama_model` | (auto-detect) | Embedding model name |
| `watch_dirs` | `["."]` | Directories to watch |
| `chunk_size` | `512` | Chunk size in tokens |
| `chunk_overlap` | `50` | Overlap between chunks in tokens |
| `db_path` | `./.go-rag` | Database directory |
| `file_glob` | `*` | Default file pattern for `add` |
| `poll_interval_secs` | `60` | Polling interval for `scan --watch` |

---

## 6. Data Model

### 6.1 Entity Relationship Diagram

```
┌──────────┐       ┌──────────┐       ┌──────────┐       ┌───────────┐
│  Source   │──1:N──│ Document │──1:N──│  Chunk   │──1:1──│ Embedding │
└──────────┘       └──────────┘       └──────────┘       └───────────┘
     │                                       │
     │                                       │ N:M
     │                                       │
     │                               ┌───────▼──────┐
     └───────────────────────────────│    Index     │
                                     │ (FTS + Vec)  │
                                     └──────────────┘
```

### 6.2 Source

Represents a directory or file collection being watched.

```go
type Source struct {
    ID        string    // SHA-256 of canonical path
    Path      string    // Absolute directory path
    Kind      string    // "directory" | "file"
    AddedAt   time.Time
    UpdatedAt time.Time
}
```

**Pebble key prefix**: `0x01`  
**Key format**: `0x01 | source_id`  
**Value**: JSON-serialized Source struct

### 6.3 Document

Represents a single ingested file. Content-addressed via SHA-256 hash.

```go
type Document struct {
    ID          string    // SHA-256(content + metadata) — canonical identity
    SourceID    string    // FK to Source
    FilePath    string    // Relative path from source root
    FileName    string    // Base filename
    FileType    string    // "pdf" | "text" | "markdown" | "docx" | "jpeg" | "png"
    MimeType    string    // e.g., "application/pdf"
    ContentHash string    // SHA-256 of raw file bytes
    Metadata    map[string]interface{}  // File-specific metadata
    ChunkCount  int       // Number of chunks this document was split into
    FileSize    int64     // Bytes
    IngestedAt  time.Time
    UpdatedAt   time.Time
    Status      string    // "pending" | "embedded" | "error"
}
```

**Pebble key prefix**: `0x02`  
**Key format**: `0x02 | document_id`  
**Value**: JSON-serialized Document struct  
**Secondary index**: `0x0A | source_id | document_id` → empty (for listing docs by source)

### 6.4 Chunk

A text segment split from a Document.

```go
type Chunk struct {
    ID             string    // SHA-256(chunk text + metadata)
    DocumentID     string    // FK to Document
    Content        string    // The chunk text
    ChunkIndex     int       // Position in document (0-based)
    TotalChunks    int       // Total chunks in parent document
    StartCharIdx   int       // Character offset in original text
    EndCharIdx     int       // Character offset in original text
    PageNumber     int       // For PDFs, 0 for non-paginated
    PreviousChunkID string   // Linked list: previous chunk (empty if first)
    NextChunkID    string    // Linked list: next chunk (empty if last)
    TokenCount     int       // Approximate token count
    CreatedAt      time.Time
}
```

**Pebble key prefix**: `0x03`  
**Key format**: `0x03 | chunk_id`  
**Value**: JSON-serialized Chunk struct  
**Secondary index**: `0x0B | document_id | chunk_index` → chunk_id (for ordered retrieval)

### 6.5 Embedding

A vector embedding for a Chunk, stored in chromem-go's internal format.

```go
type Embedding struct {
    ChunkID     string      // FK to Chunk (1:1)
    Model       string      // Embedding model name
    Dimensions  int         // Vector dimensions (e.g., 768)
    Vector      []float32   // The embedding vector
    CreatedAt   time.Time
}
```

**Storage**: chromem-go manages its own embedding storage internally (in-memory + optional persistence). The mapping between Chunk and its embedding is stored in Pebble.

**Pebble key prefix**: `0x04`  
**Key format**: `0x04 | chunk_id`  
**Value**: JSON-serialized Embedding metadata (model, dimensions, created_at). The actual vector lives in chromem-go.

### 6.6 Index

Represents the searchable indexes (FTS and vector). Not a stored entity per se — the indexes are built from Chunks and Embeddings.

**FTS (BM25) Index**:
- Inverted index stored in Pebble under prefixes `0x05`–`0x08`
- Terms tokenized from chunk content (case-folded, stop words removed, trigram fallback for short terms)
- Field-weighted: title (3.0x), headings (2.0x), body (1.0x)
- Stores `term → [(chunk_id, positions, field_weight)]` mappings

**Vector Index**:
- Managed by chromem-go in-memory
- HNSW-based approximate nearest neighbor search
- Optional persistence to disk for restart survival

### 6.7 Key-Space Schema Summary

| Prefix | Data | Scope |
|--------|------|-------|
| `0x01` | Source records | Global |
| `0x02` | Document records | Global |
| `0x03` | Chunk records | Global |
| `0x04` | Embedding metadata | Global |
| `0x05`–`0x08` | FTS inverted index | Global |
| `0x09` | Config key-value store | Global |
| `0x0A` | Source → Document secondary index | Global |
| `0x0B` | Document → Chunks ordered index | Global |
| `0x0C` | File path → Document ID lookup | Global |
| `0x0D` | Content hash index (for dedup) | Global |
| `0x0E` | Change detection state | Global |
| `0x0F` | Idempotency receipts | Global |

---

## 7. Document Change Detection

### 7.1 Two-Layer Architecture

go-rag uses a defense-in-depth approach to change detection, inspired by the patterns found in Haystack, LlamaIndex, and MuninnDB's async worker model.

```
Layer 1: fsnotify (real-time, OS-level)
    │
    │  Detects: CREATE, MODIFY, DELETE, RENAME events
    │  Latency:  <100ms
    │  Limitation: Doesn't survive restarts; may miss events under heavy load
    │
    ▼
Layer 2: Periodic Polling (survives restarts, ground truth)
    │
    │  Method:  stat() + SHA-256 comparison
    │  Interval: 60s (configurable)
    │  Limitation: Up to poll_interval delay
    │
    ▼
Content-Addressed ID Comparison (final arbiter)
    │
    │  Method:  SHA-256(content + metadata) vs stored Document.ContentHash
    │  Guarantee: Identical content = identical hash. No false positives.
    │
    ▼
Action: ADD / SKIP / UPDATE / DELETE
```

### 7.2 SHA-256 Content Addressing

Every document's identity is its SHA-256 hash. The hash input includes:

```go
func (d *Document) GenerateID() string {
    h := sha256.New()
    h.Write([]byte(d.Content))           // Full text content
    h.Write([]byte(d.MimeType))          // MIME type
    // Sorted, canonical metadata
    keys := make([]string, 0, len(d.Metadata))
    for k := range d.Metadata {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    for _, k := range keys {
        fmt.Fprintf(h, "%s=%v", k, d.Metadata[k])
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

**Design decision**: Unlike Haystack, go-rag does **not** include the embedding vector in the document ID hash. This follows the LlamaIndex pattern: ID is for identity, a separate `ContentHash` field (SHA-256 of raw file bytes) is for change detection. This allows re-embedding the same content with a different model without creating duplicate documents.

### 7.3 Change Detection State Machine

```
                    ┌──────────┐
                    │ UNKNOWN  │  (file not in DB)
                    └────┬─────┘
                         │
              fsnotify CREATE or polling discovers new file
                         │
                         ▼
                    ┌──────────┐
                    │   NEW    │────► Ingest + embed
                    └──────────┘
                         │
                         │ file ingested
                         ▼
                    ┌──────────┐
                    │ TRACKED  │  (hash stored)
                    └────┬─────┘
                         │
              ┌──────────┼──────────┐
              │          │          │
         hash changed  hash same  file deleted
              │          │          │
              ▼          ▼          ▼
        ┌──────────┐ ┌────────┐ ┌──────────┐
        │ MODIFIED │ │  SKIP  │ │ DELETED  │
        └────┬─────┘ └────────┘ └────┬─────┘
             │                       │
        re-ingest:            remove chunks,
        delete old chunks,    embeddings,
        create new chunks,    and FTS entries
        queue for embedding
             │
             ▼
        ┌──────────┐
        │ TRACKED  │  (new hash stored)
        └──────────┘
```

### 7.4 Implementation Detail

```go
type ChangeDetector struct {
    db         *pebble.DB
    watcher    *fsnotify.Watcher
    pollTicker *time.Ticker
    events     chan FileEvent
}

type FileEvent struct {
    Path      string
    EventType string  // "create", "modify", "delete"
    Hash      string  // Current SHA-256, empty for deletes
    Timestamp time.Time
}

func (cd *ChangeDetector) detectChanges(trackedFiles []string) (added, modified, deleted []string) {
    for _, path := range trackedFiles {
        currentHash := hashFile(path)
        doc, exists := cd.getDocumentByPath(path)
        
        if !exists {
            added = append(added, path)
        } else if doc.ContentHash != currentHash {
            modified = append(modified, path)
        }
    }
    
    // Detect deletes: files in DB but no longer on disk
    allPaths := cd.getAllTrackedPaths()
    for _, dbPath := range allPaths {
        if _, err := os.Stat(dbPath); os.IsNotExist(err) {
            deleted = append(deleted, dbPath)
        }
    }
    return
}
```

### 7.5 fsnotify Debouncing

Filesystem events can fire multiple times for a single logical change (e.g., a save triggers CREATE + MODIFY + CHMOD). go-rag debounces events:

- **500ms debounce window**: Wait for quiet period after last event for a given file
- **Coalescing**: Multiple events for the same file within the window are collapsed into one
- **Hash comparison**: After debounce, only act if SHA-256 actually changed

---

## 8. File Type Support

### 8.1 Extension Interface

All file readers implement the `FileReader` interface. Adding a new format requires implementing a single interface and registering it.

```go
// FileReader extracts text content from a file.
type FileReader interface {
    // SupportedExtensions returns the file extensions this reader handles (e.g., [".pdf"]).
    SupportedExtensions() []string
    
    // SupportedMimeTypes returns MIME types this reader handles.
    SupportedMimeTypes() []string
    
    // Read extracts text content from raw bytes.
    // Returns the full text content and any file-specific metadata.
    Read(ctx context.Context, data []byte, path string) (content string, metadata map[string]interface{}, err error)
    
    // Name returns a human-readable name for this reader ("PDF Reader").
    Name() string
}
```

### 8.2 Built-in Readers

| Format | Reader | Library | Extensions | Key Metadata |
|--------|--------|---------|------------|--------------|
| **PDF** | `PDFReader` | pdfcpu/pdfcpu | `.pdf` | page_count, title, author, page_number per chunk |
| **Plain Text** | `TextReader` | stdlib | `.txt`, `.log`, `.csv` | encoding, line_count |
| **Markdown** | `MarkdownReader` | stdlib | `.md`, `.markdown` | headings, frontmatter parsed into metadata |
| **Word** | `DocxReader` | stdlib (ZIP + XML) | `.docx` | author, created_at, modified_at (from doc props) |
| **JPEG** | `JPEGReader` | stdlib (image/jpeg) | `.jpg`, `.jpeg` | dimensions, exif (optional), OCR required for text |
| **PNG** | `PNGReader` | stdlib (image/png) | `.png` | dimensions, OCR required for text |

### 8.3 PDF Reader (Primary)

PDF is the primary supported format because it's the most common document format in research and enterprise contexts.

**Implementation**: Uses `pdfcpu/pdfcpu` for text extraction.

```go
type PDFReader struct{}

func (r *PDFReader) Read(ctx context.Context, data []byte, path string) (string, map[string]interface{}, error) {
    // Use pdfcpu to extract text page by page
    // Returns concatenated text with page markers
    // Metadata: title, author, subject, keywords, page_count
}
```

**Extraction approach**:
1. Parse PDF with pdfcpu's API
2. Extract text per page
3. Concatenate pages with `\n--- PAGE N ---\n` markers
4. Track page number for each chunk during splitting
5. Extract PDF metadata (title, author) for Document.Metadata

### 8.4 Image Readers (JPEG, PNG)

For v1, image readers extract EXIF metadata and dimensions. Actual text extraction from images (OCR) is deferred to v2 / plugin architecture. Images are stored as Documents with type "image" and their metadata is searchable (filename, dimensions, EXIF tags), but no text content is extracted.

**Future**: Integrate with a Go OCR library or external OCR service via the FileReader extension interface.

### 8.5 Reader Registration

Readers are registered in an `init()` function or via explicit registration:

```go
var registry = make(map[string]FileReader)

func RegisterReader(r FileReader) {
    for _, ext := range r.SupportedExtensions() {
        registry[ext] = r
    }
}

func GetReader(ext string) (FileReader, error) {
    r, ok := registry[strings.ToLower(ext)]
    if !ok {
        return nil, fmt.Errorf("no reader for extension: %s", ext)
    }
    return r, nil
}
```

### 8.6 Adding a New File Type

To add support for a new file type (e.g., `.epub` in the future):

1. Create `internal/reader/epub.go`
2. Implement the `FileReader` interface
3. Call `RegisterReader(&EPUBReader{})` in `init()`
4. No other code changes needed — the pipeline discovers readers via the registry

---

## 9. Recommended Go Libraries

### 9.1 Stack Overview

```
┌───────────────────────────────────────────────┐
│                go-rag Library Stack             │
├───────────────────┬───────────────────────────┤
│ Layer             │ Library                   │
├───────────────────┼───────────────────────────┤
│ CLI Framework     │ spf13/cobra               │
│ Interactive Prompts│ charmbracelet/huh         │
│ Terminal Output   │ pterm/pterm               │
│ PDF Extraction    │ pdfcpu/pdfcpu             │
│ File Watching     │ fsnotify/fsnotify         │
│ Vector Storage    │ philippgille/chromem-go   │
│ Embeddings        │ Ollama (HTTP, no Go lib)  │
│ Storage Engine    │ cockroachdb/pebble        │
│ Content Hash      │ crypto/sha256 (stdlib)    │
│ RAG Orchestration │ cloudwego/eino (optional) │
└───────────────────┴───────────────────────────┘
```

### 9.2 Library Details

| # | Library | Repo | Stars | License | Purpose | Critical? |
|---|---------|------|-------|---------|---------|-----------|
| 1 | **pdfcpu** | pdfcpu/pdfcpu | 8,678 | Apache-2.0 | PDF text extraction, metadata | **Yes** — primary format |
| 2 | **fsnotify** | fsnotify/fsnotify | 10,727 | BSD-3 | Cross-platform file watching | **Yes** — change detection Layer 1 |
| 3 | **chromem-go** | philippgille/chromem-go | 998 | MPL-2.0 | Embedded vector storage + HNSW search | **Yes** — vector retrieval |
| 4 | **Ollama** | ollama/ollama | 174,503 | MIT | Local embedding model serving | **Yes** — embedding generation |
| 5 | **cobra** | spf13/cobra | 44,122 | Apache-2.0 | CLI command framework | **Yes** — CLI structure |
| 6 | **pebble** | cockroachdb/pebble | — | BSD-3 | Embedded LSM-tree KV store | **Yes** — persistence layer |
| 7 | **huh** | charmbracelet/huh | 6,978 | MIT | Terminal forms and prompts | Recommended — setup wizard |
| 8 | **pterm** | pterm/pterm | ~7,000 | MIT | Pretty terminal output | Recommended — progress bars, tables |
| 9 | **eino** | cloudwego/eino | 11,873 | Apache-2.0 | RAG pipeline orchestration | Optional — structured pipeline |
| 10 | **crypto/sha256** | stdlib | — | Go stdlib | Content-addressed IDs | **Yes** — no external dep |

### 9.3 License Compatibility

All recommended libraries use permissive licenses compatible with commercial use:

- **Apache-2.0**: pdfcpu, cobra, eino
- **MIT**: Ollama, huh, pterm
- **MPL-2.0**: chromem-go (weak copyleft, file-level only — does not affect go-rag's license)
- **BSD-3-Clause**: fsnotify, pebble

No GPL, AGPL, or SSPL constraints. go-rag can be licensed under MIT, Apache-2.0, or BSL without conflict.

### 9.4 Libraries Explicitly Avoided

| Library | Reason |
|---------|--------|
| jung-kurt/gofpdf | Archived Nov 2021 |
| milvus-io/milvus-sdk-go | Deprecated and archived (Mar 2025) |
| rsc/pdf | Archived |
| unidoc/unipdf | Commercial/AGPLv3, license conflict risk |
| radovskyb/watcher | Dormant since Oct 2023, polling-only |
| nlpodyssey/cybertron | Dormant since Jun 2024, not production-grade |
| raggo | Stale, no maintenance |
| go-light-rag | Archived |

### 9.5 Pure Go Commitment

The stack is **pure Go** — no CGo, no C libraries, no external runtime dependencies beyond Ollama (which runs as a separate process). This means:

- `CGO_ENABLED=0 go build` produces a fully static binary
- Cross-compilation to any Go target works out of the box
- Deployment is a single file copy
- Works on Linux, macOS, Windows (fsnotify supports all three)

---

## 10. Non-Functional Requirements

### 10.1 Performance

| Requirement | Target | Measurement |
|-------------|--------|-------------|
| Write latency (ACK) | <10ms | Time from `add` call to Pebble commit ACK |
| Embedding latency | <100ms per chunk | Time to call Ollama `/api/embed` + store result |
| Query latency (hybrid) | <500ms for top-5 | End-to-end: embed → search → fuse → return |
| Query latency (keyword only) | <50ms for top-5 | BM25 lookup + sort + return |
| Vector search | <100ms for top-60 | chromem-go HNSW approximate NN search |
| Ingestion throughput | >10 chunks/sec sustained | Embedding work is async; write path is <10ms |
| Cold start (open DB) | <1s | Pebble open + chromem-go index load |

**Rationale**: MuninnDB achieves <10ms write ACK with the same Pebble backend. The async-after-ACK model decouples write latency from indexing work, making it scale independently.

### 10.2 Reliability

| Requirement | Target | Mechanism |
|-------------|--------|-----------|
| Data durability | fsync on every write batch | Pebble Sync writes |
| Crash safety | No data loss on SIGKILL | Pebble WAL recovery |
| Idempotent ingestion | Duplicate add = no-op | SHA-256 content-addressed IDs |
| Change detection accuracy | No false positives | SHA-256 ground truth comparison |
| Graceful shutdown | Complete in-flight writes | SIGINT handler drains queues |

### 10.3 Resource Usage

| Requirement | Target | Context |
|-------------|--------|---------|
| Binary size | <25 MB | Statically linked Go binary |
| Memory (idle) | <50 MB | No active queries or ingestion |
| Memory (active) | <500 MB | Under query load + embedding generation |
| Disk per 1000 docs | ~50–200 MB | Varies with document size and chunk count |
| CPU (idle) | <1% | No background work |
| CPU (ingestion) | Burst to 100% | Embedding generation is CPU-bound via Ollama |

### 10.4 Usability & Compatibility

| Requirement | Target |
|-------------|--------|
| Setup time | <30 seconds from `go-rag init` to first `add` |
| Supported OS | Linux (primary), macOS, Windows |
| Go version | 1.22+ |
| Ollama version | 0.1.0+ (any version with `/api/embed` endpoint) |
| No root required | Runs entirely in user space, no sudo |
| Offline mode | Works without internet if Ollama model already pulled |
| Shell completion | bash, zsh, fish (via cobra) |

---

## 11. Open Questions & Risks

### 11.1 Open Questions (14)

| # | Question | Status | Impact |
|---|----------|--------|--------|
| Q1 | **OCR strategy**: How to handle image text extraction? Defer to v2 or integrate now? | Open | PDFs with scanned pages produce no text |
| Q2 | **Token counting**: Use tiktoken-go or simple word-based heuristic? | Open | Affects chunk sizing accuracy |
| Q3 | **Embedding model migration**: What happens when user changes Ollama model? Re-embed everything? | Open | Could be hours of re-processing |
| Q4 | **chromem-go persistence**: Does chromem-go support disk persistence for restart survival? Needs verification. | Open | Affects cold start time |
| Q5 | **Large PDF memory**: pdfcpu loads full PDF into memory. What's the max file size before OOM? | Open | Affects practical PDF size limit |
| Q6 | **Concurrent add + query**: Can the user add files while querying? Locking strategy needed. | Open | UX: blocking vs eventual consistency |
| Q7 | **Chunk dedup across documents**: If two PDFs share a paragraph, should chunks be deduplicated? | Open | Trade-off: storage vs query precision |
| Q8 | **Query result dedup**: When two chunks from the same document match, should they be collapsed? | Open | UX: duplicate results from same source |
| Q9 | **Database migration**: How to handle schema changes between go-rag versions? | Open | Backward compatibility |
| Q10 | **File deletion handling**: When user deletes a file, should chunks be hard-deleted or soft-deleted? | Open | Recoverability vs disk usage |
| Q11 | **Watch directory recursion depth**: Max depth for fsnotify? Deep trees may hit OS inotify limits. | Open | Linux default: 8192 watches |
| Q12 | **Metadata-only updates**: If only metadata changes (not content), re-embed or skip? | Open | Hash includes metadata, so it would re-embed |
| Q13 | **Embedding dimension mismatch**: What if Ollama model changes dimensions? chromem-go index rebuild? | Open | Index corruption risk |
| Q14 | **MCP tool design**: 35 tools like MuninnDB or minimal 5-6 tools? | Open | Complexity vs capability |

### 11.2 Risks (7 with Mitigations)

| # | Risk | Severity | Likelihood | Mitigation |
|---|------|----------|------------|------------|
| R1 | **pdfcpu text extraction quality**: PDF text extraction is notoriously unreliable. Some PDFs produce garbled or empty output. | High | Medium | Implement fallback chain: pdfcpu → OCR (v2). Surface extraction quality metrics. Allow manual text override. |
| R2 | **Ollama dependency**: go-rag requires Ollama running separately. If Ollama is down, embeddings fail silently (async). | High | Medium | Health check on startup. `status` shows Ollama connectivity. Embedding queue with retry. Clear error messages. |
| R3 | **chromem-go maturity**: 998 stars, MPL-2.0 license. Smaller community than Qdrant/Milvus. API stability? | Medium | Low | Lightweight wrapper interface. If chromem-go becomes problematic, swap backend without changing public API. |
| R4 | **Large file memory pressure**: Loading a 500MB PDF into memory for text extraction could OOM. | Medium | Medium | Stream processing where possible. File size warning above 100MB. Configurable max file size. |
| R5 | **Index rebuild time**: If Pebble database corrupts or user changes embedding model, full re-index of 100K+ chunks could take hours. | Medium | Low | Incremental rebuild. Progress reporting. Resume-from-checkpoint. |
| R6 | **fsnotify event loss under load**: High file creation rates (e.g., unpacking an archive) can overflow the inotify queue. | Medium | Low | Layer 2 polling as safety net. Debounce coalesces bursts. Configurable poll interval. |
| R7 | **Single process limitation**: Only one go-rag process can open the Pebble database at a time (lock-based exclusivity). | Low | Medium | Clear error message. Consider read-only mode for concurrent queries (like MuninnDB Lobes). Document limitation. |

---

## Appendix A: Glossary

| Term | Definition |
|------|------------|
| **BM25** | Best Match 25 — a probabilistic ranking function for full-text search. Field-weighted in go-rag (title 3.0x, headings 2.0x, body 1.0x). |
| **Chunk** | A text segment split from a Document. Default: 512 tokens with 50-token overlap. The unit of retrieval. |
| **chromem-go** | Embedded Go vector database by Philipp Gille. In-memory HNSW index with optional persistence. |
| **Content-addressed ID** | A document identifier derived from its content (SHA-256 hash). Same content = same ID. Enables idempotent ingestion. |
| **Document** | A single ingested file in the database. Contains metadata, content hash, and links to its Chunks. |
| **Embedding** | A dense vector representation of text, generated by an embedding model (e.g., nomic-embed-text via Ollama). Used for semantic similarity search. |
| **fsnotify** | Go library for cross-platform filesystem event notifications (inotify on Linux, FSEvents on macOS, ReadDirectoryChangesW on Windows). |
| **HNSW** | Hierarchical Navigable Small World — an approximate nearest neighbor search algorithm used by chromem-go for vector similarity search. |
| **Idempotent ingestion** | Adding the same file twice produces the same result (no duplicates). Achieved via content-addressed IDs. |
| **LSM-tree** | Log-Structured Merge-tree — the storage data structure used by Pebble. Optimized for write-heavy workloads. |
| **MCP** | Model Context Protocol — a JSON-RPC protocol for AI agents to interact with tools. go-rag exposes its operations as MCP tools. |
| **Ollama** | Local LLM serving platform. go-rag uses Ollama's `/api/embed` endpoint to generate embeddings. |
| **Pebble** | Embedded KV storage engine by CockroachDB. LevelDB-family LSM-tree. Used as go-rag's persistence layer. |
| **pdfcpu** | Pure Go PDF processing library. Used for text extraction from PDFs. |
| **RAG** | Retrieval-Augmented Generation — a technique that retrieves relevant documents and provides them as context to an LLM for grounded answers. |
| **RRF** | Reciprocal Rank Fusion — a method for merging ranked lists from different retrieval strategies without score normalization. `score(d) = Σ 1/(k + rank(d, list_i))`. |
| **Source** | A directory or file collection being tracked by go-rag. Documents belong to a Source. |
| **Token** | A sub-word unit used by LLM tokenizers. Chunk sizes are measured in tokens for compatibility with embedding model context windows. |

---

## Appendix B: Comparison with Existing Systems

### B.1 MuninnDB (Inspiration)

| Aspect | MuninnDB | go-rag |
|--------|----------|--------|
| **Purpose** | Cognitive memory database for AI agents | Local RAG database for documents |
| **Storage unit** | Engram (cognitive memory trace) | Document → Chunk → Embedding |
| **Retrieval** | 6-phase ACTIVATE pipeline (FTS + HNSW + temporal + Hebbian + BFS) | 2-signal RRF (vector + FTS) |
| **Cognitive features** | ACT-R temporal decay, Hebbian learning, contradiction detection, Bayesian confidence | None (v1) |
| **Plugins** | 3-tier (Core → Embed → Enrich) | Extension interface for file types |
| **Protocols** | MBP, REST, gRPC, MCP (4 ports) | CLI + MCP |
| **Scale** | 10K → 100M+ engrams | 100 → 100K documents (v1 target) |
| **License** | BSL 1.1 (provisional patent) | TBD (likely MIT) |

**Key inspiration**: Async-after-ACK write model, single binary with embedded Pebble, prefix-partitioned key space, MCP-first tool design.

### B.2 Haystack / LlamaIndex (Pattern Source)

| Aspect | Haystack/LlamaIndex | go-rag |
|--------|---------------------|--------|
| **Language** | Python | Go |
| **Deployment** | pip install + multiple dependencies | Single binary |
| **Pipeline topology** | DAG (Haystack) / Linear (LlamaIndex) | Linear with optional fan-out |
| **ID generation** | SHA-256 of content (Haystack) / UUID + separate hash (LlamaIndex) | SHA-256 of content + metadata |
| **Change detection** | Not built-in (application concern) | Built-in 2-layer (fsnotify + polling) |
| **Vector store** | Pluggable (ChromaDB, Qdrant, Milvus, etc.) | Embedded chromem-go |
| **Target user** | Python developers building RAG pipelines | CLI users who want local RAG |

**Key adoption**: SHA-256 content-addressed IDs, chunking best practices (512 tokens, token overlap, paragraph-first splitting), duplicate policies (SKIP/OVERWRITE/FAIL).

### B.3 chromem-go vs Alternatives

| Aspect | chromem-go (chosen) | Qdrant (external) | Milvus (external) |
|--------|---------------------|-------------------|-------------------|
| **Type** | Embedded | Client-server | Client-server |
| **Deployment** | None (linked into binary) | Requires Qdrant service | Requires Milvus service |
| **Go library** | philippgille/chromem-go | qdrant/go-client | milvus-io/milvus-sdk-go (DEPRECATED) |
| **Stars** | 998 | 337 | 369 |
| **License** | MPL-2.0 | Apache-2.0 | Apache-2.0 |
| **Fit for go-rag** | Excellent — zero-dependency embedded | Poor — requires separate service | Avoid — SDK deprecated |

### B.4 pdfcpu vs Alternatives

| Aspect | pdfcpu (chosen) | unipdf | ledongthuc/pdf |
|--------|-----------------|--------|----------------|
| **License** | Apache-2.0 | Commercial/AGPLv3 | BSD-3-Clause |
| **Stars** | 8,678 | 3,082 | 605 |
| **Maintenance** | Active (Jun 2026) | Active (May 2026) | Dormant (May 2025) |
| **Extraction quality** | Good | Excellent | Basic (reader only) |
| **Pure Go** | Yes | Yes | Yes |
| **Fit for go-rag** | Best — permissive license, full toolkit, active | Overkill — commercial license restrictions | Too minimal — reader only, stale |

---

*PRD synthesized from MuninnDB architecture reference, RAG best practices research (Haystack + LlamaIndex deep dives), and Go ecosystem survey. All source URLs verified 200 OK. Research completed 2026-06-19.*
