# Feature Specification: Bounded Embedding Batches

**Feature Branch**: `010-bounded-embed-batch` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-22

**Status**: Draft

**Input**: User description: "work on the next backlog item" → resolved to **H12**
from `RAG_BOOK_AUDIT_BACKLOG.md` (next unchecked Phase 1 item after H08/009):
*"Whole-doc embed batch unbounded → OOM/timeout. Batch texts ~32–64 inside
`Ollama.Embed`, concatenate responses, per-batch retry."* Source detail:
`RAG_BOOK_AUDIT.md` §1.2 (P1, "Whole-document batch with no size cap").

**Problem (grounded in current code):** Today the embedding call sends **every
chunk of a document in a single request**. A large document that splits into
hundreds or thousands of chunks becomes one enormous request to the local
embedding service — large enough to exhaust memory or exceed the request timeout
mid-document, failing ingestion of the whole document. The fix caps how many
texts travel in a single request, so memory and per-request time stay bounded
regardless of document size, while the result callers receive is unchanged.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Ingest a large document without timeout or memory failure (Priority: P1)

A user points go-rag at a large document (a long PDF, a big transcript, a dense
report) that splits into many hundreds of chunks. Today that ingestion can fail
outright — the embedding request is too large, timing out or exhausting memory
before the document finishes. The user expects large documents to ingest
reliably, with resource use that does not grow with document length.

**Why this priority**: This is the core defect H12 names. An unbounded batch is
a latent ingestion failure for any corpus containing a large document — the kind
of silent cliff that only appears on the user's largest, most important file.
Bounding the batch removes the cliff.

**Independent Test**: Ingest a document that produces far more chunks than the
batch cap (e.g., several hundred) and confirm ingestion completes successfully
and within resource limits, where the same document would previously fail or
stall. Verifiable with a deterministic local embedding stand-in (no external
service needed for the bounding assertion).

**Acceptance Scenarios**:

1. **Given** a document that splits into N chunks where N is many times the
   batch cap, **When** the user ingests it, **Then** all N chunks are embedded
   and stored successfully (the document is queryable end-to-end).
2. **Given** the same large document, **When** ingestion runs, **Then** no single
   embedding request carries more than the batch cap of texts — peak memory and
   per-request time stay bounded by the cap, not by N.
3. **Given** a small document (fewer chunks than the cap), **When** ingested,
   **Then** behaviour is unchanged from today (one request, same result).

---

### User Story 2 - A flaky batch is retried; a dead batch is reported, never silently dropped (Priority: P2)

The local embedding service can return a transient error (a brief 5xx, a network
hiccup) on one request out of many. With batching, a transient blip affects only
one batch — it should be retried independently, and only if a batch persistently
fails should the document fail. The user must never end up with a partially
indexed document (some chunks embedded, others silently missing) — failure is
all-or-nothing and clearly surfaced.

**Why this priority**: Reliability of the bounded path. Splitting into batches
multiplies the number of requests, which multiplies the chance of a transient
blip; per-batch retry keeps that from turning large documents into flaky
ingestion. The all-or-nothing guarantee protects index integrity.

**Independent Test**: Drive ingestion against a stand-in embedding service that
fails one batch transiently (then succeeds) and confirm the document completes;
drive it against a service that fails one batch permanently and confirm the
document fails with a clear error and leaves no partial index.

**Acceptance Scenarios**:

1. **Given** a stand-in that returns a transient error for one batch then
   succeeds, **When** a multi-batch document is ingested, **Then** the transient
   batch is retried and the document ingests fully.
2. **Given** a stand-in that fails one batch permanently, **When** ingestion
   runs, **Then** the whole document fails with an error (no chunk is silently
   dropped, no partial set of vectors is committed for that document).
3. **Given** the user cancels mid-ingest, **When** cancellation propagates,
   **Then** embedding stops promptly between batches without hanging.

---

### User Story 3 - Batching is invisible to callers (Priority: P2)

Every existing consumer of the embedding step (the ingest pipeline, the query
path, tests, any future provider) sees identical behaviour: the same input texts
yield the same vectors in the same order. Batching is an internal transport
detail, not a contract change.

**Why this priority**: This is the "don't break anything" guarantee. The embedding
contract — one vector per input text, in order, with the existing
integrity/dimensionality guarantees — must hold per batch and across the
concatenated result.

**Independent Test**: Send the same set of texts in one call vs. a call path
that batches them, and assert the returned vectors are identical and in order;
assert the dimensionality and count guarantees still hold.

**Acceptance Scenarios**:

1. **Given** the same set of input texts, **When** embedded, **Then** the
   returned vectors are identical in value and order regardless of how the texts
   are grouped into requests internally.
2. **Given** any single request that returns a vector count that does not match
   the number of texts sent in that request, **When** the response is processed,
   **Then** it is rejected as an error (the count-mismatch integrity guarantee
   holds per batch, so vectors can never silently misalign).
3. **Given** the first successful response, **When** subsequent batches run,
   **Then** the discovered vector dimensionality remains consistent (the
   set-once dimensionality guarantee is preserved).

---

### Edge Cases

- **Empty input**: embedding zero texts MUST be a no-op (no request sent),
  identical to today.
- **Chunk count not a multiple of the cap**: the final partial batch MUST be sent
  and handled like any other (same retry, same integrity check).
- **Single chunk / sub-cap document**: MUST behave exactly as today — one
  request, same result, no overhead beyond a size check.
- **Persistent batch failure**: the document MUST fail whole; a partial vector
  set MUST NOT be committed (no silent gaps in the index).
- **Context cancellation**: MUST be honoured between (and within) batches so a
  cancelled ingest returns promptly rather than waiting on remaining batches.
- **Dimensionality drift mid-document**: if a later batch returns a different
  vector length than an earlier one (a model or pooling change mid-run), the
  integrity guarantee MUST catch it rather than concatenating mismatched vectors.
- **Concurrency**: multiple workers embedding different documents at once MUST
  remain safe (bounding is per-call; shared state stays protected).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The embedding step MUST send texts in batches no larger than a
  fixed cap, so that no single request's size or memory grows with the number of
  chunks in a document.
- **FR-002**: The cap MUST sit within the 32–64 range, with a single chosen
  default documented as a project assumption (see Assumptions).
- **FR-003**: The embeddings returned to the caller MUST be the concatenation of
  the per-batch results, in the original input order — identical to what a single
  unbounded request would have returned (US3 acceptance 1).
- **FR-004**: Each batch MUST be retried independently on transient failure
  (server/network errors), reusing the existing retry-with-backoff policy, so a
  blip on one batch does not fail the whole document (US2 acceptance 1).
- **FR-005**: A request returning a vector count that does not match its sent
  text count MUST be rejected as an error — per batch — so vectors can never
  silently misalign across the concatenated result (US3 acceptance 2).
- **FR-006**: A batch that fails permanently (after retries) MUST fail the whole
  embedding call; no partial result is returned and no partial vector set is
  committed for that document (US2 acceptance 2).
- **FR-007**: An empty input MUST remain a no-op (no request sent); a sub-cap
  input MUST behave exactly as a single unbounded request does today.
- **FR-008**: Context cancellation MUST be honoured between batches (and within
  the existing per-request retry), so a cancelled ingest returns promptly (US2
  acceptance 3).
- **FR-009**: The embedding contract visible to every consumer (the ingest
  pipeline, the query path, tests, future providers) MUST be unchanged —
  batching is internal and introduces no new required behavior for callers.

### Key Entities *(include if feature involves data)*

- **Embed batch**: a fixed-size slice of input texts sent in a single request to
  the embedding service. Bounded by the cap; processed in order; each carries its
  own retry and integrity check.
- **Embedding result**: the ordered list of vectors returned to the caller — one
  per input text, in input order. Contract unchanged; produced by concatenating
  per-batch results.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A document that produces many more chunks than the batch cap
  (hundreds) ingests successfully end-to-end and is queryable, where the same
  document would previously fail or stall on an oversized embedding request.
- **SC-002**: Peak memory used during embedding does not scale with document
  chunk count — it is bounded by the batch cap (verifiable by ingesting a very
  large document and observing that resource use plateaus, not climbs with N).
- **SC-003**: The vectors returned for a set of texts are identical in value and
  order regardless of how the texts are grouped into requests internally
  (verifiable with a deterministic embedding stand-in).
- **SC-004**: A transient failure on one batch is recovered by retry without
  failing the document; a permanent failure on one batch fails the document with
  a clear error and leaves no partially-indexed result.

## Assumptions

- **Batch cap default: 32** (within the audit's 32–64 range). It is an internal
  constant, **not** exposed as a config/flag knob — H12 is scoped as a robustness
  fix (S effort), and per-request tuning is a separate concern. If real corpora
  later show a different sweet spot, the constant can be revisited without a
  contract change.
- **Per-batch retry reuses the existing policy**: 3 attempts with exponential
  backoff, retry on server (5xx)/network errors, fail fast on client (4xx)
  errors. Batching applies that policy independently to each batch.
- **Sequential batch processing** within a single embedding call. The pipeline
  already parallelizes *across* documents and background workers; parallelizing
  batches *within* one call is out of scope and unnecessary for the local
  single-user model.
- **No contract change for callers**: the `Embedder` interface and its consumers
  are untouched. Batching lives entirely inside the existing Ollama
  implementation, so the ingest pipeline, query path, tests, and any future
  provider see identical behavior (Constitution Principle V — extension by
  interface — preserved).
- **Write-ACK latency untouched**: embedding runs on background workers after
  the write is acknowledged, so bounding batches affects only the async path;
  the durable-write budget is unchanged (Constitution Principle IV preserved).
- **Out of scope**: a query-embedding cache (H06) and per-request tunability
  (H22-adjacent) are separate backlog items and are not introduced here.
