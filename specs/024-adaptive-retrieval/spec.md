# Feature Specification: Adaptive Retrieval Depth & Pool-Size Tuning

**Feature Branch**: `main` (single-author Spec Kit work lands on `main` directly per `CLAUDE.md`; no feature branch)

**Created**: 2026-06-23

**Status**: Draft

**Input**: User description: "look at H22 in backlog" → `RAG_BOOK_AUDIT_BACKLOG.md` H22 (Audit §1.4): *No adaptive retrieval depth / pool-size tuning.* Optional `QueryClassifier` returning recommended `k`/`Mode` (cheap rule-based first); configurable reranker pool.

## Clarifications

### Session 2026-06-23

- Q: Does the v1 classifier recommend retrieval depth only, or depth + mode? → A: **Depth (`k`) only — Mode is never auto-changed** (Option A). The classifier recommends `k`; retrieval mode (hybrid / vector / keyword) remains the caller's explicit choice. Keeps H22 at "S" effort and preserves the cleanest no-regression story; a mode-recommending classifier can land later behind the same interface.
- Q: Should SC-001 pin a concrete latency target, or stay "measurably faster"? → A: **Anchor to the constitution's existing query-latency budgets** (Option A) — a reduced-depth/shallow-pool factoid query meets (or approaches) the keyword-only budget and no query regresses past the hybrid budget. No new SLA invented; verifiable via the eval harness.
- Q: Where is pool-utilization surfaced? → A: **Aggregate in `status` only** (Option A) — not attached to individual query responses, so the query-response contract stays unchanged. (The per-query *effective* depth/pool/mode from US3 is a separate, smaller signal and remains in the response.)
- Q: When the classifier recommends a shallow `k`, does the candidate pool shrink with it? → A: **Yes — the effective pool scales with recommended `k`** (Option A): pool = `k` + small slack, bounded by the configured pool as a ceiling and a minimum floor. The pool drives actual search cost, so shrinking it is what makes the latency target (SC-001) reachable. With no recommendation, the full configured pool is used.

## User Scenarios & Testing *(mandatory)*

> Today every query runs with a fixed retrieval depth (`k`) and a fixed reranker
> candidate pool (default 60), regardless of what kind of query it is. A factoid
> lookup ("what is the max batch size") pays for 60 candidates when 3 would do; a
> broad comparative query ("compare the caching and drift approaches across the
> corpus") is starved by a pool too small to surface every relevant passage. H22
> makes both knobs adaptive — a classifier can recommend retrieval depth, and the pool
> becomes tunable with its utilization visible — so an operator can trade recall
> against latency without changing code.

### User Story 1 - Tunable reranker candidate pool (Priority: P1)

An operator wants to grow or shrink the number of candidate passages that enter
reranking, per query and via configuration, and to see how heavily that pool is
being used — so they can rescue low-recall queries (bigger pool) or cut latency
on easy queries (smaller pool).

**Why this priority**: It is the simpler, more concrete half of H22, delivers
immediate latency/recall tuning, and is a prerequisite for judging whether the
classifier (US2) is helping. It is independently shippable and independently
valuable.

**Independent Test**: Run the same comparative query against the eval harness
(H02) with the default pool and with a larger pool, and observe recall improve;
run a factoid query with a smaller pool and observe latency drop — with no code
change, only a flag/config value.

**Acceptance Scenarios**:

1. **Given** a query that is missing relevant passages at the default pool size, **When** the operator raises the pool size for that query (CLI flag / REST field / gRPC field / MCP), **Then** the relevant passages appear in the results and recall on the eval harness does not regress.
2. **Given** an operator who wants latency-critical keyword lookups, **When** they set a smaller pool size, **Then** query latency moves toward the keyword-only latency budget versus the default pool, with no change to the top results on straightforward queries.
3. **Given** any query, **When** the operator inspects system status or the query response, **Then** they can see pool-utilization information (how many candidates were fetched versus how many survived to the final results).
4. **Given** a query that does not set a pool size, **When** it runs, **Then** the system uses the configured default (today's value, 60) so existing behavior is unchanged.

---

### User Story 2 - Adaptive retrieval depth via a query classifier (Priority: P2)

An operator wants the system to choose a sensible retrieval depth (`k`)
automatically based on the shape of the query — shallow for factoid lookups,
deeper for broad/comparative questions — rather than always using one fixed
depth. (Retrieval mode — hybrid / vector / keyword — remains the caller's
explicit choice and is never auto-changed.)

**Why this priority**: Higher leverage than US1 (the book cites ~40% latency
wins from query-type-aware depth), but more open-ended and only safe to enable
once US1's pool knob and the eval harness can prove it helps. It is the
distinctive "adaptive" half of H22.

**Independent Test**: With the classifier enabled and no explicit `k` set, run
a clearly factoid query and a clearly comparative query; observe that the two
queries use different effective retrieval depths, and that the eval harness
recall@10 does not regress versus the fixed-depth baseline.

**Acceptance Scenarios**:

1. **Given** the classifier enabled and a clearly factoid query (e.g., a short lookup), **When** the query runs without an explicit `k`, **Then** a shallower effective depth **and** a smaller effective candidate pool are used than for a broad comparative query, and the difference is observable.
2. **Given** a query where the caller explicitly sets `k`, **When** the query runs, **Then** that explicit depth is used and the classifier's recommendation is ignored (explicit beats recommended beats default).
3. **Given** the classifier disabled (default posture), **When** any query runs, **Then** retrieval depth and mode behave exactly as they do today — no classification occurs.

---

### User Story 3 - Operator visibility of the tuning knobs (Priority: P2)

An operator wants to confirm, from system status and from query responses, what
pool size is in effect, whether the classifier is enabled, and what depth/mode
was actually used for a given query — so tuning is observable rather than a
hidden knob, and so a misclassification or an undersized pool is easy to spot.

**Why this priority**: Tuning knobs without observability are guesswork; this
story closes the loop opened by US1/US2 and protects the no-regression
guarantee. It is independently testable against the status surface.

**Independent Test**: Enable pool tuning and the classifier, run a query, and
read system status plus the query response — confirm the effective pool size,
classifier enablement, and effective depth/mode are all visible and correct.

**Acceptance Scenarios**:

1. **Given** pool tuning and/or the classifier configured, **When** the operator runs `status`, **Then** the current pool-size setting, classifier enablement, and recent pool-utilization are surfaced alongside the existing retrieval configuration.
2. **Given** a query that used classifier-recommended depth and/or a non-default pool, **When** the response is inspected, **Then** the effective depth, mode, and pool size actually used are available (not just the requested values).

---

### Edge Cases

- What happens when the caller sets `k` explicitly? The classifier's recommendation is ignored and the explicit depth wins. (The classifier never recommends mode, so there is no mode conflict to resolve.)
- What happens when the configured pool is smaller than the requested or recommended `k`? The effective pool is grown to at least `k` (plus slack) so the request can be satisfied; the system never silently returns fewer than the requested top-`k`.
- What happens when the classifier misclassifies a query? It must degrade gracefully — never worse than today's fixed default depth — so a bad recommendation cannot reduce quality below the baseline.
- What happens for a query that is empty after normalization (H05)? The classifier returns the default recommendation; no crash, no special-casing of the empty string.
- What happens when the reranker is unavailable (H09)? Pool sizing still governs the fusion candidate budget; utilization surfaces the reranker-absent condition rather than hiding it.
- What happens with the existing result cache (H06), which keys on `(query, mode, k, generation)`? Classifier-recommended `k` and per-query pool values feed that key naturally, so two queries that resolve to different effective depth get separate cache entries — no cache change required, only documented.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST allow the reranker candidate-pool size to be set through configuration and overridden on an individual query, exposed identically on all four transports (CLI, REST, gRPC, MCP).
- **FR-002**: When a query does not specify a pool size, the system MUST use the configured default; the default MUST remain today's value (60) so existing behavior is preserved.
- **FR-003**: The system MUST surface pool-utilization information (candidates considered versus results returned, and rerank-pool saturation) as an **aggregate in system status**, so operators can size the pool. Utilization is NOT attached to individual query responses — the per-query *effective* depth/pool/mode from US3 is a separate, smaller signal that does remain in the response.
- **FR-004**: The system MUST provide a pluggable query-classification extension point (an interface) that returns a recommended retrieval depth (`k`) for a given query, following the same extension-by-interface pattern already used for query transformation and reranking. The v1 classifier MUST NOT recommend or change retrieval mode (hybrid / vector / keyword) — mode remains the caller's explicit choice.
- **FR-005**: The system MUST ship a default rule-based classifier — heuristic only, no model and no network — that maps obvious query shapes to depth (`k`) recommendations, and it MUST be possible to disable it so classification has no effect.
- **FR-006**: When the caller explicitly sets `k` on a query, that explicit value MUST take precedence over the classifier's recommended depth (explicit > recommended > default). Retrieval mode is never recommended, so an explicitly set mode always applies as-is.
- **FR-007**: With the classifier disabled and no explicit pool/depth/mode overrides, a query MUST produce byte-identical results to the pre-H22 system — adaptation is strictly opt-in and introduces no quality regression.
- **FR-008**: The classifier MUST run entirely in-process and MUST NOT call the embedding server (Ollama) or any other network service, so the indexing package stays dependency-free (LLM-based classification is explicitly deferred to a future adapter).
- **FR-009**: All new per-query knobs (pool size, and any classifier-recommended depth) MUST round-trip with identical semantics across CLI, REST, gRPC, and MCP — a query resolved one way over one transport resolves the same way over the others.
- **FR-010**: Pool-size and classification changes MUST be checked for quality regression using the H02 retrieval-eval harness (`make test-eval`); recall@10 MUST remain at or above the current baseline with all new behavior in its default-off posture.
- **FR-011**: When the classifier recommends a shallow `k`, the system MUST reduce the effective candidate pool for that query accordingly (`k` plus a small slack, bounded by the configured pool as a ceiling and a minimum floor), so that reduced depth actually reduces search cost. With no classifier recommendation (or classification disabled), the full configured pool is used — preserving byte-identical default behavior.

### Key Entities *(include if feature involves data)*

- **QueryClassification**: the recommended retrieval depth (`k`) for a query, plus a human-readable rationale, produced by a classifier; applied only when the caller has not explicitly set `k`. (Mode is never part of a classification.)
- **PoolSize**: the number of candidate passages entering reranking (and the fusion candidate budget); set via configuration, overridable per query.
- **PoolUtilization**: an observability signal describing how the candidate pool was consumed — how many candidates were fetched, how many survived to rerank, how many were kept — and whether the pool saturated; surfaced as an **aggregate in system status** (not per-query).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can trade recall against latency without changing code — a broad/comparative query run with a larger pool returns at least the same relevant passages as the default pool (recall ≥ baseline), while a factoid query at classifier-reduced depth and a smaller pool meets (or approaches) the constitution's keyword-only latency budget, and no query regresses past the hybrid latency budget.
- **SC-002**: With the classifier enabled and no explicit `k` set, a clearly factoid query and a clearly broad/comparative query use different effective retrieval depths — demonstrating query-type-aware depth adaptation.
- **SC-003**: `make test-eval` recall@10 is unchanged (≥ baseline) with every new behavior at its default-off posture — no quality regression for existing users.
- **SC-004**: An operator can read system status and see the current pool-size setting, recent pool utilization, and whether the classifier is enabled — confirming the tuning knobs are observable, not hidden.
- **SC-005**: A query with no overrides returns the same passages in the same order as a pre-H22 query — adaptation is strictly opt-in and behavior-preserving.

## Assumptions

- The default posture is OFF — current depth default, pool size of 60, and no classification — so H22 is a no-op for existing queries unless explicitly opted in. (Justifies SC-003/SC-005.)
- The v1 classifier is rule-based (heuristic signals such as query length, token count, and presence of comparative/listing terms). LLM-based classification is explicitly out of scope because it would couple the indexing package to the embedding server, breaking the established extension pattern; it can land later behind an adapter. (Underpins FR-005/FR-008.)
- "Pool size" governs both the fusion candidate budget and the count entering rerank, as it does today; splitting those into two separate knobs is out of scope for H22.
- Cross-transport parity (spec 003) and the H02 retrieval-eval harness are the correctness gates, consistent with every prior retrieval spec (H05/H06/H08/H09/H14).
- The H06 result cache keys on `(query, mode, k, generation)`; classifier-recommended depth and per-query pool values feed that key naturally, so no cache change is required — only a note that differing effective depth (or caller-set mode) produces a distinct cache entry.
- Explicit scope exclusions (separate backlog items, not H22): RRF `k` tuning (H08/spec 009), reranker error-retry with a larger pool (H09/spec 006), query transformation / HyDE / multi-query (H05/spec 012), and score calibration / citation anchoring (H21/spec 023).
