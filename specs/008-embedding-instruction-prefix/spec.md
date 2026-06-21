# Feature Specification: Embedding Instruction-Prefix (Asymmetric Query/Document Encoding)

**Feature Branch**: `008-embedding-instruction-prefix`

**Created**: 2026-06-21

**Status**: Draft

**Input**: User description: "work on next item in backlog" → the next unchecked item in `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 1) is the audit's **H07 — "Missing embedding instruction-prefix (nomic/E5)"** (P1, effort S). Source: `RAG_BOOK_AUDIT.md` §1.2.

> **Why this is next.** go-rag's default embedding model, `nomic-embed-text`,
> is an *instruction-tuned* model: it expects **asymmetric** prefixes —
> `search_query:` for a retrieval query and `search_document:` for an indexed
> passage (the book §4.2; the same pattern appears in E5's `query:`/`passage:`
> and BGE's instruction prefixes). go-rag currently embeds the query and every
> corpus chunk **identically and unprefixed**, so the two sit in the wrong roles
> relative to what the model was trained on. The result is silently degraded
> retrieval quality on exactly the model most users will run — nothing errors,
> the rankings just get worse than they should be. This spec makes the system
> encode each text with the role-appropriate prefix for the configured model.
>
> **Synergy with spec 004 (eval harness) and spec 005 (dim/model validation).**
> The whole premise of the audit's Phase 3 ("measure each with H02") is that
> retrieval-quality levers must now be *proven* to help. Prefixing is the
> lowest-risk of those levers, so it is the natural next one: the
> `004-retrieval-eval-harness` golden dataset can show recall@5/10 and
> NDCG@10 rising once query/document prefixes are applied, offline and
> reproducibly. Spec `005-embedding-dim-validation` already records each
> vector's model + dimensionality as provenance and defines a **Corpus
> Embedding Profile**; the prefix convention is a third axis of that same
> profile — a query must use the same prefix convention as its corpus, or the
> roles misalign again.
>
> **Scope note.** This spec is the **role-correct encoding + convention
> consistency** fix: apply the right prefix per role, gate it on the model so a
> non-prefix model is never corrupted, and never silently produce a
> half-prefixed corpus. It is deliberately narrower than **H05** (general query
> transformation — case/whitespace normalization, HyDE, multi-query), which
> remains a separate item: the prefix is the *model-mandated* role marker, not a
> learned or heuristic transform. It is also separate from **H12** (bounding the
> whole-document embed batch) — this spec mandates the *correct prefix is
> applied per role*, not how batching is sized.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Encode Queries and Documents in the Right Role (Priority: P1) 🎯 MVP

A user runs the default `nomic-embed-text` model and queries a corpus that was
indexed with the same model. Today both the query and the indexed chunks are
embedded with **no prefix**, so they are encoded symmetrically even though the
model was trained to treat a search query and a stored passage as two
different roles. Retrieval works, but worse than it should — the model never
gets the role signal it expects. After this story, a retrieval query is encoded
with the query prefix and an indexed document with the document prefix (for any
model that requires them), so each text reaches the model in the role it was
trained on. The defining property: the model receives the role-correct input
for every text, by default, on the models that need it.

**Why this priority**: This is the core P1 — the missing prefix is the defect.
Every other story is safety (don't corrupt non-prefix models) or consistency
(don't half-prefix a corpus) layered on top. An MVP that applies the correct
prefix per role, for the default model, is independently shippable and
immediately raises retrieval quality on the most common configuration.

**Independent Test**: Build a small corpus with `nomic-embed-text`, then run a
set of queries through the evaluation harness (spec 004). With role-correct
prefixes applied, recall@5/10 and NDCG@10 MUST be **higher** than with the
current unprefixed encoding, demonstrated offline and reproducibly on the
committed golden dataset.

**Acceptance Scenarios**:

1. **Given** a corpus indexed under an instruction-tuned model (e.g.
   `nomic-embed-text`), **When** a retrieval query is embedded, **Then** the
   query is encoded with the model's query-role prefix and the corpus passages
   were indexed with the document-role prefix — each text reaches the model in
   its trained role.
2. **Given** the same corpus, **When** retrieval quality is measured on the
   golden dataset, **Then** recall@5/10 and NDCG@10 are no lower — and on an
   instruction-tuned model demonstrably higher — than the current unprefixed
   behavior.
3. **Given** a query and a corpus both correctly role-prefixed, **When** the
   query runs, **Then** retrieval behaves correctly on the happy path with no
   error and no perceptible added latency.

---

### User Story 2 - Never Corrupt a Model That Doesn't Use Prefixes (Priority: P2)

Not every embedding model uses instruction prefixes — some have no query/
passage distinction, others use a different convention. Blindly prepending
`search_query:`/`search_document:` to a model that doesn't expect them is the
mirror image of the current bug: it would *corrupt* the embeddings rather than
fix them. So prefixing must be driven by **which model is configured** — on by
default for models known to need it (nomic, E5, BGE families), off for models
that don't — with an explicit, documented way for the operator to override the
convention (turn it off, or specify the exact prefixes). The defining property:
enabling the feature can never silently apply the wrong convention to a model.

**Why this priority**: US1 delivers the win on the default model; US2 makes the
feature **safe to ship generally**. Without it, a user who swaps in a plain
model would get corrupted embeddings with no warning. Valuable, but it sits
behind the core behavior — a user who never changes the default model is still
better off from US1 alone.

**Independent Test**: Configure a model that does not use instruction prefixes
(or disable prefixing via config), index a corpus, and query it. Embeddings
MUST be produced with **no** prefix applied, retrieval MUST work as it does
today, and there MUST be no quality regression or error attributable to
prefixing.

**Acceptance Scenarios**:

1. **Given** a model that does not use instruction prefixes, **When** texts are
   embedded (query or document), **Then** no prefix is applied and behavior is
   unchanged from today.
2. **Given** an operator who wants a non-default convention, **When** they set
   the prefix behavior in config (off, or custom prefixes), **Then** the system
   honors that setting exactly and applies no other prefix.
3. **Given** a model known to use prefixes, **When** no override is set,
   **Then** the documented role prefixes are applied automatically (sensible
   default, no manual configuration required for the common case).

---

### User Story 3 - Don't Silently Half-Prefix an Existing Corpus (Priority: P3)

An operator has a corpus that was indexed **without** prefixes (today's
behavior) and turns prefixing **on** (or upgrades to a prefixed model) without
re-embedding. Now the stored passages are unprefixed but new queries arrive
prefixed — the two roles are misaligned again, the exact failure this feature
exists to fix, just reintroduced by the toggle. Rather than silently mixing
conventions, the system must detect that the corpus's embedding convention
(query/document prefix on or off, and which prefixes) does not match the
currently configured one, and direct the operator to **re-embed** the corpus so
the whole corpus uses one consistent convention. The defining property: the
system never serves a query whose prefix convention differs from its corpus
without telling the operator.

**Why this priority**: This is consistency for the transition from today's
unprefixed corpora. It depends on US1's role encoding and on the Corpus
Embedding Profile machinery from spec 005 (the convention is a third axis of
that profile alongside model and dimensionality). It is not required to ship
the core encoding, so it sits last.

**Independent Test**: Index a corpus with prefixing off, then enable prefixing
and issue a query without re-embedding. The system MUST detect the convention
mismatch and warn/refuse (directing the operator to re-embed) — it MUST NOT
silently return results scored across a mixed-convention corpus.

**Acceptance Scenarios**:

1. **Given** a corpus embedded under convention X (e.g. unprefixed), **When**
   the configured convention is now Y (e.g. nomic prefixes) and a query is
   issued without re-embedding, **Then** the system detects the mismatch and
   warns/refuses with a clear instruction to re-embed — it does not silently
   score across mixed conventions.
2. **Given** the same corpus, **When** the operator re-embeds under convention
   Y, **Then** the whole corpus uses one consistent convention and queries
   succeed normally — re-embedding creates **no duplicate documents** (document
   identity is content-addressed over content + metadata, not the prefix).

---

### Edge Cases

- **Query text that already begins with a role prefix** (a user literally types
  `search_query: …`) — the system must not **double**-prefix; idempotent
  application, one prefix max.
- **Empty or whitespace-only text** — prefixing yields just the prefix token;
  must not error or produce a degenerate vector silently mis-scored.
- **Asymmetric conventions where only the query takes a prefix** (some BGE
  variants prefix the query but not the passage) — the system must support
  "query has a prefix, document has none" as a valid convention, not assume
  both roles are always prefixed.
- **Custom prefix strings** supplied via config — applied verbatim per role;
  validation should reject obviously malformed values (e.g. a prefix containing
  a newline) rather than embedding garbage.
- **Toggling the convention on an existing corpus** — must route through US3
  (re-embed or warn), never produce a half-prefixed corpus; re-embedding must
  not duplicate documents (Principle II).
- **Models added later via the Embedder interface** — a new provider must be
  able to declare its own prefix convention without core changes (Principle V);
  the default prefix map must be overridable per provider.
- **The prefix lives in the embedding path, not the write path** — applying it
  must not regress the <10ms write-ACK budget (Principle IV); prefixing happens
  in the async embed workers and at query time, never inline on the write.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST distinguish the embedding **role** of a text — a
  *retrieval query* versus an *indexed document* — and, for models that require
  asymmetric encoding, MUST apply the role-appropriate prefix before embedding.
- **FR-002**: For the default `nomic-embed-text` model, the system MUST apply
  the query-role prefix to retrieval queries and the document-role prefix to
  indexed documents **by default** (no manual configuration required for the
  common case).
- **FR-003**: The system MUST NOT apply instruction prefixes to a model that
  does not use them; prefix behavior MUST be governed by the configured model
  (and overridable in config) so enabling the feature can never corrupt a
  non-prefix model.
- **FR-004**: An operator MUST be able to override the prefix convention per
  model — disable prefixing, or supply explicit query/document prefix strings —
  and the system MUST apply exactly that convention and no other.
- **FR-005**: The prefix convention in use MUST be recorded as embedding
  provenance (alongside model and dimensionality, spec 005), so the convention
  is visible in status and so a query's convention can be matched to its
  corpus.
- **FR-006**: When the configured prefix convention differs from the convention
  a corpus was embedded under, the system MUST detect the mismatch and direct
  the operator to re-embed; it MUST NOT silently score across a
  mixed-convention corpus.
- **FR-007**: Re-embedding a corpus under a different convention MUST NOT
  create duplicate documents — document identity is content-addressed over
  content + canonicalized metadata (Principle II); only the stored vectors
  change, so toggling prefixes is a re-embed, not a re-ingest.
- **FR-008**: Prefix application MUST be idempotent (never double-prefix) and
  deterministic, MUST add negligible per-text overhead, and MUST occur only in
  the async embed path and at query time — never inline on the write path — so
  the <10ms write-ACK budget (Principle IV) is preserved.
- **FR-009**: The role-correct prefix MUST be applied identically regardless of
  where a query originates (CLI, REST, gRPC, MCP) and regardless of which
  transport ingested the documents (cross-transport parity, Principle V) — a
  query gets the same query prefix and a document the same document prefix
  everywhere.
- **FR-010**: A new embedding provider MUST be able to declare its own prefix
  convention through the extension interface without core changes (Principle V);
  the default prefix map is overridable per provider.

### Key Entities *(include if feature involves data)*

- **Embedding Role**: the purpose of a text at the moment it is embedded —
  *query* (retrieval) or *document* (indexed passage). The asymmetry
  instruction-tuned models are trained to expect.
- **Instruction Prefix**: the role-specific text prepended before embedding
  (e.g. `search_query:`, `search_document:`), or none. Applied per role,
  per the model's convention.
- **Prefix Convention** (a.k.a. embedding encoding profile, third axis): which
  prefixes — if any — the corpus was embedded under, recorded as provenance
  alongside model and dimensionality. A query MUST use the same convention as
  its corpus; a mismatch is the US3 condition.
- **Embedding Provenance** (extended): the per-vector record of how a vector
  was produced — model, dimensionality (spec 005), and now prefix convention —
  the basis for detecting a mixed or mismatched corpus.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a `nomic-embed-text` corpus, retrieval quality (recall@5/10,
  NDCG@10) measured by the evaluation harness (spec 004) is **demonstrably no
  lower — and higher** — with role-correct query/document prefixes than with
  the current unprefixed encoding, reproducibly on the committed golden dataset.
- **SC-002**: A model that does not use instruction prefixes shows **no quality
  change and no corruption** when the feature is enabled — prefixing is
  correctly skipped/gated for that model.
- **SC-003**: An operator can see, via the standard status view, which prefix
  convention is in effect for the corpus, and is warned before querying a
  corpus whose convention differs from the current configuration.
- **SC-004**: Toggling the convention on an existing corpus never creates
  duplicate documents (identity unchanged) and never silently mis-scores — it
  either re-embeds the corpus consistently or warns/refuses.
- **SC-005**: The role-correct prefix is applied identically across every query
  transport (CLI/REST/gRPC/MCP) and the ingest pipeline — a query is encoded
  the same way regardless of origin.
- **SC-006**: The feature adds no perceptible write-ACK latency (prefixing is
  in the async embed path, never the write path) and no perceptible happy-path
  query latency, preserving the existing performance budgets.

## Assumptions

- **Default model is `nomic-embed-text`, prefixing on by default.** The audit
  (§1.2) identifies it as the default and documents the
  `search_query:`/`search_document:` convention; this spec adopts that as the
  sane default, overridable in config. This is a reasonable default, not a
  clarifying question.
- **Prefixing is the lowest-risk retrieval-quality lever and the eval harness
  is how we prove it helps.** The audit's Phase 3 premise ("measure each with
  H02") holds; this spec treats a measurable quality gain on the golden dataset
  as the success signal.
- **The Corpus Embedding Profile from spec 005 is the natural home for the
  prefix convention.** Model + dimensionality are already recorded as
  provenance there; the prefix convention is a third axis of the same profile.
  This spec assumes it *extends* that machinery rather than inventing a
  parallel store.
- **Narrower than H05 (query transformation).** Case/whitespace normalization,
  HyDE, and multi-query are general transforms and remain a separate item. The
  prefix here is the *model-mandated* role marker, not a learned or heuristic
  transform.
- **Narrower than H12 (batch bounding).** How the whole-document embed batch is
  sized is a separate item; this spec only mandates that the *correct prefix is
  applied per role* on whatever batch path exists.
- **Out of scope: learned/custom prefix tuning per query, online A/B of prefix
  variants, auto-detection of a model's convention by probing.** The convention
  is configured/declared, not discovered at runtime.
- **No API-signature prescription here.** This spec mandates the *behavioral*
  role distinction and convention consistency; how the role is threaded through
  the embed call (a parameter, a wrapper, per-provider declaration) is a
  `/speckit-plan` decision, governed by Principle V (interface extension).
- **The remaining audit items (H01, H04–H06, H08–H28) remain tracked in
  `RAG_BOOK_AUDIT_BACKLOG.md`** — this spec is solely the H07 fix.
