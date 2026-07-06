---
name: gortex-index-1-dirs-search
description: "Work in the index +1 dirs · Search area — 344 symbols across 13 files (81% cohesion)"
---

# index +1 dirs · Search

344 symbols | 13 files | 81% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/index/bench_test.go`
- `internal/index/filter_test.go`
- `internal/index/fts.go`
- `internal/index/fts_test.go`
- `internal/index/index.go`
- `internal/index/retrieval.go`
- `internal/index/retrieval_seam_test.go`
- `internal/index/retrieval_test.go`
- `internal/index/testhelpers_test.go`
- `internal/index/vector.go`
- `internal/index/vector_contract_test.go`
- `internal/index/vector_test.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | TODO |
| `internal/index/bench_test.go` | embed, i, vec, err, b, ... |
| `internal/index/filter_test.go` | hits, vec, r2, hits2, TestRetrieval_SetFilter_PreFusion, ... |
| `internal/index/fts.go` | chunkID, err, field, field, Score, ... |
| `internal/index/fts_test.go` | hits, f, TestFTS_CaseFoldingAndStopwords, hits, f, ... |
| `internal/index/index.go` | VectorIndex |
| `internal/index/retrieval.go` | SetFilter, EmbedFunc, Search, embed, out, ... |
| `internal/index/retrieval_seam_test.go` | err, realR, closure@62, r, fakeVectorIndex, ... |
| `internal/index/retrieval_test.go` | kwBase, vec, hits, mk, fts, ... |
| `internal/index/testhelpers_test.go` | err, db, newTestFTS, t, closure@17 |
| `internal/index/vector.go` | all, vec, all, data, out, ... |
| `internal/index/vector_contract_test.go` | ids, contractSameHits, b, again, t, ... |
| `internal/index/vector_test.go` | hits, v, v, path, t, ... |

## Entry Points

- `internal/index/retrieval_test.go::TestRetrieval_SetRRFK_ChangesFusionScore`

## Connected Communities

- **engine +19 dirs** (67 cross-edges)
- **engine +13 dirs** (13 cross-edges)
- **. +2 dirs · MigrateFromChunks** (4 cross-edges)
- **engine +12 dirs** (3 cross-edges)
- **reader +8 dirs** (3 cross-edges)
- **cli +13 dirs** (3 cross-edges)
- **model +2 dirs** (3 cross-edges)
- **index +2 dirs** (3 cross-edges)
- **index +1 dirs · Delete** (3 cross-edges)
- **engine +7 dirs** (2 cross-edges)
- **storage/migrate +1 dirs · Run** (1 cross-edges)
- **reader +19 dirs** (1 cross-edges)
- **engine +3 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-60"
smart_context with task: "understand index +1 dirs · Search", format: "gcx"
find_usages with id: "internal/index/retrieval_test.go::TestRetrieval_SetRRFK_ChangesFusionScore", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
