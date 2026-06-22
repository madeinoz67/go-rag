# Feature Specification: Query Transformation Seam + Normalization

**Feature Branch**: `012-query-transform` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-22

**Status**: Draft

**Input**: User description: "next in backlog" → resolved to **H05** from
`RAG_BOOK_AUDIT_BACKLOG.md` (Phase 3 retrieval-quality cluster, first item):
*"No query transformation. Lightweight normalization now (case/whitespace); pluggable
`QueryTransformer` interface so HyDE/multi-query land behind `internal/index` without
Ollama coupling."* Source detail: `RAG_BOOK_AUDIT.md` §1.4 (P0, "No query transformation
whatsoever").

**Problem (grounded in current code):** The query string a user types is passed
**raw** into retrieval — no transformation step. The keyword index already
lowercases and splits terms internally, but the semantic (vector) path embeds the
raw query verbatim, and there is **no place** to alter the query before retrieval.
That matters because the biggest retrieval-quality levers (the book §6.1–6.2 cites a
40%→85% war story from synonym expansion alone) are *query-side* transforms —
normalization, acronym/synonym expansion, HyDE, multi-query — and go-rag currently
has no seam to add any of them. This spec delivers that seam with a safe default
(normalization) and leaves the advanced transforms pluggable for later, behind the
retrieval layer with no coupling to the embedding model.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Queries are normalized before retrieval; cosmetic variants retrieve the same results (Priority: P1) 🎯 MVP

A user types a query with cosmetic noise — extra spaces, inconsistent capitalization,
trailing whitespace — and expects the same relevant documents as the clean form.
Today the semantic path can be thrown by raw whitespace/casing differences; after
this change a single normalization step runs before retrieval so cosmetic variants
are equivalent.

**Why this priority**: This is the "lightweight normalization now" half of H05. It is
the safe, always-on default that lands through the new seam, and it makes retrieval
robust to how the query happens to be typed (an AI agent's reformulation, a pasted
string, a user's capitalization habit).

**Independent Test**: Run the same query in a clean form and a cosmetically-noisy
form (repeated spaces, different case) and confirm both return the same relevant
documents (measurable: identical ranking on a fixed corpus).

**Acceptance Scenarios**:

1. **Given** a corpus, **When** the user queries `"Some Term"` vs `"  some   term "`
   (case + whitespace variants), **Then** both return the same ranked results.
2. **Given** any query, **When** it is already clean, **Then** normalization is a
   no-op (idempotent) — results are unchanged from the un-normalized path.
3. **Given** a query that becomes empty after normalization (was only whitespace),
   **When** retrieval runs, **Then** it is handled cleanly (no garbage embed / no
   crash) — the same outcome as an empty query today.

---

### User Story 2 - A real extension point: custom transforms can be plugged in (Priority: P2)

The transformation step is a genuine seam, not a hard-coded function. A custom
transformer (e.g. a future HyDE, multi-query, or acronym-expansion module) can be
supplied and is honored at retrieval time — landing behind the retrieval layer with
**no coupling to the embedding model** and no change to the core retrieval code.

**Why this priority**: This is the strategic half of H05 — the *point* of the seam
is that the big quality levers (HyDE, multi-query, synonym expansion) can be added
later without re-architecting. The interface existing and working is what makes
those future items cheap.

**Independent Test**: Inject a test transformer that demonstrably alters the query
(e.g. appends a synonym); confirm retrieval uses the transformed query (the synonym
changes the results), proving the seam is live and honored.

**Acceptance Scenarios**:

1. **Given** the retrieval layer with a custom transformer plugged in, **When** a
   query runs, **Then** retrieval operates on the transformer's output (provable by
   a transformer that changes the result set).
2. **Given** the default (no custom transformer), **When** a query runs, **Then**
   the built-in normalizer is used — no caller change required.
3. **Given** a future multi-query transformer that produces more than one query,
   **Then** the seam does not preclude it — the extension point accommodates
   yielding one or more transformed queries (design note; multi-query itself is not
   implemented in this spec).

---

### User Story 3 - Normalization is safe — no quality regression (Priority: P2)

Normalization must not make retrieval worse. Because the semantic path embeds the
query, normalizing it (e.g. lowercasing) creates a query/document asymmetry
(documents were embedded as-is), which *could* perturb semantic matching. The change
is therefore gated by the retrieval-quality harness: it ships only if it does not
regress retrieval quality.

**Why this priority**: A "quality" feature that regresses quality is a bug. The
eval harness (spec 004) exists exactly to prove each retrieval change helps (or at
least does not hurt) before it ships — this is that gate for H05.

**Independent Test**: Run the H02 eval harness before and after normalization;
confirm recall@10 / MRR are not worse than the baseline.

**Acceptance Scenarios**:

1. **Given** the H02 golden dataset, **When** retrieval runs with normalization
   enabled, **Then** recall@10 and MRR are no worse than the pre-normalization
   baseline (no regression — SC-002).
2. **Given** normalization is on, **When** the same query runs over CLI, REST,
   gRPC, and MCP, **Then** results are identical across transports (the transform is
   applied in the shared engine path, so parity holds).

---

### Edge Cases

- **Empty-after-normalization**: a query of only whitespace must not produce a
  transformed empty string that gets embedded to garbage — it must be treated as the
  empty-query case (handled/rejected as today).
- **Idempotency**: normalizing an already-clean query is a no-op; normalizing twice
  equals once.
- **Unicode / CJK / accents**: normalization MUST be Unicode-aware (case-folding,
  whitespace) and MUST NOT corrupt or drop non-ASCII characters. (Related but
  separate: token-estimate-on-CJK is audit H26.)
- **Query/document case asymmetry (vector path)**: lowercasing the query for the
  semantic embedder creates an asymmetry vs documents embedded verbatim. For the
  default model this is safe (largely case-insensitive) and is GATED by SC-002; a
  fully consistent doc-side normalization would require re-embedding the corpus and
  is explicitly out of scope.
- **Multi-query future**: the seam must not assume exactly one output query — it
  must accommodate yielding multiple sub-queries (for future multi-query), even
  though the default normalizer yields one.
- **No transformer / nil transformer**: retrieval with no transformer configured
  behaves exactly as today (the default normalizer is always present; there is no
  "raw" escape hatch that bypasses safety, but a caller can supply an identity
  transformer if truly needed).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A query-transformation step MUST run before retrieval, applied to the
  query in the shared engine path so every transport benefits identically.
- **FR-002**: The default transformation MUST normalize the query: Unicode-aware
  whitespace trimming/collapsing and case-folding (the "lightweight normalization"
  the audit names).
- **FR-003**: The transformation MUST be provided through a pluggable seam (an
  interface) in the retrieval layer, with the default normalizer built in — no
  external dependency (no embedding-model coupling) for the default.
- **FR-004**: A caller-supplied transformer MUST be honored at retrieval time
  (US2 acceptance 1) — the seam is live, not theoretical.
- **FR-005**: The seam MUST NOT preclude future transforms that yield more than one
  query (multi-query) — the extension point accommodates one-or-more outputs.
- **FR-006**: A query that is empty after transformation MUST be handled as the
  empty-query case (no garbage embed, no crash) — never silently embedded.
- **FR-007**: Normalization MUST be idempotent (normalize(normalize(q)) ==
  normalize(q)) and MUST be a no-op on already-clean queries.
- **FR-008**: Normalization MUST be Unicode-aware and MUST NOT corrupt non-ASCII
  (CJK, accented) input.
- **FR-009**: Retrieval quality MUST NOT regress — the H02 harness MUST show
  recall@10 and MRR no worse than the pre-normalization baseline (SC-002) before
  the change ships.

### Key Entities *(include if feature involves data)*

- **Query transformer**: the seam — a component that maps an incoming query to one
  or more transformed queries before retrieval. Default instance: the normalizer.
  Lives in the retrieval layer; the default carries no external dependency.
- **Normalized query**: the query after transformation — the string (or strings)
  retrieval actually searches/embeds. For the default normalizer: trimmed,
  whitespace-collapsed, case-folded.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A cosmetically-noisy query (extra/repeated whitespace, different
  casing) returns the same relevant documents, in the same order, as its clean
  form — measurable as identical ranking on a fixed corpus.
- **SC-002**: The H02 retrieval harness reports recall@10 and MRR no worse than the
  pre-normalization baseline (no quality regression — the ship gate).
- **SC-003**: A custom query transformer plugged into the seam demonstrably
  changes the retrieved results (its transformation is honored), proving the
  extension point works end-to-end.

## Assumptions

- **Default normalizer = Unicode-aware trim + collapse-whitespace + case-fold
  (ToLower).** Applied to the query only (not documents). The keyword side already
  lowercases internally, so the normalizer's visible effect is mainly on the
  semantic path and on establishing the seam; it is harmless (idempotent) on the
  keyword side.
- **Query/document case asymmetry is accepted and GATED.** Documents are embedded
  verbatim (with the H07 document-role prefix); lowercasing only the query creates
  a minor asymmetry. For the default model (largely case-insensitive) this is safe,
  and SC-002 is the gate. A fully consistent doc-side normalization (lowercasing
  chunk text at ingest too) would change the embedding profile and require
  re-embedding the corpus — explicitly **out of scope** (a future item).
- **The seam lives in the retrieval layer (`internal/index`/retrieval).** The
  default normalizer is pure (no Ollama). Advanced transforms (HyDE, multi-query,
  synonym/acronym expansion) are **enabled by the seam but NOT implemented** in this
  spec — they need an LLM or dictionaries and are separate future items.
- **Interface shape is plan territory**, but the spec requires it accommodate
  yielding one-or-more transformed queries (so multi-query lands later without
  re-architecting the seam).
- **No new external surface for end users**: this is a retrieval-internal change.
  No new CLI flag or config key is required for the default (normalization is always
  on); a future item may expose transformer selection.
- **Out of scope**: implementing HyDE / multi-query / acronym-expansion, re-embedding
  the corpus for consistent doc-side case normalization, the query/result cache (H06),
  and CJK token-estimation (H26).
