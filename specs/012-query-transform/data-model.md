# Data Model: Query Transformation Seam (H05)

> No storage entities change — this is a pure query-path change. No new persisted
> data, no record shape change (Constitution Principle II intact). This file
> describes the **in-memory entity** (the seam) and the **normalized-query contract**.

## Entities

### QueryTransformer (in-memory, the seam)

The extension point that alters the query before retrieval. Defined in
`internal/index`; the default is pure-Go; future advanced transforms implement it
in an adapter package (no Ollama in `internal/index`).

| Aspect | Value |
|--------|-------|
| Shape | `Transform(ctx, query string) ([]string, error)` |
| Returns | one-or-more transformed queries (slice → future multi-query; default returns exactly 1) |
| Error | non-nil when the transformed query is empty (FR-006) or the transform itself fails |
| Default | `NormalizingTransformer` (always on) |
| Lifetime | one per `Engine`; applied once per query at the top of `Engine.Query` |

### NormalizingTransformer (the default)

The built-in, always-on transformer. Pure Go (stdlib only).

| Aspect | Value |
|--------|-------|
| Operations | Unicode case-fold (`strings.ToLower`) + collapse whitespace runs + trim ends |
| Idempotent | yes — `norm(norm(q)) == norm(q)` (FR-007) |
| No-op on clean input | yes |
| Unicode-safe | yes — does not corrupt CJK/accents (FR-008) |
| Empty result | returns `(nil, error)` — handled as the empty-query case (FR-006) |

### Normalized query

The string retrieval actually searches/embeds (the first element of the transformer's
output, for the current single-query path).

| Aspect | Value |
|--------|-------|
| Source | `req.Query` after `e.qTransformer.Transform` |
| Consumed by | `checkEmbeddingMismatch`, `queryEmbed` (H07 `search_query:` prefix), `SearchWithRerank` (FTS + vector) |
| Relationship to documents | normalized **query** vs verbatim-embedded **documents** (minor case asymmetry, gated by SC-002) |

## Relationships

```text
QueryRequest.Query ──► Engine.Query
                          │  e.qTransformer.Transform(ctx, req.Query)   ← the seam (default: NormalizingTransformer)
                          ▼
                    []string (normalized; len 1 today)
                          │  req.Query = [0]
                          ▼
          ┌───────────────┼────────────────┐
          ▼               ▼                ▼
   checkEmbedding   queryEmbed        SearchWithRerank
   Mismatch         (H07 query         ┌─ fts.Search (already lowercases)
   (probe)           prefix + embed)   └─ semantic → vec.Query
```

## Validation rules (from requirements)

- **FR-001**: the transform runs before retrieval, in the shared engine path (all transports, all modes).
- **FR-002**: default normalizes (Unicode case-fold + whitespace).
- **FR-003**: pluggable seam in `internal/index`; default carries no external dependency.
- **FR-004**: a caller-supplied transformer is honored.
- **FR-005**: the seam accommodates one-or-more outputs (multi-query future).
- **FR-006**: empty-after-transform → error, never a garbage embed.
- **FR-007**: idempotent.
- **FR-008**: Unicode-aware, non-ASCII-safe.
- **FR-009**: no retrieval-quality regression (eval gate).

No persisted state; no migration.
