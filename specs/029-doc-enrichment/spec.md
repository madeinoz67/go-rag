# Feature Specification: Document Auto-Tag & Summary Enrichment

**Feature Branch**: `029-doc-enrichment`

**Created**: 2026-06-25

**Status**: Draft

**Input**: User description: "doc-level auto-tag and summary enrichment." Today the
database tags nothing and summarizes nothing: documents are ingested as raw
content + vectors, and the existing **tag filter** (spec 014: `--tags`,
`--source`, `--type`) reads `Document.Metadata["tags"]` at query time but
**nothing populates it** — so the single biggest documented retrieval-quality lever
(metadata-filtered retrieval) sits unused unless a user hand-tags every file.
This feature adds a **background, document-level** enrichment step that, after a
document is durably ingested, asks the local model to produce a small set of
**tags** and a one-line **summary**, and stores them as a **non-identity sidecar**
on the document. Tags flow immediately into the existing filter; the summary is
surfaced so an operator or agent can see what a document is about without opening
it. Granularity is deliberately **per document, not per chunk** — one model call
per document, not N — which is the cost profile that makes local enrichment
viable.

> **Scope dependency (must acknowledge):** Enrichment is model-based generation.
> The PRD lists "no LLM inference" as a v1 non-goal (N4). This feature revises
> that posture **for background, local-only document enrichment only** — it uses
> the already-bundled local model, never the cloud, never on the query/ACK path.
> The PRD revision is a prerequisite tracked as a dependency; the constitution
> (local-first, pure-Go, content-addressed identity, async-after-ACK) is honoured.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Documents are auto-tagged, so the tag filter just works (Priority: P1)

A user ingests a corpus and wants to narrow retrieval by topic — "only the
security docs", "only the Sigenergy notes" — using the existing tag filter. Today
that filter returns nothing useful because no document carries tags unless a human
typed them into a front-matter block. After this feature, every newly ingested
document is tagged automatically in the background: the local model reads the
document and assigns a small, consistent set of tags, which land in
`Document.Metadata["tags"]` — exactly what the existing filter already reads. The
user now gets metadata-filtered retrieval for free, with no manual tagging.

**Why this priority**: The entire retrieval-quality payoff. Metadata pre-filtering
(filter then search, rather than search then eyeball) is the highest-ROI
retrieval improvement available, and the filter plumbing already exists — only the
tag population is missing. One model call per document is the cost profile that
delivers it.

**Independent Test**: Ingest a document on a clear topic; wait for background
enrichment; query with the tag filter for that topic; confirm the document is
returned. Confirm a clearly-off-topic tag does not match.

**Acceptance Scenarios**:

1. **Given** a freshly ingested document on a recognizable topic, **When** background enrichment completes, **Then** the document carries a small set of relevant tags in its metadata.
2. **Given** documents carrying auto-generated tags, **When** the user queries with the existing tag filter constrained to one of those tags, **Then** only documents carrying that tag are returned — the existing filter consumes the auto-tags unchanged.
3. **Given** enrichment is enabled, **When** a document is ingested, **Then** chunk count, content, vectors, and the document's identity are unchanged — tags are a non-identity sidecar.

---

### User Story 2 - Every document has a one-line summary (Priority: P2)

An operator or agent browsing results wants to know what a document is *about*
without opening it — a concise, model-written summary surfaced on status and hit
previews. Today only the file path and a content snippet are visible. After this
feature, each enriched document carries a short summary (a single line / handful
of key points), surfaced wherever document metadata is shown.

**Why this priority**: Decision-usefulness when triaging a corpus, but secondary
to the retrieval win (US1). Cheaper to add once the enrichment call exists
(summary rides the same call as tags).

**Independent Test**: Ingest a document; after enrichment, confirm the document's
summary is non-empty, human-readable, and reflects the document's actual topic;
confirm it appears in the status/hit output.

**Acceptance Scenarios**:

1. **Given** an enriched document, **When** its metadata is read, **Then** a concise summary is present and reflects the document's content.
2. **Given** the summary, **When** the operator or agent views document status or a hit, **Then** the summary is surfaced alongside the existing path/snippet.
3. **Given** a document too short or empty to summarize meaningfully, **When** enriched, **Then** the summary is gracefully absent (not a failure, not garbage).

---

### User Story 3 - Enrichment is background, local, non-identity, and resilient (Priority: P3)

Enrichment must never get in the way of the database's core job. It runs **in the
background after the durable ingest ACK** (never blocking ingest), uses the
**local model only** (never the cloud), is a **non-identity sidecar** (never
changes a document's content-addressed identity), and **degrades gracefully** — if
the model is down or returns bad output, the document still ingests and queries
normally, just untagged; enrichment is retried later or marked failed, never
looping forever. It can be disabled, and pre-feature documents can be back-filled.

**Why this priority**: The safety/robustness backbone that lets US1/US2 ship
without risk. Required for the feature to land alongside an existing corpus and
the <10 ms write-ACK budget, but lower priority than the user value itself.

**Independent Test**: Ingest with the model unreachable; confirm the document
still ingests and queries (untagged), enrichment is retried/marked-failed rather
than looping, and the write ACK is unaffected. Then re-run enrichment on a
pre-feature document and confirm it gains tags/summary.

**Acceptance Scenarios**:

1. **Given** ingest is in progress, **When** enrichment runs, **Then** the write ACK latency is unaffected (enrichment is strictly post-ACK/background).
2. **Given** the model is unreachable or returns unparseable output, **When** a document is ingested, **Then** it still ingests and queries correctly (untagged), and the failure is recorded without infinite retry.
3. **Given** enrichment is off (the default) or a document predates the feature, **When** queried, **Then** it behaves exactly as today (no tags/summary, no error) — and can be back-filled on demand.
4. **Given** the local-only constraint, **When** enrichment runs, **Then** it never makes a network/cloud call — it uses the bundled local model.

---

### Edge Cases

- **Model unreachable / slow**: enrichment MUST NOT block ingest or query; the document is usable untagged, and enrichment is retried on a later pass or marked permanently failed (no infinite retry loop that could stall the worker).
- **Empty or trivially-short documents**: nothing meaningful to tag or summarize → enrichment records "nothing to enrich" and does not retry pointlessly.
- **Re-ingestion / content change**: on a genuine content change (new content hash), the document is re-enriched; on an unchanged re-add, enrichment is preserved (idempotent — re-add stays a no-op).
- **Pre-feature documents**: carry no tags/summary and load/query without error; back-fill is an explicit re-enrich pass (no cheap rescan — consistent with prior sidecar features).
- **Noisy / inconsistent tags**: the model may produce variable tags; the tag set is kept small and the prompt constrained to a controlled vocabulary / conservative output to keep tags stable and useful.
- **Non-text / unsupported readers**: enrichment is skipped gracefully (no failure) for documents with no extractable text.
- **Large documents**: enrichment summarizes at the document level from available text (not the full chunk graph); bounded input to the model.
- **Tag identity**: tags MUST NOT become part of document identity or affect dedup — they are a sidecar (idempotent ingestion preserved).
- **Determinism**: tags/summary are model-generated (inherently non-deterministic); back-to-back enrichment of the same document may differ slightly. This is acceptable and documented; identity/corpus state is unaffected.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST enrich newly ingested **documents** (not chunks) in the background after the durable ingest ACK, producing a small set of **tags** and a concise **summary** per document.
- **FR-002**: Enrichment MUST be a **non-identity sidecar**: it MUST NOT change a document's content-addressed identity, its change-detection hash, or chunk content/vectors, and re-adding an unchanged file MUST remain a no-op.
- **FR-003**: Auto-generated tags MUST land in the document metadata location the **existing tag filter already reads**, so metadata-filtered retrieval works with no query-surface change.
- **FR-004**: Enrichment MUST run on background workers **after** the <10 ms write ACK and MUST NOT add latency to ingest or query.
- **FR-005**: Enrichment MUST use the **local model only** — no network or cloud calls (local-first).
- **FR-006**: Enrichment MUST be **opt-in** via configuration (default off in v1), so it only runs when an operator enables it (it consumes local model resources).
- **FR-007**: Enrichment MUST degrade gracefully: a model failure, unreachability, or unparseable output MUST NOT prevent the document from ingesting or querying; the document is simply untagged, and the failure is retried later or marked failed without infinite looping.
- **FR-008**: The system MUST provide a way to **back-fill** enrichment for pre-feature documents (an explicit re-enrich pass), and such documents MUST load/query without error before back-fill (graceful absence).
- **FR-009**: Enrichment MUST be **bounded and resilient**: a per-call circuit/rate guard prevents a misbehaving model from stalling the worker or flooding it; permanently-failed documents are marked so they are not retried indefinitely.
- **FR-010**: Tags and summary MUST be **surfaced** wherever document metadata is shown (status, hit preview), identically across every transport.
- **FR-011**: Enrichment MUST be **deterministic in structure** (fields present/absent are predictable) even though tag/summary text is model-generated; an absent summary (short/empty doc) is a clean "absent", not an error.

### Key Entities *(include if feature involves data)*

- **Document Enrichment (sidecar)**: the per-document enrichment result — a small `tags` set, a concise `summary`, and provenance (the model that produced it, when). Non-identity; stored alongside the document, not in its identity. Absent on pre-feature or un-enriched documents.
- **Tags**: a small set of short, topic-describing labels assigned to a document. Populate the existing `Document.Metadata["tags"]`, consumed by the existing tag filter.
- **Summary**: a single concise description of what the document is about, surfaced on status/hits.
- **Enricher**: the background service that produces a document's enrichment from its text, using the local model. Conceptually the document-level sibling of the embedder.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For an ingested document on a clear topic, after background enrichment completes, querying with the existing tag filter constrained to an assigned tag returns that document — proving auto-tags flow into metadata-filtered retrieval with no query change.
- **SC-002**: Enriched documents carry a non-empty, topic-accurate summary surfaced on status/hits; documents too short to summarize carry an absent (not malformed) summary.
- **SC-003**: The write-ACK latency on ingest is unchanged with enrichment enabled versus the pre-feature baseline (enrichment is strictly post-ACK) — the <10 ms ACK budget is preserved.
- **SC-004**: With the local model unreachable, a document still ingests and queries (untagged), the failure is recorded, and enrichment does not loop indefinitely — confirmed across all transports.
- **SC-005**: Document identity, chunk content/vectors, chunk count, and idempotent re-add behaviour are byte-identical with enrichment on versus off (non-identity sidecar); pre-feature documents load/query without error and gain enrichment on back-fill.

## Assumptions

- **Granularity is per document, not per chunk.** One model call per document (tags + summary together) is the cost profile that makes local enrichment viable; chunk-level entity/topic extraction is a deliberate future phase, out of scope here.
- **Local model only (Constitution I).** Enrichment uses the already-bundled local model; no cloud providers, no network egress. A small tagging/summary model is configured (the specific model is a plan decision).
- **Opt-in, default off in v1.** Enrichment consumes local model resources and needs a tagging model available, so it is off by default and enabled via config — a safe v1 posture; the value is realized when enabled.
- **PRD scope dependency.** The PRD's "no LLM inference" non-goal (N4) is revised **for background, local-only document enrichment only**. This is a product-scope change the principal has directed; the constitution is honoured (local-first, content-addressed identity, async-after-ACK, pure-Go). The PRD revision itself is a tracked prerequisite, not part of this spec.
- **Tags feed the existing filter; no new query surface.** Auto-tags populate the metadata the existing tag filter (spec 014) already reads — `--tags`/`--source`/`--type` work unchanged.
- **Out of scope for v1**: chunk-level entity/topic extraction, relationship graphs (GraphRAG), hypothetical-question generation, cross-document entity resolution, cloud LLM providers, and auto-enabling enrichment by default. Back-fill of pre-feature docs is supported but is an explicit re-enrich pass, not an automatic rescan.
- **Tag/summary text is model-generated and therefore non-deterministic**; structural determinism (presence/absence) is required, exact-text determinism is not. Identity and corpus state are unaffected by re-enrichment.
