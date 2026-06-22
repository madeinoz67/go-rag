# Feature Specification: Boundary-Aware Chunking

**Feature Branch**: `013-boundary-chunking` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-22

**Status**: Draft

**Input**: User description: "next backlog item" → resolved to **H10** from
`RAG_BOOK_AUDIT_BACKLOG.md` (Phase 3 retrieval-quality cluster):
*"No boundary-aware chunking + doc comment lies about it. Implement the documented
paragraph→sentence→word cascade (M) OR correct the misleading package doc (S).
Decide which."* Source detail: `RAG_BOOK_AUDIT.md` §1.1 (P0, "Chunker has zero
boundary awareness — and its doc comment lies about it").

**Problem (grounded in current code):** `chunk.Split` (`internal/chunk/chunk.go`)
is a pure word-window: it tokenizes on whitespace and emits fixed-size windows of
`perChunk` words stepping by `perChunk − overlapWords`. It has **no** sentence or
paragraph awareness — it will cut a chunk mid-sentence and span across paragraph
breaks. Yet the package documentation *claims* it uses *"a paragraph → sentence →
word cascade with a ~50-token minimum."* That cascade does not exist. So the system
has both a quality gap (boundary-less chunking — the book §3.2 "headline failure
mode": it "destroys meaning") and a truthfulness gap (the doc is false).

> **Scope (resolved 2026-06-22, clarify Q1 → Option A):** implement the
> paragraph→sentence→word cascade. The package doc becomes accurate as a
> consequence; the doc-only (S) alternative was not chosen.

## Clarifications

### Session 2026-06-22

- Q: H10 scope — implement the boundary-aware cascade (M) or correct the doc only (S)? → A: Option A — implement the paragraph→sentence→word cascade (M); the doc becomes true as a consequence.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Chunks respect sentence and paragraph boundaries (Priority: P1) 🎯 MVP

A user ingests prose (markdown notes, a report, a book chapter). Today chunks are
cut at fixed word counts — mid-sentence, across paragraph breaks — so a single
chunk can contain the tail of one sentence and the start of an unrelated next one,
and a chunk can straddle two paragraphs. After this change, a chunk ends at a
sentence boundary whenever one falls within the target window, and paragraph breaks
are respected (a chunk does not silently span paragraphs).

**Why this priority**: This is the book §3.2 headline fix — boundary-aware chunks
are coherent units, which is the foundation of both keyword (whole-term) and
semantic (whole-idea) retrieval quality.

**Independent Test**: Feed prose with known sentence/paragraph structure; assert
that chunks end at sentence boundaries (not mid-sentence) and that paragraph breaks
are honored wherever the paragraph fits the size budget.

**Acceptance Scenarios**:

1. **Given** prose of several sentences, **When** chunked, **Then** no chunk ends
   mid-sentence unless the single sentence itself exceeds the size budget (the
   cascade's word-level fallback).
2. **Given** multi-paragraph text where each paragraph fits the size budget,
   **When** chunked, **Then** chunks do not span paragraph breaks.
3. **Given** a single sentence larger than the size budget, **When** chunked,
   **Then** it is split at word boundaries (the cascade degrades gracefully) rather
   than failing or producing one oversized chunk.

---

### User Story 2 - The package documentation is accurate (Priority: P1)

Whatever the chunker does, its documentation must describe it truthfully. Today the
doc claims a cascade that does not exist. After this change (under either option),
the doc matches reality.

**Why this priority**: A false doc is a trust bug — operators tune chunk size
expecting boundary awareness that isn't there. This requirement holds under BOTH
options (M makes the doc true by implementing the cascade; S makes it true by
removing the false claim).

**Independent Test**: Read the package doc; confirm every claim it makes is
implemented and verified by a test.

**Acceptance Scenarios**:

1. **Given** the package documentation, **When** a reader checks any behavioral
   claim against the implementation + tests, **Then** the claim is true.

---

### User Story 3 - Retrieval quality does not regress (and may improve) (Priority: P2)

Changing chunk boundaries shifts what each chunk contains, which affects retrieval.
The change is gated by the H02 eval harness: it must not regress retrieval quality
(and more-coherent chunks should plausibly help).

**Why this priority**: A chunking change that hurts retrieval is a regression. The
harness exists to prove each retrieval change helps (or at least doesn't hurt).

**Independent Test**: Run the H02 harness before/after; confirm recall@10/MRR are
no worse.

**Acceptance Scenarios**:

1. **Given** the H02 golden dataset, **When** re-chunked with boundary awareness,
   **Then** recall@10 and MRR are no worse than the current word-window baseline
   (no regression).

---

### Edge Cases

- **Over-long single sentence** (> size budget): MUST fall back to word-splitting
  that sentence (the cascade's word level) — never one oversized chunk, never a
  failure.
- **No sentence terminators** (e.g. a list, a log line, CJK without punctuation):
  MUST degrade gracefully to the word-window behavior rather than producing one
  giant chunk.
- **CJK sentence boundaries** (`。！？`): sentence detection SHOULD recognize them
  (don't silently mis-boundary CJK prose). (Related: token-estimate-on-CJK is H26.)
- **Tiny tail** (< minimum tokens): MUST merge into the previous chunk (existing
  behavior preserved).
- **Overlap**: neighbor chunks MUST still share content (the existing overlap
  guarantee), now at sentence/structural boundaries rather than arbitrary word
  counts.
- **Whitespace preservation**: segments currently normalize internal whitespace
  (join with single spaces); the cascade should preserve readability and not drop
  content.
- **Idempotent ingestion** (Principle II): re-chunking under the new splitter
  changes chunk identities, so re-ingestion after this change is expected (a
  migration/re-add, not a dedup violation) — documented, not silent.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Implement the documented paragraph→sentence→word cascade so chunks
  become boundary-aware (scope resolved: clarify Q1 → Option A, 2026-06-22). The
  package documentation becomes accurate as a consequence (FR-005).

- **FR-002**: A chunk MUST end at a sentence boundary whenever a sentence boundary
  falls within the target-size window (US1 acceptance 1).
- **FR-003**: Paragraph breaks MUST be respected — a chunk MUST NOT span a paragraph
  boundary when the paragraph fits the size budget (US1 acceptance 2).
- **FR-004**: A single sentence larger than the size budget MUST be split at word
  boundaries (graceful degradation), never emitted as one oversized chunk (US1
  acceptance 3).
- **FR-005**: The package documentation MUST accurately describe the implemented
  behavior — no claim that isn't true (holds under both options; US2).
- **FR-006**: Retrieval quality MUST NOT regress — the H02 harness MUST show
  recall@10 and MRR no worse than the word-window baseline (US3, SC-003).
- **FR-007**: Neighbor chunks MUST continue to share overlap content (the existing
  overlap contract), now at structural boundaries.

### Key Entities *(include if feature involves data)*

- **Segment** (existing): one chunk's text + char offsets + token count. Unchanged
  shape; produced differently (boundary-aware assembly vs fixed word-window).
- **Sentence / paragraph boundary**: structural units the cascade uses to assemble
  coherent chunks. Internal to the splitter; not persisted as new data.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Chunks end at sentence boundaries (not mid-sentence) wherever the
  source has a boundary within the window — verifiable on structured prose.
- **SC-002**: Paragraph breaks are honored (no chunk spans paragraphs when the
  paragraph fits the budget) — verifiable on multi-paragraph input.
- **SC-003**: The H02 retrieval harness shows recall@10 and MRR no worse than the
  word-window baseline (no regression; improvement is a bonus).
- **SC-004**: The package documentation is accurate — every behavioral claim is
  implemented and test-covered.

## Assumptions

- **Scope is M (implement the cascade)** — resolved via clarify Q1 (Option A,
  2026-06-22). It is the P0 headline fix and makes the doc true with real value,
  rather than just removing a false claim.
- **The cascade is the documented shape**: prefer paragraph boundaries, then
  sentence boundaries, then word-level fallback for over-long sentences; greedy-fill
  to the size budget; preserve the minimum-tail-merge and overlap guarantees. Exact
  algorithm (sentence detector, overlap granularity) is plan territory.
- **Sentence detection is punctuation-based** (`. ! ?` + CJK `。！？`); no NLP/ML
  dependency (Principle III — pure Go, no CGo). A sentence-segmenter library is not
  introduced; a focused rule-based detector suffices.
- **Re-chunking changes chunk identities**: existing vaults re-ingest after the
  change (content-addressed identity means re-add is idempotent — no duplicates,
  Principle II). This is expected, not a bug.
- **Out of scope**: near-duplicate chunk dedup (H20), per-chunk metadata enrichment
  / `section_context` (H23 — separate item), markdown-heading-aware chunking (H23),
  the CJK token-estimate fix (H26), and LLM-driven chunking.
