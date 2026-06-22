# Data Model: Metadata Filtering at Retrieval (H14)

> No persisted entity changes. The filter is request-state + retrieval logic; `Document`
> attributes (`FilePath`, `FileType`, `Metadata`) are the match targets, read via the existing
> `lookupDoc` resolver. Principle II intact.

## Entities

### Filter (request-state, in-memory)

The scoping predicate for a query. Optional on every `QueryRequest`; absent = no filtering.

| Field | Go type | Match target | Semantics |
|-------|---------|--------------|-----------|
| `Source` | `string` | `Document.FilePath` | Glob match (empty = no constraint) |
| `Type` | `string` | `Document.FileType` | Exact, case-insensitive (empty = no constraint) |
| `Tags` | `[]string` | `Document.Metadata["tags"]` | Conjunction: doc must carry ALL (nil/empty = no constraint) |

**Validation**: `Filter.Empty()` returns true when all dimensions are unset → no filtering.
`Filter.Matches(filePath, fileType string, docTags []string) bool` evaluates the conjunction.

### Keep predicate (retrieval-internal)

```go
keep func(chunkID string) bool   // nil = no filter (today's behavior)
```

Built by `Engine.Query` from `QueryRequest.Filter` + the `docOf`/`lookupDoc` resolvers.
Applied to FTS and Vector candidate lists **before RRF fusion** by `Retrieval.Search`.

## Relationships

```text
QueryRequest.Filter ──► Engine.Query
                          │  build keep(chunkID) from Filter + lookupDoc
                          ▼
                    Retrieval.SearchWithRerank(ctx, query, k, mode, docOf, keep, ...)
                          │
                    ┌─────┴──────┐
                    ▼            ▼
              FTS candidates  Vector candidates
                    │            │
                    ▼            ▼
              keep(chunkID)?  keep(chunkID)?   ← filter pre-fusion
                    │            │
                    └────┬───────┘
                         ▼
                   RRF fusion (filtered)
                         │
                         ▼
                   collapseByDoc (filtered set)
                         │
                         ▼
                   rerank (filtered pool)
                         │
                         ▼
                   top-k results (scoped)
```

## Validation rules (from requirements)

- **FR-001**: filter has source/type/tags dimensions.
- **FR-002**: conjunction across dimensions.
- **FR-003**: filtered results only from matching docs; matches-nothing → empty.
- **FR-004**: no filter → today's behavior (byte-identical).
- **FR-005**: filter applied pre-fusion (FTS + Vector candidate lists).
- **FR-006**: filter applies in all modes.
- **FR-007**: collapse + rerank operate on the filtered set.
- **FR-008**: filter on CLI/REST/gRPC/MCP (parity).
- **FR-009**: empty dimensions ignored.
