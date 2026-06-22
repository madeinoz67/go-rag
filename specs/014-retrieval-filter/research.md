# Phase 0 — Research: Metadata Filtering at Retrieval (H14)

> Grounded in: `internal/index/retrieval.go` (Search/SearchWithRerank, reciprocalRankFusion,
> collapseByDoc), `internal/engine/{query.go,helpers.go}` (docOf, lookupDoc — chunk→Document
> resolution), `internal/model/model.go` (Document: FilePath, FileType, Metadata — no Tags field),
> `internal/cli/query.go` (the dead `--source` flag).

## 1. Filter application site: Retrieval layer, pre-fusion (not inside FTS/Vector)

**Decision**: Apply the filter at the `Retrieval.Search`/`SearchWithRerank` level — get
the FTS candidate list and the Vector candidate list as today, filter each by the predicate
(chunk→doc→attributes), THEN fuse. The `FTS.Search`/`Vector.Query` signatures stay unchanged.

**Rationale**: FTS indexes `{body}` per chunkID — it doesn't know document attributes
(FilePath/FileType/tags). "Pre-FTS pruning" in the audit's literal sense (prune during the
BM25 scan) would require re-indexing doc attributes into FTS — a heavy restructuring. Filtering
the candidate lists **pre-fusion** achieves the user-facing outcome (scoped results, no
non-matching chunks scored/ranked) without touching FTS/Vector internals. The Retrieval layer
already has access to a `docOf` resolver (passed by the engine), which it can use to resolve
chunk→doc→attributes for the filter check.

**Alternatives rejected**:
- *Enrich FTS indexing with doc attributes (source/type/tags as FTS fields)*: restructures FTS
  (currently title/heading/body weighted fields); heavier; changes the BM25 scoring. Rejected
  for v1.
- *Post-fusion filter (filter the final hits)*: correct but wasteful (fuses+scores non-matching
  chunks, then discards). The audit wants pre-fusion. Rejected.

## 2. Filter representation: a `Filter` struct + a `keep(chunkID) bool` predicate

**Decision**: Define `index.Filter{Source, Type, Tags}` with a `Matches(attrs) bool` method.
The engine builds a `keep func(chunkID string) bool` closure that resolves chunk→doc→attributes
(via `lookupDoc`) and delegates to `Filter.Matches`. `Retrieval.Search`/`SearchWithRerank` take
the `keep` predicate (a `func(string) bool`, nil = no filter) and apply it to candidate lists.

**Rationale**: The Retrieval layer stays attribute-agnostic (it just calls `keep(chunkID)`).
The engine owns the chunk→doc→attribute resolution (it already does this for `docOf`/`FilePath`
in the result-building loop). A nil `keep` predicate = today's behavior (zero overhead, no
function-call per candidate). `Filter` as a struct (not a func) makes it serializable for
transports (proto/JSON).

**Alternatives rejected**:
- *Thread the `Filter` struct itself into Retrieval (not a predicate)*: would couple Retrieval
  to Document attributes + the DB lookup. The predicate decouples cleanly. Rejected.
- *Thread Filter into FTS.Search/Vector.Query*: changes those signatures; they'd need attribute
  resolution. Rejected (see §1).

## 3. Source filtering: glob match on FilePath

**Decision**: `Source` is a glob pattern matched against the document's `FilePath` (e.g.,
`docs/**`, `*.md`, `meetings/*`). Uses `path.Match` or `filepath.Match` semantics. An empty
`Source` = no source constraint.

**Rationale**: The existing CLI `--source` flag (currently dead) is described as "filter by
source file glob." Glob is the natural, user-friendly semantic for path filtering. The document's
`FilePath` is already stored (persisted, resolved via `lookupDoc`).

**Alternatives rejected**:
- *Exact path match*: too rigid (can't match a folder). Rejected.
- *Regex*: overkill for path filtering; glob is the standard. Rejected.

## 4. Type filtering: exact match on FileType

**Decision**: `Type` is matched exactly against the document's `FileType` (e.g., `.md`,
`markdown`). An empty `Type` = no type constraint. Matching is case-insensitive (normalize both
sides to lowercase).

**Rationale**: `FileType` is a small enum-like set (pdf|text|markdown|docx|...). Exact match
is unambiguous and predictable. Case-insensitive handles user input variance (`.MD` vs `.md`).

## 5. Tag filtering: membership in document Metadata

**Decision**: `Tags` is a `[]string`; a document matches the tag filter if it carries ALL
specified tags (conjunction). Tags are stored in `Document.Metadata["tags"]` (a `[]string`
or comma-separated). If the document has no tags or the Metadata key is absent, it doesn't
match a tag filter.

**Rationale**: The `Document` model has `Metadata map[string]any` but no dedicated `Tags`
field (verified: `model.go:25-33`). Tags-in-metadata is the reasonable default (no schema
change). Conjunction (ALL tags) is the stricter, more predictable default — a user specifying
multiple tags wants docs that have all of them.

**Alternatives rejected**:
- *Add a dedicated `Tags []string` field to Document*: schema change + migration. Rejected
  for v1; Metadata is the right home (the spec's assumption).
- *ANY-tags (disjunction)*: less predictable; a user listing tags usually wants all. Rejected.

## 6. Efficiency: pre-fusion candidate filtering (with poolSize headroom)

**Decision**: Filter the FTS top-poolSize and Vector top-poolSize candidate lists before fusion.
The existing `poolSize=60` provides headroom for moderately selective filters. For very
selective filters (matching <2% of docs), the pool may thin → fewer results. Oversampling
(increasing poolSize for filtered queries) is a future optimization.

**Rationale**: For an M-effort item, filtering the existing pool is correct and sufficient
for typical use. The audit's "pre-FTS + post-vector" is satisfied (both candidate lists are
filtered before fusion). True in-scan pruning (fetching only matching chunks) is a heavier
optimization that would require doc-attribute indexing — deferred.

## 7. Transport exposure: mirror H08 (rrf_k) — CLI + REST + gRPC + MCP

**Decision**: Expose the filter on all four transports:
- CLI: wire the existing `--source` flag + add `--type` and `--tags` (comma-separated).
- REST: `queryRequest` gains `source`/`type`/`tags` JSON fields.
- gRPC: `QueryRequest` proto gains `source`/`type`/`tags` fields; regen.
- MCP: `go_rag_query` inputSchema gains `source`/`type`/`tags` properties.

**Rationale**: Cross-transport parity (spec 003 FR-002/003, constitution Principle V).
The filter is part of the query contract; all transports must express it identically. This
mirrors exactly how H08 exposed `rrf_k` across the four transports (including proto regen).

## 8. Filter × collapse × rerank ordering

**Decision**: Filter is applied FIRST (pre-fusion), then RRF fuses the filtered lists, then
`collapseByDoc` (top-1 per doc among the filtered set), then rerank operates on the filtered +
collapsed pool. This ordering ensures no non-matching chunk reaches fusion/collapse/rerank.

**Rationale**: The filter scopes the candidate universe; everything downstream operates within
that scope. This is the most efficient (no wasted work on non-matching chunks) and correct
(collapse/rerank never see filtered-out docs).
