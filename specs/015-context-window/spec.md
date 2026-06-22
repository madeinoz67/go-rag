# Feature Specification: Context Window — Sibling-Chunk Expansion

**Feature Branch**: `015-context-window` *(commits directly to `main` per project convention.)*

**Created**: 2026-06-22 · **Status**: Draft

**Input**: "next backlog item" → **H15** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 3, last item):
*"No parent-child / context expansion (plumbing exists, unused). `ContextWindow` option that fetches
sibling chunks via the existing `PreviousChunkID`/`NextChunkID`."* Source: `RAG_BOOK_AUDIT.md` §1.4
(P1: "small chunks retrieve well but lack context — return parent/sentence-window").

**Problem:** Chunks are deliberately small for retrieval precision, but a hit in isolation often
lacks the surrounding context a reader needs — the sentence before/after, the setup, the conclusion.
go-rag's `Chunk` model already carries `PreviousChunkID` / `NextChunkID` (a linked list within each
document), but these are unused — queries return raw isolated chunk text. The fix: a per-query
`ContextWindow` option that, for each hit, fetches and includes its sibling chunks' text, so a
reader (or an AI agent) sees the hit in its document context.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A hit includes its surrounding context (Priority: P1) 🎯 MVP

A user queries and gets ranked hits. With `ContextWindow=N`, each hit is augmented with the text of
up to N previous and N next sibling chunks (via the linked list), so the reader sees the hit in
context, not as an isolated fragment.

**Why this priority**: This is H15's core — the book §6.4 headline: small chunks retrieve well but
lack context; expanding to siblings gives the reader the surrounding passage without storing
duplicate parents. The linked-list plumbing exists; this wires it.

**Independent Test**: Ingest a multi-chunk document; query with ContextWindow=1; assert each hit's
result includes the previous and/or next chunk's text as context.

**Acceptance Scenarios**:

1. **Given** a multi-chunk document, **When** the user queries with `ContextWindow=1`, **Then** each
   hit includes up to 1 previous + 1 next sibling chunk's text as context.
2. **Given** a hit at the start of a document (no previous chunk), **When** `ContextWindow=1`,
   **Then** only the next sibling is included (no error, no missing-previous crash).
3. **Given** a hit at the end (no next chunk), **When** `ContextWindow=1`, **Then** only the
   previous sibling is included.
4. **Given** `ContextWindow=0` (the default), **When** the user queries, **Then** results are
   byte-identical to today — no context, just the hit text.

---

### User Story 2 - Context is clearly distinguished from the hit (Priority: P2)

The expanded context is not additional ranked hits — it is context FOR a hit. The result shape
clearly distinguishes the hit from its context siblings (so a caller doesn't treat context as
separate retrievable results).

**Why this priority**: Semantic clarity — context chunks are not independently retrieved; they're
context. The result shape must reflect that (the hit is the primary result; context annotates it).

**Independent Test**: Inspect the result structure; confirm context chunks are distinguishable from
the primary hit (not flattened into the ranked list).

**Acceptance Scenarios**:

1. **Given** a result with context expansion, **When** a caller reads the result, **Then** the
   primary hit and its context siblings are clearly distinguishable (separate fields, not
   interleaved).
2. **Given** context chunks, **When** the result is serialized, **Then** context chunks do NOT
   appear as ranked hits (they don't affect the ranking or the top-k count).

---

### User Story 3 - Cross-transport + opt-in (Priority: P2)

`ContextWindow` is opt-in (default 0 = off), exposed identically on CLI/REST/gRPC/MCP, and does
not regress retrieval quality (the eval harness confirms no change).

**Why this priority**: Consistency (parity) + safety (opt-in, no default behavior change).

**Independent Test**: Same query with ContextWindow over CLI/REST/gRPC/MCP → identical context;
`make test-eval` shows no regression.

**Acceptance Scenarios**:

1. **Given** the query operation on any transport, **When** `ContextWindow=N` is supplied, **Then**
   the results include context identically across CLI/REST/gRPC/MCP.
2. **Given** the H02 eval harness, **When** run with default (no ContextWindow), **Then** recall@10
   is unchanged (context expansion is opt-in, not a ranking change).

---

### Edge Cases

- **No previous/next** (first/last chunk in doc): include only available siblings; no error.
- **PreviousChunkID/NextChunkID empty**: if the linked list is not populated for a chunk, no context
  is available for that direction (graceful, not an error). The plan MUST verify whether the pipeline
  currently populates these fields; if not, populate them as part of H15.
- **ContextWindow=0**: off (default) — byte-identical to today.
- **Large ContextWindow**: requesting more siblings than exist → return all available (no error).
- **Context chunks from a different document**: MUST NOT happen — siblings are within the same
  document (the linked list is per-document). If PreviousChunkID/NextChunkID somehow reference
  another document's chunk, that is a data-integrity bug (not expected).
- **Context chunks and rerank**: context chunks are NOT reranked (they're context, not candidates).
  Rerank operates on the candidate pool as today; context is expanded AFTER ranking.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A query MUST accept an optional `ContextWindow` (integer) specifying how many sibling
  chunks to include on each side of a hit (previous + next). Default 0 = off.
- **FR-002**: For each hit, when `ContextWindow > 0`, the result MUST include up to N previous and
  N next sibling chunks' text, fetched via the `PreviousChunkID`/`NextChunkID` linked list.
- **FR-003**: The pipeline MUST populate `PreviousChunkID`/`NextChunkID` during chunk creation (if
  not already — the plan verifies and populates if needed).
- **FR-004**: Context chunks MUST be clearly distinguishable from the primary hit in the result
  structure (not flattened into the ranked list, not counted in top-k).
- **FR-005**: `ContextWindow=0` (the default) MUST produce results byte-identical to today.
- **FR-006**: Missing siblings (first/last chunk, or empty linked-list IDs) MUST be handled
  gracefully — only available siblings are included, never an error.
- **FR-007**: Context expansion MUST happen AFTER ranking/rerank — context chunks do not affect the
  ranking or the top-k count.
- **FR-008**: `ContextWindow` MUST be exposed identically on CLI/REST/gRPC/MCP (cross-transport
  parity).

### Key Entities

- **ContextWindow**: a per-query option (integer, default 0) — how many sibling chunks each side to
  include. Opt-in; 0 = no context expansion.
- **Expanded hit**: a ranked hit augmented with its context siblings' text (previous N + next N),
  distinguishable from the primary hit in the result structure.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A query with `ContextWindow=1` returns each hit augmented with up to 1 previous + 1
  next sibling chunk text (verifiable on a multi-chunk document).
- **SC-002**: `ContextWindow=0` (default) produces results byte-identical to today (no change).
- **SC-003**: The H02 eval harness shows no regression (context is opt-in, not a ranking change).
- **SC-004**: Same `ContextWindow` over CLI/REST/gRPC/MPC returns identical context (parity).

## Assumptions

- **PreviousChunkID/NextChunkID may not be currently populated** by the pipeline. The plan MUST
  verify; if empty, populate them during chunk creation (a small pipeline change, set
  `chunks[i].PreviousChunkID = chunks[i-1].ID` / `NextChunkID = chunks[i+1].ID` for i>0/i<len-1).
- **ContextWindow is per-query** (`QueryRequest.ContextWindow int`, default 0 = off).
- **Context text is returned alongside the hit**, not as additional ranked results — the result
  shape gains a context field (e.g., `Context []ContextChunk` on `QueryHit`), clearly distinguishable.
- **Sibling lookup** uses the existing chunk → `lookupChunk(db, PreviousChunkID/NextChunkID)` chain
  (a Pebble Get per sibling; O(N) per hit, negligible for small N).
- **Transport exposure**: CLI `--context-window`, REST/gRPC/MCP request fields (like H14's filter).
  Proto regen needed for gRPC (same pattern as H08/H14).
- **Out of scope**: parent-document retrieval (a different pattern — retrieving a larger parent
  chunk that was split); this is sibling/neighbor context via the linked list only. Also out of
  scope: the query/result cache (H06).
