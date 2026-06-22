# Feature Specification: Metadata Filtering at Retrieval

**Feature Branch**: `014-retrieval-filter` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-22

**Status**: Draft

**Input**: User description: "next backlog item" → resolved to **H14** from
`RAG_BOOK_AUDIT_BACKLOG.md` (Phase 3 retrieval-quality cluster):
*"No metadata filtering at retrieval. Optional `Filter` (source/type/tags) in `Search`
— pre-FTS filter plus post-filter on vector hits."* Source detail: `RAG_BOOK_AUDIT.md`
§1.3 (P2, vector layer has no predicate) and §1.4 (P1, "No metadata filtering pre- or
post-retrieval — `Search` has no filter param").

**Problem (grounded in current code):** A query searches the entire vault — there is
no way to narrow it to a subset of documents. A user who wants "results only from my
meeting-notes folder," "only PDFs," or "only documents tagged `security`" cannot express
that; they get the whole index ranked and must mentally filter. The CLI even has a
`--source` flag, but it is unwired (the query request carries no filter, so it does
nothing). The fix: an optional filter (by source path, file type, and/or tags) applied
during retrieval — pruning the keyword candidate set before scoring and filtering the
vector hits — so a query can be scoped to a document subset.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Scope a query to a document subset (Priority: P1) 🎯 MVP

A user narrows a query to documents matching a filter — by source path (a folder or
glob), by file type, and/or by tag — and receives results **only** from that subset.
A filter that matches nothing returns no results (not an error).

**Why this priority**: This is H14's core — the ability to scope retrieval. It is the
difference between "search everything" and "search the relevant slice," which is how
real users query growing vaults.

**Independent Test**: Ingest documents with known source/type/tag attributes; issue a
query with a filter; assert every returned result comes from a document matching the
filter, and that an unfiltered query still returns everything.

**Acceptance Scenarios**:

1. **Given** a vault with documents from two folders, **When** the user queries with a
   source filter for folder A, **Then** only folder-A documents appear in the results.
2. **Given** mixed file types (.md, .txt, .pdf), **When** the user filters by type
   `.md`, **Then** only markdown documents appear.
3. **Given** documents tagged `security` and others tagged `ops`, **When** the user
   filters by tag `security`, **Then** only `security`-tagged documents appear.
4. **Given** a filter that matches no document, **When** the user queries, **Then** the
   result set is empty (no error, no crash).
5. **Given** no filter, **When** the user queries, **Then** behavior is identical to
   today (every document is eligible) — the filter is purely opt-in.

---

### User Story 2 - The filter is applied efficiently (Priority: P2)

The filter prunes the keyword candidate set **before** scoring and filters vector hits
**after** retrieval, so a filtered query is no slower than an unfiltered one (bounded by
the filter check, not a full re-scan). The filter does not require materializing the
whole index.

**Why this priority**: A filter that works by retrieving everything and discarding is
correct but wasteful. The audit asks for pre-FTS pruning + post-vector filtering, which
keeps filtered retrieval cheap.

**Independent Test**: Time a filtered vs unfiltered query on a sizeable vault; assert
the filtered one is not slower (and ideally faster, having less to score/rank).

**Acceptance Scenarios**:

1. **Given** a vault, **When** a source/type/tag filter is applied, **Then** the keyword
   leg scores only matching candidates (pre-FTS pruning) and the vector leg filters its
   top-k to matching hits — the filter is not a full post-hoc scan of all chunks.
2. **Given** a filtered query, **When** it runs, **Then** it completes no slower than
   the same query unfiltered.

---

### User Story 3 - The filter is consistent across every transport (Priority: P2)

The filter is expressed the same way over the CLI, REST, gRPC, and MCP — the same
source/type/tag values yield identical results regardless of how the query arrives.
The existing CLI `--source` flag is wired (it currently does nothing).

**Why this priority**: Cross-transport parity (spec 003) — the engine is the single
source of truth, so a filter expressed on any transport produces the same scoped result.

**Independent Test**: Issue the same query + filter over CLI, REST, gRPC, and MCP;
assert identical result sets.

**Acceptance Scenarios**:

1. **Given** the query operation on any transport, **When** a source/type/tag filter is
   supplied, **Then** the results are scoped identically across CLI, REST, gRPC, and MCP.
2. **Given** the CLI `--source` flag, **When** set, **Then** it actually filters (today
   it is a no-op) — the gap is closed.

---

### Edge Cases

- **No filter**: MUST behave exactly as today (every document eligible) — the filter is
  opt-in and defaults to "no filter."
- **Filter matches nothing**: MUST return an empty result set, not an error.
- **Multiple dimensions**: a filter with source AND type AND tags MUST be a conjunction
  (all must match) — the stricter, more predictable default.
- **Tags**: a document with no tags MUST NOT match a tag filter; a tag filter matches a
  document that carries the tag(s).
- **Source glob**: source filtering uses path-glob semantics (e.g., `docs/**`, `*.md`
  by path) consistent with the existing `--source` flag's intent.
- **Filter × mode**: the filter MUST apply in keyword, semantic, and hybrid modes.
- **Filter × collapse-by-doc**: the per-document collapse (top-1 per doc) MUST apply
  AFTER filtering (collapse among the filtered set), not before.
- **Filter × rerank**: rerank operates on the filtered candidate pool.
- **Empty/whitespace filter values**: a dimension left empty MUST be ignored (treated as
  "no constraint on this dimension"), not matched against empty attributes.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A query MUST accept an optional filter with up to three dimensions —
  source (path glob), type (file type), and tags — that scopes results to matching
  documents.
- **FR-002**: Filter dimensions MUST combine as a conjunction (a document must match
  every specified dimension to be eligible).
- **FR-003**: A filtered query MUST return only results from documents matching the
  filter; a filter matching nothing MUST return an empty result set (not an error).
- **FR-004**: An unfiltered query (no filter / empty filter) MUST behave identically to
  today — the filter is opt-in and has zero effect when absent.
- **FR-005 (efficiency)**: The filter MUST be applied as pre-FTS pruning (keyword
  candidates) and post-vector filtering (vector hits) — not a full post-hoc scan of all
  chunks.
- **FR-006**: The filter MUST apply in all retrieval modes (keyword, semantic, hybrid).
- **FR-007**: The per-document collapse and rerank MUST operate on the filtered set
  (filter first, then collapse/rerank).
- **FR-008**: The filter MUST be expressible identically on the CLI, REST, gRPC, and MCP
  (cross-transport parity), and the CLI `--source` flag MUST be wired (it is currently a
  no-op).
- **FR-009**: An empty filter dimension (blank source/type, no tags) MUST be ignored
  (treated as "no constraint"), never matched against empty attributes.

### Key Entities *(include if feature involves data)*

- **Filter**: the scoping predicate — `{source (path glob), type (file type), tags
  ([]string)}`. Conjunction across dimensions. Optional on every query; absent = no
  filtering.
- **Document attributes** (the filter's match targets): the document's source path, file
  type, and tags (metadata). Resolved from a chunk via its document — the filter evaluates
  document attributes, applied to that document's chunks.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A filtered query returns results **only** from documents matching the
  filter (source/type/tags); verifiable by checking every result's document attributes.
- **SC-002**: An unfiltered query is byte-identical in results to today's behavior
  (no regression — the filter is opt-in).
- **SC-003**: The same filter over CLI, REST, gRPC, and MCP returns identical result
  sets (parity).
- **SC-004**: The H02 eval harness shows no regression (default queries carry no filter;
  recall@10/MRR unchanged) — the filter is purely additive.

## Assumptions

- **Source** = the document's file path, matched by glob (e.g., `docs/**`, a folder
  prefix). Consistent with the existing (unwired) CLI `--source` flag's intent.
- **Type** = the document's file type (e.g., `.md`), matched exactly.
- **Tags** = document tags (metadata); a tag filter matches a document carrying the
  specified tag(s). If tags are not yet a first-class Document field, the plan maps them
  to document metadata (a reasonable default; the filter's contract is unchanged).
- **Conjunction**: multiple filter dimensions AND together (all must match).
- **Filter evaluates document attributes**, applied to that document's chunks (a chunk is
  eligible iff its document matches). Resolved via the existing chunk→document lookup
  (the engine already resolves chunk→doc for `docOf`/`FilePath`).
- **Transport exposure**: CLI flags (`--source` wired + `--type`/`--tags`) and
  REST/gRPC/MCP request fields (proto additions), for parity — consistent with how H08
  exposed `rrf_k` on all transports.
- **No storage change**: the filter is request-state + retrieval logic, not persisted
  per-document data (existing document attributes are the match targets). Constitution
  Principle II (content-addressed identity) untouched.
- **Out of scope**: the query/result cache (H06), parent-child / context expansion (H15),
  full-text-within-filtered-set query rewriting, and range/numeric filters (only the
  source/type/tags dimensions the audit names).
