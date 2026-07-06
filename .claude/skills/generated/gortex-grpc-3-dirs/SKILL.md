---
name: gortex-grpc-3-dirs
description: "Work in the grpc +3 dirs area — 187 symbols across 7 files (77% cohesion)"
---

# grpc +3 dirs

187 symbols | 7 files | 77% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/engine/migrate_plan.go`
- `internal/engine/types.go`
- `internal/grpc/engine_adapter.go`
- `internal/grpc/server.go`
- `internal/grpc/server_test.go`
- `proto/gen/gorag.pb.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | int32 |
| `internal/engine/migrate_plan.go` | Total, Estimate, ModelChange, Consistent, StaleTotal, ... |
| `internal/engine/types.go` | New, IngestSummary, Deleted, Modified, AvgKept, ... |
| `internal/grpc/engine_adapter.go` | i, Scan, files, i, created, ... |
| `internal/grpc/server.go` | Adapter, eng, UnimplementedGoragServer |
| `internal/grpc/server_test.go` | Dimensions, Model, fakeEmbed |
| `proto/gen/gorag.pb.go` | PageNumber, String, OllamaUrl, PoolSize, mi, ... |

## Connected Communities

- **grpc +8 dirs** (10 cross-edges)
- **engine +19 dirs** (10 cross-edges)
- **grpc +1 dirs · QueryRequest** (2 cross-edges)
- **protobuf/runtime +2 dirs** (2 cross-edges)

## How to Explore

```
get_communities with id: "community-53"
smart_context with task: "understand grpc +3 dirs", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
