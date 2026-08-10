# Data Model — Spec 060 MuninnDB Bridge

**Scope**: the entities the bridge introduces or consumes inside go-rag. The MuninnDB-side engram shape is governed by MuninnDB's own schema; go-rag only constructs `WriteRequest` values and reads `Read`/`Activate` responses. See [contracts/muninn-grpc-client.md](contracts/muninn-grpc-client.md) for the wire shapes.

v1 is **stateless**: the bridge holds no durable per-chunk records in go-rag's Pebble store. The only persistent go-rag state is the `BridgeConfig` below. All promotion state lives in MuninnDB (the `idempotent_id` forward index is the correctness layer).

---

## E1 — BridgeConfig (persistent, `.go-rag/config.json`)

**Flat fields on `config.Config` with a `Bridge*` prefix** — matches the codebase precedent (`EnrichmentEnabled/Model/Provider/Endpoint/APIKey` + `EffectiveEnrichmentEnabled()`), NOT a nested object (research.md R9). A `EffectiveBridgeEnabled()` method resolves absent ⇒ default-off. Written by `go-rag bridge muninn init`.

| Config field | Type | Default | Notes |
|---|---|---|---|
| `BridgeEnabled` | bool | `false` | Master switch. OFF ⇒ no egress, no bridgeProc, no processJob enqueue (FR-001). |
| `BridgeEndpoint` | string | `"127.0.0.1:8477"` | MuninnDB gRPC endpoint. **Loopback-only** — `net.ParseIP` at validation + `grpc.WithContextDialer` refusal at dial (FR-002, defense vs DNS rebinding). |
| `BridgeSourceVault` | string | `"default"` | The go-rag vault being bridged. |
| `BridgeTargetVault` | string | `"go-rag"` | The dedicated MuninnDB vault engrams land in (FR-004). Auto-created on first write. |
| `BridgeMaxInFlight` | int | 8 | Storm-limit: max concurrent `BatchWrite` calls (FR-013/NFR-006). |
| `BridgeRatePerSec` | int | 0 | Token-bucket cap on promotions/sec; 0 = off. |
| `BridgeWorkers` | int | 4 | bridgeProc pool size. |
| `BridgeBatchSize` | int | 50 | Chunks per `BatchWrite` (MuninnDB max 50). |

**Target vault key (the `mk_` secret)** — NOT a config field. Read from env **`GORAG_BRIDGE_TOKEN`** at start, mirroring the existing `GORAG_ADMIN_PASSWORD` pattern (referenced via env, never inlined in config.json, never logged). This reconciles FR-003's "referenced, never inlined" with the codebase reality that all other secrets are inline plaintext — env-reference is the one mechanism that already exists (research.md R9). Carried as `Authorization: Bearer` gRPC metadata by the outbound interceptor.

**Backfill state** (FR-012/013/014): `BridgeBackfillAutoOnEnable` bool `true`; pause is an in-memory flag on the bridge coordinator (E6), not config. (Per-process pause/resume; restart re-walks — free under the UPSERT no-op.)

**Validation rules** (`Config.Validate`, beside the existing Ollama URL check): `BridgeEndpoint` resolves to loopback; `GORAG_BRIDGE_TOKEN` non-empty when `BridgeEnabled`; `BridgeTargetVault` non-empty; `1 ≤ BridgeWorkers ≤ 64`; `BridgeBatchSize ≤ 50`.

---

## E2 — Promotion seam: processJob enqueue + decoupled bridgeProc

The bridge does **not** subscribe to the event bus. (research.md R9: the bus is document-level with no chunk content; enrichment hooks `processJob` directly, and that is the right seam for an in-process promoter.)

**Incremental promotion** — a 2-line hook in `Pipeline.processJob` (`internal/pipeline/workers.go`), mirroring how `p.OnNotifyEmbed()` pokes the embedder:

```
// inside processJob, after enrichDocument, chunks already in hand as j.chunks:
if p.bridge != nil {
    p.bridge.Enqueue(ws, j.docID, j.vault, j.chunks, ModeChangeEvent)  // non-blocking
}
```

`Enqueue` is non-blocking (buffered channel; sheds/backpressures when full — FR-011). The pipeline worker never blocks on MuninnDB egress (unlike enrich, the bridge calls an external process that may be down, so it must not run inline).

**Decoupled `bridgeProc`** — `internal/bridge/muninn/processor.go` (new), peer to `internal/embedproc.Processor`:
- goroutine pool (size `BridgeWorkers`) drains the promotion queue;
- bounded by a `BridgeMaxInFlight` semaphore + `BridgeRatePerSec` token bucket (FR-013/NFR-006);
- circuit breaker against MuninnDB (3-state, mirrors `internal/enrich/circuit.go`);
- `Stop()` drains with a bounded timeout (the `embedproc` lesson — `select { case <-done: case <-time.After(drainTimeout) }`, abandons in-flight on timeout; safe because v1 is stateless + the UPSERT no-op makes a later re-walk free).

Started in `Engine.pipeline` gated by `EffectiveBridgeEnabled()`; stopped in `Engine.Close` between `bus.Close()` and `embedProc.Stop()` (it snapshots chunks from `j.chunks`, so it must drain before `pipe.Close()` waits out the pipeline).

**Backfill promotion** — a separate walker goroutine started on enable (E6), reads `ListDocuments` + `GetDocumentChunks` from `internal/storage`, enqueues `PromotionJob{Mode:Backfill}` at lower priority. Same bridgeProc drain path.

---

## E3 — PromotionJob (in-memory only)

```go
type SyncMode int
const (
    ModeChangeEvent SyncMode = iota  // triggered by EventEmbedded
    ModeBackfill                      // auto-on-enable corpus walk
)

type PromotionJob struct {
    DocumentID string
    WS         [8]byte   // vault SipHash prefix (for storage reads)
    Vault      string    // source go-rag vault name
    Mode       SyncMode
    Priority   int       // ChangeEvent(2) > Backfill(1)
}
```

Jobs are not durable — loss on daemon restart is acceptable because (a) `EventEmbedded` for in-flight docs re-fires on the next ingest of that doc, and (b) the UPSERT no-op makes a backfill re-walk free (stateless correctness, research.md R8).

---

## E4 — Chunk → WriteRequest mapping (mapper; the load-bearing contract)

For each `model.Chunk` (+ parent `model.DocumentMeta`), the mapper builds a `muninn_v1.WriteRequest`. Maintainer-confirmed invariants (research.md R4) are **non-negotiable**:

| `WriteRequest` field | Source | Value |
|---|---|---|
| `concept` | concept cascade (E5) | e.g. `"Token Refresh Flow [3/12]"` |
| `content` | `chunk.Content` | raw chunk text |
| `embedding` | — | **`nil`** (invariant — MuninnDB re-embeds; mismatched dims silently break vector search) |
| `stability` | — | **`30.0`** (invariant — Ebbinghaus anchor for reference material) |
| `confidence` | — | `1.0` (default) |
| `vault` | config | `target_vault` |
| `idempotent_id` | `"chunk:" + chunk.ChunkID` | the content-addressed key (UPSERT pins on this) |
| `upsert_mode` | — | **`true`** |
| `memory_type` | — | reference/fact type code (MuninnDB enum) |
| `type_label` | — | `"go-rag-chunk"` |
| `tags` | provenance | `["go-rag", source_vault, chunk.FileType]` + `"low-quality"` if `extraction_quality < 0.5` |
| `associations` | wikilinks (BL-004, shipped) | one per outbound wikilink, `rel_type=references`, `weight=0.6–0.8` |

**Metadata block** (carried in tags/associations as MuninnDB permits): `chunk_id`, `document_id`, `source_path`, `file_type`, `chunk_index`, `total_chunks`, `extraction_quality` — for reverse lookup and the Memory & Graph view.

**Cognitive-hygiene invariant (NFR-002)**: because `idempotent_id` is content-addressed and `embedding` is nil, re-promoting an unchanged chunk sends a byte-identical `WriteRequest` ⇒ MuninnDB leaves the existing engram alone (no `access_count`/weight bump). Verified via `Read` before/after.

---

## E5 — Concept cascade (rule-based, no LLM)

Priority order (RFC §concept.go, fed by shipped BL-004/005/006 metadata):
1. `chunk.SectionContext` (nearest heading) — stripped of `#`.
2. Document title (frontmatter / PDF metadata) if non-empty and not filename-like.
3. Filename without extension + `[index+1/total]` when `total > 1`.
4. First 60 chars of `chunk.Content`, trimmed to a word boundary + position suffix.

LLM enrichment is deferred to MuninnDB's own enrich plugin (operates on already-promoted engrams).

---

## E6 — BackfillState (in-memory; not durable)

```go
type BackfillState struct {
    Running   bool
    Paused    bool
    Cursor    string   // last docID paged (in-memory; restart re-walks from start, UPSERT-no-op makes it free)
    Promoted  int64
    Skipped   int64
    Failed    int64
    StartedAt time.Time
}
```

Reported via `go-rag bridge muninn status` and the console (FR-017). Pause/resume (FR-014) toggles `Paused`; the worker park-checks it between pages.

---

## E7 — MemoryGraphProjection (read path; the view's model)

The view (Q3 = live target-vault graph) does NOT re-derive a graph go-rag-side. It projects MuninnDB reads:

- **Browse**: `Activate(target_vault, context=…)` streaming `ActivateResponse` → list of `{engram_id, concept, score, last_access}`.
- **Detail**: `Read(target_vault, engram_id)` → full engram `{concept, content, tags, access_count, stability, state, created_at, updated_at}`.
- **Edges**: visible via the engram's `associations` (returned by `Read`) — wikilink/reference edges the mapper wrote.

No `ListEngrams` RPC exists upstream (research.md R5), so v1 is browse/detail-driven, not a full enumerate. A complete vault inventory waits on MuninnDB #557/#558.

---

## What is explicitly NOT in the go-rag data model (v1)

- **No `0x21` engram-record store** — the RFC's idempotency registry is obsolete under UPSERT (research.md R2). Server-side dedup is the correctness layer.
- **No `0x20` cursor** — backfill resume is in-memory; restart re-walks (free under the no-op).
- **No `0x22` error ring** — failed promotions are logged + surfaced via status; a durable dead-letter is a follow-up if real traffic justifies it.
- **No on-query co-retrieval edges** — the Hebbian on-query hook is stubbed off in v1 (touches `SearchWithRerank`); it returns in a follow-up.
