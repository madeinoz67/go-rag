# Feature Specification: Crash-Safe Background Embedder

**Feature Branch**: `030-crash-safe-embedding`

**Created**: 2026-06-25

**Status**: Draft

**Input**: User description: "spec the MuninnDB approach." MuninnDB runs the
**embedder** as a background scan-by-flag processor (the same `RetroactiveProcessor`
machinery as its enricher — `cmd/muninn/server.go` →
`NewRetroactiveProcessor(store, embedPlugin, DigestEmbed)`). On startup it runs an
**initial scan** then polls: it finds records missing the `DigestEmbed` flag,
micro-batches them, embeds, and sets the flag — with a circuit breaker, backoff, and
permanent-fail marking. **The DB itself is the queue.** go-rag's current embed is
already async-after-ACK (Constitution IV) but is **coupled to the per-ingest job**
(an in-memory queue): if the process dies after the durable ACK but before the
embedding is persisted, that job is lost — the chunk is durable but **vector-less**,
silently invisible to semantic search until manually re-ingested. This feature adopts
MuninnDB's model: the embedder runs **decoupled from ingest as a self-healing
background process** — crash-safe (pending work auto-recovers on restart),
cross-document batched, and circuit-breaker-guarded.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Embeddings are crash-safe (Priority: P1)

An operator ingests documents. Today, if the daemon is killed (SIGKILL, power loss,
crash) between the durable ACK and the background embedding landing, the affected
documents are **orphaned**: their chunks are stored, but they have no embedding
vector, so semantic search silently misses them — and nothing recovers them short of
a manual re-ingest. After this feature, a crash mid-embed is **recoverable**: on the
next start, any document whose embedding is pending or unfinished is automatically
detected and re-embedded. No silent recall loss, no manual re-ingest.

**Why this priority**: The single load-bearing benefit. A local-first tool that can
silently lose retrievability on a crash undermines its own thesis (durable, dependable
local storage). Crash-safety is the gap MuninnDB's architecture closes that go-rag's
async-after-ACK-in-memory-queue does not.

**Independent Test**: Ingest a document; kill the process before its embedding
persists; restart; confirm the document is returned by semantic search with no manual
re-ingest (the background embedder recovered it).

**Acceptance Scenarios**:

1. **Given** a document durably ingested but not yet embedded, **When** the process is killed and restarted, **Then** the embedding is automatically generated on startup and the document is retrievable by semantic search.
2. **Given** a corpus with pre-existing orphaned (vector-less) chunks from past crashes, **When** the daemon starts, **Then** they are detected and embedded — recovered without re-ingest.
3. **Given** the background embedder is recovering pending work, **When** a query runs concurrently, **Then** already-embedded documents return normally and not-yet-recovered ones are simply absent (no error, no partial/wrong results) until the embedder catches up.

---

### User Story 2 - Decoupled + batched + resilient (Priority: P2)

The embedder runs as its own background process, independent of the per-document
ingest job, so it can **micro-batch across documents** (bulk ingest of many small
documents hits the backend in fewer, larger calls) and an **embedder failure (backend
unreachable) never stalls ingestion or marks documents errored** — it's
circuit-breaker-guarded and retried. Today a backend outage during ingest marks
documents errored and the per-doc queue has no cross-doc batching.

**Why this priority**: Throughput on bulk ingest + resilience to a flaky backend.
Secondary to crash-safety (US1) but a direct consequence of the decoupled-processor
model that delivers it.

**Independent Test**: Bulk-ingest many small documents with the backend up → fewer
embedding calls than documents (cross-doc batching observed); then with the backend
down → ingestion still ACKs and succeeds, the backlog reflects pending, no infinite
retry.

**Acceptance Scenarios**:

1. **Given** a bulk ingest of many small documents, **When** the background embedder runs, **Then** embeddings are generated in cross-document micro-batches (fewer backend calls than one-per-document).
2. **Given** the embedding backend is unreachable, **When** documents are ingested, **Then** ingestion still ACKs and succeeds (the documents are stored); embedding is deferred and retried, not failed.
3. **Given** repeated embedding failures, **When** the circuit breaker opens, **Then** the embedder fast-fails instead of stalling, and permanently-failed embeddings are marked (not retried indefinitely).

---

### User Story 3 - Compatible + observable (Priority: P3)

The refactor is **purely structural** — existing embeddings, vectors, document/chunk
identity, and retrieval results are unchanged (the embed path just moves behind a
self-healing processor). The background embedder's backlog (pending + failed counts)
is **visible in status**, and the **<10 ms write ACK is preserved**.

**Why this priority**: Required to land safely alongside an existing corpus + the
performance budget. Lower priority than crash-safety (US1) and throughput/resilience
(US2), but a hard gate — any visible behaviour change means the refactor was wrong.

**Independent Test**: Run the full existing suite + eval before/after — identical
results and recall; status reports the embedder backlog; the ACK budget is unchanged.

**Acceptance Scenarios**:

1. **Given** the existing test suite + retrieval-eval harness, **When** run after the refactor, **Then** results and recall are identical — the change is structural (SC-005).
2. **Given** the daemon is running, **When** the operator checks status, **Then** the background embedder's pending and failed counts are reported.
3. **Given** ingestion, **When** measured, **Then** the <10 ms write ACK is unchanged (embedding remains strictly post-ACK/background).

---

### Edge Cases

- **Crash mid-embed (the core case)**: pending work MUST auto-recover on restart — the defining behaviour.
- **Backend unreachable during embed**: circuit breaker fast-fails; the document is NOT errored (ingestion succeeded); embedding is retried when the backend returns.
- **Bulk ingest**: cross-document micro-batching reduces backend calls.
- **Pre-existing orphaned chunks** (from past crashes, pre-feature): detected and recovered on the first run (no special migration).
- **Permanent embed failure** (bad model/config, unparseable): marked permanently failed (not retried indefinitely); surfaced in status.
- **Idempotent re-embed**: re-running on already-embedded content is a no-op (no duplicate vectors, no double work).
- **Concurrent ingest + background embed**: coordination so a document isn't double-embedded or raced.
- **Empty corpus**: the embedder is idle and harmless.
- **Migrate interaction**: `Migrate` (re-embed stale-*model* docs) is distinct from this (recover *missing* embeddings); the two must compose (Migrate marks model-stale; this recovers missing — neither clobbers the other).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST make embedding crash-safe: if the process terminates after a document is durably ingested but before its embedding is persisted, the embedding MUST be automatically (re)generated on the next start — no manual re-ingest, no permanently vector-less (orphaned) chunks.
- **FR-002**: Embedding MUST remain strictly async-after-ACK — the <10 ms write-ACK budget is preserved; the embedder runs in the background.
- **FR-003**: The embedder MUST run as a background process decoupled from the per-ingest write path, so an embedder failure (backend unreachable) does NOT fail ingestion, stall the write path, or mark documents errored.
- **FR-004**: A circuit breaker MUST guard embedding: a failing/unreachable backend fast-fails rather than stalling; permanently-failed embeddings are marked (not retried indefinitely); transient failures are retried.
- **FR-005**: The embedder MUST micro-batch across documents for throughput on bulk ingest (fewer backend calls than one-per-document).
- **FR-006**: Embedding MUST be idempotent — re-running on already-embedded content is a no-op (no duplicate vectors, no redundant work).
- **FR-007**: The background embedder's backlog (pending + permanently-failed counts) MUST be surfaced in status.
- **FR-008**: Existing embeddings, vectors, document/chunk identity, and retrieval results MUST be unchanged by the refactor — it is purely structural (the embed path's scheduling + recovery change; its outputs are identical).

### Key Entities *(include if feature involves data)*

- **Pending-embed state**: the detectable signal that a chunk needs (re)embedding — the thing a crash leaves behind and a restart must find. Whether implemented as a digest/flag bit (MuninnDB) or a "chunk present without an embedding record" scan is a plan decision; the spec requires the outcome (crash-recoverable, idempotent).
- **Background embedder**: the self-healing processor that detects pending-embed work and generates + persists vectors, off the ACK path, with circuit-breaker + cross-doc batching.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A document ingested, then the process killed before its embedding persisted, is fully retrievable by semantic search after a restart — with no manual re-ingest (the background embedder recovered it).
- **SC-002**: The <10 ms write ACK is unchanged with the background embedder enabled (async-after-ACK preserved).
- **SC-003**: With the embedding backend unreachable, ingestion still ACKs and succeeds; the backlog reflects pending; failed embeddings are marked (no infinite loop).
- **SC-004**: A bulk ingest of many small documents embeds with cross-document batching — measurably fewer backend calls than one per document.
- **SC-005**: Existing embeddings, vectors, document/chunk identity, and retrieval results (incl. the retrieval-eval harness recall) are byte-identical before vs after — the refactor is purely structural.

## Assumptions

- **The MuninnDB model is the reference**: embedder as a background scan-for-pending processor (crash recovery via re-scan on startup), circuit breaker, cross-document micro-batching. The exact persistence mechanism for "pending-embed state" — a digest/flag byte (`DigestEmbed`, MuninnDB) vs a "chunk-without-embedding-record" scan over the existing prefixes (`0x03` chunks vs `0x04` embeddings) — is a **plan decision**, not prescribed here. The spec requires the *outcome* (crash-safe, idempotent, recoverable); the plan picks the go-rag-idiomatic mechanism grounded in MuninnDB's design.
- **Distinct from `Migrate` (spec 017/H11)**: Migrate re-embeds *stale-model* documents (drift); this recovers *missing* embeddings (crash orphans / pending work). The two are orthogonal and must compose.
- **Local-only, pure-Go, no new dependency** — reuses the existing embedder/Ollama client, the H12 within-call batching (subsumed by cross-doc micro-batching), and Pebble. Constitution I/III honoured.
- **The <10 ms write ACK is non-negotiable** (Constitution IV); the embedder stays off the ACK path.
- **Out of scope for v1**: hot-swapping the embedder at runtime (MuninnDB's admin.go plugin swap), embedding-provider plugins, and changing the embedding model/dimensionality (that is `Migrate`). Embedding generation/quality is unchanged — only its scheduling, batching, and crash-recovery move.
