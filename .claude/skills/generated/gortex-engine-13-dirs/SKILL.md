---
name: gortex-engine-13-dirs
description: "Work in the engine +13 dirs area — 469 symbols across 34 files (60% cohesion)"
---

# engine +13 dirs

469 symbols | 34 files | 60% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `external-call::dep:go.opentelemetry.io/otel`
- `external-call::dep:go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`
- `external-call::dep:go.opentelemetry.io/otel/exporters/prometheus`
- `external-call::dep:go.opentelemetry.io/otel/exporters/stdout/stdouttrace`
- `external-call::dep:go.opentelemetry.io/otel/sdk/metric`
- `external-call::dep:go.opentelemetry.io/otel/sdk/resource`
- `external-call::dep:go.opentelemetry.io/otel/sdk/trace`
- `external-call::dep:go.opentelemetry.io/otel/sdk/trace/tracetest`
- `external-call::dep:go.opentelemetry.io/otel/semconv/v1.26.0`
- `external-call::dep:go.opentelemetry.io/otel/trace`
- `internal/audit/audit.go`
- `internal/cli/serve.go`
- `internal/cli/serve_bind_test.go`
- `internal/engine/audit_test.go`
- `internal/engine/baseline.go`
- `internal/engine/baseline_lifecycle_test.go`
- `internal/engine/cache_safety_test.go`
- `internal/engine/context_window_test.go`
- `internal/engine/drift.go`
- `internal/engine/drift_test.go`
- `internal/engine/engine.go`
- `internal/engine/filter_test.go`
- `internal/engine/get_chunk.go`
- `internal/engine/get_chunk_test.go`
- `internal/engine/health.go`
- `internal/engine/index_cache_test.go`
- `internal/engine/ingest.go`
- `internal/engine/poison_test.go`
- `internal/engine/wikilinks_test.go`
- `internal/observe/observe_test.go`
- `internal/observe/otel.go`
- `internal/observe/spans.go`
- `internal/pipeline/reprocess_test.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | Notify, Background, close, context, signal, ... |
| `external-call::dep:go.opentelemetry.io/otel` | go.opentelemetry.io/otel |
| `external-call::dep:go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp |
| `external-call::dep:go.opentelemetry.io/otel/exporters/prometheus` | go.opentelemetry.io/otel/exporters/prometheus |
| `external-call::dep:go.opentelemetry.io/otel/exporters/stdout/stdouttrace` | go.opentelemetry.io/otel/exporters/stdout/stdouttrace |
| `external-call::dep:go.opentelemetry.io/otel/sdk/metric` | go.opentelemetry.io/otel/sdk/metric |
| `external-call::dep:go.opentelemetry.io/otel/sdk/resource` | go.opentelemetry.io/otel/sdk/resource |
| `external-call::dep:go.opentelemetry.io/otel/sdk/trace` | go.opentelemetry.io/otel/sdk/trace |
| `external-call::dep:go.opentelemetry.io/otel/sdk/trace/tracetest` | go.opentelemetry.io/otel/sdk/trace/tracetest |
| `external-call::dep:go.opentelemetry.io/otel/semconv/v1.26.0` | go.opentelemetry.io/otel/semconv/v1.26.0 |
| `external-call::dep:go.opentelemetry.io/otel/trace` | go.opentelemetry.io/otel/trace |
| `internal/audit/audit.go` | closure@123, Close, a, dbPath, SetGlobal, ... |
| `internal/cli/serve.go` | closure@36, newServeCmd, cmd |
| `internal/cli/serve_bind_test.go` | t, b, cmd, TestServeBootGate_OptInFlagRegistered, err |
| `internal/engine/audit_test.go` | TestAudit_QueryIngestEvents, s, err, ap, t, ... |
| `internal/engine/baseline.go` | RecordedAt, conv, convention, pre, refreshBaselineAfterMigrate, ... |
| `internal/engine/baseline_lifecycle_test.go` | b, t, ok, t, waitForBaseline, ... |
| `internal/engine/cache_safety_test.go` | paths, err, g, e, dir, ... |
| `internal/engine/context_window_test.go` | text, path, path, t, TestQuery_ContextWindow_Expansion, ... |
| `internal/engine/drift.go` | ctx, CachedLiveVersion, RefreshDriftVerdict, v, currentVerdict |
| `internal/engine/drift_test.go` | e, e, e, t, v, ... |
| `internal/engine/engine.go` | Engine, classifier, poolFetchedSum, embedProc, db, ... |
| `internal/engine/filter_test.go` | err, err, t, err, dir2, ... |
| `internal/engine/get_chunk.go` | chunkID, res, Chunk, ok, d, ... |
| `internal/engine/get_chunk_test.go` | id, t, q, id, e, ... |
| `internal/engine/health.go` | DriftVerdict, ctx, Health, storageOpen, OK, ... |
| `internal/engine/index_cache_test.go` | pair, err, err, res, cfg, ... |
| `internal/engine/ingest.go` | glob, span, glob, Add, err, ... |
| `internal/engine/poison_test.go` | h, res, err, err, t, ... |
| `internal/engine/wikilinks_test.go` | e2, b, body, s, q1, ... |
| `internal/observe/observe_test.go` | span, got, closure@96, err, tp, ... |
| `internal/observe/otel.go` | err, Init, ctx, e, exp, ... |
| `internal/observe/spans.go` | ctx, attrs, v, StartSpan, name, ... |
| `internal/pipeline/reprocess_test.go` | n, cleanup, r3, p, TestReprocess_BypassesDedup, ... |

## Entry Points

- `internal/engine/filter_test.go::TestQuery_Filter_SourceScopes`

## Connected Communities

- **engine +12 dirs** (55 cross-edges)
- **engine +19 dirs** (38 cross-edges)
- **engine +3 dirs** (20 cross-edges)
- **cli +13 dirs** (19 cross-edges)
- **cli +7 dirs** (9 cross-edges)
- **reader +8 dirs** (7 cross-edges)
- **audit +4 dirs** (7 cross-edges)
- **reader +19 dirs** (7 cross-edges)
- **daemon +15 dirs** (7 cross-edges)
- **daemon +5 dirs** (6 cross-edges)
- **engine +2 dirs · waitForEpoch** (6 cross-edges)
- **otel +1 dirs** (6 cross-edges)
- **pipeline +2 dirs** (5 cross-edges)
- **engine +7 dirs** (5 cross-edges)
- **engine +6 dirs** (4 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (4 cross-edges)
- **reader +7 dirs** (2 cross-edges)
- **pipeline +4 dirs** (2 cross-edges)
- **storage +2 dirs** (2 cross-edges)
- **audit** (2 cross-edges)
- **engine · checkEmbeddingMismatch** (2 cross-edges)
- **config +2 dirs · pipeline** (2 cross-edges)
- **engine +1 dirs · Watch** (1 cross-edges)
- **daemon +2 dirs** (1 cross-edges)
- **engine · Stats** (1 cross-edges)
- **grpc +2 dirs** (1 cross-edges)
- **engine · ollamaVersion** (1 cross-edges)
- **storage +1 dirs** (1 cross-edges)
- **engine · expandContext** (1 cross-edges)
- **rest +2 dirs** (1 cross-edges)
- **upgrade +1 dirs** (1 cross-edges)
- **eval +6 dirs** (1 cross-edges)
- **watcher +3 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-217"
smart_context with task: "understand engine +13 dirs", format: "gcx"
find_usages with id: "internal/engine/filter_test.go::TestQuery_Filter_SourceScopes", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
