---
name: gortex-rest-2-dirs
description: "Work in the rest +2 dirs area — 199 symbols across 7 files (86% cohesion)"
---

# rest +2 dirs

199 symbols | 7 files | 86% cohesion

## When to Use

Use this skill when working on files in:
- `external-call::dep:github.com/prometheus/client_golang/prometheus/promhttp`
- `internal/observe/prometheus.go`
- `internal/rest/engine_adapter.go`
- `internal/rest/get_chunk.go`
- `internal/rest/openapi.go`
- `internal/rest/server.go`
- `internal/rest/types.go`

## Key Files

| File | Symbols |
|------|---------|
| `external-call::dep:github.com/prometheus/client_golang/prometheus/promhttp` | github.com/prometheus/client_golang/prometheus/promhttp |
| `internal/observe/prometheus.go` | MetricsHandler |
| `internal/rest/engine_adapter.go` | handleMigrate, f, res, out, w, ... |
| `internal/rest/get_chunk.go` | Document, r, Chunk, getChunkResponse, id, ... |
| `internal/rest/openapi.go` | w, handleOpenAPI |
| `internal/rest/server.go` | v, r, token, handlerFor, closure@129, ... |
| `internal/rest/types.go` | NoRerank, Name, Flagged, Queries, Chunks, ... |

## Entry Points

- `internal/rest/engine_adapter.go::Server.handleQuery`

## Connected Communities

- **engine +19 dirs** (10 cross-edges)
- **audit +4 dirs** (4 cross-edges)
- **reader +19 dirs** (3 cross-edges)
- **engine +12 dirs** (3 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (3 cross-edges)
- **engine +7 dirs** (3 cross-edges)
- **rest +1 dirs · chunkDTO** (2 cross-edges)
- **engine +2 dirs · Get** (2 cross-edges)
- **engine +1 dirs · ResetChunk** (2 cross-edges)
- **engine +13 dirs** (2 cross-edges)
- **engine +3 dirs** (2 cross-edges)
- **reader +7 dirs** (1 cross-edges)
- **cli +13 dirs** (1 cross-edges)
- **engine +2 dirs · Score** (1 cross-edges)
- **index +1 dirs · Filter** (1 cross-edges)
- **rest +1 dirs · poisonedChunk** (1 cross-edges)
- **model +1 dirs · documentMetaDTO** (1 cross-edges)
- **cli +7 dirs** (1 cross-edges)
- **rest · toMigrationPlan** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-81"
smart_context with task: "understand rest +2 dirs", format: "gcx"
find_usages with id: "internal/rest/engine_adapter.go::Server.handleQuery", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
