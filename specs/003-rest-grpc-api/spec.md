# Feature Specification: Multi-Transport Server APIs (REST + gRPC + MCP)

**Feature Branch**: `003-rest-grpc-api`

**Created**: 2026-06-20

**Status**: Draft

**Input**: User description: "i now want to look at implementing REST and gRPC apis added when server is started" → refined to: "research how MuninnDB architects this and spec the same."

> **Reference architecture: MuninnDB** (`scrypster/muninndb`, `docs/architecture.md`).
> MuninnDB exposes every operation through an **interface layer of protocol
> adapters that all funnel into one unified engine** — "they are interface
> adapters, not separate implementations." Each transport is a thin adapter
> (`internal/transport/{rest,grpc}/engine_adapter.go`) over the same core, so a
> record written via one protocol is immediately retrievable via any other. Each
> protocol listens on its own loopback port by default, with per-service address
> overrides. This spec adopts that exact shape for go-rag.
>
> **Scope note — what we copy and what we defer.** go-rag copies MuninnDB's
> *architecture* (adapters-over-one-engine, per-port loopback, cross-transport
> consistency, single-instance server) for a **v1 transport set of REST + gRPC +
> MCP**. MuninnDB's **MBP** (native binary protocol), **Web UI**, and
> **vault-scoped API keys + TLS** are deliberately deferred — MBP because gRPC
> already covers typed high-throughput clients and the constitution favors
> simplicity; Web UI and auth/TLS because PRD §2.2 marks them out of scope for v1
> (local-trusted model). Each deferral is logged in Assumptions with rationale.
> This extends the per-vault daemon from spec `002-document-vaults`
> (`go-rag --vault <name> start`) and the constitution's Principle V (MCP-First);
> MCP remains first-class, not replaced.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One Engine, Many Transports: Query From Anything (Priority: P1) 🎯 MVP

A developer starts the go-rag server once. From that point, the same corpus is
queryable over REST (HTTP/JSON, curl-friendly), gRPC (typed, polyglot), and MCP
(JSON-RPC, for AI agents) — and all three return identical results because they
are adapters over a single shared engine, exactly as MuninnDB does it. The
defining property is **transport equivalence**: there is one query engine, and
the protocols are just ways in.

**Why this priority**: This is the heart of the MuninnDB pattern being copied.
"Multiple protocols" is worthless if they disagree; the value is one engine
served uniformly. The read path (query) is the lowest-risk, highest-frequency
operation and proves the adapter-over-engine architecture end to end.

**Independent Test**: Start the server, issue the same query over REST, gRPC,
and MCP, and assert the three result sets are identical — and identical to what
`go-rag query` returns for the same input.

**Acceptance Scenarios**:

1. **Given** an ingested corpus and a running server, **When** a client sends a
   query over REST (HTTP/JSON), **Then** it receives ranked results with source
   file, page number, and relevance score.
2. **Given** the same server and query, **When** a client sends it over gRPC,
   **Then** it receives results identical to the REST response.
3. **Given** the same server and query, **When** an AI agent issues it over MCP,
   **Then** it receives the same results as REST and gRPC — all three adapters
   read from one engine and agree.
4. **Given** a query supporting hybrid, keyword-only, and vector-only modes with
   a top-K limit and a source/path filter, **When** a client selects any mode
   over any transport, **Then** the engine honours it identically across
   transports.

---

### User Story 2 - Write Once, Read Anywhere: Full Surface, Cross-Transport (Priority: P2)

A developer or automation pipeline manages the corpus over the API — adds files,
inspects status — and relies on MuninnDB's cross-transport guarantee: a document
ingested over REST is immediately queryable over gRPC or MCP, and vice versa. The
full operation surface the CLI exposes is available uniformly across transports,
with go-rag's async-after-ACK write contract preserved at the API boundary.

**Why this priority**: Read-only is half a product. Write-path parity plus
cross-transport read-after-write is what makes the API a true alternative to the
CLI and proves the adapters share not just the read engine but the write path and
the single writer. It sits second because writes carry idempotency and
single-writer concerns the read path does not.

**Independent Test**: Over REST, add a known file and confirm via a gRPC status
request that the corpus grew and the new document is queryable over gRPC; then
re-add the same file over MCP and confirm it is skipped as unchanged.

**Acceptance Scenarios**:

1. **Given** a running server, **When** a client adds a file/directory over REST,
   **Then** supported files ingest, unchanged ones skip, and a new/skipped/errored
   summary returns — matching the CLI `add` report.
2. **Given** a document just ingested over REST, **When** a different client
   immediately queries for it over gRPC or MCP, **Then** it is already retrievable
   — cross-transport read-after-write holds (the MuninnDB guarantee).
3. **Given** a file already ingested unchanged, **When** a client re-adds it over
   any transport, **Then** it is skipped with no duplicate documents or embeddings
   (content-addressed idempotency preserved across the API boundary).
4. **Given** a running server, **When** a client requests status over any
   transport, **Then** it receives corpus counts, storage size, active embedding
   model, and embedding-service reachability — the same view the CLI `status`
   shows.
5. **Given** the embedding service (Ollama) temporarily unreachable, **When** a
   client adds over the API, **Then** the request ACKs promptly (async-after-ACK)
   and indexes when the service returns; the client is never blocked on embedding
   latency.

---

### User Story 3 - A MuninnDB-Style Server: Loopback Ports, One Writer, Clean Lifecycle (Priority: P3)

An operator runs the go-rag server as a long-lived local service, MuninnDB-style:
each protocol on its own loopback port with per-service address overrides, a
single instance enforced so go-rag's one-writer invariant holds, many concurrent
clients brokered through that one writer, and a health/metrics surface plus clean
shutdown. This is what separates an API endpoint from a service an operator can
leave running unattended.

**Why this priority**: Per-port binding, single-instance enforcement,
concurrency, observability, and graceful lifecycle are the operational layer that
makes the adapter architecture trustworthy in production. They matter only once
read and write paths exist, so P3 — but they are required before the server is
run unattended.

**Independent Test**: Start the server (confirm each transport on its own
loopback port), fire concurrent query and add requests from several clients,
confirm correct results and a consistent corpus, check health, request shutdown,
and confirm it drains and exits cleanly with no data loss.

**Acceptance Scenarios**:

1. **Given** the server started, **When** the operator inspects listening
   interfaces, **Then** REST, gRPC, and MCP each bind their own loopback address,
   individually overridable via per-service address flags (MuninnDB's
   `--rest-addr` / `--grpc-addr` / `--mcp-addr` pattern).
2. **Given** a running server, **When** several clients send overlapping query
   and add requests concurrently, **Then** all complete correctly, reads during
   writes are served (eventual-consistent per the constitution), and the
   single-writer invariant holds — no corruption, no double-writes.
3. **Given** an attempt to start a second go-rag server against the same database
   directory, **When** the instance lock is already held, **Then** the second
   refuses to start with a clear, actionable error (single-instance enforcement,
   MuninnDB's PID-file discipline).
4. **Given** a running server, **When** a client checks health/readiness, **Then**
   it reports server up, storage open, and embedding-service reachability.
5. **Given** a running server under traffic, **When** the operator requests
   graceful shutdown, **Then** in-flight requests drain, background indexing
   settles, storage is fsynced, and the process exits with no data loss.

---

### Edge Cases

- Two API clients add the same file simultaneously — single-writer serialization
  plus content-addressed identity must yield exactly one document, no duplicate
  embeddings.
- Embedding service (Ollama) down during an API ingest — ACK fast, index
  asynchronously, surface "indexing pending" in status.
- Malformed, oversized, or unknown-operation requests — reject with
  transport-appropriate error semantics (HTTP status / gRPC status); never crash
  the server.
- Client disconnects mid-query, or drops a streamed-result connection — cancel
  cleanly, release resources, no leaked work or partial writes.
- Configured port already in use, or an operator attempts to bind beyond
  loopback — fail fast with a clear message; non-loopback binding is out of scope.
- A query returns zero results, or more than the requested top-K — return an empty
  set cleanly; cap strictly at top-K.
- A transport is asked to serve a vault that does not exist (per spec 002 vault
  scoping) — reject with a clear not-found error.
- One transport's listener fails to bind at startup (e.g., port taken) — the
  server reports which transport failed and does not start in a half-up state.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001 (adapter-over-engine)**: The system MUST expose every operation
  through protocol adapters that all invoke a single shared engine — REST, gRPC,
  and MCP are interfaces to the same engine, not separate implementations.
- **FR-002 (transport equivalence)**: For identical inputs, REST, gRPC, and MCP
  MUST return identical results, and all three MUST agree with the CLI — full
  cross-transport parity over the operation surface.
- **FR-003 (cross-transport read-after-write)**: A document written via any one
  transport MUST be immediately retrievable via any other transport, because all
  adapters share one engine and one storage writer.
- **FR-004**: The server MUST expose, over REST, gRPC, and MCP, a query operation
  supporting hybrid, keyword-only, and vector-only modes, with a configurable
  top-K limit and optional source/path filtering, returning ranked results with
  source file, page number (for paginated sources), and relevance score.
- **FR-005**: The server MUST expose, over REST, gRPC, and MCP, an add/ingest
  operation accepting a file or directory path and returning a
  new/skipped/errored summary consistent with the CLI.
- **FR-006**: The server MUST expose, over REST, gRPC, and MCP, a status
  operation returning corpus counts, storage size, active embedding model, and
  embedding-service reachability.
- **FR-007**: Content-addressed idempotency MUST hold across the API boundary —
  re-ingesting an unchanged file over any transport is a no-op creating no
  duplicate documents or embeddings.
- **FR-008 (async-after-ACK at the boundary)**: An API ingest request MUST be
  acknowledged promptly and MUST NOT block the client on embedding or indexing
  latency; the <10ms write-ACK budget is preserved regardless of transport.
- **FR-009 (single writer)**: The server MUST hold go-rag's single-writer lock
  and broker concurrent API clients through it without corruption or
  double-writes; reads during writes are eventual-consistent.
- **FR-010 (per-port loopback)**: REST, gRPC, and MCP MUST each listen on their
  own distinct loopback address by default, each independently overridable via a
  per-service address flag.
- **FR-011 (single instance)**: The server MUST enforce a single instance per
  database directory and refuse to start when the instance lock is already held,
  with a clear, actionable error.
- **FR-012 (health & metrics)**: The server MUST expose a health/readiness
  indication (and basic metrics) reporting server up-time, storage-open state, and
  embedding-service reachability.
- **FR-013 (graceful shutdown)**: The server MUST support graceful shutdown —
  drain in-flight requests, settle background indexing, fsync storage, exit with
  no data loss.
- **FR-014 (vault scoping)**: Operations MUST be scoped to the vault the server
  was started against (per spec 002).
- **FR-015 (error semantics)**: Errors MUST map to transport-appropriate
  semantics (HTTP status codes for REST; gRPC status codes for gRPC) with
  actionable messages.
- **FR-016 (pure-Go, single binary)**: All transport dependencies MUST be pure-Go
  (no CGo); the server MUST ship inside the same single statically-linked binary
  built with `CGO_ENABLED=0`, preserving Principles I and III.

### Key Entities *(include if feature involves data)*

- **Unified Engine**: the single core that owns storage, indexing, and the query
  pipeline. Every transport adapter calls into it; this is the source of
  cross-transport equivalence (MuninnDB's "one engine" principle).
- **Transport Adapter**: a thin per-protocol boundary (REST, gRPC, MCP) that
  translates a wire request into an engine operation and the result back onto the
  wire. Adapters add no independent logic — they are interfaces, not
  implementations.
- **Server (daemon)**: the long-running process that opens the unified engine,
  acquires the single-writer lock, and brings up the transport listeners.
  Introduced for MCP in spec 002; extended here to serve REST and gRPC too.
- **API Client**: any non-CLI consumer — application, script, editor plugin, or
  AI agent — over REST, gRPC, or MCP. Local and trusted in v1.
- **Operation surface**: the operations exposed uniformly across CLI, REST, gRPC,
  and MCP — query, add/ingest, status (and config/scan where applicable).
- **Vault scope**: the corpus a server instance serves (per spec 002).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A client in a non-Go language can complete a query round-trip over
  REST and over gRPC (and an AI agent over MCP) with no CLI invocation, receiving
  cited, ranked results.
- **SC-002 (transport equivalence)**: For the same query and parameters, REST,
  gRPC, MCP, and the CLI return identical ranked results — verifiable parity, not
  approximate.
- **SC-003 (cross-transport consistency)**: A document added over one transport
  is retrievable over the others immediately — write-once, read-anywhere.
- **SC-004**: API query responses are perceived as effectively instant for
  keyword-only lookups and fast (on the order of the CLI hybrid target) for
  hybrid search, regardless of transport.
- **SC-005**: Re-ingesting the same unchanged file over the API produces zero
  duplicate documents or embeddings — idempotency holds across the boundary.
- **SC-006**: The server sustains many concurrent clients with overlapping read
  and write requests, with no state corruption, no double-writes, and no crashes.
- **SC-007**: The server starts with each transport on its own loopback port,
  becomes ready, reports health, and shuts down cleanly with no data loss.
- **SC-008**: The server ships inside the existing single binary with no new
  runtime dependencies — local-first and pure-Go guarantees are preserved.

## Assumptions

- **Architecture source**: MuninnDB's interface-layer-of-adapters-over-one-engine
  design (`scrypster/muninndb` `docs/architecture.md` §1, §6) is the reference
  pattern. go-rag copies the shape: one engine, per-transport adapters,
  cross-transport read-after-write, per-port loopback binding, single-instance
  server.
- **v1 transport set = REST + gRPC + MCP.** REST (HTTP/JSON, OpenAPI-described,
  curl-friendly) and gRPC (protobuf, unary + streaming) are added; MCP is the
  already-mandated third (Principle V), including a stdio transport for local AI
  clients.
- **MBP deferred.** MuninnDB's native binary protocol (TCP + MessagePack, 16-byte
  framed, pipelined) is NOT in go-rag v1. gRPC already serves typed
  high-throughput multi-language clients; MBP adds complexity the constitution
  discourages. Revisit if a Go/Python SDK needs sub-gRPC latency.
- **Web UI deferred (PRD §2.2).** MuninnDB serves a browser dashboard on its own
  port; go-rag explicitly out-of-scopes a web UI for v1.
- **Auth/TLS deferred (PRD §2.2).** MuninnDB uses vault-scoped API keys + TLS +
  CLI-loopback TLS. go-rag v1 is trusted-local: loopback-only, no auth, no TLS —
  the API trusts the local user exactly as the CLI does. When go-rag later
  supports non-loopback exposure, MuninnDB's vault-scoped API-key model is the
  intended direction.
- **Loopback-only binding.** All transports default to `127.0.0.1` on distinct
  ports, preserving Principle I. Non-loopback binding and cross-machine access are
  out of scope.
- **Port scheme (proposed, plan-finalizable).** To avoid colliding with
  MuninnDB's 8474–8477/8750, go-rag proposes a distinct loopback base — e.g.
  REST, gRPC, and MCP on consecutive ports — each overridable via per-service
  address flags. Exact port assignment is a `/speckit-plan` decision.
- **Full read+write parity over all three transports**, consistent with CLI/MCP
  parity. (Read-only query API would be a smaller scope the user can request —
  the single most consequential scope lever, flagged here.)
- **Builds on the spec 002 daemon.** The server is the per-vault daemon
  (`go-rag --vault <name> start`), extended from MCP-only to REST + gRPC + MCP.
- **Pure-Go transports.** Candidate libraries (gRPC-Go and a pure-Go HTTP/JSON
  layer) are expected pure-Go and permissively licensed; library selection and a
  Principle III compliance check are deferred to `/speckit-plan`, which will also
  pass the Constitution Check gate.

### Out of Scope (this feature)

- MBP / native binary protocol (deferred — see Assumptions).
- Web UI / browser dashboard (PRD §2.2).
- Authentication, authorization, API keys, TLS/certificates (PRD §2.2).
- Binding to non-loopback interfaces, remote/networked deployment, cross-machine
  access.
- Rate limiting, quotas, usage metering, per-client isolation, billing.
- Cluster/replication/federation (MuninnDB has it; go-rag v1 is single-node).
- Replacing, deprecating, or downgrading the existing CLI.
- CLI-to-running-server dispatch (MuninnDB's CLI can talk to a live server;
  go-rag's CLI stays direct in v1 — optional future enhancement).
- Changes to storage, indexing, chunking, or embedding subsystems beyond routing
  existing operations through the server.
