# Feature Specification: Reranker Error Surfacing

**Feature Branch**: `006-rerank-error-surfacing`

**Created**: 2026-06-21

**Status**: Draft

**Input**: User description: "H09 — Reranker errors silently swallowed (audit §1.4). On a rerank error, or when the reranker returns a score count that does not match the candidate count, go-rag currently discards the error and returns fallback-ordered hits as if nothing happened. Fix: log the error, surface a rerank-failed indicator, optionally retry with a larger pool, never swallow. Pulled from `RAG_BOOK_AUDIT_BACKLOG.md` as the next item (Phase 1, P0, S)."

## Clarifications

### Session 2026-06-21

- Q: Rerank status contract shape (boolean flag vs tri-state enum) → A: Single boolean `RerankFailed` flag (Option B). `true` only when reranking was attempted and failed (error or score-count mismatch); non-failure cases (succeeded / reranking not configured / empty candidate pool) are `false`/absent and are NOT distinguished on the response.
- Q: Scope — also fix the sibling silently-swallowed candidate-retrieval error on the rerank path? → A: Yes, in scope (Option A). A retrieval failure on the rerank path is surfaced as a query error rather than masquerading as empty results; both swallows in the same function are fixed together.
- Q: What goes in the rerank-failure log? → A: Error cause + metadata only (Option A). The log records the error message, reranker model, candidate count, and score count; query text and candidate content are never logged.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Know when reranking was skipped (Priority: P1)

A local go-rag operator runs a hybrid query that uses a cross-encoder reranker to
re-order candidates. The reranker call fails — the model endpoint is down, the
response is malformed, or it returns a partial score list whose length does not
match the candidate count. Today the operator silently receives possibly-misranked
results with no indication anything went wrong. The operator needs every query
response to state clearly whether the ordering was produced by the reranker or by
the fallback ordering, so they can decide whether to trust the results, fix the
reranker, or re-query.

**Why this priority**: Silent wrong results are a P0 correctness/observability gap.
The operator cannot trust retrieval quality they cannot see. This story is the
minimum viable fix and is independently valuable — it converts a hidden failure
into a visible one.

**Independent Test**: Run a query against a vault with reranking enabled while the
reranker endpoint is unreachable; assert the response still returns results AND
carries a clearly visible rerank-failed indicator, and that the failure is written
to the log.

**Acceptance Scenarios**:

1. **Given** a vault with reranking enabled and the reranker reachable, **When** the operator queries, **Then** results are returned and the response indicates reranking succeeded (no failure indicator).
2. **Given** a vault with reranking enabled and the reranker unreachable, **When** the operator queries, **Then** results are still returned in fallback order AND the response carries a rerank-failed indicator AND the error is recorded in the log.
3. **Given** a reranker that returns fewer or more scores than candidates, **When** the operator queries, **Then** the response carries the rerank-failed indicator and fallback-ordered results (a score-length mismatch is treated identically to an outright error).

---

### User Story 2 - Consistent failure signal across every interface (Priority: P2)

The operator accesses go-rag over its programmatic interfaces (interactive,
HTTP-style, and RPC-style transports). Whichever interface they use, the
rerank-failed signal must be present and mean the same thing, so tooling built on
any transport can react to degraded results identically.

**Why this priority**: Cross-transport parity is a core product guarantee. A fix
visible on only one transport would recreate the silent-failure problem on the
others, so it must land on all of them together.

**Independent Test**: Issue the same failing-reranker query over every supported
transport and assert each response exposes the rerank-failed indicator with the
same semantics.

**Acceptance Scenarios**:

1. **Given** a reranker failure, **When** the operator queries over each supported transport, **Then** every response carries the rerank-failed indicator.
2. **Given** a successful rerank, **When** the operator queries over any transport, **Then** none of the responses carry the failure indicator.

---

### User Story 3 - Optional recovery retry on failure (Priority: P3)

For recall-sensitive workloads, an operator may opt into an automatic second
attempt that reruns reranking against a larger candidate pool when the first
attempt fails, to salvage reranked quality before falling back. This is opt-in and
off by default, so the common path adds no latency.

**Why this priority**: Recovery is a refinement; the core requirement is
observability (User Story 1). Many operators will prefer the simpler, faster
fail-straight-to-fallback behavior, so retry is not forced on them.

**Independent Test**: Enable the retry option, point the reranker at a flaky
endpoint, and query; assert the system attempts once more with a larger pool before
reporting rerank-failed.

**Acceptance Scenarios**:

1. **Given** retry-with-larger-pool is enabled and the reranker fails, **When** the operator queries, **Then** the system retries once with a larger candidate pool; on success results are reranked normally, on failure the rerank-failed indicator is surfaced.
2. **Given** retry-with-larger-pool is disabled (the default), **When** the reranker fails, **Then** no retry occurs and fallback results with the failure indicator are returned immediately.

---

### Edge Cases

- **Candidate retrieval fails on the rerank path**: the hybrid search that feeds the reranker can also fail. That retrieval-stage error is surfaced as a query error — not masked as empty results, and not relabeled as a rerank failure. This is distinct from a rerank failure, which degrades to fallback-ordered results with `RerankFailed=true`.
- **Empty candidate pool**: when there is nothing to rerank, no rerank is attempted and `RerankFailed` stays `false` — this is not a failure and is not surfaced as one.
- **Reranking not enabled**: when reranking is not configured for a query, `RerankFailed` stays `false` (not a failure); the operator already knows reranking is off, so no per-response signal is needed.
- **Partial scores parse**: if only some scores are parseable, this is a length mismatch → rerank-failed; the system never silently drops the unparseable candidates.
- **Concurrent failing queries**: each query's failure indicator and log entry must be independent — none lost, none coalesced into another query's response.
- **Reranker returns scores from the wrong model**: out of scope for this item (embedding/model drift is H11); a well-formed score array of matching length still counts as a successful rerank here.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: On a rerank error OR a score-count mismatch between reranker output and candidate count, the system MUST return results in fallback (pre-rerank) order rather than discarding the query or returning nothing.
- **FR-002**: Every query response MUST carry a single boolean `RerankFailed` flag, set `true` only when reranking was attempted but failed (rerank error or score-count mismatch). When reranking succeeds, is not configured, or faces an empty candidate pool, the flag is `false`/absent — those non-failure cases are not distinguished on the response.
- **FR-003**: On rerank failure, the system MUST record the failure in the application log with enough detail to diagnose the cause — at minimum the error message, the reranker model, the candidate count, and the returned score count. The query text and candidate content MUST NOT be written to the log.
- **FR-004**: The `RerankFailed` flag MUST be exposed consistently across every supported query interface, with identical semantics on each (a transport-agnostic contract).
- **FR-005**: The system MUST NOT discard a rerank error silently — a failure is always observable via both the response status and the log.
- **FR-006**: An operator MAY enable an optional, off-by-default behavior that retries reranking once against a larger candidate pool before falling back; when disabled, failure goes straight to fallback.
- **FR-007**: The rerank-failed path MUST preserve result-count semantics — it returns up to the requested number of results whenever enough candidates exist — so a reranker outage degrades ranking quality, not result completeness.
- **FR-008**: Retrieval-stage errors MUST remain distinguishable from rerank-stage errors — a retrieval failure must never be relabeled as a rerank failure, and a rerank failure must never be relabeled as a retrieval failure.
- **FR-009**: On the rerank path, a failure of the candidate-retrieval call MUST be surfaced (propagated as a query error) rather than silently discarded — a retrieval failure must not masquerade as empty results. (The non-rerank path already propagates; this brings the rerank path to parity.)

### Key Entities *(include if feature involves data)*

- **Query Response**: the result of a query. Gains a single boolean `RerankFailed` flag and, when failed, may carry a short diagnostic reason. The existing result list and per-result scores are unchanged on the happy path.
- **Rerank Failure Record**: a logged event capturing when and why a rerank failed (error message, reranker model, candidate count vs. score count). It deliberately excludes the query text and candidate content.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of rerank failures produce both a visible response-level failure indicator and a log entry — it is impossible for a rerank failure to pass silently.
- **SC-002**: For any query response, an operator can determine whether the returned ordering came from the reranker or from the fallback — without inspecting logs.
- **SC-003**: The same failing-reranker query yields the same `RerankFailed` flag value over every supported transport.
- **SC-004**: Degraded (rerank-failed) responses still return the requested number of results whenever enough candidates exist, so a reranker outage degrades ranking quality rather than blocking retrieval.
- **SC-005**: The existing retrieval-quality regression harness shows no recall@10 regression attributable to this change on the happy path — confirming the fix is observability-only when reranking succeeds.
- **SC-006**: A candidate-retrieval failure on the rerank path is reported as a query error, never returned as silent empty results.

## Assumptions

- **Graceful degradation, not fail-closed**: when reranking fails, the system returns fallback-ordered results plus a failure indicator (matching the audit's recommendation), rather than rejecting the query outright. An operator who wants fail-closed behavior can enforce it in their own tooling by treating the failure indicator as an error.
- **Retry is opt-in and off by default**: the retry-with-larger-pool behavior ships disabled so the core fix adds no latency to the common path.
- **Sensitive query text in logs**: excluded. The failure log records only error cause and metadata (model, candidate count, score count, error message) — never query text or candidate content. This sets the precedent the unbuilt audit-log item (H18) will inherit.
- **Cross-transport parity**: surfacing the rerank status follows the project's existing parity pattern across interfaces; each transport gains the status field in its idiomatic form.
- **Scope boundary**: this item covers rerank-stage error surfacing AND the sibling silently-swallowed candidate-retrieval error on the rerank path (the same anti-pattern in the same function). Embedding/model drift detection (H11) and reranker pool-size tuning (H22) remain separate backlog items.
