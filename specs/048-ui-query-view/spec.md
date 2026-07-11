# Feature Specification: go-rag Management Console — Query View (Slice 2)

**Feature Branch**: `048-ui-query-view`

**Created**: 2026-07-11

**Status**: Draft

**Input**: User description: *"Specify the Query view — Slice 2 of the 8-view sidebar established in spec 046-ui-app-shell. It replaces the current Query placeholder. It is the retrieval half of the product — 'the go-rag retrieval UI' named in spec 046. An operator enters a natural-language query and sees ranked chunk results with scores, citations, and section context; can toggle retrieval controls (mode, top-k, threshold, filters) and see what the engine actually used; and can opt in to see injection-flagged chunks that are excluded by default."*

## Context & Background

Slice 0 (spec 046) shipped the console app shell — the 4th loopback transport, embedded
vendored SPA, spec 045 Bearer auth gate, 8-view sidebar, and one real view (Dashboard).
Slice 1 (spec 047) replaced the Documents placeholder with a read-only corpus browse. **This
spec replaces the Query placeholder (view 3)** with the retrieval surface — exactly as reserved
in spec 046's Slice Decomposition ("Slice 2 — Query view (retrieval UI) → spec 048").

The Query view is a **read-only presentation layer** over the existing retrieval engine — the
same `Engine.Query` path the CLI (`go-rag query`), MCP, REST, and gRPC adapters already call.
It lets an operator answer three questions from the browser, without running a write-action:
*what chunks match this query*, *how confident is each match and where does it come from*, and
*what retrieval parameters were actually used*. It is the product's other half — go-rag
*retrieves*, and until this slice that retrieval lived only in the CLI and the RPC layer.

The view reuses verbatim — and changes none of — the spec 046 shell, the Alpine `goragApp`
root, the 4-layer hand-written CSS, `go:embed` static serving, the loopback UI transport, and
the spec 045 Bearer-session guard. It introduces **no new transport, no new storage, no new
auth, no new engine capability, and no Node/build chain**. It surfaces retrieval results the
engine already produces; it does not change how retrieval works.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run a query and see ranked results (Priority: P1)

An operator opens the **Query** sidebar item, types a natural-language query, and submits it.
The view shows the ranked matching chunks — each result with its relevance score, its citation
(source file, page when paginated, chunk index), the section breadcrumb it sits under, and the
chunk text (preview by default, expandable to full). The number of results respects the
operator's top-k. This is the "what matches my query" view and the entire reason the console
exists as a retrieval UI.

**Why this priority**: the gate to the rest of the view. Without running a query and seeing
ranked hits, inspection, controls, and transparency have nothing to operate on. This single
story is a viable MVP.

**Independent Test**: Ingest a known small corpus on an isolated DB; from the browser run a
query whose answer is known to be in one chunk; that chunk ranks first; its citation and
section breadcrumb match `go-rag query` output for the same input; the hit count equals the
top-k requested.

**Acceptance Scenarios**:

1. **Given** a corpus, **When** the operator submits a non-empty query, **Then** ranked hits
   render, each showing score, citation, section breadcrumb, and chunk text.
2. **Given** the same query and parameters, **When** the result is compared to `go-rag query`,
   **Then** the hits, their order, and their scores are identical (cross-transport parity).
3. **Given** a top-k of N, **When** results return, **Then** at most N hits render (fewer if
   the threshold trims or the corpus has fewer matches).
4. **Given** the view is read-only, **When** the operator runs a query, **Then** no document
   is added, removed, reingested, or re-embedded — retrieval mutates nothing.

---

### User Story 2 - Inspect a result's detail, context, and provenance (Priority: P1)

An operator clicks a result and a detail surface opens showing the full chunk text, its
section path and heading depth, the surrounding sibling chunks (when context expansion is
on), the document's auto-generated summary and enrichment state when present, and provenance
signals — extraction method/quality for PDF-derived chunks, near-duplicate siblings, and any
Obsidian wikilink targets the chunk carries. This is the "where did this result really come
from, and how much should I trust it" view — the second half of retrieval being useful.

**Why this priority**: a ranked list of previews alone is not "retrieval that lands." Without
the detail surface — full text, context, and provenance — the operator cannot judge whether a
hit is the right one.

**Independent Test**: Open a known top result; the full chunk text matches the CLI's chunk
content for that chunk ID; with context-window on, the sibling chunks render and match `go-rag
query --context-window`; the section breadcrumb matches `go-rag chunk` output.

**Acceptance Scenarios**:

1. **Given** a hit, **When** its detail opens, **Then** the full chunk text, section path,
   heading depth, and citation render.
2. **Given** context expansion is enabled, **When** the detail renders, **Then** the sibling
   chunks on either side render and are labelled as context (distinct from the hit itself).
3. **Given** an enriched source document, **When** the detail renders, **Then** the document
   summary and enrichment state are shown; **given** an un-enriched document, **Then** a clear
   empty state is shown (not an error).
4. **Given** a chunk with provenance signals (extraction quality, near-duplicates, wikilinks),
   **When** the detail renders, **Then** those signals are shown; **given** a chunk with none,
   **Then** they are omitted cleanly (not an error).
5. **Given** the detail is read-only, **When** the operator interacts, **Then** no mutation of
   any document, chunk, or embedding occurs.

---

### User Story 3 - Control retrieval and see what the engine actually used (Priority: P2)

An operator can tune the retrieval before submitting: choose the mode (hybrid / semantic /
keyword), set top-k, set a minimum-score threshold, turn reranking on or off, narrow with
filters (tag, source file, file type — combined by intersection), and optionally turn on
context expansion, near-duplicate collapse, and cache bypass. After the query returns, the
view states what the engine **actually** used — the effective mode, effective top-k, and
effective candidate pool — so the operator can tell whether a per-query override or the
adaptive-depth classifier acted, and it warns plainly when reranking was attempted and failed
(results are still valid, but in fallback order).

**Why this priority**: essential for anything beyond a first pass, and the transparency is
what makes the console trustworthy versus a black box — but the view is already valuable at P1
with default retrieval and no controls.

**Independent Test**: Run the same query in keyword mode vs hybrid mode; the hits differ and
the effective-mode indicator reflects the choice; set a threshold that trims half the results;
the hit count drops; trigger a rerank failure (reranker misconfigured) and confirm the
fallback banner appears while results still render.

**Acceptance Scenarios**:

1. **Given** the controls, **When** the operator selects a mode and sets top-k, **Then** the
   next query uses them and the effective-mode / effective-k indicators reflect the choice.
2. **Given** a tag or source filter, **When** applied, **Then** only hits from matching
   documents remain; multiple filters combine by intersection.
3. **Given** a threshold, **When** set above some hits' scores, **Then** those hits are
   dropped and the remaining hits all score at or above the threshold.
4. **Given** a query where adaptive depth (spec 024) or a per-query override changed k or
   pool, **When** results render, **Then** the effective top-k and effective pool are shown.
5. **Given** reranking was attempted and failed, **When** results render, **Then** a clear
   rerank-failed banner shows and the hits are presented as valid-but-fallback-ordered.
6. **Given** near-duplicate collapse is toggled on, **When** results render, **Then**
   duplicate-near hits collapse to one representative per group.

---

### User Story 4 - The view is read-only, quarantine-by-default, and shell-consistent (Priority: P2)

The Query view introduces no writes, no new authentication, no Node/build chain, and renders
inside the authenticated shell using the established component system. It honours the engine's
**quarantine-by-default** posture: chunks flagged as injection-poisoning are excluded from
results unless the operator explicitly opts in; when opted in, each such hit displays its
poisoning verdict so the retrieved text is treated as untrusted. It degrades gracefully on
empty corpora, no-match queries, and an unreachable embedder. This is a constraint (mirroring
spec 046/047 US4), proven once so every later view inherits it.

**Why this priority**: not a feature but a hard invariant (read-only this slice; quarantine-
by-default; no Node; single binary). P2 because the view is functional before the invariant is
formally proven, but it must hold before the slice ships.

**Independent Test**: Inspect every network call the view issues — all are read-only requests
to guarded routes; ingest a poisoned chunk, run a query that would match it, confirm it is
absent by default and appears only with the opt-in toggle carrying its verdict; confirm the
view renders inside `goragApp` with no full page reload; confirm no `package.json` /
`node_modules` / build config is introduced.

**Acceptance Scenarios**:

1. **Given** the view in use, **When** its network calls are inspected, **Then** every call is
   a read-only request to a guarded `/api/*` route — no create / update / delete.
2. **Given** a chunk flagged as injection-poisoning, **When** a matching query runs with
   defaults, **Then** that chunk is excluded; **when** the operator opts in to include flagged
   chunks, **Then** it appears and carries its poisoning verdict.
3. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts
   are introduced.
4. **Given** an empty corpus or a no-match query, **When** a query runs, **Then** a healthy
   empty state renders (not an error).
5. **Given** the embedder is unreachable (needed for semantic/vector mode), **When** a query
   runs, **Then** a clear error renders and keyword mode is suggested — no silent failure.
6. **Given** a session that expires mid-query, **When** a fetch returns 401, **Then** the
   shell routes back to login (no crash, no silent failure).

---

### Edge Cases

- **Empty or whitespace-only query** — submit is disabled or returns a clear "enter a query"
  state; no backend call.
- **No results** (threshold too high, or genuinely no match) — a healthy empty-result state,
  with a hint to lower the threshold or broaden filters.
- **Empty corpus** — a healthy empty state, not an error.
- **Embedder unreachable** (semantic/keyword-with-vector mode needs local Ollama) — a clear
  error; suggest keyword mode, which needs no embedder for the lexical half.
- **Embedding-dimension mismatch** (query model ≠ corpus model) — the engine's mismatch guard
  error is surfaced plainly so the operator knows to re-embed or switch model, not silently
  swallowed.
- **Rerank attempted and failed** — results are valid but in fallback (RRF) order; a plain
  rerank-failed banner appears (mirroring the CLI's stderr warning).
- **All would-be hits are quarantined** — under defaults the result list is empty; the empty
  state hints that flagged chunks exist and can be opted into.
- **Adaptive depth changed k/pool** — the effective k and pool are shown so the operator is
  not surprised by a result count that differs from their requested top-k.
- **Very long chunk text** — a truncated preview with expand-to-full; no layout breakage.
- **Context expansion on a boundary chunk** (first/last in a document) — sibling context
  renders on the available side only.
- **Source file deleted after ingestion** — citation still resolves to the ingested content
  (identity is content-addressed); no broken link, optionally a read-only "source absent"
  note.
- **Near-duplicate cluster** — without dedup, duplicates render distinctly (the engine returns
  them); with dedup on, one representative per group.
- **Mid-query session expiry** — graceful return to the login screen.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST provide a query input and, on submit, run retrieval over the
  active vault returning ranked hits via the existing engine retrieval path (identical to
  `go-rag query`).
- **FR-002**: Each hit MUST show its relevance score, citation (source path, page when
  paginated, chunk index), section breadcrumb and heading depth, and chunk text (preview with
  expand-to-full).
- **FR-003**: The operator MUST be able to open a detail surface for any hit showing the full
  chunk text, section path and depth, sibling context chunks (when expansion is on), the
  document summary and enrichment state when present, and provenance signals (extraction
  method/quality, near-duplicate siblings, wikilink targets) when present.
- **FR-004**: The operator MUST be able to choose retrieval mode (hybrid / semantic / keyword),
  set top-k, set a minimum-score threshold, and toggle reranking on or off, per query.
- **FR-005**: The operator MUST be able to filter results by tag, by source, and by file type,
  combined by intersection.
- **FR-006**: The view MUST surface what the engine actually used — effective mode, effective
  top-k, and effective candidate pool — and MUST show a plain rerank-failed banner when
  reranking was attempted and failed (results remain valid, in fallback order).
- **FR-007**: The view MUST honour quarantine-by-default — chunks flagged as injection-poisoning
  are excluded unless the operator opts in; when opted in, each such hit MUST display its
  poisoning verdict so the retrieved text is treated as untrusted.
- **FR-008**: The operator MUST be able to optionally enable context expansion (sibling chunks),
  near-duplicate collapse, and cache bypass for a query.
- **FR-009**: The view MUST be strictly read-only — no add, remove, reingest, re-embed, or any
  state mutation; every network call is a read-only request to a guarded route.
- **FR-010**: The view MUST render inside the authenticated shell, gated by the existing spec
  045 / spec 046 Bearer guard, with no new authentication surface.
- **FR-011**: The view MUST ship inside the single binary via the existing embedded, vendored
  SPA — no Node / Vite / Tailwind build chain.
- **FR-012**: The view MUST render healthy states for empty corpus, no-result queries,
  in-flight queries, an unreachable embedder, and an embedding-dimension mismatch — no silent
  failures.
- **FR-013**: Hits, order, and scores shown MUST match `go-rag query` and the other transports
  byte-for-byte for the same input (cross-transport parity, as the Slice 0 Dashboard and
  Slice 1 Documents view).

### Key Entities *(include if feature involves data)*

- **Query**: the operator's natural-language input; the single retrieval trigger.
- **Retrieval Mode**: hybrid (default) / semantic / keyword — selects which index signals
  contribute to ranking.
- **QueryHit**: one ranked result — its chunk text, fused relevance score, citation (source,
  page, chunk index), section breadcrumb and heading depth, optional sibling context, document
  summary/enrichment state, provenance signals (extraction method/quality, near-duplicates,
  wikilinks), and poisoning verdict.
- **Effective Retrieval State**: the mode, top-k, and candidate pool the engine actually used
  for this query (explicit, classifier-recommended, or default), plus rerank success/failure.
- **Filter**: a tag / source / file-type intersection narrowing the candidate pool before
  ranking.
- **Poisoning Verdict**: the per-chunk injection-poisoning assessment carried on each hit;
  flagged chunks are excluded by default and surfaced only on opt-in, always with their
  verdict visible.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can run a query against a 1,000-document corpus and see the ranked
  top results within 1 second on a loopback connection.
- **SC-002**: 100% of operators can identify the source document and section of any result at
  a glance from the citation and section breadcrumb, without opening the CLI.
- **SC-003**: Results shown in the Query view are identical to `go-rag query` for the same
  input — zero drift in hits, order, or scores.
- **SC-004**: The view always tells the operator what retrieval mode, top-k, and candidate
  pool were actually used, and plainly warns when reranking failed.
- **SC-005**: No write action is possible from the Query view — verifiable by inspecting every
  network call the view issues.
- **SC-006**: Injection-flagged chunks are excluded by default; an operator who opts in sees
  the poisoning verdict on each included hit — never silent inclusion.
- **SC-007**: The view introduces zero new build tooling — a single `make build` still produces
  one binary that serves the console with no Node chain.

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp`
  root, and spec 045 Bearer auth unchanged — exactly as spec 047 did.
- This slice is read-only and adds **no new engine capability**. The engine returns a single
  fused relevance score per hit; per-stage score breakdown (BM25 / vector / rerank
  contributions) is not available on the hit and is out of scope for this slice (see Open
  Questions). This corrects an assumption in the original feature description.
- All retrieval data already exists in the engine via `Engine.Query` (and the retrieval specs
  009 / 014 / 015 / 016 / 019 / 023 / 024 / 025 / 026 / 029 / 036 / 041 / 042); plan confirms
  whether the existing REST query endpoint suffices for the UI transport or a read-only UI
  query accessor is added (the engine path is shared regardless, so parity holds).
- Quarantine-by-default is the engine's existing behaviour (`IncludeQuarantined=false` default,
  spec 019); the view exposes the opt-in and the verdict, it does not change the policy.
- Single-operator use; no multi-user or RBAC concerns (PRD N2).
- Desktop-first per `docs/style-guide.md`; mobile is not a target.
- The result cache (spec 016) is respected by default; cache bypass is an opt-in toggle, not
  the default.

---

## Open Questions (to resolve in plan / tasks)

- **Per-stage score breakdown** — the engine returns one fused Score per hit, not separate
  BM25/vector/rerank contributions. Surfacing a breakdown is an engine change and is out of
  scope for this read-only UI slice; this view shows the fused score plus effective mode/k/pool
  and rerank status. A future engine spec could add per-stage scores; defer.
- **Result cache indicator** — whether to surface a cache hit/miss (spec 016) on each query,
  or keep the cache invisible with only a bypass toggle. Lean: invisible + bypass toggle only.
- **Adaptive-depth prominence** — whether the classifier having chosen k/pool (spec 024) is
  shown as a distinct callout or merely reflected in the effective-k/pool indicators. Lean:
  reflect via effective indicators with a short note when the classifier acted.
- **Large result sets** — queries return at most top-k hits, so no pagination is needed beyond
  k; confirm k is bounded by a sane ceiling in the UI regardless of operator input.
- **include-quarantined persistence** — whether the opt-in toggle persists across queries or
  resets to the safe default each query. Lean: reset to default each query so the safe
  quarantine-by-default posture is the resting state.
- **Live re-query** — whether changing a control auto-re-runs the query or requires an explicit
  submit. Lean: explicit submit (matches CLI mental model; avoids surprise embedding cost).
