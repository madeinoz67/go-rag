# Feature Specification: Local RAG Database (go-rag v1)

**Feature Branch**: `001-local-rag-database`

**Created**: 2026-06-19

**Status**: Draft

**Input**: Derived from `PRD_RAG_Database.md` — the full v1 product specification.

> Scope note: this baseline spec covers the entire go-rag v1 product as defined in
> the PRD. The PRD's six user stories are consolidated into four SpecKit user
> stories below, ordered so that User Story 1 alone is a viable MVP and each later
> story adds independent value. Source mapping is noted per story.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Index and Query a Document Folder (Priority: P1) 🎯 MVP

A user points go-rag at a folder of their own documents (PDFs, Word files,
Markdown, plain text), runs one-time setup, ingests the folder, and then asks
natural-language questions — receiving answers grounded in their own files, each
result citing its source file and (for PDFs) the page it came from.

**Why this priority**: This is the entire value proposition. Without ingest plus
query, go-rag does nothing useful. Setup, ingest, and query form one inseparable
minimal loop that proves local RAG end-to-end. (Covers PRD US1 First-Time Setup,
US2 Ingest, US4 Query.)

**Independent Test**: Add a single PDF whose contents are known, ask a question
whose answer appears in it, and receive a cited, correct answer.

**Acceptance Scenarios**:

1. **Given** an empty directory and a reachable local embedding service, **When**
   the user runs setup and then ingests a folder, **Then** every supported file is
   processed and the user is shown a summary of new, skipped, and errored files.
2. **Given** an ingested folder, **When** the user asks a question, **Then** the
   top-K ranked results return, each showing the matching text, its source file
   path, the page number (for paginated documents), and a relevance score.
3. **Given** a file that has already been ingested unchanged, **When** the user
   ingests it again, **Then** it is skipped with no duplicate work performed.
4. **Given** a folder containing some unsupported file types, **When** the user
   ingests it, **Then** supported types are processed and unsupported types are
   skipped without failing the rest of the batch.

---

### User Story 2 - Inspect Database Status and Health (Priority: P2)

A user who ingested files earlier wants to see what is currently in the database —
how many sources, files, chunks, and embeddings; storage size; which embedding
model is in use; the last activity timestamps; and whether the embedding service is
reachable — so they can trust what is searchable and spot problems.

**Why this priority**: Operability and trust matter, but they are not required for
the core RAG loop to function. (Covers PRD US5 Database Status.)

**Independent Test**: After ingesting a known folder, run the status command and
confirm the document/chunk counts and a healthy indicator match expectations.

**Acceptance Scenarios**:

1. **Given** an ingested database, **When** the user runs the status command,
   **Then** counts (sources, files, chunks, percentage embedded), storage size,
   embedding model, and last-activity timestamps are displayed.
2. **Given** the embedding service is unreachable, **When** the user runs status,
   **Then** a degraded health indicator is shown rather than a crash or silent
   success.

---

### User Story 3 - Configure the Database (Priority: P2)

A user with a non-default setup — a remote embedding service, a preferred embedding
model, a specific watched directory, or custom chunk sizing — wants to view and
change these settings, with invalid values rejected clearly.

**Why this priority**: Needed for non-default environments, but sensible defaults
cover the common local case, so it is not on the critical MVP path. (Covers PRD
US6 Configuration.)

**Independent Test**: Change the embedding service URL, restart, and confirm the
new value is in effect.

**Acceptance Scenarios**:

1. **Given** default settings, **When** the user views configuration, **Then** all
   current values are printed.
2. **When** the user sets a key to a valid value (for example, the embedding
   service URL), **Then** the change persists across restarts.
3. **When** the user sets an invalid value (a malformed URL, or a non-positive
   integer for a numeric field), **Then** the change is rejected with a clear,
   actionable error message and the previous value is retained.

---

### User Story 4 - Keep the Database Current Automatically (Priority: P3)

A user who constantly adds, edits, and removes files in a living research directory
wants the database to stay in sync automatically — detecting new, modified, and
deleted files without manual re-ingestion — with each change logged.

**Why this priority**: Highly valuable for active collections, but manual re-ingest
is an acceptable v1 fallback, so it is not required for launch. (Covers PRD US3
Continuous Watching.)

**Independent Test**: Start the watcher, add a new file and see it indexed, then
delete a tracked file and see it removed.

**Acceptance Scenarios**:

1. **Given** a watched directory, **When** a new supported file appears, **Then** it
   is detected (with short debouncing to coalesce editor save bursts) and indexed.
2. **Given** a tracked file, **When** its content changes, **Then** its old chunks
   and embeddings are replaced by fresh ones.
3. **Given** a tracked file, **When** it is deleted, **Then** its chunks,
   embeddings, and search entries are removed.
4. **Given** the watcher is running, **When** the user interrupts it, **Then** it
   shuts down gracefully, completing any in-flight work.

---

### Edge Cases

- **Scanned or image-only PDFs** produce little or no extractable text — the system
  surfaces low extraction quality rather than crashing or silently indexing empties.
- **Very large files** (for example a 500 MB PDF) risk memory pressure — the system
  warns above a configurable size threshold and skips or streams files that exceed a
  maximum.
- **Concurrent ingest and query** — queries see the last committed state; the
  behavior is defined (eventual consistency) rather than undefined.
- **Embedding service down during ingest** — file writes still acknowledge promptly;
  embedding work queues and retries; status reports degraded health.
- **Embedding model changed** — re-embedding the existing corpus is required; the
  path is clear and reported, not silent.
- **Duplicate content across different files** — documents are de-duplicated by
  content identity; the behavior of near-duplicate query results is defined.
- **Deep directory trees** that exceed the host's real-time event limits are caught
  by a periodic polling safety net so changes are never permanently missed.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST ingest PDF, plain text, Markdown, and Word (.docx)
  documents, extracting their text content.
- **FR-002**: The system MUST split each document into overlapping chunks sized for
  the embedding model's context window.
- **FR-003**: The system MUST generate a vector embedding for every chunk using a
  local embedding service.
- **FR-004**: The system MUST store documents, chunks, and embeddings durably on the
  user's local machine, surviving process restarts and crashes with no data loss.
- **FR-005**: Ingesting a file whose content is already present MUST be a no-op
  (idempotent ingestion).
- **FR-006**: The system MUST support hybrid search that combines semantic
  (meaning-based) and keyword (exact-term) retrieval into a single ranked result
  list.
- **FR-007**: Every query result MUST include the matching text, its source file
  path, the page number for paginated documents, and a relevance score.
- **FR-008**: The system MUST let the user choose semantic-only, keyword-only, or
  hybrid retrieval per query.
- **FR-009**: The system MUST provide a status view showing source, document, chunk,
  and embedding counts; storage size; the embedding model in use; last activity; and
  a health indicator.
- **FR-010**: The system MUST persist user-configurable settings (embedding service
  URL, model, watched directories, chunk size, chunk overlap, polling interval) and
  validate them on change.
- **FR-011**: The system MUST detect file additions, modifications, and deletions in
  watched directories and reflect them in the database.
- **FR-012**: All core operations the user can run MUST also be accessible to AI
  coding agents as first-class tools, not only to humans via the command line.
- **FR-013**: The system MUST operate entirely locally — no document content or
  query leaves the user's machine for any core operation.
- **FR-014**: The system MUST ship as a single, dependency-free binary requiring no
  containers, cloud accounts, or background services beyond an optional local
  embedding service.

### Key Entities *(include if feature involves data)*

- **Source**: A directory or file collection being tracked. Identified by its path
  and kind (directory or file).
- **Document**: A single ingested file. Has a type (PDF, text, Markdown, Word,
  image), a content hash used for change detection, descriptive metadata (title,
  author, page count), and links to its chunks.
- **Chunk**: A segment of a document's text and the unit of retrieval. Knows its
  position, page number, and ordering relative to its sibling chunks.
- **Embedding**: A vector representation of a chunk used for semantic search. Tied
  one-to-one to a chunk and to the model that produced it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new user can go from installation to their first grounded query
  against a folder of documents in under 30 seconds (excluding one-time embedding
  model download).
- **SC-002**: A hybrid query over a database of roughly 1,000 documents returns its
  top 5 ranked results in under 500 milliseconds.
- **SC-003**: A keyword-only query over the same database returns its top 5 results
  in under 50 milliseconds.
- **SC-004**: Re-ingesting an unchanged file is effectively instant, and ingesting a
  new small document acknowledges the write to the user in under 10 milliseconds
  while indexing continues in the background.
- **SC-005**: The installed tool occupies under 25 MB on disk and uses under 50 MB
  of memory when idle.
- **SC-006**: 100% of core operations succeed with no network connection once an
  embedding model is available locally.
- **SC-007**: Re-running setup and ingest over an unchanged collection performs zero
  redundant embedding work.

## Assumptions

- Users have, or will install, a local embedding service; v1 targets Ollama
  specifically as the embedding provider.
- Users run on Linux, macOS, or Windows.
- v1 does not extract text from images (OCR); images are indexed by metadata and
  filename only, with OCR deferred to a later version.
- v1 is a single-user, single-process local tool — no multi-user access, no
  authentication, no concurrent writers.
- Default chunking (approximately 512 tokens with 50-token overlap) suits most text
  documents; users may tune it via configuration.
- Documents are assumed to be text-extractable; scanned PDFs may yield poor results,
  which are surfaced to the user rather than treated as success.
- A single process may hold the database open at a time; concurrent query while
  ingesting sees the last committed state.
