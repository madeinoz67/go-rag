# Feature Specification: go-rag Management Console — Documents View (Slice 1)

**Feature Branch**: `047-ui-documents-view`

**Created**: 2026-07-08

**Status**: Draft

**Input**: User description: *"Specify the Documents view — view 2 of the 8-view sidebar established in spec 046-ui-app-shell Slice 0. It replaces the current Documents placeholder. It should surface the document corpus — browse/list documents, inspect a document's chunks/metadata, and the read-only surface operators need before any Documents write-actions (add/remove/reingest) become their own later slice."*

## Context & Background

Slice 0 (spec 046) shipped the console app shell — the 4th loopback transport, embedded
vendored SPA, spec 045 Bearer auth gate, 8-view sidebar, and one real view (Dashboard).
The other seven views are placeholder panels. **This spec replaces the Documents
placeholder (view 2)** with the first real corpus-management surface, exactly as reserved
in spec 046's Slice Decomposition ("Slice 1 — Documents view: list, status, summaries
from spec 029 → spec 047").

The Documents view is a **read-only presentation layer** over the existing engine — the
same document/chunk data the CLI (`go-rag documents`, `go-rag files`, `go-rag chunk`) and
the RPC layer (list-documents, get-chunk, batch-get-chunks, chunk section context — specs
035/037/038/039/025/041) already expose. It lets an operator answer three questions from
the browser, without running any write-action: *what's in my corpus*, *what state is each
document in*, and *what does a given document actually contain*. Write-actions (add /
remove / reingest / re-embed) are deliberately a later, separately-specced slice.

The view reuses verbatim — and changes none of — the spec 046 shell, the Alpine `goragApp`
root, the 4-layer hand-written CSS, `go:embed` static serving, the loopback UI transport,
and the spec 045 Bearer-session guard. It introduces **no new transport, no new storage,
no new auth, and no Node/build chain**.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Browse the document corpus (Priority: P1)

An operator opens the **Documents** sidebar item and sees a list of every document in the
active vault. Each row shows the document's display name / source path, size, chunk count,
embedding status (complete / pending / failed / drifted), enrichment status and tags
(spec 029), and ingestion timestamp. The list is sortable by several columns and paginated
so a corpus of any size is browseable without loading every row at once. This is the
"what is in my vault" view and the entry point to every other action in this view.

**Why this priority**: the gate to the rest of the view. Without a list of documents,
inspection and search have nothing to operate on.

**Independent Test**: Ingest a known small corpus on an isolated DB; open Documents; the
row count equals the document count reported by `go-rag status`; each row's chunk count
matches that document's chunk count.

**Acceptance Scenarios**:

1. **Given** a corpus of N documents, **When** Documents opens, **Then** N rows render
   (paginated) and the total matches `go-rag status`.
2. **Given** the list, **When** the operator sorts by a column (name, size, chunk count, or
   ingestion date), **Then** the row order reflects that sort.
3. **Given** a corpus larger than one page, **When** the operator paginates, **Then** every
   document is reachable exactly once — no duplicates, no missing rows.
4. **Given** the list is read-only, **When** the operator interacts with it, **Then** no
   document is added, removed, reingested, or re-embedded.

---

### User Story 2 - Inspect a single document's contents and state (Priority: P1)

An operator clicks a document and a detail surface opens showing: the document's full
metadata (identity hash, content hash, source path or URL, size, type, timestamps), its
enrichment summary and tags (spec 029) when present, and the document's chunks — each
chunk's text together with its section path / depth and surrounding section context (specs
025/037/041). This is the "what does this document actually contain, and how was it
chunked" view — the second half of the explicit ask.

**Why this priority**: a list of names alone is not "inspecting a document's chunks and
metadata." Without the detail surface the view does not meet the request.

**Independent Test**: Open a known document; the chunk list length equals the document's
chunk count from the list row; selecting a chunk shows its text and section context
matching `go-rag chunk` output; the summary/tags section matches the document's enrichment
state.

**Acceptance Scenarios**:

1. **Given** a document with K chunks, **When** its detail opens, **Then** K chunks are
   listed and the count matches the list row.
2. **Given** a chunk, **When** selected, **Then** its text and its section path / depth
   render (and section context where the source had structure).
3. **Given** an enriched document, **When** the detail renders, **Then** the spec 029
   summary and tags are shown; **given** an un-enriched document, **Then** a clear empty
   state is shown (not an error).
4. **Given** the detail is read-only, **When** the operator interacts, **Then** no mutation
   of the document, its chunks, or its embeddings occurs.

---

### User Story 3 - Find documents by name, tag, or status (Priority: P2)

An operator can narrow the list to find what they need: free-text search over document
names / source paths and over chunk content (using the existing full-text index), filter
by tag, and filter by embedding or enrichment status — for example "documents that failed
to embed", "drifted documents", or "documents tagged security". Filters and searches
combine by intersection and clear back to the full list.

**Why this priority**: essential once a corpus grows beyond a screenful, but the view is
already valuable for small corpora at P1 without it.

**Independent Test**: Ingest documents with distinct content and tags; search a term that
appears in one document's chunks; only that document remains; apply a status filter; only
matching-status documents remain; clear filters; the full list returns.

**Acceptance Scenarios**:

1. **Given** a search term, **When** submitted, **Then** only documents whose name / source
   path or whose chunk content matches remain.
2. **Given** a tag filter, **When** applied, **Then** only documents carrying that tag
   remain.
3. **Given** a status filter (e.g. "embedding failed"), **When** applied, **Then** only
   documents in that state remain.
4. **Given** combined filters, **When** applied, **Then** the intersection is shown; **when**
   cleared, **Then** the full list is restored.

---

### User Story 4 - The view is read-only and shell-consistent (Priority: P2)

The Documents view introduces no writes, no new authentication, no Node/build chain, and
renders inside the authenticated shell using the established component system. It degrades
gracefully on empty and edge-case corpora. This is a constraint (mirroring spec 046 US4),
proven once so every later view inherits it.

**Why this priority**: not a feature but a hard invariant (read-only this slice; no Node;
single binary). P2 because the view is functional before the invariant is formally proven,
but it must hold before the slice ships.

**Independent Test**: Inspect every network call the view issues — all are read-only
requests to guarded routes; confirm the view renders inside `goragApp` with no full page
reload; confirm no `package.json` / `node_modules` / build config is introduced.

**Acceptance Scenarios**:

1. **Given** the view in use, **When** its network calls are inspected, **Then** every call
   is a read-only request to a guarded `/api/*` route — no create / update / delete.
2. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts
   are introduced.
3. **Given** an empty corpus, **When** Documents opens, **Then** a healthy empty state
   renders (not an error).
4. **Given** a session that expires mid-browse, **When** a fetch returns 401, **Then** the
   shell routes back to login (no crash, no silent failure).

---

### Edge Cases

- **Empty corpus** — a healthy empty state, not an error.
- **Document with zero chunks** (ingestion failed partway) — the row shows a "0 chunks /
  failed" state; the detail shows metadata plus an empty chunk list.
- **Document with failed embeddings** — an "embedding failed (n)" status badge.
- **Drifted embeddings** (embedding model changed since ingestion) — a "drifted" status
  badge.
- **Document not yet enriched** (enrichment disabled or still pending) — the summary/tags
  section shows an empty state, not an error.
- **Very large corpus** (thousands of documents) — pagination keeps the list responsive.
- **Very large document** (thousands of chunks) — the chunk list is paginated or
  lazily loaded.
- **Source changed since ingestion** (content-hash mismatch; a reingest delta exists per
  spec 043) — a read-only "source changed / stale" indicator is shown; **no reingest action
  in this slice**.
- **Non-text documents** (PDFs with images / tables per spec 031) — metadata and whatever
  chunk text was extracted render normally; binary-only sections appear as their extracted
  text.
- **Tag filter matching no documents** — an empty-result state, not an error.
- **Mid-browse session expiry** — graceful return to the login screen.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST list every document in the active vault, each row showing
  display name / source path, size, chunk count, embedding status, enrichment status, tags,
  and ingestion timestamp.
- **FR-002**: The list MUST be sortable by at least name, size, chunk count, and ingestion
  date.
- **FR-003**: The list MUST paginate so a corpus of any size is browseable without loading
  every row at once.
- **FR-004**: The operator MUST be able to open a detail surface for any document showing
  its full metadata, enrichment summary and tags, and chunk list.
- **FR-005**: The detail MUST render each chunk's text together with its section path /
  depth, and section context where the source had structure.
- **FR-006**: The view MUST surface each document's embedding state (complete / pending /
  failed / drifted) and enrichment state, read-only.
- **FR-007**: The operator MUST be able to free-text search documents by name / source path
  and by chunk content.
- **FR-008**: The operator MUST be able to filter the list by tag and by embedding /
  enrichment status.
- **FR-009**: The view MUST be strictly read-only — no add, remove, reingest, re-embed, or
  any state mutation.
- **FR-010**: The view MUST render inside the authenticated shell, gated by the existing
  spec 045 / spec 046 Bearer guard, with no new authentication surface.
- **FR-011**: The view MUST ship inside the single binary via the existing embedded,
  vendored SPA — no Node / Vite / Tailwind build chain.
- **FR-012**: The view MUST render a healthy empty state for an empty corpus and degrade
  gracefully for edge documents (zero chunks, failed embeddings, un-enriched, drifted).
- **FR-013**: Document and chunk counts shown MUST match `go-rag status` and the other
  transports byte-for-byte (cross-transport parity, as the Slice 0 Dashboard).

### Key Entities *(include if feature involves data)*

- **Document**: the unit of ingestion. Canonical identity is its SHA-256 content-plus-metadata
  hash; it carries source path / URL, a distinct content hash, size, type, chunk count,
  embedding state, tags, enrichment summary, and timestamps.
- **Chunk**: a segment of a document's extracted text, carrying its text, ordinal position,
  and — where the source had structure — its section path / depth.
- **Section Context**: the heading ancestry surrounding a chunk, giving each chunk its place
  in the document's structure.
- **Enrichment Summary & Tags**: the spec 029 local-model-generated per-document summary and
  auto-tags; optional, present only when enrichment has run.
- **Embedding State**: a per-document rollup of embedding progress (complete / pending /
  failed / drifted) derived from the index.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can open Documents and locate any specific document in a corpus
  of 1,000 within 10 seconds (using search or filter).
- **SC-002**: The document list renders its first page in under 1 second on a loopback
  connection for a corpus of 1,000 documents.
- **SC-003**: 100% of operators can tell a document's embedding and enrichment state at a
  glance from the list, without opening the CLI.
- **SC-004**: Opening any document's detail shows a chunk count identical to its list row
  and to `go-rag status` — zero drift.
- **SC-005**: No write action is possible from the Documents view — verifiable by
  inspecting every network call the view issues.
- **SC-006**: The view introduces zero new build tooling — a single `make build` still
  produces one binary that serves the console with no Node chain.

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine
  `goragApp` root, and spec 045 Bearer auth unchanged.
- This slice is read-only; write-actions (add / remove / reingest / re-embed) are a later,
  separately-specced slice.
- All document / chunk / section / enrichment data already exists in the engine (via the
  document store and specs 025 / 029 / 035 / 037 / 038 / 039 / 041 / 043); plan confirms
  whether a new read-only engine accessor is needed or existing ones suffice.
- Enrichment summaries and tags are present only when spec 029 enrichment is or has been
  enabled; the view shows an empty state otherwise.
- Single-operator use; no multi-user or RBAC concerns (PRD N2).
- Desktop-first per `docs/style-guide.md`; mobile is not a target.
- Content search reuses the existing full-text index (read-only); no new index is built.

---

## Open Questions (to resolve in plan / tasks)

- Exact pagination mechanism (cursor vs offset) and default page size — defer to plan.
- Whether content search returns document-level matches only or also jumps to the matching
  chunk within the detail view — defer to plan.
- Whether live document updates (spec 040 watch-documents) surface in this slice or the
  view requires manual refresh — lean manual refresh for Slice 1.
- Whether a read-only "source changed since ingestion" indicator (spec 043 reingest-delta)
  ships here or with the later write-actions slice — lean include the read-only indicator
  here, the action with the write slice.
