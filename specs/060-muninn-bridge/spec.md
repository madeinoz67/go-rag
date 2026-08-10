# Feature Specification: MuninnDB Bridge + Memory & Graph View

**Feature Branch**: `060-muninn-bridge`

**Created**: 2026-08-10

**Status**: Draft

**Input**: Promote go-rag document chunks into MuninnDB as engrams (content-addressed UPSERT), and retire the last console placeholder (`memory-graph`) with a real view over the bridge's read path. Transport gRPC; pure Go; one spec covering bridge core + view.

## Decisions Settled Before Specify

These were resolved with the principal (Stephen) upstream of this spec and are recorded here so planning does not re-litigate them:

- **Upstream dependency is met.** MuninnDB issue #556 (UPSERT write mode) merged as PR #659 (`e4d6ad21`, 2026-08-03). Verified surface (see `research.md` R1): `WriteRequest.idempotent_id` (field 7) + `upsert_mode: true` (field 12, bool), pinned via a durable forward index at prefix `0x2F`/`0x30`, cross-surface proto/gRPC/REST/MCP/CLI. **No `upsert_key` field; no Outcome enum** — `WriteResponse` returns only `{id, created_at}`.
- **The shipped UPSERT semantics are safe for this bridge.** Verified verbatim from the engine + MCP docs: same `idempotent_id` + identical content ⇒ **left alone** (strict no-op, touches no cognitive state); same key + changed content ⇒ **EVOLVED** (supersede). Promoted chunks are content-addressed, so the key is `"chunk:"+chunkID`. An unchanged chunk re-promoted across restarts/re-ingest ⇒ left alone (no-op). A changed chunk gets a new `chunk_id` (Principle II) ⇒ a new key ⇒ `CREATED`, never the EVOLVED path. The bridge never produces the case where a changed-content UPSERT would forge a learning signal. The no-op is verified via `Read` (assert `access_count`/`updated_at` unchanged), not via a response outcome.
- **Transport = gRPC** (typed, pure-Go `grpc-go` + a generated client; no CGo). Not MCP-over-HTTP.
- **Scope = one spec**: bridge core + the Memory & Graph view retire together.
- **The bridge is egress.** go-rag is local-first/air-gapped (constitution Principle I). The bridge is an opt-in, background, loopback-only exception — the same shape of carve-out as document enrichment (spec 029) and the management console (spec 046). A PRD §2.2 non-goal revision is required (see below).

### Resolved During Specify (principal, 2026-08-10)

- **Q1 — Backfill trigger: auto-on-enable (option B), storm-limited and pausable.** Enabling the bridge on an existing vault starts the backfill automatically, but it is rate-bounded (max-in-flight / token-bucket) and the operator can pause/resume. This keeps the one-step UX of auto-enable while preventing a corpus storm on a large vault. (FR-012/FR-013/FR-014, US2.)
- **Q2 — Vault topology: dedicated source and target vaults with a target vault key (option A).** The bridge maps one dedicated go-rag source vault to one dedicated MuninnDB target vault, authenticated with that target vault's `mk_` key (referenced from config, never inlined, never logged). No mixing with the operator's personal memory. (FR-004.)
- **Q3 — Memory & Graph view: live MuninnDB entity graph scoped to the bridged target vault (option A, confirmed).** The view projects what is actually in the target vault rather than a go-rag-side re-derivation.

## User Scenarios & Testing *(mandatory)*

The "user" throughout is the single operator (Stephen) driving the management console and CLI. Every story is independently shippable.

### User Story 1 - Opt-in Bridge Promotes New Chunks to Memory (Priority: P1)

When the operator enables the bridge and points it at a local MuninnDB instance, every document added or re-ingested thereafter has its chunks promoted into MuninnDB as engrams — keyed by content identity, so re-adding the same document is a no-op at the memory store. The bridge is off by default; a fresh install never attempts egress.

**Why this priority**: This is the whole point of the feature — go-rag becomes a first-class producer for the operator's memory store, and content-addressed UPSERT makes "no duplicate memories, no forged reinforcement" a server-enforced guarantee instead of client-side best-effort. Without it, the other two stories have nothing to feed or display.

**Independent Test**: Enable the bridge against a scratch MuninnDB vault, add a document, and confirm the document's chunks are retrievable as engrams in that vault. Re-ingest the same document and confirm the store reports a no-op for every chunk.

**Acceptance Scenarios**:

1. **Given** the bridge is disabled, **When** the operator adds a document, **Then** no egress to MuninnDB occurs and ingest behaves exactly as today.
2. **Given** the bridge is enabled and pointed at a reachable loopback MuninnDB, **When** the operator adds a new document, **Then** each chunk lands as one engram in the target vault, retrievable as a memory.
3. **Given** a document already promoted, **When** the operator re-ingests the unchanged document, **Then** the memory store reports a no-op for every chunk and mutates no cognitive state (no access reinforcement, no duplicate engrams).
4. **Given** a document already promoted, **When** the document's content changes and is re-ingested, **Then** the changed chunks produce fresh engrams (new identity), leaving the prior engrams intact.
5. **Given** the bridge is enabled but MuninnDB is unreachable, **When** the operator adds a document, **Then** the write ACKs in the normal budget, the document is fully usable in go-rag, and the bridge records degraded status without throwing at the operator.

---

### User Story 2 - Auto-Backfill on Enable, Storm-Limited and Pausable (Priority: P2)

When the operator enables the bridge on a vault that already holds an ingested corpus, the backfill starts automatically — no separate command needed — but it is rate-limited so it cannot storm MuninnDB, and the operator can pause and resume it at will. The memory store thus reflects the full vault without a manual kick and without saturating the target.

**Why this priority**: Without auto-backfill, enabling the bridge on an existing vault leaves the memory graph thin until traffic catches up. Auto-on-enable removes the manual step; storm-limiting and pause keep a 10k-chunk vault safe and operator-controllable. Ships after US1 (promotion) since it depends on the same write path.

**Independent Test**: On a scratch vault with N existing documents, enable the bridge and confirm the backfill begins automatically, promotes all N documents' chunks at the configured rate, and can be paused mid-run and resumed to completion without duplicates.

**Acceptance Scenarios**:

1. **Given** a vault with existing documents and the bridge disabled, **When** the operator enables the bridge, **Then** a backfill of the existing corpus begins automatically, bounded by the configured rate/concurrency caps.
2. **Given** a backfill running, **When** the operator pauses it, **Then** promotion stops promptly, the backfill holds its place, and the console shows paused state.
3. **Given** a paused backfill, **When** the operator resumes, **Then** it continues from where it paused and produces no duplicate engrams for already-promoted chunks.
4. **Given** a backfill running on a large vault, **When** the operator runs a foreground query, **Then** query latency stays within its normal budget (the backfill's caps hold).
5. **Given** a backfill running, **When** the operator adds a new document concurrently, **Then** both the backfill and the new-document promotion complete without interfering with each other.

---

### User Story 3 - Memory & Graph View (Priority: P3)

The last console placeholder (`memory-graph`) is retired. The 9th sidebar item becomes a live view that reads the bridged MuninnDB vault and presents its engrams and entity relationships, so the operator can see what go-rag has contributed to memory without leaving the console.

**Why this priority**: Pure visibility — valuable but dependent on US1 producing data. It is the read surface over the bridge and retires the final placeholder, but it ships last.

**Independent Test**: With the bridge enabled and at least one document promoted, open the Memory & Graph view and confirm it renders engrams and their entity edges from the target vault.

**Acceptance Scenarios**:

1. **Given** promoted engrams exist in the target vault, **When** the operator opens the Memory & Graph view, **Then** engrams and their entity relationships render.
2. **Given** the bridge is disabled or MuninnDB is unreachable, **When** the operator opens the view, **Then** a clear degraded/empty state renders (not a crash, not a hang).
3. **Given** an unauthenticated request, **When** the view's endpoint is hit without a valid session, **Then** it is refused (spec 045 Bearer guard).

---

### Edge Cases

- MuninnDB reachable but rejecting writes (auth failure, missing vault, schema mismatch) → bridge logs the rejection, surfaces it in the console, and continues; never crashes the daemon.
- MuninnDB vault is wiped while the bridge is enabled → subsequent promotions recreate engrams as `CREATED` (correct — the store is source of truth).
- Concurrent ingest of the same document from two paths → exactly one set of engrams (server-enforced dedup on the content key).
- Very large backfill → bounded worker concurrency; resumable; does not exhaust memory or starve foreground queries.
- Bridge enabled with no/invalid MuninnDB config → a clear configuration error at enable time, not a silent no-op later.
- Promotion of a chunk whose embedding model later changes → content identity is unchanged by re-embedding, so the engram stays the same (no spurious duplicate) — consistent with constitution Principle II's identity/change-detection distinction.
- Daemon shutdown while promotions are in flight → shutdown completes promptly; in-flight promotions are shed without wedging `stop` (cf. the spec 045 embedproc-drain lesson).

## Requirements *(mandatory)*

### Functional Requirements

**Configuration & enablement**

- **FR-001**: The operator MUST be able to enable and disable the bridge via go-rag configuration; it MUST be OFF by default, and a fresh install with no memory store configured MUST never attempt egress.
- **FR-002**: The bridge MUST accept only a loopback memory-store endpoint; non-loopback endpoints MUST be refused at configuration-validation time.
- **FR-003**: The bridge MUST authenticate to MuninnDB using a credential supplied via a header or config field — never embedded in a URL, never logged.
- **FR-004**: The bridge MUST be configurable with a dedicated source go-rag vault and a dedicated target MuninnDB vault, plus the target MuninnDB vault's key (referenced from config, never inlined, never logged). Promotion MUST be scoped to this source→target pair only — no mixing with the operator's personal memory.

**Promotion (write path)**

- **FR-005**: When enabled, the bridge MUST promote each chunk of an added or re-ingested document into the target MuninnDB vault as one engram, keyed by a content-addressed identity derived from that chunk.
- **FR-006**: Promotion MUST be idempotent at the memory store: re-promoting an unchanged chunk MUST be a strict no-op that mutates no cognitive state (no access reinforcement, no weight change, no duplicate engram).
- **FR-007**: Promotion MUST occur asynchronously AFTER the write acknowledgement. The write-ACK budget MUST be unaffected by the bridge's presence, absence, or latency.
- **FR-008**: A chunk whose content changed (new identity) MUST produce a fresh engram, not an in-place mutation.
- **FR-009**: The bridge MUST report per-promotion outcomes (created vs no-op vs failed) to the observability surface.

**Resilience**

- **FR-010**: If MuninnDB is unreachable or returns errors, the bridge MUST degrade gracefully: log, surface degraded status, and continue serving every core go-rag operation (ingest, index, query, console) without interruption.
- **FR-011**: The bridge MUST NOT accumulate unbounded promotion work when MuninnDB is down — it MUST bound its queue or shed load, and NEVER block a write ACK or foreground operation.

**Backfill**

- **FR-012**: When the bridge is first enabled on a vault with an existing corpus, it MUST automatically begin a backfill that promotes the already-ingested chunks to the target MuninnDB vault — no separate operator command required to start it.
- **FR-013 (storm-limit)**: The backfill MUST be rate-bounded — a configurable maximum of promotions in flight and/or a token-bucket throttle — so enabling the bridge on a large vault never saturates MuninnDB or starves foreground go-rag operations.
- **FR-014 (pause/resume)**: The operator MUST be able to pause and resume the backfill at any time (CLI and/or console). A paused backfill MUST hold its place so resumption continues without duplicates and without re-walking already-promoted chunks.

**Memory & Graph view (read path)**

- **FR-015**: The console MUST ship a Memory & Graph view (9th sidebar item) that reads the bridged MuninnDB vault and presents its engrams and entity relationships. This MUST retire the `memory-graph` placeholder.
- **FR-016**: The Memory & Graph view endpoint MUST be guarded by the spec 045 Bearer session.
- **FR-017**: Bridge health, promotion status, and backfill progress (including paused state) MUST be visible in the console alongside the existing Operations and Observability surfaces.

**Governance**

- **FR-018**: The bridge MUST be recorded as an opt-in egress exception via a PRD §2.2 non-goal revision (background, loopback-only, never a core operation) — mirroring the enrichment (spec 029 / N4) and console (spec 046 / N7) carve-outs.

### Non-Functional Requirements

- **NFR-001 (Performance — write budget)**: Write ACK latency MUST remain within the constitution's `<10ms` budget whether MuninnDB is up, down, slow, or mis-configured (Principle IV).
- **NFR-002 (Cognitive hygiene — verified property)**: The idempotent no-op MUST be a verified property, not an aspiration: a concurrent re-promotion of an unchanged chunk leaves MuninnDB cognitive state byte-identical. This MUST be asserted by a test that exercises N concurrent re-promotions.
- **NFR-003 (Pure Go)**: The bridge MUST ship under `CGO_ENABLED=0`; the MuninnDB client and all its transitive dependencies MUST be pure Go and permissively licensed (constitution Principle III).
- **NFR-004 (No remote egress)**: Only loopback endpoints are permitted; the binary MUST NOT initiate a connection to a non-loopback memory store.
- **NFR-005 (Daemon stability)**: MuninnDB failure or in-flight promotion at shutdown MUST NEVER wedge `go-rag stop`; the shutdown path MUST drain or shed promotion work within the daemon's existing stop budget.
- **NFR-006 (Storm-limit, measurable)**: Backfill concurrency and promotion rate MUST be bounded by configurable caps; with caps set, a backfill on a very large vault MUST keep MuninnDB promotion traffic at or below the configured rate and MUST NOT degrade foreground query latency beyond its normal budget.

### Key Entities *(include if feature involves data)*

- **Bridge Promotion**: the act of mapping one go-rag chunk to one MuninnDB engram. Carries: source chunk content identity, target vault, outcome (created / no-op / failed), and timestamp.
- **Bridge Configuration**: endpoint (loopback only), source go-rag vault, target MuninnDB vault name, target MuninnDB vault key (referenced, never inlined), enabled flag, and backfill controls (rate/concurrency caps, paused state). Lives in the existing go-rag config file.
- **Memory Graph Node**: a MuninnDB engram that originated from a promoted go-rag chunk.
- **Memory Graph Edge**: a MuninnDB entity relationship among promoted engrams (projected read-only in the view).

## PRD §2.2 Non-Goal Revision *(feature-specific)*

This feature revises PRD §2.2 to permit a narrow egress exception, consistent with two established precedents:

- **Precedent A — enrichment (spec 029, revised N4):** background, opt-in, local-model document enrichment is allowed despite the local-first posture.
- **Precedent B — console (spec 046, revised N7):** a single-operator management console is allowed despite the original "no web UI" non-goal.

The bridge adds a third carve-out of the same shape:

> An **opt-in, background, loopback-only** bridge to a local MuninnDB memory store is permitted. It MUST be OFF by default; it MUST never be a core operation (ingest, index, query never depend on it); it MUST refuse non-loopback endpoints; and all write-path work MUST happen async-after-ACK so the `<10ms` budget is unaffected.

This is a PRD behavior change, not a constitution amendment: constitution Principle I ("no network egress for any core operation") stands unchanged because the bridge is not a core operation. The §2.2 revision will be applied to `docs/internals/PRD_RAG_Database.md` as part of this spec's implementation.

## Constitution & Keyspace Considerations *(feature-specific)*

Recorded here so `/speckit-plan`'s Constitution Check gate and the storage-discipline rule have them up front:

- **Principle I (Local-First):** compliant — opt-in, background, loopback-only, never a core operation. Requires the §2.2 revision above, not a principle amendment.
- **Principle II (Content-Addressed Identity):** the bridge's UPSERT key derives from chunk content identity — this is exactly why the no-op works and why ACCUMULATE is safe. Compliant by construction.
- **Principle III (Pure Go):** gRPC client is pure Go (`grpc-go` + generated proto). Adds permissively-licensed (Apache-2.0) dependencies. No CGo.
- **Principle IV (Async-After-ACK):** promotion fires async after ACK. Compliant (NFR-001).
- **Principle V (Extension by Interface):** the bridge is a new engine-adjacent capability behind an interface; the Memory & Graph view is a UI adapter over its read path. Compliant.
- **Keyspace / migration:** the v1 design intends **no new on-disk go-rag keyspace** — promotion is stateless against MuninnDB as the source of truth, and re-promotion is absorbed by the no-op. If planning determines a local promoted-chunk cache is needed for performance, that cache MUST add a numbered, idempotent migration in `internal/storage/migrate`, bump `migrate.ExpectedVersion`, update PRD §6.7, and register the prefix in `docs/internals/keyspace-registry.md`. The plan MUST state the schema-version impact either way.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With the bridge enabled, adding a document makes that document's chunks retrievable as memories in the target store within seconds, with no measurable impact on write responsiveness versus the bridge-disabled baseline.
- **SC-002**: Re-ingesting an unchanged document produces zero duplicate memories and zero reinforcement of existing ones — verifiable by the store reporting a no-op outcome for every chunk, and by cognitive state being byte-identical before and after a concurrent re-promotion.
- **SC-003**: With the memory store offline, every go-rag ingest/query/console operation remains fully functional and within normal latency budgets; the bridge surfaces a degraded status without erroring at the operator.
- **SC-004**: An operator can backfill an existing vault and observe the full corpus populate the memory store; the Memory & Graph view renders the store's engrams and their entity relationships.
- **SC-005**: On a fresh install with no memory store configured, go-rag never initiates an outbound connection; the bridge is inert until explicitly configured and enabled.

## Assumptions

- The MuninnDB UPSERT surface (PR #659) is the target; content-addressed keys make the merged ACCUMULATE semantics safe for this use case (changed chunk → new key → `CREATED`, never `UPDATED_CHANGED`).
- Transport is gRPC (settled), adding a pure-Go `grpc-go` + generated client and a proto dependency. No CGo, no MCP-over-HTTP.
- The "user" is the single operator; the console is the single-operator management console (spec 046).
- Default v1 promotion is **stateless** — no durable local promoted-chunk store; MuninnDB is the source of truth and absorbs re-promotion via the no-op. An optional in-process cache may skip known-promoted chunks within a process lifetime, but nothing durable is added unless planning proves it necessary.
- Incremental promotion subscribes to the existing add/reingest engine events (the same seam enrichment and the pipeline use); initial-corpus promotion is auto-on-enable (backfill), storm-limited and pausable per Q1.
- Loopback-only is enforced at config-validation (refuse any non-loopback endpoint).
- The Memory & Graph view's read path queries the target MuninnDB vault directly; go-rag does not duplicate the memory graph locally.

## Open Questions *(for the plan phase)*

All three specify-time questions are resolved (see "Resolved During Specify" above). The items below are for `/speckit-plan`, not the principal:

- Exact storm-limit defaults (max-in-flight, token-bucket rate) — pick sensible defaults in plan, expose as config knobs.
- Whether stateless v1 promotion needs a durable promoted-chunk marker for backfill resume-after-daemon-restart. Pause/resume within a process is required (FR-014); resume across a daemon restart is a plan-phase decision that, if taken, triggers the storage-discipline migration rule (numbered migration + `ExpectedVersion` bump + keyspace-registry entry).
