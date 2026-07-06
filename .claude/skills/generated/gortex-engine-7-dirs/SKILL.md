---
name: gortex-engine-7-dirs
description: "Work in the engine +7 dirs area — 318 symbols across 25 files (61% cohesion)"
---

# engine +7 dirs

318 symbols | 25 files | 61% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/config/config.go`
- `internal/engine/baseline_test.go`
- `internal/engine/config.go`
- `internal/engine/embedding_profile.go`
- `internal/engine/helpers.go`
- `internal/engine/migrate_plan_test.go`
- `internal/engine/parity_test.go`
- `internal/engine/query.go`
- `internal/engine/reenrich.go`
- `internal/engine/reenrich_test.go`
- `internal/engine/status.go`
- `internal/engine/threat.go`
- `internal/engine/types.go`
- `internal/eval/benchmark.go`
- `internal/eval/run.go`
- `internal/model/model.go`
- `internal/model/model_test.go`
- `internal/model/neardup_test.go`
- `internal/pipeline/load.go`
- `internal/storage/db.go`
- `internal/storage/embedqueue.go`
- `internal/storage/near.go`
- `internal/storage/poison.go`
- `proto/gen/gorag.pb.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | TrimSuffix, Date, Unmarshal, HasPrefix, DeepEqual |
| `internal/config/config.go` | err, ok, path, data, ok, ... |
| `internal/engine/baseline_test.go` | data, ok, err, b, k, ... |
| `internal/engine/config.go` | db, out, err, n, docs, ... |
| `internal/engine/embedding_profile.go` | p, Convention, db, closure@53, Vector, ... |
| `internal/engine/helpers.go` | db, closure@48, closure@40, prefix, Open, ... |
| `internal/engine/migrate_plan_test.go` | after, err, t, before, cleanup, ... |
| `internal/engine/parity_test.go` | gc, rc, err, gs, client, ... |
| `internal/engine/query.go` | enrichment |
| `internal/engine/reenrich.go` | ctx, en, closure@49, ReEnrich, docText, ... |
| `internal/engine/reenrich_test.go` | err, sum, TestReEnrich_DisabledIsNoop, reFirstDoc, doc, ... |
| `internal/engine/status.go` | dims, dv, closure@85, embedPending, t, ... |
| `internal/engine/threat.go` | closure@62, closure@55, ListThreatSources, err |
| `internal/engine/types.go` | DimCounts, CorpusBaselineRecordedAt, ModelCounts, EmbeddingConventionDrift, Dimensions, ... |
| `internal/eval/benchmark.go` | closure@97, docToChunks, stem, closure@107, dID, ... |
| `internal/eval/run.go` | ID, closure@202, FilePath, s, ChunkRef, ... |
| `internal/model/model.go` | Tags, GeneratedAt, Model, EnrichInfo, Summary, ... |
| `internal/model/model_test.go` | TestChunk_SectionContext_PreFeatureShape, err, t, pre |
| `internal/model/neardup_test.go` | TestChunk_NearDup_PreFeatureShape, pre, err, t |
| `internal/pipeline/load.go` | fts, db, closure@45, EmbeddingModelStats, vec, ... |
| `internal/storage/db.go` | prefix, PrefixScanByte, fn, path, PrefixScan, ... |
| `internal/storage/embedqueue.go` | closure@52, fn, ScanEmbedQueue |
| `internal/storage/near.go` | closure@38, fn, ScanNearDup |
| `internal/storage/poison.go` | fn, closure@33, closure@25, ScanThreatSources, ScanQuarantine, ... |
| `proto/gen/gorag.pb.go` | GetDocuments, GetVaults, GetChunks, GetChunkCount, GetDirs, ... |

## Entry Points

- `internal/engine/parity_test.go::TestCrossTransport_FullSurfaceParity`

## Connected Communities

- **engine +19 dirs** (34 cross-edges)
- **engine +12 dirs** (28 cross-edges)
- **reader +19 dirs** (7 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (7 cross-edges)
- **engine +13 dirs** (5 cross-edges)
- **cli +13 dirs** (4 cross-edges)
- **daemon +15 dirs** (4 cross-edges)
- **engine +2 dirs · waitForEpoch** (3 cross-edges)
- **engine +10 dirs** (3 cross-edges)
- **index +1 dirs · Search** (3 cross-edges)
- **embed · ForRole** (2 cross-edges)
- **pipeline +4 dirs** (2 cross-edges)
- **engine +2 dirs · Get** (2 cross-edges)
- **engine · Stats** (2 cross-edges)
- **storage +1 dirs** (2 cross-edges)
- **reader +8 dirs** (2 cross-edges)
- **index +1 dirs · Delete** (1 cross-edges)
- **storage/migrate +1 dirs · TestRunMigrationsUsesExpectedVe…** (1 cross-edges)
- **pipeline +2 dirs** (1 cross-edges)
- **engine · checkEmbeddingMismatch** (1 cross-edges)
- **. +2 dirs · MigrateFromChunks** (1 cross-edges)
- **model +2 dirs** (1 cross-edges)
- **cli +7 dirs** (1 cross-edges)
- **config +2 dirs · ApplyEnvOverrides** (1 cross-edges)
- **enrich · TestBreaker_ProbeAfterReset** (1 cross-edges)
- **grpc +2 dirs** (1 cross-edges)
- **engine +6 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-88"
smart_context with task: "understand engine +7 dirs", format: "gcx"
find_usages with id: "internal/engine/parity_test.go::TestCrossTransport_FullSurfaceParity", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
