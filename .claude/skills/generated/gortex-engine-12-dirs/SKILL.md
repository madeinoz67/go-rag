---
name: gortex-engine-12-dirs
description: "Work in the engine +12 dirs area — 977 symbols across 35 files (70% cohesion)"
---

# engine +12 dirs

977 symbols | 35 files | 70% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/chunk/chunk.go`
- `internal/config/config.go`
- `internal/engine/baseline_test.go`
- `internal/engine/cache.go`
- `internal/engine/cache_embed_test.go`
- `internal/engine/cache_result_test.go`
- `internal/engine/concurrency_test.go`
- `internal/engine/convention_guard_test.go`
- `internal/engine/drift_test.go`
- `internal/engine/engine.go`
- `internal/engine/engine_test.go`
- `internal/engine/index_cache_test.go`
- `internal/engine/mismatch_test.go`
- `internal/engine/neardup_e2e_test.go`
- `internal/engine/parity_test.go`
- `internal/engine/query_prefix_test.go`
- `internal/eval/run_test.go`
- `internal/grpc/server_test.go`
- `internal/index/fts.go`
- `internal/mcp/http_test.go`
- `internal/mcp/server_test.go`
- `internal/pipeline/enrich_fail_test.go`
- `internal/pipeline/enrich_test.go`
- `internal/pipeline/pipeline.go`
- `internal/pipeline/prefix_wiring_test.go`
- `internal/pipeline/workers.go`
- `internal/rest/get_chunk_test.go`
- `internal/rest/server.go`
- `internal/rest/server_test.go`
- `internal/rest/types.go`
- `internal/storage/db.go`
- `internal/storage/db_test.go`
- `internal/watcher/watcher_test.go`
- `proto/gen/gorag.pb.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | json, httptest, Get, panic, Marshal, ... |
| `internal/chunk/chunk.go` | MinTokens, overlap, overlap, Size, NewSplitter, ... |
| `internal/config/config.go` | Default |
| `internal/engine/baseline_test.go` | openTempDB, t, got, dataDir, got2, ... |
| `internal/engine/cache.go` | profileFP, embedCacheKey, text |
| `internal/engine/cache_embed_test.go` | newCacheEngineEmb, got, t, emb, TestEmbedCacheKey_DiffersByProfile, ... |
| `internal/engine/cache_result_test.go` | err, err, db, got, dataDir, ... |
| `internal/engine/concurrency_test.go` | docs, closure@59, i, ollama, eng, ... |
| `internal/engine/convention_guard_test.go` | eng, closure@70, eng, db, err, ... |
| `internal/engine/drift_test.go` | v, TestDrift_OfflineEmbedderSkipsVersion, t, err, e |
| `internal/engine/engine.go` | Close |
| `internal/engine/engine_test.go` | eng, TestEngine_SetConfig_Persists, res, dirs, t, ... |
| `internal/engine/index_cache_test.go` | err, addDoc, content, e, t, ... |
| `internal/engine/mismatch_test.go` | dataDir, err, err, em, st, ... |
| `internal/engine/neardup_e2e_test.go` | t, write, closure@40, err, err, ... |
| `internal/engine/parity_test.go` | restSrv, closure@95, addOverREST, resp, err, ... |
| `internal/engine/query_prefix_test.go` | err, snapshot, found, err, mu, ... |
| `internal/eval/run_test.go` | err, TestRunner_EndToEnd_CommittedGolden, bl, loaded, err, ... |
| `internal/grpc/server_test.go` | err, dp, t, cfg, err, ... |
| `internal/index/fts.go` | db, NewFTS |
| `internal/mcp/http_test.go` | resp, err, ts, err, TestHTTPToolsList, ... |
| `internal/mcp/server_test.go` | in, e, content, dbPath, closure@98, ... |
| `internal/pipeline/enrich_fail_test.go` | dp, closure@56, p, TestEnrich_PermanentFailureMarksFailed, t, ... |
| `internal/pipeline/enrich_test.go` | TestPipeline_NoEnricherIsNoop, err, t, dir, t, ... |
| `internal/pipeline/pipeline.go` | e, i, fts, em, New, ... |
| `internal/pipeline/prefix_wiring_test.go` | err, err, dir, body, err, ... |
| `internal/pipeline/workers.go` | j, worker |
| `internal/rest/get_chunk_test.go` | err, resp, qbody, eng, id, ... |
| `internal/rest/server.go` | h, r, token, h, eng, ... |
| `internal/rest/server_test.go` | srv, resp, closure@219, err, err, ... |
| `internal/rest/types.go` | EffectivePool, EffectiveK, Hits, EffectiveMode, queryResponse, ... |
| `internal/storage/db.go` | Pebble, path, err, db, Infof, ... |
| `internal/storage/db_test.go` | db1, dir, err, t, ok, ... |
| `internal/watcher/watcher_test.go` | dir, t, t, cd, newDetector, ... |
| `proto/gen/gorag.pb.go` | GetOk, GetConsistent, GetSkipped, GetDriftVerdict, GetTargetModel, ... |

## Entry Points

- `internal/rest/server_test.go::TestREST_MetricsEndpoint`
- `internal/engine/parity_test.go::TestCrossTransport_RRFK_Parity`
- `internal/engine/parity_test.go::TestCrossTransport_PoisoningParity`
- `internal/engine/parity_test.go::TestCrossTransport_NoCache_Parity`
- `internal/engine/parity_test.go::TestCrossTransport_EffectiveDepthPoolMode_Parity`

## Connected Communities

- **engine +13 dirs** (90 cross-edges)
- **engine +19 dirs** (82 cross-edges)
- **cli +13 dirs** (53 cross-edges)
- **engine +3 dirs** (36 cross-edges)
- **engine +7 dirs** (31 cross-edges)
- **cli +7 dirs** (16 cross-edges)
- **grpc +2 dirs** (15 cross-edges)
- **reader +19 dirs** (15 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (14 cross-edges)
- **pipeline +2 dirs** (14 cross-edges)
- **index +1 dirs · Search** (13 cross-edges)
- **pipeline +4 dirs** (12 cross-edges)
- **engine · Stats** (8 cross-edges)
- **grpc +8 dirs** (7 cross-edges)
- **daemon +15 dirs** (7 cross-edges)
- **eval +6 dirs** (7 cross-edges)
- **grpc +3 dirs** (6 cross-edges)
- **engine +6 dirs** (5 cross-edges)
- **engine +2 dirs · Get** (4 cross-edges)
- **. +2 dirs · MigrateFromChunks** (4 cross-edges)
- **reader +8 dirs** (4 cross-edges)
- **daemon +5 dirs** (3 cross-edges)
- **reader +7 dirs** (3 cross-edges)
- **engine +10 dirs** (3 cross-edges)
- **storage +1 dirs** (3 cross-edges)
- **engine +1 dirs · QueryHit** (3 cross-edges)
- **watcher +3 dirs** (2 cross-edges)
- **engine · ollamaVersion** (2 cross-edges)
- **audit +4 dirs** (2 cross-edges)
- **rest +2 dirs** (2 cross-edges)
- **engine +2 dirs · waitForEpoch** (2 cross-edges)
- **engine +1 dirs · Watch** (1 cross-edges)
- **storage +2 dirs** (1 cross-edges)
- **config +2 dirs · pipeline** (1 cross-edges)
- **storage/migrate +1 dirs · Run** (1 cross-edges)
- **embed · ForRole** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-91"
smart_context with task: "understand engine +12 dirs", format: "gcx"
find_usages with id: "internal/rest/server_test.go::TestREST_MetricsEndpoint", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
