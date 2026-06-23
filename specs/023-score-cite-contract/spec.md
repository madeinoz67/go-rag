# Feature Specification: Score Calibration + Citation Contract

**Feature Branch**: `023-score-cite-contract` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-23

**Status**: Draft

**Input**: "look at H21 in backlog" → **H21** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 6):
*"Score not calibrated + citation contract under-documented. Normalize scores to [0,1]
within a result set; document `chunk_id` as the canonical citation anchor; add a
`chunk_index` ordinal within a document."* (Audit §1.5; book §7.3 — *"require the model
to cite sources… verify those citations"*; §8.2 — ambiguity detection treats score as a
confidence proxy.)

**Why this matters**: today go-rag's hit scores are raw RRF-fusion values — dimensionless
and uncomparable across queries or modes. A client or downstream LLM can't judge "is hit #1
actually relevant?" from the number, can't set a meaningful threshold, and the citation
contract (`chunk_id` as the stable anchor) is undocumented. This blocks grounded
attribution (the book's §7.3 "cite sources… verify those citations" pattern).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Calibrated scores a client can act on (Priority: P1)

Stephen queries go-rag from an LLM pipeline and needs to decide whether hit #1 is actually
relevant — or whether to escalate / say "I don't know." Today the raw RRF score (e.g.
0.033) is meaningless for that decision. He wants scores **normalized to [0,1]** within a
result set, so the best hit is always ~1.0 and he can set a threshold ("only use hits
above 0.5") that means something.

**Why this priority**: uncalibrated scores block the book's confidence-based grounding
pattern (§8.2); a client can't act on the number without calibration.

**Independent Test**: run a query returning multiple hits; the top hit's normalized score
is 1.0 (or close), and scores decrease monotonically; a `--threshold` set on the
normalized scale filters correctly.

**Acceptance Scenarios**:

1. **Given** a query returning 5 hits, **When** the result is returned, **Then** the top
   hit's normalized score is 1.0 and scores decrease monotonically through the set.
2. **Given** the same result, **When** the operator sets `--threshold 0.5`, **Then** only
   hits whose normalized score ≥ 0.5 are returned.
3. **Given** two different queries with different raw-score magnitudes, **Then** their
   normalized top hits are both ~1.0 (comparable across queries).

---

### User Story 2 - Stable citation anchor + chunk ordinal (Priority: P2)

Stephen's LLM pipeline generates responses that cite go-rag chunks. He needs a **stable,
deterministic citation handle** — one that survives re-ingestion and resolves to the exact
text. `chunk_id` (SHA-256 content-addressed) is already that handle, but the contract is
**undocumented**. He also wants a human-readable `chunk_index` (ordinal within the source
document) so citations can say "Doc X, chunk 3" instead of an opaque hash.

**Why this priority**: enables grounded attribution (book §7.3); builds on US1 (the client
needs both a calibrated score AND a stable citation to act).

**Independent Test**: query a multi-chunk doc; each hit carries `chunk_index` (0-based
ordinal within the document); the citation contract is documented.

**Acceptance Scenarios**:

1. **Given** a query hit, **Then** the hit carries `chunk_index` — the 0-based ordinal of
   that chunk within its source document.
2. **Given** the same document re-ingested (content unchanged), **Then** the `chunk_id`
   + `chunk_index` are identical (stable citation — idempotent, Constitution II).
3. **Given** the citation contract documentation, **Then** a client developer knows:
   `chunk_id` is the canonical citation anchor (content-addressed, stable); `chunk_index`
   is the human-readable ordinal; `document_id` is the doc-level handle.

---

### Edge Cases

- **Single-hit result**: normalization with one hit → score = 1.0 (trivially calibrated).
- **All hits tied**: equal raw scores → equal normalized scores (no artificial ordering).
- **Zero-hit result**: no scores to normalize (empty result unchanged).
- **Reranked results**: when a reranker is active, the reranker scores (already 0..1 from
  H09's `parseScores`) should be used as the normalized scores directly — no double-
  normalization.
- **Threshold semantics**: `--threshold` applies to the NORMALIZED score (0..1), documented
  as relative-within-result, NOT absolute confidence across queries.
- **chunk_index after re-chunking**: if the chunker changes (size/overlap), `chunk_index`
  shifts but `chunk_id` stays stable (content-addressed) — documented.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST normalize hit scores to [0,1] within each result set, so the
  top hit is 1.0 and scores decrease monotonically (or are equal for ties).
- **FR-002**: The `--threshold` flag MUST apply to the NORMALIZED score (0..1 scale), not
  the raw RRF/rerank score.
- **FR-003**: When a reranker is active, its scores (already 0..1) MUST be used as the
  normalized scores directly (no double-normalization).
- **FR-004**: Each query hit MUST carry `chunk_index` — the 0-based ordinal of the chunk
  within its source document (alongside the existing `chunk_id`).
- **FR-005**: The system MUST document the citation contract: `chunk_id` is the canonical
  citation anchor (SHA-256 content-addressed, stable across re-ingestion); `chunk_index` is
  the human-readable ordinal; `document_id` is the doc-level handle; `file_path` + `page`
  provide human-display provenance.
- **FR-006**: The normalized score MUST be surfaced on every transport (CLI/REST/gRPC/MCP)
  as the hit's score (replacing the raw RRF value); the raw score is retained internally
  for debugging but not exposed by default.
- **FR-007**: Normalization MUST NOT change the ranking order (only the score values); the
  eval harness recall@10 MUST be unchanged.

### Key Entities *(include if feature involves data)*

- **NormalizedScore**: the calibrated score (0..1) within a result set. Top hit = 1.0;
  others scaled proportionally. Computed post-ranking, pre-return. The `--threshold` acts
  on this scale.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A query returning N hits has the top hit's score = 1.0 and all subsequent
  scores ≤ 1.0, decreasing monotonically (or equal for ties) — verifiable.
- **SC-002**: `--threshold 0.5` on a normalized result filters hits below 0.5 — verifiable.
- **SC-003**: Each hit carries `chunk_index` (0-based ordinal within the document) —
  verifiable by querying a known multi-chunk doc.
- **SC-004**: `chunk_id` + `chunk_index` are stable across re-ingestion of unchanged
  content (Constitution II) — verifiable.
- **SC-005**: The citation contract is documented (chunk_id = anchor, chunk_index = ordinal,
  threshold = relative-within-result) — verifiable by reading the docs.
- **SC-006**: `make test-eval` recall@10 unchanged (ranking order preserved); `go
  build/vet/test` green.

## Assumptions

- **Normalization method**: min-max within the result set (top = 1.0; the lowest = scaled
  proportionally). Simple, deterministic, no parametric assumptions. Alternative: softmax
  (rejected — introduces a temperature parameter; min-max is parameter-free + the book's
  §8.2 doesn't mandate a specific calibration).
- **Reranked results**: the reranker's 0..1 scores (H09) ARE the normalized scores (they're
  already calibrated by the cross-encoder's max-relative normalization). No additional
  normalization applied.
- **Threshold semantics**: documented as **relative-within-result** (not absolute confidence
  across queries). A threshold of 0.5 means "top half of THIS result set" — not "50%
  confident" (which would require a calibrated probability model, out of scope).
- **chunk_index**: 0-based ordinal within the document (matches the existing `ChunkIndex`
  field on `model.Chunk`). Already stored; just surfaced on the hit payload.
- **Scope**: score normalization + chunk_index surfacing + citation-contract docs. **Out of
  scope**: absolute-confidence calibration (logistic/Platt scaling — needs a calibration
  dataset); softmax temperature; per-hit `match_mode` (keyword/semantic/both — audit H21
  optional, separate future work).
- **Constitution gates**: II (chunk_id stable — content-addressed), V (normalized score +
  chunk_index surfaced on all transports).
