---
name: gortex-grpc-8-dirs
description: "Work in the grpc +8 dirs area — 462 symbols across 11 files (82% cohesion)"
---

# grpc +8 dirs

462 symbols | 11 files | 82% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `external-call::dep:google.golang.org/grpc`
- `external-call::dep:google.golang.org/grpc/credentials/insecure`
- `external-call::dep:google.golang.org/grpc/status`
- `external-call::dep:google.golang.org/grpc/test/bufconn`
- `internal/grpc/engine_adapter.go`
- `internal/grpc/server_test.go`
- `proto/gen/gorag.pb.go`
- `proto/gen/gorag_grpc.pb.go`
- `proto/gorag.proto`
- `specs/003-rest-grpc-api/contracts/gorag.proto`

## Key Files

| File | Symbols |
|------|---------|
| `` | new, error |
| `external-call::dep:google.golang.org/grpc` | google.golang.org/grpc |
| `external-call::dep:google.golang.org/grpc/credentials/insecure` | google.golang.org/grpc/credentials/insecure |
| `external-call::dep:google.golang.org/grpc/status` | google.golang.org/grpc/status |
| `external-call::dep:google.golang.org/grpc/test/bufconn` | google.golang.org/grpc/test/bufconn |
| `internal/grpc/engine_adapter.go` | out, ReleaseChunk, err, flagged, err, ... |
| `internal/grpc/server_test.go` | got, TestGRPC_Query_EmptyQuery_InvalidArgument, t, eng, t, ... |
| `proto/gen/gorag.pb.go` | GetModified, String, unknownFields, Modified, IngestSummary, ... |
| `proto/gen/gorag_grpc.pb.go` | ctx, ctx, opts, out, interceptor, ... |
| `proto/gorag.proto` | Dirs, RescanPoisoning, Query, ListPoisoned, Reprocess, ... |
| `specs/003-rest-grpc-api/contracts/gorag.proto` | Scan, Gorag, GetConfig, Health, Migrate, ... |

## Connected Communities

- **engine +19 dirs** (22 cross-edges)
- **engine +12 dirs** (6 cross-edges)
- **grpc +1 dirs · QueryRequest** (3 cross-edges)
- **grpc +2 dirs** (2 cross-edges)
- **engine +13 dirs** (2 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (2 cross-edges)

## How to Explore

```
get_communities with id: "community-161"
smart_context with task: "understand grpc +8 dirs", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
