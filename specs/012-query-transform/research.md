# Phase 0 — Research: Query Transformation Seam + Normalization (H05)

> Each item: Decision · Rationale · Alternatives rejected. Grounded in code read
> this session: `internal/index/fts.go` (`Tokenize` already lowercases + splits on
> non-letter/digit — FTS is *already* case/whitespace-normalized), `internal/index/
> retrieval.go` (`Reranker` interface — the pattern to mirror), `internal/engine/
> query.go` (`req.Query` flows: empty-check → `checkEmbeddingMismatch` → `queryEmbed`
> (H07 prefix) → `SearchWithRerank`).

## 1. The interface lives in `internal/index`, applied by the engine (mirror `Reranker`)

**Decision**: Define `QueryTransformer` in `internal/index` (new `transform.go`),
with a pure-Go default `NormalizingTransformer`. The engine holds a `qTransformer`
(default = the normalizer) and applies it once at the top of `Engine.Query`. A
future HyDE/multi-query transformer implements the same interface in a separate
adapter package and is injected — `internal/index` never imports Ollama.

**Rationale**: This is exactly the established `Reranker` pattern (`type Reranker
interface` in `internal/index/retrieval.go`; the Ollama cross-encoder lives in
`internal/rerank`; the engine wires it per-query). H05's "behind `internal/index`
without Ollama coupling" maps 1:1 onto that pattern. Reusing it keeps the codebase
consistent and the dependency graph clean (Constitution Principle V).

**Alternatives rejected**:
- *Interface + default in `internal/engine`*: would put the seam at the wrong layer
  — the audit wants it behind `internal/index` so future implementers depend on the
  index package, not the engine. Rejected.
- *A new `internal/transform` package*: over-modular for one interface + one default
  that belongs with the retrieval types. Rejected; `internal/index/transform.go`
  co-locates it with `Reranker`.

## 2. Interface shape: returns `[]string` (one-or-more), to future-proof multi-query

**Decision**:
```go
type QueryTransformer interface {
    Transform(ctx context.Context, query string) ([]string, error)
}
```
The default normalizer returns exactly one element (`[]string{normalized}`), or an
error if the normalized result is empty (FR-006). `Engine.Query` uses the first
element for the current single-query retrieval; the slice return is the seam that
lets a future multi-query transformer yield N sub-queries without changing the
interface (FR-005). Multi-query *fan-out* in the engine is explicitly future work
(not implemented here).

**Rationale**: The spec requires the seam accommodate multi-query (FR-005). A
`string` return would force an interface change later; `[]string` costs one line
now and is the documented future-proof shape. Returning an `error` lets the
empty-after-normalize case be handled at the seam (FR-006) rather than special-cased
in the engine.

**Alternatives rejected**:
- *`Transform(query) string`*: precludes multi-query without a breaking change.
  Rejected.
- *Two interfaces (`QueryTransformer` + `MultiQueryTransformer`)*: needless split;
  one slice-returning interface covers both. Rejected.

## 3. Injection point: top of `Engine.Query`, replacing the bare empty-check

**Decision**: In `Engine.Query`, replace `if req.Query == "" { return ... ErrInvalid }`
with a transform step:
```go
transformed, err := e.qTransformer.Transform(ctx, req.Query)
if err != nil {
    return nil, err  // includes empty-after-normalization (FR-006)
}
req.Query = transformed[0]  // single-query retrieval; multi-query fan-out is future work
```
Everything downstream (`checkEmbeddingMismatch`, `queryEmbed`'s H07 prefix, and
`SearchWithRerank` for both FTS and vector) then uses the normalized query.

**Rationale**: There is exactly one place `req.Query` is consumed (verified: lines
18, 43, 46–48, 68 of `query.go`). Transforming it once, before any of those, reaches
all paths uniformly. Normalization happens BEFORE the H07 prefix is applied (normalize
the raw query, then prepend `search_query:`) — correct, because the prefix is a
role marker, not content.

**Alternatives rejected**:
- *Transform inside `Retrieval.Search`*: too late — the query is embedded via
  `queryEmbed` which is built in `Engine.Query` before `Search` is called. Rejected.
- *Transform separately for FTS and vector*: risks divergence (two transforms).
  One transform, one query. Rejected.

## 4. Normalization operations: Unicode-aware trim + whitespace-collapse + case-fold

**Decision**: `normalizeQuery(s)`:
1. `strings.ToLower` — Unicode case-fold (handles ASCII + non-ASCII; safe for the
   default nomic model).
2. Collapse runs of whitespace to a single space and `strings.TrimSpace` the ends.

**Rationale**: The audit names "case/whitespace." `strings.ToLower` is Unicode-aware
(FR-008 — does not corrupt CJK/accents; CJK has no case so it's a no-op there).
Whitespace collapse + trim is the cosmetic robustness win. This is idempotent
(FR-007) and a no-op on already-clean input.

**Alternatives rejected**:
- *Also strip punctuation / stopwords*: FTS's `Tokenize` already drops non-letter/digit
  and stopwords for the keyword path; doing it here would affect the vector path's
  embedding (could hurt). Out of scope for "lightweight." Rejected.
- *Stemming / lemmatization*: a heavier, dictionary-dependent transform — exactly
  the kind of *advanced* transform the seam enables later, not part of the default.
  Rejected for H05.

## 5. The vector query/document case-asymmetry is accepted and gated by SC-002

**Decision**: Normalize the **query only**. Documents remain embedded verbatim
(with their H07 `search_document:` prefix). The minor query/document case
asymmetry this creates is safe for the default nomic model (largely case-insensitive)
and is GATED by the eval harness (SC-002: no regression). A fully consistent
doc-side normalization (lowercasing chunk text at ingest) would change the
embedding profile and require re-embedding the corpus — out of scope, a future item.

**Rationale**: The "lightweight" framing and idempotent-ingestion principle (II)
rule out a corpus re-embed. The eval gate makes the asymmetry risk measurable and
bounded — if a future model is case-sensitive and regresses, the default can drop
case-folding without touching the seam.

**Alternatives rejected**:
- *Normalize documents at ingest too (consistent)*: requires re-embedding the whole
  corpus (migration, embedding-profile change). Heavy; explicitly out of scope.
  Rejected.
- *Case-fold only on the FTS path (already done) and skip the vector path*: would
  mean the normalizer is a no-op for the vector side, giving up the one place
  normalization has visible effect. Rejected — gate it instead.

## 6. The seam is injection-tested (US2) via an in-package engine test

**Decision**: `Engine.qTransformer` is an unexported field, defaulted to the
normalizer in both constructors. The US2 test (in package `engine`) sets
`e.qTransformer` to a fake that appends a synonym and asserts the results change.
No new public constructor is added (YAGNI) — when a *production* custom
transformer exists (HyDE), a constructor option is added then.

**Rationale**: Keeps the public surface minimal (spec: no new config/flag for the
default) while proving the seam is live. Mirrors how other engine internals are
tested in-package.

**Alternatives rejected**:
- *A public `NewWithQueryTransformer` constructor now*: no production caller needs
  it yet; adding it is speculative. Rejected (add when needed).
- *Config-driven transformer selection*: spec explicitly defers this (no new config
  key for the default). Rejected.

## 7. SC-002 (no regression) is near-trivial for the harness, and that's fine

**Decision**: The H02 eval queries are already clean (lowercase, single-spaced),
so normalization is a no-op for them and the harness shows zero change. SC-002
passes by construction. The gate's real value is catching an *accidental* regression
(e.g. a normalization bug that mangles queries), not measuring an improvement.

**Rationale**: H05's value is the **seam** (enabling HyDE/multi-query/synonym
expansion later — the big levers), not the normalization itself. The spec says so
honestly; SC-002 is a no-regression gate, not an improvement target.
