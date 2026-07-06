---
name: gortex-engine-3-dirs
description: "Work in the engine +3 dirs area — 217 symbols across 16 files (56% cohesion)"
---

# engine +3 dirs

217 symbols | 16 files | 56% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/engine/cache.go`
- `internal/engine/cache_embed_test.go`
- `internal/engine/engine.go`
- `internal/engine/engine_test.go`
- `internal/engine/helpers.go`
- `internal/engine/index_cache_test.go`
- `internal/engine/neardup.go`
- `internal/engine/poison_test.go`
- `internal/engine/query.go`
- `internal/engine/query_test.go`
- `internal/engine/query_transform_test.go`
- `internal/engine/status_test.go`
- `internal/engine/types.go`
- `internal/index/retrieval.go`
- `internal/model/model.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | uint64 |
| `internal/engine/cache.go` | effPool, Enabled, req, resultKey, epoch, ... |
| `internal/engine/cache_embed_test.go` | i, Embed, out, texts |
| `internal/engine/engine.go` | err, indexes, fts, vec |
| `internal/engine/engine_test.go` | err, t, TestEngine_Query_RejectsEmpty, eng |
| `internal/engine/helpers.go` | docID, ok, lookupDoc, db, raw |
| `internal/engine/index_cache_test.go` | TestQuery_ReusesSharedIndex, f2, v1, t, v2, ... |
| `internal/engine/neardup.go` | listsSibling, nd, chunkID, s |
| `internal/engine/poison_test.go` | e, i, inc, err, TestQuery_Poisoning_QuarantinedByDefault, ... |
| `internal/engine/query.go` | filePath, em, f, effPool, filterOn, ... |
| `internal/engine/query_test.go` | res, eng, err, err, cfg, ... |
| `internal/engine/query_transform_test.go` | e, base, TestQuery_CustomTransformer_Honored, err, got, ... |
| `internal/engine/status_test.go` | err, st, eng2, want, TestEngine_Status_AdaptiveKnobs_ReflectConfig, ... |
| `internal/engine/types.go` | ContextWindow, Threshold, Query, NoRerank, Mode, ... |
| `internal/index/retrieval.go` | s, ParseMode |
| `internal/model/model.go` | IngestedAt, MimeType, ID, Enrichment, FileName, ... |

## Entry Points

- `internal/engine/status_test.go::TestEngine_Status_PoolUtilization`

## Connected Communities

- **engine +19 dirs** (23 cross-edges)
- **engine +12 dirs** (17 cross-edges)
- **engine +13 dirs** (15 cross-edges)
- **engine +7 dirs** (9 cross-edges)
- **otel +1 dirs** (5 cross-edges)
- **engine +2 dirs · Get** (4 cross-edges)
- **index +1 dirs · Search** (4 cross-edges)
- **engine · expandContext** (4 cross-edges)
- **index +2 dirs** (3 cross-edges)
- **daemon +15 dirs** (3 cross-edges)
- **audit +4 dirs** (3 cross-edges)
- **engine +2 dirs · waitForEpoch** (3 cross-edges)
- **reader +19 dirs** (3 cross-edges)
- **index +1 dirs · Filter** (2 cross-edges)
- **engine · checkEmbeddingMismatch** (2 cross-edges)
- **cli +7 dirs** (1 cross-edges)
- **engine +2 dirs · Score** (1 cross-edges)
- **embed · ForRole** (1 cross-edges)
- **index · TestRuleBasedClassifier_Shallow…** (1 cross-edges)
- **engine +10 dirs** (1 cross-edges)
- **daemon +5 dirs** (1 cross-edges)
- **eval +6 dirs** (1 cross-edges)
- **cli +13 dirs** (1 cross-edges)
- **pipeline +4 dirs** (1 cross-edges)
- **config +2 dirs · pipeline** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-38"
smart_context with task: "understand engine +3 dirs", format: "gcx"
find_usages with id: "internal/engine/status_test.go::TestEngine_Status_PoolUtilization", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
