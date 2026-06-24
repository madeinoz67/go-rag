# Feature Specification: Near-Duplicate Chunk Detection

**Feature Branch**: `026-near-dup-detection`

**Created**: 2026-06-24

**Status**: Draft

**Input**: User description: "work on backlog item H20" — audit finding **H20**
(`RAG_BOOK_AUDIT_BACKLOG.md`, Phase 6, §1.1): *"Doc-level dedup only (no
near-duplicate). SimHash/shingle-Jaccard near-dup flagging at ingest (brute-force
fine at local <10K scale)."* Today the database treats two files as "the same"
only when they are byte-for-byte identical (exact content-hash dedup). Everything
that is *nearly* the same — a lightly-edited revision, a copy-pasted section, an
editor re-save — is ingested as independent content. This feature adds fuzzy,
near-duplicate detection so the system knows "these are ~the same passage" and
can keep retrieval results diverse instead of redundant.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Query results aren't dominated by near-identical passages (Priority: P1)

A user queries the corpus and receives ranked hits. Today, if several ingested
documents contain near-identical text (a revised section, a boilerplate block, a
passage quoted in multiple files), the top results can be dominated by the *same
passage repeated* — crowding out genuinely distinct evidence and wasting the
result budget. After this feature, near-duplicate chunks are recognised and the
user can retrieve a **diverse** set: near-duplicates are collapsed to a single
representative (the highest-scoring) so each result slot carries distinct
information.

**Why this priority**: This is the entire user-facing value of H20 — the
retrieval-quality payoff. Near-dup detection that doesn't change what users see
in results is invisible. Collapsing redundant near-dups so the top-k is diverse
is the one story that, shipped alone, still delivers measurable benefit (and is
provable on the existing retrieval-eval harness).

**Independent Test**: Ingest a document and a near-identical revision of it
(differing by a small edit), query for a phrase they share, and confirm the
returned results are not redundant — near-duplicates are collapsed to one
representative per cluster. Fully testable end-to-end with two fixture documents
and one query.

**Acceptance Scenarios**:

1. **Given** a corpus containing a document and a near-identical revision of it (e.g. a paragraph with one sentence changed), **When** the user queries for a phrase present in both, **Then** the near-identical passages do not each occupy separate result slots — at most one representative of the near-duplicate cluster appears in the ranked results when de-duplication is enabled.
2. **Given** de-duplication is enabled over any transport (CLI, REST, gRPC), **When** the user reads the result, **Then** the collapsed/de-duplicated behaviour is identical across all transports for the same query.
3. **Given** de-duplication is disabled (the default), **When** the user queries, **Then** results are returned exactly as today (no behaviour change unless the user opts in) — the feature is non-disruptive by default.

---

### User Story 2 - Every chunk knows its near-duplicate relationships (Priority: P2)

A user ingests a corpus. The system fingerprints each chunk and groups it with
any near-duplicates — a deterministic **near-duplicate cluster** (one canonical
representative plus its members). This is the correctness backbone that makes
US1 trustworthy: without reliable, position/cluster-accurate detection, the
collapse in US1 would be guessed or miss real near-dups. It is independently
testable by inspecting the clusters produced for a fixture corpus, without going
through query.

**Why this priority**: The correctness backbone for US1. Without accurate
detection the collapse is untrustworthy. Independently testable by enumerating
chunks and their cluster membership (no query needed).

**Independent Test**: Ingest a set of documents containing known near-duplicate
pairs (a revision, a copy-pasted section across two files) and known-distinct
passages; enumerate the stored chunks and assert each carries the correct cluster
membership — near-dups grouped, distinct passages not.

**Acceptance Scenarios**:

1. **Given** two chunks whose text is highly similar but not byte-identical (e.g. a paragraph with a typo fixed, or whitespace/formatting differences), **When** they are ingested, **Then** both are assigned to the same near-duplicate cluster.
2. **Given** a chunk that shares one copied section with an otherwise-different document, **When** clustered, **Then** that chunk is grouped with its near-duplicate counterpart at the **chunk** level (the retrieval unit), even though the documents differ overall.
3. **Given** two clearly-distinct chunks (different topic, different wording), **When** clustered, **Then** they are **not** grouped (no false-positive merge).
4. **Given** the feature is enabled, **When** a document is ingested, **Then** chunk sizes, content, overlap, and the retrieval index are unchanged — detection adds a relationship attribute, it does not alter the chunks themselves.

---

### User Story 3 - Operators can see and control near-duplicate handling (Priority: P3)

An operator wants visibility and control: the database status reports how many
near-duplicate chunks and clusters exist; a query option toggles whether
near-duplicates are collapsed or merely flagged; and chunks ingested before the
feature (or with the feature disabled) load and query without error, simply
carrying no near-duplicate information.

**Why this priority**: Robustness, observability, and migration safety. Lower
priority than the core value (US1) and correctness (US2), but required so the
feature can ship safely alongside an existing corpus and be tuned.

**Independent Test**: Ingest a mixed corpus, confirm status reports near-duplicate
counts; toggle the collapse option and confirm results differ; read a chunk
written before the feature and confirm it loads with absent (not malformed)
near-duplicate info.

**Acceptance Scenarios**:

1. **Given** a corpus with near-duplicate chunks, **When** the operator checks status, **Then** near-duplicate chunk and cluster counts are reported.
2. **Given** the collapse option, **When** toggled on/off for the same query, **Then** the result set changes accordingly (collapsed vs full) and the choice is honoured identically across transports.
3. **Given** a chunk persisted before this feature was enabled, **When** it is retrieved or clustered, **Then** it carries no near-duplicate information (absent) rather than causing a read or parse failure.

---

### Edge Cases

- **Near-identical except whitespace/formatting**: two chunks differing only in line endings, capitalisation of a header, or surrounding whitespace MUST be detected as near-duplicates (these are the most common real-world near-dups from editor re-saves).
- **Shared section across different documents**: a boilerplate/legal/policy block copy-pasted into two unrelated files — the duplicated **chunk** pair is a near-duplicate even though the documents are otherwise distinct (chunk-level granularity).
- **Clearly-distinct content**: two chunks on different topics or with different wording MUST NOT be flagged as near-duplicates (precision guard; the threshold is chosen so legitimately different content is never collapsed).
- **Cross-document near-duplicate**: the common case — a chunk in document A near-duplicate to a chunk in document B. Detection MUST work across documents, not only within one.
- **Short or sparse chunks**: very short chunks produce unreliable fingerprints and MUST NOT produce spurious near-duplicate matches.
- **Borderline similarity**: content that is moderately similar (e.g. two different summaries of the same topic) — the threshold determines the boundary; it MUST be configurable and default to conservative (only flag high-similarity pairs).
- **Re-ingestion after enabling the feature**: unchanged files remain a no-op (near-duplicate info is a sidecar, not part of identity — hard requirement).
- **Pre-feature chunks**: chunks written before the feature carry no near-duplicate info and MUST load/query without error (graceful absence).
- **The "wrong" version survives collapse**: when collapsing, the highest-scoring (most relevant) representative is kept; if two near-dups are equally relevant, the choice MUST be deterministic and documented.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST detect near-duplicate **chunks** — chunks whose text is highly similar but not byte-identical — during ingestion. Detection is at chunk granularity (the retrieval unit), not document granularity.
- **FR-002**: The system MUST derive near-duplicate detection locally, without invoking an LLM, a network call, or any cloud service (honours Local-First).
- **FR-003**: The system MUST treat near-duplicate information as a non-identity sidecar: it MUST NOT change a chunk's or document's identity, and re-adding an unchanged file MUST remain a no-op that produces no duplicate content (idempotent ingestion preserved).
- **FR-004**: The system MUST surface near-duplicate relationships identically on every transport — per-hit cluster information on query results, and aggregate counts on status — with the same value for the same chunk across CLI, REST, and gRPC.
- **FR-005**: The system MUST provide an opt-in query-time de-duplication that collapses near-duplicate hits to one representative per cluster (the highest-scoring). De-duplication MUST be off by default (flag-only) so default behaviour is unchanged.
- **FR-006**: Near-duplicate detection MUST be deterministic for a given input and threshold, and the similarity threshold MUST be configurable.
- **FR-007**: Detection MUST NOT alter a chunk's retrievable text, size, token count, overlap, or the vectors/indexes built from it — the chunking and embedding geometry is unchanged.
- **FR-008**: The system MUST ingest and query documents whose chunks have no near-duplicates without error; for such chunks, near-duplicate information is absent rather than a failure. Pre-feature chunks MUST likewise load with absent (not malformed) near-duplicate information.
- **FR-009**: The system MUST NOT collapse or flag clearly-distinct chunks as near-duplicate (precision guard): the threshold and method MUST be chosen so legitimately different content is never merged.

### Key Entities *(include if feature involves data)*

- **Chunk**: the retrievable text segment. Gains a near-duplicate-cluster attribute describing which (if any) near-duplicate cluster it belongs to and its siblings. Existing identity, linked-list, section-context, and poisoning attributes are unchanged.
- **Near-Duplicate Cluster**: a group of chunks deemed near-identical under the configured threshold. Has one canonical representative (the highest-scoring/most-relevant, chosen deterministically) and a set of members. A chunk belongs to at most one cluster for a given threshold.
- **Fingerprint**: a compact, order-insensitive signature computed from a chunk's text, used to find near-duplicates efficiently. The fingerprint algorithm and the comparison method are plan decisions, constrained to be local, deterministic, and pure-Go.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a fixture containing a document and a near-identical revision, querying a shared phrase with de-duplication enabled returns a non-redundant result set — near-duplicate passages occupy at most one slot per cluster — measurably increasing the diversity of the top-k versus the same query without the feature.
- **SC-002**: A near-duplicate cluster and per-hit cluster information are surfaced with an identical value across CLI, REST, and gRPC for the same chunk and query, requiring zero additional user actions.
- **SC-003**: Re-adding an unchanged file is a no-op — document and chunk counts are unchanged (idempotency preserved).
- **SC-004**: On the project's existing retrieval-eval harness, enabling near-duplicate collapse does **not regress** overall retrieval quality versus the pre-feature baseline — it reduces redundancy in the result set without losing relevant coverage (recall of distinct relevant passages is maintained).
- **SC-005**: Documents whose chunks have no near-duplicates, and chunks written before the feature, ingest and query without error and carry absent near-duplicate information — confirmed across all transports; and clearly-distinct chunks are never collapsed (no false-positive merges).

## Assumptions

- **"Near-duplicate" means high text similarity at chunk granularity**, not document-level, because the chunk is the retrieval unit and partial overlaps (one copied section) are exactly what pollutes results. Document-level near-dup is a weaker signal and is out of scope for v1.
- **Detection is flag-by-default; collapse is opt-in.** Silently hiding/collapsing results by default would be surprising, so the default leaves results unchanged and the user opts into de-duplication per query. Dropping or quarantining near-duplicate content (as opposed to flagging/collapsing) is **out of scope** for v1 — too risky (could discard the "better" version).
- **H20 captures, fingerprints, and surfaces near-duplicates; it does not merge or delete content.** Back-fill of near-duplicate info for pre-feature chunks requires re-ingestion (Reprocess); there is no cheap rescan, consistent with prior sidecar features whose signal is derived at ingest time.
- **The fingerprint and comparison method (e.g. a locality-sensitive hash, shingling, or MinHash-style signature) is a plan decision**, not prescribed by this spec — constrained to be local, deterministic, pure-Go, and suitable for a local corpus (the audit notes brute-force is acceptable below ~10K documents).
- **Out of scope for v1**: cross-language near-duplicates (translation pairs); *semantic* near-duplicates (different wording, same meaning — that is an embedding-similarity concern, separate from text near-dup); and web-scale global clustering. The feature targets local-scale corpora.
- **Structural-only, no LLM** — consistent with the PRD's LLM-generation exclusion and with the audit framing of H20 as the no-LLM fuzzy-dedup item.
- **Threshold default is conservative** (only high-similarity pairs flagged) to protect precision (FR-009); the exact value is tuned against the eval harness during planning.
