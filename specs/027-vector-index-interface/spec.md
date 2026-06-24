# Feature Specification: Swappable Vector Index

**Feature Branch**: `027-vector-index-interface`

**Created**: 2026-06-24

**Status**: Draft

**Input**: User description: "work on backlog item H27" — audit finding **H27**
(`RAG_BOOK_AUDIT_BACKLOG.md`, Phase 7, §1.3): *"Brute-force `*Vector` has no
`Index` interface (no HNSW escape hatch). Extract an `Index` interface
(`Add/Delete/Query`) before scaling pressure hits."* Today the vector store is a
single concrete implementation — a brute-force, linear-scan cosine search — wired
directly into retrieval. There is no boundary at which it can be substituted. The
store also carries three correctness behaviours (skip mismatched-dimensionality
vectors rather than garbage-score them; stay safe under concurrent ingest and
query; rank deterministically with stable tie-breaks) that are incidental to its
implementation rather than documented guarantees. The audit — and a long-standing
code comment anticipating an approximate-nearest-neighbour (ANN) backend — expect
to swap the store as the corpus grows, but the seam to do so safely does not exist.
This feature adds that seam: it makes the vector store's contract explicit and its
implementation substitutable, while one implementation exists and can validate the
contract — the cheapest moment to do so.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The vector backend is freely replaceable (Priority: P1)

A developer maintains the system as the corpus grows toward the scale where
brute-force vector search becomes a latency cost. They want to substitute the
brute-force store with an approximate-nearest-neighbour (ANN) backend without
touching retrieval, fusion, rerank, or any transport. Today the store is a concrete
type wired directly into retrieval, so substitution means surgery across every
caller. After this feature, retrieval depends on the store's *contract*, not its
implementation, so a new backend is a drop-in — landing the escape hatch the audit
asks for while only one implementation exists.

**Why this priority**: This is the audit's explicit ask ("extract an interface…
before scaling pressure hits"). The cost of extracting the seam grows with every
new caller that takes the concrete store directly; landing it now is the cheapest
it will ever be, and it retires a long-standing deferred TODO. The other two
stories protect the value this one creates.

**Independent Test**: Substitute a second vector-store implementation (a test
double or an alternative algorithm) for the same corpus and confirm retrieval
returns identical results for identical queries — proving retrieval depends on the
contract, not the implementation. Fully testable with a fixture corpus and one
substitute implementation; no query-path change required.

**Acceptance Scenarios**:

1. **Given** retrieval is wired through the store's contract, **When** a second implementation satisfying the contract is provided, **Then** retrieval returns the same ranked results for the same corpus and query as the original brute-force implementation.
2. **Given** the feature is in place, **When** the brute-force store remains the active implementation, **Then** no query, status, ingest, or migration behaviour changes for any user — the seam is invisible.
3. **Given** a developer adds a new backend, **When** they implement the contract, **Then** no change to retrieval, fusion, rerank, or transport code is required.

---

### User Story 2 - The store's correctness invariants are explicit guarantees, not accidents (Priority: P2)

The system must remain correct regardless of which vector backend is active. Today
three correctness behaviours are incidental to the brute-force implementation, not
documented guarantees: (a) a stored vector whose dimensionality differs from the
query's is skipped, never garbage-scored — the anti-silent-corruption guard; (b)
the store is safe under concurrent ingest writes and query reads; (c) ranking is
deterministic with stable tie-breaking. A naive substitute (a typical ANN library)
would violate all three — silently re-introducing wrong scores on a mixed or
mid-migration corpus, racing under concurrent load, and returning non-deterministic
order. After this feature these behaviours are part of the store's explicit
contract: any backend must provide them, and one that cannot is rejected or wrapped.

**Why this priority**: The correctness backbone for US1. A replaceable backend that
silently corrupts results is worse than no seam. This is the part a single
"extract an interface" pass misses — the real deliverable is the *contract*, not
the method set.

**Independent Test**: With a backend that would score all vectors regardless of
dimensionality, confirm a mixed-dimensionality corpus is still handled correctly
(mismatched vectors excluded) — i.e. the guard is enforced by the contract or a
wrapper, not lost when the implementation changes. Testable with a fixture
mixed-dimensionality corpus and one query.

**Acceptance Scenarios**:

1. **Given** a corpus containing vectors of differing dimensionality (a mixed or mid-migration corpus), **When** queried, **Then** mismatched vectors are excluded from scoring under any backend — never silently garbage-scored.
2. **Given** concurrent ingestion and querying, **When** both proceed, **Then** the store remains correct and safe (no races, no corruption).
3. **Given** the same corpus and query, **When** queried repeatedly, **Then** ranking is identical and deterministic, with ties resolved by a stable rule.

---

### User Story 3 - Zero behavioural change for every existing consumer (Priority: P3)

The feature is purely structural. Query results, result scores, latency, the
retrieval-eval baseline, cross-transport parity, and ingest/migration behaviour are
identical before and after. No transport gains or loses a field; no new config key
is required; existing persisted corpora load and query unchanged. The seam is
added; nothing any user can observe changes.

**Why this priority**: Required for a safe landing alongside an existing corpus and
an established eval baseline. Lower priority than the seam (US1) and the contract
(US2), but a hard gate — any visible change means the extraction was done wrong.

**Independent Test**: Run the full existing retrieval and query test suite and the
retrieval-eval harness before and after; assert identical results and identical
recall.

**Acceptance Scenarios**:

1. **Given** the existing test suite, **When** run after the feature, **Then** every retrieval, query, fusion, and rerank test passes unchanged.
2. **Given** the retrieval-eval harness, **When** run before and after, **Then** retrieval quality (recall) is identical — the structural change is quality-neutral.
3. **Given** any transport (CLI, REST, gRPC, MCP), **When** the same query is issued before and after, **Then** the response is identical.

---

### Edge Cases

- **Mixed / dimensionality-mismatched corpus (mid-migration)**: the length-skip guard MUST be preserved under any backend — an ANN backend that scores everything MUST be wrapped or rejected (this is the silent-corruption failure mode the guard exists to prevent).
- **Non-deterministic backends**: approximate search can return run-to-run variation; the contract MUST require deterministic, stably tie-broken output, or eval and cross-transport parity guarantees break.
- **Concurrent ingest + query**: the pipeline writes vectors from background workers while queries read; the contract MUST be safe under this concurrency regardless of the backend's locking model.
- **Backend-owned persistence / ID schemes**: some ANN libraries own their storage format and use integer IDs; the contract MUST preserve the system's content-addressed string chunk-IDs and not force a storage-model or identity change.
- **Persistence asymmetry**: the brute-force store persists one way; an ANN backend persists another. Save/load MUST NOT be part of the core retrieval-facing contract (it is backend-specific), or every backend is forced to fake it.
- **Tie-breaking determinism**: equal-score vectors MUST resolve to a stable order so results are reproducible across runs and backends.
- **Empty or single-vector corpus**: degenerate corpora MUST query correctly under any backend.
- **Backend cannot satisfy an invariant**: a backend that cannot provide determinism or the dimensionality-skip MUST be rejected at construction or wrapped so the invariant holds — never silently accepted.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST define an explicit, implementation-neutral contract for the vector store covering at least add, delete, and nearest-neighbour query, expressed over the existing result and vector types. Retrieval MUST depend on this contract, not on any single concrete implementation.
- **FR-002**: The contract MUST guarantee that a stored vector whose dimensionality differs from the query is excluded from scoring, never garbage-scored — preserving the existing anti-silent-corruption behaviour under any backend.
- **FR-003**: The contract MUST require deterministic ranking with stable tie-breaking, so an identical corpus and query produce identical results regardless of backend.
- **FR-004**: The contract MUST be safe for concurrent ingestion writes and query reads.
- **FR-005**: The existing brute-force store MUST remain the active implementation and MUST satisfy the contract as its reference implementation. No second backend is shipped as a product feature in this work (the seam is proven with a test double, not a shipped backend).
- **FR-006**: The feature MUST be behaviour-preserving: query results, scores, retrieval-eval quality, cross-transport parity, ingest, and migration are identical before and after.
- **FR-007**: The contract MUST NOT mandate a specific persistence model (save/load) on the core retrieval surface; persistence remains an implementation detail of each backend.
- **FR-008**: The contract MUST preserve the system's content-addressed string chunk-identifiers — no migration to an integer-ID or backend-owned identifier scheme.
- **FR-009**: Any backend that cannot satisfy FR-002, FR-003, or FR-004 MUST be rejected at construction or wrapped so the invariant holds — never silently accepted.

### Key Entities *(include if feature involves data)*

- **Vector Store**: the subsystem that holds chunk embeddings and returns nearest neighbours for a query. Today one concrete brute-force (linear-scan cosine) implementation.
- **Vector Index Contract**: the explicit, implementation-neutral agreement defining what any vector store MUST provide — add/delete/query plus the correctness invariants (dimensionality-skip, determinism, concurrency). The brute-force store is its reference implementation; a future approximate-nearest-neighbour backend is another.
- **Nearest-Neighbour Backend**: a concrete implementation of the Vector Index Contract. Brute-force today; an approximate-nearest-neighbour (ANN) algorithm in future. Must be local, pure-Go, and honour the contract's invariants.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A second implementation of the vector-store contract (demonstrated via a test double) returns identical retrieval results to the brute-force store for the same corpus and query — proving retrieval depends on the contract, not the implementation.
- **SC-002**: On the project's existing retrieval-eval harness, retrieval quality (recall) is identical before and after the feature — the structural change is quality-neutral with no regression.
- **SC-003**: The full existing retrieval, query, fusion, rerank, and cross-transport parity test suites pass unchanged — zero behavioural regression across CLI, REST, gRPC, and MCP.
- **SC-004**: The store's correctness invariants (dimensionality-mismatch skip, deterministic ranking, concurrency safety) are explicitly part of the contract and remain enforced — a backend that would violate them is rejected or wrapped rather than silently accepted.
- **SC-005**: The brute-force store remains the sole shipped implementation; no second backend is introduced as a product feature in this work, and no persisted corpus requires migration.

## Assumptions

- **Interface-only; no HNSW/ANN backend is shipped.** The audit's own wording ("extract an interface… before scaling pressure hits") asks for the seam, not a new algorithm. Brute-force remains adequate below ~10K documents (PRD local scale); the realistic trigger for an ANN backend is the same multi-user-MCP event that flips RBAC and other scale items to P0. Until then, only the seam is added. Shipping a backend is a separate future finding.
- **The real deliverable is the contract, not the method set.** Three existing behaviours (dimensionality-skip, concurrency, deterministic tie-break) are today incidental to the brute-force implementation; this feature promotes them to explicit guarantees. A naive backend swap that skipped this step would silently re-introduce wrong scoring on a mixed corpus — the exact failure mode the dimensionality guard exists to prevent.
- **Persistence is backend-specific and out of the core retrieval contract.** The brute-force store persists one way; an ANN backend would persist another (e.g. a serialized graph). Save/load is therefore NOT part of the retrieval-facing contract.
- **The future backend is constrained to local, pure-Go, no foreign-function/C dependencies** (constitution Principle III and the existing dependency posture). The anticipated shape is a pure-Go HNSW-style library (e.g. chromem-go, already named in the PRD's allowed-dependency list); the specific library is a plan decision, not prescribed here.
- **No persisted-corpus migration.** The store's on-disk shape is unchanged; the feature is structural and invisible to existing data.
- **Out of scope for v1**: a shipped approximate-nearest-neighbour backend; vector quantization; GPU acceleration; changing the embedding model or dimensionality; any user-visible query, status, or transport change.
