---
name: gortex-protobuf-runtime-2-dirs
description: "Work in the protobuf/runtime +2 dirs area — 267 symbols across 3 files (81% cohesion)"
---

# protobuf/runtime +2 dirs

267 symbols | 3 files | 81% cohesion

## When to Use

Use this skill when working on files in:
- `external-call::dep:google.golang.org/protobuf/runtime/protoimpl`
- `internal/grpc/engine_adapter.go`
- `proto/gen/gorag.pb.go`

## Key Files

| File | Symbols |
|------|---------|
| `external-call::dep:google.golang.org/protobuf/runtime/protoimpl` | google.golang.org/protobuf/runtime/protoimpl |
| `internal/grpc/engine_adapter.go` | req, vals, GetConfig, err |
| `proto/gen/gorag.pb.go` | ms, mi, mi, mi, ProtoReflect, ... |

## Connected Communities

- **grpc +8 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-130"
smart_context with task: "understand protobuf/runtime +2 dirs", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
