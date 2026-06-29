# go-rag × MuninnDB Bridge — Architecture

> **Author**: Stephen Eaton  
> **Status**: Design — implementation tracked in backlog  
> **Location**: `docs/bridge-muninn.md`  
> **Related**: [MuninnDB integration proposal](https://github.com/scrypster/muninndb/discussions), [bridge backlog](docs/bridge-muninn-backlog.md)

---

## Overview

The bridge is a module embedded inside the go-rag binary that continuously promotes document chunks from go-rag into MuninnDB engrams. It is invoked via `go-rag bridge muninn <command>` and runs as a goroutine pool inside the go-rag daemon — no separate process, no additional port, no extra binary.

The design rationale is simple: go-rag and MuninnDB are complementary systems. go-rag retrieves document chunks given an explicit query. MuninnDB remembers knowledge and surfaces it proactively through Hebbian learning, Ebbinghaus temporal decay, and Bayesian confidence. A document chunk promoted to MuninnDB becomes an engram that gains weight the more it's retrieved, surfaces automatically when contextually relevant, and decays naturally when no longer useful. The bridge is the conduit between retrieval and memory.

---

## Design principles

**Single binary.** The bridge runs inside go-rag, not alongside it. The go-rag binary already carries the document store, index, embedding pipeline, REST server, gRPC server, and MCP server. Adding bridge workers as goroutines is consistent with this design. Users run `go-rag start`; bridge workers start automatically if MuninnDB is configured.

**Direct internal access.** The bridge calls go-rag's internal Go APIs (storage reads, event subscriptions) directly — it does not call go-rag's own gRPC endpoint. Calling yourself over the network to read your own Pebble database would be absurd. The boundary is only on the outbound side: MuninnDB is an external process and is reached over gRPC or MBP.

**Correctness before performance.** The sync state store ensures every chunk is either promoted or not — never duplicated, never silently lost. The MuninnDB client retries transient failures. Items that exhaust retries go to a dead-letter log, not /dev/null. An eventually-consistent MuninnDB is acceptable; a MuninnDB with duplicate engrams is not.

**Graceful degradation.** The bridge negotiates MuninnDB capabilities at startup and disables integration patterns whose RPCs are absent, logging one warning per missing capability. The rest of go-rag — query, ingest, MCP tools — is unaffected by MuninnDB availability.

**Minimal footprint.** The bridge state store lives in a reserved Pebble key prefix inside go-rag's existing database. No additional data directory, no separate Pebble instance, no Redis, no Postgres.

---

## Architecture

```
go-rag process
│
├── cmd/go-rag/bridge/muninn/        CLI subcommands
│   ├── init.go                      go-rag bridge muninn init
│   ├── start.go                     go-rag bridge muninn start
│   ├── stop.go                      go-rag bridge muninn stop
│   ├── status.go                    go-rag bridge muninn status
│   ├── sync.go                      go-rag bridge muninn sync
│   ├── promote.go                   go-rag bridge muninn promote
│   └── gaps.go                      go-rag bridge muninn gaps
│
├── internal/events/                 Internal event bus (new)
│   ├── bus.go                       Publish/subscribe, buffered channels
│   └── types.go                     DocumentIngested · DocumentEmbedded · DocumentDeleted
│
├── internal/pipeline/               Existing; emits events on bus after each phase
│
├── internal/bridge/muninn/          Bridge module
│   ├── bridge.go                    Top-level coordinator (Bridge struct)
│   ├── client.go                    MuninnDB gRPC/MBP client wrapper
│   ├── sync.go                      Change-event worker + full-sync worker
│   ├── scheduler.go                 Cron scheduler for full-sync
│   ├── mapper.go                    Chunk → RememberItem translation
│   ├── concept.go                   Concept extraction cascade
│   ├── state.go                     Pebble key-space wrapper (0x20–0x22)
│   └── capabilities.go              MuninnDB feature negotiation + cache
│
├── internal/storage/                Existing Pebble store; bridge reads directly
│
└── proto/muninn/muninn.proto        MuninnDB client stubs (outbound only)
```

### Runtime topology

```
  go-rag daemon
  ┌─────────────────────────────────────────────────────┐
  │                                                       │
  │  Ingest pipeline ──────────► Event bus               │
  │  Embedding worker ─────────► (buffered channel)      │
  │                                    │                  │
  │                         ┌──────────┼──────────┐      │
  │                         ▼          ▼          ▼      │
  │                   Change-event  On-query  Scheduler   │
  │                    worker       hook      (cron)      │
  │                         │          │          │      │
  │                         └──────────┴──────────┘      │
  │                                    │                  │
  │                           Promotion queue             │
  │                           (buffered chan)             │
  │                                    │                  │
  │                    ┌───────────────┼───────────────┐  │
  │                    ▼               ▼               ▼  │
  │                 Worker 1       Worker 2       Worker N │
  │                    │               │               │  │
  │                    └───────────────┼───────────────┘  │
  │                                    │                  │
  │                         ┌──────────▼──────────┐      │
  │                         │   Pebble 0x20–0x22  │      │
  │                         │   (sync state)       │      │
  │                         └─────────────────────┘      │
  │                                                       │
  └──────────────────────────────────────────────────────┘
                              │
                    MuninnDB gRPC :8477
                    (or MBP :8474)
```

---

## Components

### Event bus (`internal/events`)

The event bus is a lightweight in-process publish/subscribe system. It exists to decouple the ingest pipeline from the bridge — the pipeline emits events; the bridge subscribes to them. This replaces the polling loop that would otherwise be required.

```go
type EventType int
const (
    EventDocumentIngested  EventType = iota // metadata committed; embedding pending
    EventDocumentEmbedded                   // async embedding complete — safe to promote
    EventDocumentReIngested                 // file modified and re-ingested
    EventDocumentDeleted                    // file removed from vault
)

type DocumentEvent struct {
    Type       EventType
    DocumentID string
    SourcePath string
    Vault      string
    Before     *model.DocumentMeta // populated on ReIngested
    After      *model.DocumentMeta
    EmittedAt  time.Time
}

type Bus struct { /* channel map, mutex */ }

func (b *Bus) Publish(e DocumentEvent)
func (b *Bus) Subscribe(vault string, types []EventType) <-chan DocumentEvent
func (b *Bus) Unsubscribe(ch <-chan DocumentEvent)
```

**Why `EventDocumentEmbedded` is separate from `EventDocumentIngested`**: go-rag commits document metadata synchronously but runs embedding asynchronously. Promoting a chunk before its vector embedding exists produces an engram without semantic search capability in MuninnDB. The bridge listens only for `EventDocumentEmbedded`, which fires after the embedding worker writes the last chunk vector. This eliminates the `sync_delay_secs` guessing buffer from early designs.

Subscriber channels are buffered (depth 256). A slow bridge worker receives a `ErrSlowConsumer` log warning but does not block the pipeline. The bus is a singleton initialised at daemon startup.

### Sync worker pool (`internal/bridge/muninn/sync.go`)

Four goroutines (configurable via `bridge.muninn.workers`) consume jobs from the promotion queue. Each job is a `PromotionJob`:

```go
type SyncMode int
const (
    ModeChangeEvent SyncMode = iota
    ModeOnQuery
    ModeFullSync
)

type PromotionJob struct {
    DocumentID string
    Vault      string
    Mode       SyncMode
    Priority   int       // higher = processed first; OnQuery > ChangeEvent > FullSync
}
```

Workers pull chunks from go-rag's internal storage directly (`internal/storage.GetDocumentChunks(documentID)`), map them to `RememberItem` structs via the mapper, batch them into groups of ≤50, and call `MuninnClient.BatchRemember`. The Pebble state store is checked before mapping — chunks whose content hash is unchanged since last promotion are skipped.

### MuninnDB client (`internal/bridge/muninn/client.go`)

A wrapper around the generated gRPC stub (or MBP connection) that handles:

- Auth token injection as `Authorization: Bearer <token>` gRPC metadata
- Keepalive pings every 30s
- Exponential backoff on `RESOURCE_EXHAUSTED` (rate limit): 1s, 2s, 4s, 8s, 16s, max 60s ±20% jitter
- Exponential backoff on transient failures: same schedule, up to 5 retries
- Automatic reconnection on connection loss
- Capability cache (populated from `GetServerCapabilities` at startup)

```go
type MuninnClient struct {
    conn    *grpc.ClientConn
    svc     muninnpb.MuninnServiceClient
    healthy atomic.Bool
    caps    *ServerCapabilities
}

func (c *MuninnClient) BatchRemember(ctx context.Context, vault string, items []RememberItem) (*BatchResult, error)
func (c *MuninnClient) PatchEngram(ctx context.Context, vault, id string, patch EngramPatch) error
func (c *MuninnClient) FindByMetadata(ctx context.Context, vault, key, val string) ([]Engram, error)
func (c *MuninnClient) Healthy() bool
func (c *MuninnClient) Capabilities() *ServerCapabilities
```

All bridge components call this wrapper. No component imports the raw gRPC stub directly — the transport is swappable underneath without changing callers.

### State store (`internal/bridge/muninn/state.go`)

The sync state store lives in go-rag's existing Pebble database under reserved key prefixes. No second database instance.

| Prefix | Key format | Value |
|---|---|---|
| `0x20` | `vault_name` | Last `ingested_at` cursor (RFC3339) — used by change-event fallback poll |
| `0x21` | `chunk_id` | `EngramRecord{id, content_hash, promoted_at}` |
| `0x22` | `vault_name \| seq_uint32` | `ErrorEntry` — ring buffer, 1000 entries per vault |

The `0x21` prefix is the idempotency registry. Before promoting a chunk, the worker reads `0x21 | chunk_id`. If found and the content hash matches the current chunk, the chunk is skipped. If found but hash differs (document re-ingested with changes), the engram is updated. If not found, the engram is created.

When MuninnDB's `BatchRemember` gains upsert mode with `idempotency_key`, the `0x21` prefix becomes an optional performance cache rather than a correctness requirement. Without it, the bridge calls `FindByMetadata("chunk_id", id)` on MuninnDB before promoting to check for existing engrams.

### Concept extractor (`internal/bridge/muninn/concept.go`)

Derives the `concept` label for each engram using a priority cascade. No LLM calls — this is rule-based only. LLM enrichment is deferred to MuninnDB's own enrich plugin, which can operate on already-promoted engrams.

```
Priority 1: section_heading from chunk metadata (stripped of # prefix)
            → "Token Refresh Flow"

Priority 2: document title (PDF metadata / docx properties / YAML frontmatter)
            if non-empty and does not look like a filename
            → "JWT Authentication Architecture"

Priority 3: filename without extension + chunk position
            → "auth-design [3/12]"

Priority 4: first 60 chars of chunk content, trimmed to word boundary
            → "Authentication tokens expire after 15 minutes… [3/12]"
```

Position suffix `[chunk_index+1/total_chunks]` is appended whenever `total_chunks > 1`. Single-chunk documents get no suffix. The position suffix is omitted from Priority 1 when the heading itself uniquely identifies the section.

The `section_heading` field is populated by go-rag's readers at ingest time (see GR-021 in the backlog). For markdown, it is the nearest preceding ATX heading. For docx, the nearest preceding Heading-style paragraph. For PDF, the outline entry covering the page range if present.

### Chunk→engram mapper (`internal/bridge/muninn/mapper.go`)

Translates a `model.Chunk` and its parent `model.DocumentMeta` into a `RememberItem`:

| `RememberItem` field | Source | Notes |
|---|---|---|
| `concept` | Concept extractor | See above |
| `content` | `chunk.Content` | Raw chunk text |
| `idempotency_key` | `"chunk:" + chunk.ChunkID` | Scoped to vault |
| `write_mode` | `UPSERT` | Requires MB-003 |
| `tags[0]` | `"go-rag"` | Fixed provenance tag |
| `tags[1]` | vault name | Which go-rag vault |
| `tags[2]` | `chunk.FileType` | pdf · markdown · docx · text |
| `tags[3]` | `"low-quality"` | Added if `extraction_quality < 0.5` |
| `tags[4..]` | `config.extra_tags` | Per-vault user-defined tags |
| `metadata["chunk_id"]` | `chunk.ChunkID` | Idempotency + reverse lookup |
| `metadata["document_id"]` | `chunk.DocumentID` | Parent doc reference |
| `metadata["source"]` | `"go-rag"` | Provenance marker |
| `metadata["gorag_vault"]` | vault name | Cross-system reference |
| `metadata["source_path"]` | `chunk.SourcePath` | Relative path from vault root |
| `metadata["file_type"]` | `chunk.FileType` | |
| `metadata["page_number"]` | `chunk.PageNumber` | PDFs only; `"0"` otherwise |
| `metadata["chunk_index"]` | `chunk.ChunkIndex` | 0-based |
| `metadata["total_chunks"]` | `chunk.TotalChunks` | |
| `metadata["extraction_quality"]` | `chunk.Metadata["extraction_quality"]` | Float string, 0.0–1.0 |
| `metadata["promoted_at"]` | `time.Now().UTC().Format(time.RFC3339)` | |
| `metadata["sync_mode"]` | `"change_event"` · `"on_query"` · `"full_sync"` | Audit trail |

Tags are additive on re-promotion — the mapper does not remove tags MuninnDB may have assigned between syncs.

**Sub-chunking for oversized content**: if `len(chunk.Content) > caps.MaxEngramContentBytes`, the mapper splits at the sentence boundary nearest to the limit. Sub-chunks share the parent concept with `[a]`, `[b]`, `[c]` suffixes appended to the position indicator. Both sub-chunk engram IDs are stored under the parent `chunk_id` in the `0x21` state store.

---

## Sync modes

### Change-event sync (primary)

**Trigger**: `EventDocumentEmbedded` on the internal event bus.

**Flow**:
```
EventDocumentEmbedded received
    → check vault config: enabled? (skip if not)
    → enqueue PromotionJob(documentID, vault, ModeChangeEvent, priority=2)

Worker picks up job:
    → storage.GetDocumentChunks(documentID)     // direct Pebble read
    → for each chunk:
          hash = sha256(chunk.Content)
          if state.GetEngram(chunk.ChunkID).hash == hash: skip
          else: mapper.Map(chunk, doc) → RememberItem
    → client.BatchRemember(vault, items[0:50])  // gRPC/MBP
    → state.SetEngram(chunk.ChunkID, {engramID, hash, now})
```

**Lag**: sub-2s from `EventDocumentEmbedded` to MuninnDB write under normal load.

**On deletion** (`EventDocumentDeleted`): fetch all chunk IDs for the document from the `0x21` state store, apply `orphan_policy` (tag or delete), remove `0x21` entries for that document.

### On-query hook (opt-in, default off)

**Trigger**: Hook in `internal/index.SearchWithRerank()`, called only when `bridge.muninn.on_query.enabled = true`.

**Flow**:
```go
func (idx *Index) SearchWithRerank(ctx context.Context, req SearchRequest) ([]ChunkResult, error) {
    results, err := idx.search(ctx, req)
    if err != nil { return nil, err }

    // non-blocking; caller sees no latency addition
    if idx.bridge != nil && idx.bridge.OnQueryEnabled(req.Vault) {
        go idx.bridge.EnqueueOnQuery(results, req.Vault)
    }

    return results, nil
}
```

Only chunks with `score >= on_query.score_threshold` (default 0.6) and at most `max_promote_per_query` (default 10) chunks are enqueued per query. Jobs have `priority=3` (highest) — they jump the queue ahead of full-sync jobs.

**Purpose**: every time a chunk is promoted via the on-query hook, MuninnDB re-activates its engram. Co-activation with other engrams in the same query batch strengthens Hebbian edges. Over time, the engrams that are genuinely useful to agents gain activation weight and begin surfacing proactively via `Activate` — without the agent knowing the specific concept label.

### Scheduled full sync

**Trigger**: cron schedule per vault (default `0 3 * * *`). Also triggered by `go-rag bridge muninn sync`.

**Flow**:
```
for each enabled vault:
    page_token = ""
    loop:
        docs = storage.ListDocuments(vault, status="embedded", page_size=100, after=page_token)
        for each doc:
            chunks = storage.GetDocumentChunks(doc.ID)
            for each chunk:
                if state.GetEngram(chunk.ChunkID).hash == sha256(chunk.Content): skip
                enqueue PromotionJob(doc.ID, vault, ModeFullSync, priority=1)
        if docs.NextPageToken == "": break
        page_token = docs.NextPageToken
```

Full-sync jobs have `priority=1` (lowest) — they yield queue space to change-event and on-query jobs. A full sync is a recovery net, not the primary path; it should not starve more timely operations.

---

## Idempotency

Chunk identity in go-rag is content-addressed: the `chunk_id` is a SHA-256 of the chunk's content and its document context. The same file content always produces the same `chunk_id`. Re-ingesting an unchanged file produces identical `chunk_id`s.

The bridge's idempotency strategy uses two layers:

**Layer 1 — Local state store (`0x21`)**: fastest path. Before promoting, check `state.GetEngram(chunk_id)`. If the stored content hash matches the current chunk's hash, skip. This is a local Pebble read — zero network calls.

**Layer 2 — MuninnDB upsert (`idempotency_key`)**: correctness guarantee when the state store is lost (wipe, corruption). When `BatchRemember` upsert mode is available (MB-003), the bridge sends `idempotency_key = "chunk:" + chunk_id` with `write_mode = UPSERT`. MuninnDB enforces "one engram per key per vault" server-side. A duplicate promotion updates the existing engram rather than creating a new one.

**Layer 2 fallback — `FindByMetadata`**: when MB-003 is not available but MB-002 is, the bridge calls `FindByMetadata("chunk_id", chunk_id)` before promoting. If an engram is found, the `0x21` entry is rebuilt from the response and promotion is skipped.

**State store loss recovery**: `go-rag bridge muninn migrate` reads all `0x21` entries, verifies each engram still exists in MuninnDB via `GetEngram`, removes stale entries, and optionally vacates the local state store entirely if MB-002 + MB-003 are available.

---

## Transport layer

The bridge communicates with MuninnDB over one of two transports, configurable per deployment:

### gRPC (default, `:8477`)

Standard protobuf-over-HTTP/2. All management operations (`GetServerCapabilities`, `EnsureVault`, `ListEngrams`, `FindByMetadata`, `GetEngram`, `WatchTriggers`) use gRPC regardless of transport setting. gRPC is also the default for write operations (`BatchRemember`, `PatchEngram`).

Proto stubs live at `proto/muninn/muninn.proto` and are compiled into go-rag as a client stub only — go-rag never serves MuninnDB RPCs.

### MBP (opt-in, `:8474`)

MuninnDB's native binary protocol with pipelining support. When `bridge.muninn.transport = "mbp"` is set, write-path operations (`BatchRemember`, `BatchPatch`) use MBP. All other operations remain on gRPC.

MBP's pipelining allows the bridge to have multiple `BatchRemember` calls in-flight simultaneously, rather than waiting for each ACK before sending the next. For a full-sync of 10,000 chunks (200 batches of 50), this reduces transport overhead from ~2.4s (gRPC sequential) to ~400ms (MBP pipelined at depth 8).

```json
{
  "bridge": {
    "muninn": {
      "transport":       "mbp",
      "mbp_addr":        "127.0.0.1:8474",
      "mbp_pipeline_depth": 8
    }
  }
}
```

MBP support requires either a published protocol spec or Go SDK support from MuninnDB (see the integration proposal). The `MuninnClient` interface is transport-agnostic — swapping the transport is a one-file change.

---

## Configuration reference

All bridge config lives under the `bridge.muninn` key in `.go-rag/config.json`. Written by `go-rag bridge muninn init`; readable via `go-rag config get bridge.muninn.*`.

```json
{
  "bridge": {
    "muninn": {
      "addr":              "127.0.0.1:8477",
      "transport":         "grpc",
      "mbp_addr":          "127.0.0.1:8474",
      "mbp_pipeline_depth": 8,
      "token":             "",
      "connect_timeout_ms": 5000,
      "request_timeout_ms": 30000,

      "workers":           4,
      "queue_depth":       10000,

      "defaults": {
        "target_vault":      "go-rag",
        "score_threshold":   0.0,
        "batch_size":        50,
        "orphan_policy":     "tag",
        "auto_create_vault": true,
        "extra_tags":        [],

        "change_event": {
          "enabled":     true,
          "batch_size":  50
        },
        "on_query": {
          "enabled":               false,
          "score_threshold":       0.6,
          "max_promote_per_query": 10
        },
        "full_sync": {
          "enabled":   true,
          "schedule":  "0 3 * * *",
          "on_start":  false,
          "page_size": 100
        }
      },

      "vaults": {
        "cyber-notes": {
          "target_vault":    "security",
          "score_threshold": 0.5,
          "extra_tags":      ["security", "reference"],
          "on_query": {
            "enabled":          true,
            "score_threshold":  0.7
          }
        },
        "personal": {
          "enabled": false
        }
      }
    }
  }
}
```

**Key fields:**

`orphan_policy`: what to do when a go-rag document is deleted. `"tag"` adds `"orphaned"` to the engram's tags and lets Ebbinghaus decay handle eventual fade. `"delete"` removes the engram from MuninnDB immediately. `"ignore"` takes no action. Default: `"tag"`.

`auto_create_vault`: if true, `bridge muninn start` creates the `target_vault` in MuninnDB if it doesn't exist. If false, a missing vault is a fatal startup error. Default: `true`.

`score_threshold`: minimum go-rag chunk score to promote in full-sync and change-event modes. 0.0 promotes all chunks. Useful values depend on the embedding model and vault content; 0.3–0.5 is a reasonable starting point for filtered promotion.

---

## CLI commands

| Command | Description |
|---|---|
| `go-rag bridge muninn init` | Guided wizard: configure address, auth, vault, write config |
| `go-rag bridge muninn init --non-interactive` | Non-interactive mode; all values as flags |
| `go-rag bridge muninn start` | Start bridge workers (integrated into `go-rag start` if configured) |
| `go-rag bridge muninn stop` | Graceful shutdown; drains queue, max 30s |
| `go-rag bridge muninn status` | Per-vault lag, throughput, errors; MuninnDB health |
| `go-rag bridge muninn status --json` | Machine-readable output |
| `go-rag bridge muninn sync` | Trigger manual full sync (foreground, streaming progress) |
| `go-rag bridge muninn sync --vault <name>` | Single vault |
| `go-rag bridge muninn sync --dry-run` | Report what would be promoted without writing |
| `go-rag bridge muninn promote --doc <id>` | Promote a specific document |
| `go-rag bridge muninn promote --path <path>` | Promote by source path |
| `go-rag bridge muninn promote --chunk <id>` | Promote a single chunk |
| `go-rag bridge muninn gaps` | Gap detection: orphaned engrams + undocumented concepts |
| `go-rag bridge muninn gaps --fix-orphans` | Apply orphan policy immediately |
| `go-rag bridge muninn reset` | Wipe sync cursors and engram index (forces full re-sync) |
| `go-rag bridge muninn migrate` | Transition idempotency from local state store to MuninnDB |

---

## Prometheus metrics

Bridge metrics are added to go-rag's existing metrics endpoint (`:7881/metrics` by default). No additional port.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gorag_bridge_sync_lag_seconds` | Gauge | `vault`, `mode` | Seconds behind latest embedded document |
| `gorag_bridge_engrams_promoted_total` | Counter | `vault`, `mode` | Engrams successfully written |
| `gorag_bridge_engrams_updated_total` | Counter | `vault`, `mode` | Existing engrams updated (upsert) |
| `gorag_bridge_engrams_skipped_total` | Counter | `vault`, `reason` | Skipped: `unchanged`, `below_threshold`, `disabled` |
| `gorag_bridge_engrams_failed_total` | Counter | `vault`, `error` | Failed after all retries |
| `gorag_bridge_engrams_split_total` | Counter | `vault` | Chunks split due to max content size |
| `gorag_bridge_muninn_healthy` | Gauge | | 1 = connected and healthy |
| `gorag_bridge_batch_duration_seconds` | Histogram | `vault`, `transport` | BatchRemember call duration |
| `gorag_bridge_rate_limit_total` | Counter | `vault` | MuninnDB rate limit hits |
| `gorag_bridge_hebbian_edges_total` | Counter | `vault` | Explicit Hebbian edges written (Obsidian backlinks) |
| `gorag_bridge_contradictions_total` | Counter | `vault` | Contradiction pairs detected |

---

## Integration patterns

The bridge enables the following patterns, each with its own enablement condition:

| Pattern | Mode | Requires |
|---|---|---|
| Document promotion on ingest | Change-event sync | Default config |
| Usage-weighted Hebbian accumulation | On-query hook | `on_query.enabled: true` |
| Vault seeding / recovery | Full sync | Default config |
| Context expansion (ActivateWithRAG) | On-demand | `GetEngram` (MB-001) |
| Multi-vault semantic routing | On-demand | `Activate` |
| Obsidian backlinks → Hebbian edges | Post-promotion | `StrengthenEdge` (MB-009) + wikilink metadata (GR-020) |
| Contradiction detection | Post-promotion | `AdjustConfidence` (MB-010) |
| Semantic trigger → go-rag query | Reverse/push | `WatchTriggers` (MB-012) |
| Knowledge gap detection | `gaps` command | `ListEngrams` (MB-007) |
| Temporal versioning | Change-event | Document version history (BL-016/BL-017) |

---

## Capability negotiation

On every `bridge muninn start` (and after reconnect), the bridge calls `GetServerCapabilities` and caches the result for the session. Workers check capabilities before calling advanced RPCs:

```go
func (b *Bridge) promoteDocument(ctx context.Context, job PromotionJob) {
    items := b.mapper.Map(chunks, doc)

    if b.client.Capabilities().HasBatchUpsert {
        // set idempotency_key + UPSERT on each item
    } else {
        // fall back to local state store for idempotency
    }

    b.client.BatchRemember(ctx, job.Vault, items)

    if b.client.Capabilities().HasStrengthenEdge {
        b.writeWikilinkEdges(ctx, job.Vault, chunks)
    }
}
```

Missing capabilities are logged once at startup:

```
WARN missing MuninnDB capabilities:
  BatchRemember upsert: local state store required for idempotency
    → upgrade to MuninnDB v0.7+ to fix
  StrengthenEdge: Obsidian backlink → Hebbian edge pattern disabled
  WatchTriggers: semantic trigger → go-rag query pattern disabled
```

The bridge does not fail on missing capabilities; it degrades. The patterns that require them are disabled silently after the startup warning.

---

## Error handling

**BatchRemember failures** are retried with exponential backoff. After 5 retries, the batch is split into individual items and each is retried once more. Items that fail individually are written to the `0x22` error log. Dead-lettered items are visible in `go-rag bridge muninn status`.

**MuninnDB connection loss**: in-flight jobs are held in the promotion queue (up to `queue_depth`). On reconnect, the queue drains normally. If the queue fills, change-event and full-sync workers pause until space is available — they do not drop events.

**go-rag daemon restart**: the event bus subscription is re-established on next `bridge muninn start`. The `0x20` cursor records the last successfully promoted `ingested_at` timestamp; the change-event worker picks up from there. Documents promoted during the outage are caught by the next full sync.

---

## Known limitations and open questions

**Q1**: Does MuninnDB's `BatchRemember` count each item in a batch as a Hebbian co-activation event, or process the batch atomically? This affects whether rapid bulk promotion creates spurious Hebbian associations between unrelated chunks.

**Q2**: What is MuninnDB's current maximum content size per engram? This determines whether sub-chunking is needed in practice, and what the typical split rate will be for PDF-heavy vaults.

**Q3**: When `UPSERT` updates an existing engram, does it reset the `access_count` or accumulate it? The bridge assumes accumulation (re-promotion counts as an access for Hebbian weight purposes).

**Q4**: The on-query hook fires on every query, including queries that return no relevant results. Should it only fire when at least one result exceeds `score_threshold`? Current design: yes, threshold-filtered, so zero-result queries produce zero promotions regardless.

**Q5**: `FindByMetadata` requires a secondary index on `metadata["chunk_id"]` in MuninnDB's Pebble store to be viable at scale. Without the index, idempotency fallback (layer 2 without MB-003) degrades to a full vault scan. The bridge should detect this and warn if `FindByMetadata` is slow.

---

*Architecture authored 2026-06-25. Implementation tracked in [bridge backlog](docs/bridge-muninn-backlog.md). Upstream API requirements tracked in the [MuninnDB integration proposal](https://github.com/scrypster/muninndb/discussions).*
