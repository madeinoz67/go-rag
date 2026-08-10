# Phase 0 Research — Spec 060 MuninnDB Bridge

**Compiled**: 2026-08-10 · **Method**: live-code verification (constitution: live code wins; memory/docs drift)

This research resolves every technical unknown needed to plan the bridge. The single most important finding is that a **complete prior architecture already exists** (`docs/RFC-bridge-muninndb/bridge-muninn.md`, 2026-06-25, post-review 2026-06-30) and the go-rag-side prep it required is **already shipped**. Spec 060 builds on that RFC; it does not reinvent it. The second finding is that the shipped UPSERT surface differs from what spec 060's draft (and project memory) recorded — corrected below.

---

## R1 — The shipped UPSERT surface (corrected from spec 060 draft)

**Decision**: the bridge writes with `WriteRequest{ idempotent_id, upsert_mode: true, ... }`. Not `upsert_key`. No `Outcome` enum.

**Rationale (verified from live code at muninndb `e4d6ad21`, the #659 merge commit)**:

- `WriteRequest` proto fields (verbatim): `concept(1) content(2) tags(3) confidence(4) stability(5) vault(6) idempotent_id(7) associations(8) embedding(9) memory_type(10) type_label(11) upsert_mode(12)`.
- `WriteResponse`: `id(1) created_at(2)` — **only**. There is no Outcome field; the response cannot tell created-vs-no-op-vs-evolved apart.
- The gRPC service is `MuninnDB` on `:8477`. RPCs: `Hello, Write, BatchWrite, Read, Forget, BatchForget, Stat, Link, Activate(stream), ListVaults, AdjustConfidence, Subscribe(stream)`.
- The durable forward index that pins engram→key lives at prefix `0x2F`/`0x30` in MuninnDB (internal to MuninnDB; the bridge does not touch it). Keyed by `sha256(idempotent_id)`.

**Semantics (verbatim from `internal/mcp/tools.go` field doc + engine tests)**:
> "With op_id set, keep one stable memory per key across repeated writes: created on first use, and on later writes with the SAME op_id either **left alone (identical content)** or **EVOLVED (changed content — a new version supersedes the old one, which stays retrievable as history)**."

- `upsert_mode` without `idempotent_id` is rejected (`invalid params: 'upsert_mode' requires 'op_id'`) — fail-loud.
- The evolve step inherits tags/confidence/trust from the predecessor; only content/concept/importance come from the new call.

**Implication for the bridge (content-addressed chunks)**:
- Unchanged chunk re-promoted → same `idempotent_id` ("chunk:"+chunkID) + identical content → **left alone** (no-op). NFR-002 cognitive hygiene holds.
- Changed chunk in go-rag → new `chunk_id` (content-addressed, Principle II) → new `idempotent_id` → **CREATED** (never the evolve path). The ACCUMULATE-vs-RESET debate is moot for this bridge; content-addressing sidesteps it.
- NFR-002's verification mechanism is **`Read` before/after** (assert `access_count`, `updated_at`, `last_access` unchanged), **not** a response Outcome field. Spec 060's draft assumed an Outcome enum — corrected in the plan.

**Alternatives considered**: MCP-over-HTTP (uses `op_id`+`upsert_mode` param names) — rejected; Stephen chose gRPC (typed, pure-Go). MBP — deferred by the maintainer review (gRPC is v1).

---

## R2 — Prior architecture exists: build on `bridge-muninn.md`, do not reinvent

**Decision**: spec 060's plan adopts the RFC's component layout, mapper, and config shape, updated for (a) the now-shipped UPSERT (stateless-v1 option), (b) Stephen's auto-on-enable + storm-limit + pause decision, and (c) the Memory & Graph view (new since the RFC).

**Rationale**: `docs/RFC-bridge-muninndb/bridge-muninn.md` is a complete, maintainer-reviewed architecture. The maintainer **approved** the bridge on 2026-06-30 with the framing *go-rag retrieves, MuninnDB remembers*. Key components already designed:

- `internal/events/` — in-process pub/sub (`DocumentIngested`, `DocumentEmbedded`, `DocumentReIngested`, `DocumentDeleted`). The bridge subscribes to `DocumentEmbedded` (not `Ingested`) so chunks promote only after their vector exists — eliminates a sync-delay guess. **Status: needs to be confirmed whether this bus exists yet (Explore agent checking) or is new work.**
- `internal/bridge/muninn/` — `bridge.go` (coordinator), `client.go` (gRPC wrapper + Bearer metadata + backoff), `sync.go` (worker pool), `mapper.go` (chunk→WriteRequest), `concept.go` (rule-based concept cascade), `state.go` (optional Pebble state), `capabilities.go` (Hello negotiation).
- Mapper (load-bearing, maintainer-confirmed invariants): `embedding: nil` (MuninnDB re-embeds; mismatched dims silently break vector search), `stability: 30.0`, `idempotency_key` → `idempotent_id = "chunk:"+chunkID`, `upsert_mode: true`, tags `[go-rag, vault, file_type, ...]`, full `metadata` block for reverse lookup.
- Sync modes: change-event (primary, on `DocumentEmbedded`), full-sync (recovery, cron + manual), on-query (opt-in Hebbian hook in `SearchWithRerank`).
- CLI: `go-rag bridge muninn {init,start,stop,status,sync,promote,gaps,reset,migrate}`.
- Metrics: 11 prometheus instruments under `gorag_bridge_*` on the existing `:7881/metrics`.

**What the RFC planned that UPSERT now obsoletes (stateless-v1 option)**:
- The RFC's `0x21` idempotency registry was a correctness requirement with `FindByMetadata` fallback. With UPSERT shipped, server-side dedup on `idempotent_id` is the correctness layer; `0x21` becomes an optional perf cache. **Stephen's spec 060 default is stateless v1** (no new go-rag keyspace, no migration) — the plan honours that, with the local cache as an explicit later option.
- Keyspace: the RFC reserved `0x20–0x22` (cursor / engram-record / error-ring). These are **still free** in the current registry (`0x1C–0xFE` free; `0x20–0x22` never allocated). If v1 later adds the perf cache, those bytes remain the natural reservation — but v1 ships no new prefix.

**Alternatives considered**: design the bridge from scratch — rejected, ~84KB of prior design + maintainer review exists. Ignore the RFC — rejected, it carries the maintainer's load-bearing write invariants.

---

## R3 — go-rag-side prep (BL-001..006) already shipped

**Decision**: no prep work; the bridge consumes an existing surface.

**Rationale** (from `bridge-map-post-review.md` §7): specs 035 (`GetChunk`), 036 (Wikilinks), 037 (`GetChunkContext`), 038 (`BatchGetChunks`), 041 (`section_depth`), 042 (`extraction_quality`) all shipped to `main` in v0.3.0 (2026-07-02). The mapper's `concept.go` cascade (section_heading → title → filename+position → first-60-chars) and the wikilink→`Link` edge source both read fields that already exist on `model.Chunk.metadata`.

---

## R4 — Maintainer write invariants (load-bearing)

From the 2026-06-30 review, verbatim in intent. The mapper MUST:

1. **`embedding: nil`** — never pass go-rag's vectors. Unless dims match MuninnDB's exactly, passing them silently breaks MuninnDB's vector search.
2. **`stability: 30.0`** — the Ebbinghaus anchor for reference material (default is tuned for conversational memory).
3. **Hebbian weights**: `0.6–0.8` for explicit wikilink/backlink edges (`Link`); `0.1–0.2` for on-query co-retrieval edges. Wikilinks are editorial intent; co-retrieval is a weak correlation.
4. **Transport: gRPC only for v1** — skip MBP until MuninnDB publishes frame types.

---

## R5 — Memory & Graph view read path (Q3 = live target-vault graph)

**Decision**: the view calls MuninnDB `Activate`/`Read`/`Link`-derived reads scoped to the target vault; it does not re-derive a graph go-rag-side.

**Rationale**: MuninnDB exposes `Read(id)` (full engram incl. access_count, tags, state), `Activate` (streaming activation — the recall surface), and `Link` edges. The entity graph is MuninnDB's own; go-rag projects it read-only. There is no `ListEngrams` RPC (that needs MuninnDB #557/#558-class work, still open) — so the v1 view is `Activate`-driven (search/browsing by context) + `Read` for detail, not a full enumerate. Full enumerate is a known gap (RFC §4: knowledge-gap detection partial).

**Implication**: FR-012 (Memory & Graph view) v1 renders an `Activate`-driven browse + per-engram `Read` detail, scoped to the target vault. A full vault enumerate waits on upstream `ListEngrams`.

---

## R6 — Auth (outbound) and loopback enforcement

**Decision**: the bridge carries the target vault's `mk_` key as `Authorization: Bearer <key>` gRPC metadata (`metadata.New` + `credentials.PerRPCCredentials` or a unary interceptor). Loopback-only enforced at config-validation.

**Rationale**: matches the RFC `client.go` design and spec 060 FR-002/FR-003. MuninnDB gRPC auth is Bearer-token in metadata. The key is referenced from `.go-rag/config.json` (`bridge.muninn.token`, never in a URL, never logged). go-rag has no existing *outbound* gRPC client today (it is a gRPC server only) — the bridge is the first outbound gRPC client, so the plan adds a `grpc.Dial` + interceptor pattern.

---

## R7 — Constitution alignment (no amendment; PRD §2.2 revision only)

All five principles verified against live code/RFC:

- **I (Local-First)**: the bridge is opt-in, background, loopback-only, never a core op. Principle I stands unchanged. PRD §2.2 carve-out required (drafted in spec 060). ✓
- **II (Content-Addressed Identity)**: `idempotent_id = "chunk:"+chunkID` derives from content identity — this is why the no-op works. ✓
- **III (Pure Go)**: `grpc-go` + generated `muninn_v1` client stub, Apache-2.0, no CGo. Adds one proto dependency. ✓
- **IV (Async-After-ACK)**: promotion fires off `DocumentEmbedded` async; `<10ms` write budget unaffected (RFC worker pool, NFR-001). ✓
- **V (Extension by Interface)**: `MuninnClient` interface is transport-agnostic; the view is a UI adapter. ✓

**Keyspace/schema-version impact**: **none for v1** (stateless — no new prefix, no migration, no `ExpectedVersion` bump). If the optional perf cache lands later, it allocates from `0x20–0x22` (still free) with a numbered migration + registry row at that time.

---

## R8 — Open items deferred to plan/tasks (not the principal)

- Storm-limit defaults (max-in-flight, token-bucket rate) — pick in plan, expose as config (`bridge.muninn.workers`, `queue_depth`, new `rate_limit`).
- Backfill resume-after-daemon-restart: pause/resume within a process is required (FR-014); cross-restart resume either (a) re-walks and relies on UPSERT no-op (stateless, simple) or (b) adds a durable cursor at `0x20`. Default: (a) stateless re-walk — the UPSERT no-op makes it free.
- Whether `internal/events` exists yet or is new work — confirmed by the Explore agent (pending its report); the RFC marks it "(new)".
- On-query Hebbian hook (RFC §on-query) — opt-in, default off; v1 may ship disabled with the seam stubbed, given it touches `SearchWithRerank`.

---

## R9 — go-rag seam findings (Explore agent, code-grounded)

Confirmed from live code (the agent hit a Gortex wedge and recovered via `git show`; findings are file:symbol-accurate):

**Seam (corrects the spec's "subscribes to events" assumption)**. Enrichment does NOT use the event bus — it hooks `Pipeline.processJob` directly (`internal/pipeline/workers.go::enrichDocument`), where `j.chunks` is already in memory. The bus (`internal/events/bus.go`) is document-level only: `DocumentEvent.After = model.Document` (no chunks); `ChunkDelta` carries IDs without content; `EventReingested` is reserved-but-not-emitted in the spec 040 MVP. So an in-process promoter that needs chunk content has two paths: (i) hook `processJob` like enrich (chunks in hand, no round-trip), or (ii) subscribe to the bus + fetch each chunk by ID. **(i) is the right seam.** But — unlike enrich (which calls a local Ollama) — the bridge calls an *external* process that may be down, so it must NOT block the pipeline worker. Architecture: `processJob` snapshots `j.chunks` → non-blocking enqueue to a **decoupled `bridgeProc`** (the `internal/embedproc.Processor` pattern, not the enrich-inline pattern). The bridgeProc drains with bounded concurrency + circuit breaker + drain-timeout.

**Config (corrects the "nested `bridge.muninn` object" assumption)**. `internal/config/config.go::Config` is **flat fields with a shared prefix** — enrichment = `EnrichmentEnabled/Model/Provider/Endpoint/APIKey` + `EffectiveEnrichmentEnabled()`. No nested-subsystem substruct exists. The bridge follows the flat precedent: `BridgeEnabled/BridgeEndpoint/BridgeSourceVault/BridgeTargetVault/BridgeTargetVaultKey/BridgeMaxInFlight/BridgeRatePerSec` + `EffectiveBridgeEnabled()`, with loopback validation in `Config.Validate` (net is already imported).

**Secret handling (corrects "referenced, never inlined")**. Every existing key (`captioning_api_key`, `enrichment_api_key`, `rerank_api_key`, `mcp_token`) is stored **inline plaintext** in config.json — there is no "referenced" mechanism. The spec's "referenced, never inlined, never logged" is a new discipline. Reconciliation: read the target vault key from env `GORAG_BRIDGE_TOKEN` (mirrors the existing `GORAG_ADMIN_PASSWORD` pattern — referenced via env, not inlined in config, never logged). This satisfies the intent without inventing a new config mechanism.

**UI graduation**. Sidebar is hardcoded HTML in `internal/ui/web/templates/index.html` (9 nav-items; the 9th, line ~202, targets `memory-graph`; placeholder render at lines ~1288-1292). To graduate: (1) delete `"memory-graph"` from `placeholderViews` (`internal/ui/placeholder.go`), (2) add `mux.HandleFunc("GET /api/memory-graph/...", s.guard(s.handleMemoryGraph))` in `ui.go::Handler`, (3) replace the placeholder `<section>` with a real Alpine view. Precedents: `internal/ui/observability.go` (054), `settings.go`/`system.go` (055/056). **Naming gotcha**: `internal/ui/bridgeops.go` is the Operations view (spec 049), NOT this bridge — the handler must NOT be named `bridge*` (use `memory_graph.go`).

**Daemon lifecycle / drain**. `Engine.Close` (`internal/engine/engine.go:326`) order: `bus.Close()` → `embedProc.Stop()` → `pipe.Close()` → drop indexes → flush caches. The spec 045 embedproc-drain lesson is codified at `internal/embedproc/processor.go:95-125`: `select { case <-done: case <-time.After(drainTimeout) }` (`drainTimeout=5s`), abandons the worker on timeout with a slog.Warn; recovery works because the 0x14 queue is durable. The bridge's `bridgeProc.Stop` MUST mirror this bounded drain (NFR-005). Since v1 promotion is stateless (no durable queue), abandoned in-flight promotions are simply lost — safe under the UPSERT no-op (the next backfill re-walk redisCOVERS them; must be documented).

**Outbound gRPC (first in the repo)**. No `grpc.NewClient`/`grpc.Dial` exists in go-rag today (server-only). `grpc-go` is already a dependency (server). The bridge is the first outbound client. Bearer injection via `grpc.WithUnaryInterceptor` attaching `metadata.Pairs("authorization", "Bearer "+key)` (mirrors the server-side interceptor shape). Loopback enforcement: `net.ParseIP(host)` at config-validation **plus** `grpc.WithContextDialer` refusing non-loopback at dial time (defense-in-depth — DNS could otherwise resolve a loopback hostname to a public IP). Verify no transitive CGo via the generated `muninn_v1` proto package (Principle III).

---

## Source artifacts

- `docs/RFC-bridge-muninndb/bridge-muninn.md` — the architecture (authoritative for component layout/mapper/config)
- `docs/RFC-bridge-muninndb/bridge-map-post-review.md` — the maintainer-review reconciliation (where the two disagree, this one is current)
- `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md` / `muninndb-bridge-backlog.md` — BL/MB item tracking
- muninndb `proto/muninn/v1/service.proto` @ `e4d6ad21` — the gRPC contract (verified verbatim)
- muninndb `internal/engine/engine.go` + `internal/mcp/tools.go` @ `e4d6ad21` — UPSERT semantics (verified)
- `docs/internals/keyspace-registry.md` — prefix allocation (0x20–0x22 free)
