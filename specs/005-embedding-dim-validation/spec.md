# Feature Specification: Embedding Model/Dimension Mismatch Validation

**Feature Branch**: `005-embedding-dim-validation`

**Created**: 2026-06-21

**Status**: Draft

**Input**: User description: "look at next item on the audit list backlog" → the next unchecked item in `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 1) is the audit's **H03 — "Embedding dim/model mismatch unvalidated → silent corruption"** (P0, effort S). Source: `RAG_BOOK_AUDIT.md` §1.2.

> **Why this is next.** H03 is a **P0 silent-killer**: changing the embedding
> model (or a library silently changing pooling behavior) mid-corpus produces
> query embeddings whose vector length or semantic space no longer matches the
> stored vectors. Today go-rag computes a similarity score over mismatched
> vectors with **no length check and no error** — the result is either garbage
> scores (returned as if valid) or an index panic, and the user has no way to
> tell retrieval just broke. The book's §4.6 war story is exactly this: *"a
> library update silently changed pooling behavior once, dropping retrieval
> accuracy 15%"* — invisible without a guard. This spec closes that blind spot.
>
> **Synergy with spec 004.** The retrieval-quality evaluation harness shipped in
> `004-retrieval-eval-harness` is the natural way to *verify* this guard: it can
> exercise mismatch scenarios deterministically and prove the guard turns a
> silent 15% accuracy drop into a loud, identifiable error. H03 is the first
> silent-failure mode (audit §6(a)) the harness can now make visible.
>
> **Scope note.** This spec is the **validation + refusal guard**: detect
> dimension/model mismatch and refuse to return garbage. It is deliberately
> narrower than audit H11 (full drift monitoring / version-pinning / auto-reindex
> trigger), which remains a separate future item. Here we make the failure
> **loud and safe**; H11 later makes it **observable over time**.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Never Silently Corrupt: Refuse a Mismatched Query (Priority: P1) 🎯 MVP

A maintainer changes the configured embedding model (or upgrades a model whose
pooling behavior shifted) and forgets to re-index, then issues a query. Today
the query vector has a different length or lives in a different semantic space
than the stored vectors, and the system either returns plausible-but-wrong
rankings or panics — with nothing telling the maintainer *why*. After this
story, the system **detects the mismatch and refuses**, surfacing a clear error
that names the expected model/dimensionality versus the actual, so the
maintainer knows exactly what to do (re-index). The defining property: a
mismatched embedding can never again produce a score that looks valid.

**Why this priority**: This is the core P0 — silent corruption is the failure
mode. Every other story is visibility or graceful-degradation on top of this
refusal. An MVP that turns silent corruption into a loud error is independently
shippable and immediately removes the blind spot.

**Independent Test**: Configure model A, ingest, then switch the config to model
B (different dimensionality or model name) without re-indexing, and issue a
query. The query MUST fail with a message identifying the mismatch (expected
model/dim vs actual) — it MUST NOT return ranked results and MUST NOT crash.

**Acceptance Scenarios**:

1. **Given** a corpus embedded under model A (dimension N), **When** a query is
   embedded under model B whose vector length differs (dimension M ≠ N),
   **Then** the system rejects the query with an error stating the stored
   dimensionality and the query's dimensionality — no results are returned and
   no panic occurs.
2. **Given** a corpus embedded under model A, **When** a query is embedded under
   a model with the **same** dimension but a **different name** (a different
   semantic space), **Then** the system flags the model-name mismatch and
   refuses (same-dimensionality does not make a different model safe).
3. **Given** a corpus and a query both under model A, **When** the query runs,
   **Then** retrieval behaves exactly as before (no false alarms on the happy
   path; the guard adds negligible overhead).

---

### User Story 2 - See Embedding Consistency Before It Bites (Priority: P2)

An operator wants to know the corpus's embedding health *without* issuing a
query — what model and dimensionality the stored vectors use, and whether they
are consistent. After a partial migration or an interrupted re-index, some
vectors may be under the old model and some under the new; today that is
invisible. This story surfaces it in the standard status view: the active model,
the dimensionality, and a flag when the corpus holds more than one model or
dimensionality (drift). The maintainer sees drift *before* it causes wrong
results.

**Why this priority**: Refusal (US1) protects a running query; visibility (US2)
lets an operator notice the problem and act proactively. Valuable, but it sits
behind the core refusal — a maintainer who never checks status is still safe
because of US1.

**Independent Test**: Ingest under model A, partially re-embed one document
under model B, then run status. The status MUST report both the majority model
and a clear inconsistency flag with counts (e.g. "12 vectors under model A, 3
under model B").

**Acceptance Scenarios**:

1. **Given** a consistent corpus, **When** the operator views status, **Then**
   it reports the embedding model and dimensionality and shows no inconsistency.
2. **Given** a corpus with embeddings under two models, **When** the operator
   views status, **Then** it flags the inconsistency and reports the count per
   model so the operator can decide whether to re-index.

---

### User Story 3 - Degrade Gracefully Through a Partial Migration (Priority: P3)

During a migration, some stored vectors are under the old model and some under
the new. Rather than failing every query (US1's hard refusal assumes a query
that doesn't match the *majority*) or silently mis-scoring the minority, the
system scores the query against the **matching majority** and **skips** the
mismatched minority, logging a warning. Queries stay correct for the matching
vectors; the mismatched ones are excluded, not corrupted. This keeps the corpus
usable mid-migration instead of forcing an all-or-nothing re-index.

**Why this priority**: This is resilience for the transient mixed state. It
depends on US1's detection (you can only skip what you can detect) and US2's
visibility. It is not required to ship the core refusal, so it sits last.

**Independent Test**: Build a corpus with 80% of vectors under model A and 20%
under model B; query under model A. Results MUST be drawn only from the
model-A vectors (correct), the model-B vectors MUST be skipped, and a warning
MUST be logged — the query MUST NOT fail and MUST NOT include mis-scored
model-B results.

**Acceptance Scenarios**:

1. **Given** a mixed corpus (majority model A, minority model B) and a query
   under model A, **When** the query runs, **Then** only model-A vectors are
   scored, the model-B vectors are skipped, and a warning records how many were
   skipped.
2. **Given** the same mixed corpus and a query under model B (the minority),
   **When** the query runs, **Then** the system refuses with a clear message
   (the query does not match the majority) rather than scoring against the small
   minority as if it were the whole corpus.

---

### Edge Cases

- **Same dimensionality, different model** — must still be treated as a mismatch
  (a different model is a different semantic space even at equal length).
- **Empty corpus** — no stored vectors to compare against; the first ingest
  establishes the corpus model/dim; a query against an empty corpus returns no
  results without error.
- **Corpus where every vector differs from the query with no clear majority** —
  refuse (cannot safely score); report the ambiguity.
- **A stored vector whose recorded dimensionality disagrees with its actual
  vector length** (corrupted entry) — skip it with a warning; never panic.
- **Reranker dimension/model** — out of scope; this spec governs the embedding
  (retrieval) space only, not the cross-encoder rerank space.
- The guard must not regress the happy-path latency budget (a length check is
  O(1) per query).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST detect when a query embedding's vector length
  differs from the stored vectors' length and MUST NOT compute or return a
  similarity score from a length-mismatched pair (no garbage cosine, no panic).
- **FR-002**: The system MUST record each stored embedding's model and
  dimensionality as provenance, so mismatch is detectable (not just inferable
  from the first vector).
- **FR-003**: When a query embedding's model **or** dimensionality does not
  match the corpus majority, the system MUST surface a clear error identifying
  both the expected (stored) and actual (query) values, and MUST NOT return
  ranked results for that query.
- **FR-004**: The standard status view MUST report the corpus's active embedding
  model and dimensionality, and MUST flag inconsistency when more than one model
  or dimensionality is present (with per-model/dimensionality counts).
- **FR-005**: When a query matches the stored majority but a minority of stored
  vectors differ, the system MUST score only the matching vectors, skip the
  mismatched ones, and log a warning noting how many were skipped — the query
  MUST NOT fail and MUST NOT include mis-scored mismatched vectors.
- **FR-006**: The mismatch check MUST be local and pure (no network, no external
  service) and MUST add negligible overhead to the happy path (a length/model
  comparison), preserving the query-latency budget.
- **FR-007**: The validation MUST be exercisable through every transport that
  serves queries (CLI, REST, gRPC, MCP) so the guard is consistent everywhere a
  query can originate (cross-transport parity, constitution Principle V).

### Key Entities *(include if feature involves data)*

- **Embedding Provenance**: the model name and dimensionality recorded for each
  stored vector — the basis for detecting mismatch. (go-rag already stores
  `Embedding.Model` and `Embedding.Dimensions`; this spec makes them *checked*.)
- **Corpus Embedding Profile**: the majority model + dimensionality across all
  stored vectors in a vault — the "expected" profile a query must match (US1) or
  be scored against (US3). Derived from the stored provenance, not a new store.
- **Query Embedding**: the vector produced for a query, carrying its model and
  dimensionality — compared against the Corpus Embedding Profile before scoring.
- **Mismatch Verdict**: for a query, one of *match* (score normally), *refuse*
  (query ≠ majority → error, US1), or *partial* (query = majority but some
  stored vectors differ → skip minority + warn, US3).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After an embedding-model change without re-indexing, a query
  produces a clear, identifiable error naming the mismatch — never plausible-but
  -wrong rankings and never a crash.
- **SC-002**: An operator can see the corpus's embedding model, dimensionality,
  and any inconsistency in the standard status view **without issuing a query**.
- **SC-003**: A corpus mid-migration (mixed models) degrades gracefully —
  queries return correct results from the matching majority while mismatched
  vectors are skipped with a warning — rather than failing or returning noise.
- **SC-004**: The guard adds no perceptible happy-path query latency (a length/
  model comparison), so existing latency budgets are preserved.
- **SC-005**: The behavior is verifiable offline and deterministically, including
  via the existing evaluation harness (spec 004), which can exercise mismatch
  scenarios and prove silent corruption is now a loud error.
- **SC-006**: The guard is consistent across every query transport (CLI/REST/
  gRPC/MCP) — a mismatch is refused identically regardless of where the query
  originates.

## Assumptions

- **Majority = expected.** The corpus's expected model + dimensionality is the
  majority among stored embeddings — a sensible default for a single-user local
  database where one model is active at a time; the mixed case is the transient
  mid-migration state (US3).
- **Refuse-the-query vs skip-the-vector distinction.** When the *query* doesn't
  match the corpus majority, refuse (US1) — the query is wrong for this corpus.
  When the *query* matches the majority but some *stored* vectors differ, skip
  those vectors (US3) — the corpus is mid-migration. This distinction is the
  core behavior; documented here as the default.
- **Provenance already partially exists.** `Embedding.Model` and
  `Embedding.Dimensions` are already stored (audit §1.2). This spec mandates they
  be *checked*, not that a new storage format be invented.
- **Narrower than H11.** Full drift monitoring (baselines, version-pinning,
  startup refuse-or-reindex, ollama-version tracking) is audit H11 and remains a
  separate future item. This spec is the refusal/safety guard only.
- **Reranker space out of scope.** The cross-encoder rerank model has its own
  dimensionality concerns; this spec governs the retrieval embedding space only.
- **Dependency on spec 004 for verification.** The evaluation harness is the
  intended way to prove this guard works; extending it with a mismatch scenario
  is noted as a verification path, not duplicated here.
- **The remaining audit items (H01, H04, H05, H07–H10, H12–H28) remain tracked
  in `RAG_BOOK_AUDIT_BACKLOG.md`** — this spec is solely the H03 fix.
