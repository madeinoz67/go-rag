---
name: gortex-rest-2-dirs-writeengineerr
description: "Work in the rest +2 dirs · writeEngineErr area — 220 symbols across 7 files (87% cohesion)"
---

# rest +2 dirs · writeEngineErr

220 symbols | 7 files | 87% cohesion

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
| `internal/rest/engine_adapter.go` | r, handlePoisonReset, i, err, plan, ... |
| `internal/rest/get_chunk.go` | getChunkResponse, err, Chunk, r, handleGetChunk, ... |
| `internal/rest/openapi.go` | handleOpenAPI, w |
| `internal/rest/server.go` | method, token, guard, h, w, ... |
| `internal/rest/types.go` | Queries, Summary, AdaptiveDepthEnabled, EnrichedDocs, EnrichmentEnabled, ... |

## Entry Points

- `internal/rest/engine_adapter.go::Server.handleQuery`

## Connected Communities

- **engine +19 dirs** (12 cross-edges)
- **audit +4 dirs** (4 cross-edges)
- **engine +7 dirs** (3 cross-edges)
- **reader +17 dirs** (3 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (3 cross-edges)
- **engine +12 dirs** (3 cross-edges)
- **engine +2 dirs · Get** (2 cross-edges)
- **engine +3 dirs** (2 cross-edges)
- **engine +1 dirs · ResetChunk** (2 cross-edges)
- **engine +13 dirs** (2 cross-edges)
- **rest +1 dirs · documentMetaDTO** (2 cross-edges)
- **index +1 dirs · Filter** (1 cross-edges)
- **rest +1 dirs · poisonedChunk** (1 cross-edges)
- **cli +13 dirs** (1 cross-edges)
- **engine +2 dirs · Score** (1 cross-edges)
- **rest · toMigrationPlan** (1 cross-edges)
- **audit +1 dirs** (1 cross-edges)
- **reader +7 dirs** (1 cross-edges)
- **cli +7 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-81"
smart_context with task: "understand rest +2 dirs · writeEngineErr", format: "gcx"
find_usages with id: "internal/rest/engine_adapter.go::Server.handleQuery", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
