---
name: gortex-eval-6-dirs
description: "Work in the eval +6 dirs area — 312 symbols across 14 files (68% cohesion)"
---

# eval +6 dirs

312 symbols | 14 files | 68% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/cli/config_cli.go`
- `internal/cli/eval.go`
- `internal/cli/migrate.go`
- `internal/config/config.go`
- `internal/engine/cache.go`
- `internal/engine/engine.go`
- `internal/eval/benchmark.go`
- `internal/eval/embedder.go`
- `internal/eval/embedder_test.go`
- `internal/eval/run.go`
- `internal/eval/run_test.go`
- `internal/mcp/server.go`
- `internal/reader/pdftext.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | FormatUint, strconv, Itoa, bool, StringsAreSorted, ... |
| `internal/cli/config_cli.go` | printConfig, ok, k, keys, v, ... |
| `internal/cli/eval.go` | cleanup, resp, cfg, ollamaReachable, useVault, ... |
| `internal/cli/migrate.go` | consistent, consistencyLabel |
| `internal/config/config.go` | EmbeddingPrefix, WatchDirs, RRFK, Config, CaptioningAPIKey, ... |
| `internal/engine/cache.go` | tags, FilterSource, write, EffPool, closure@192, ... |
| `internal/engine/engine.go` | cfg, newClassifier |
| `internal/eval/benchmark.go` | runner, err, corpusToChunks, cleanup, k, ... |
| `internal/eval/embedder.go` | DeterministicEmbedder, Model, Dimensions, NewDeterministicEmbedder |
| `internal/eval/embedder_test.go` | t, d, TestDeterministicEmbedder_MetaData |
| `internal/eval/run.go` | em, run, PerQueryResult, eng, eng, ... |
| `internal/eval/run_test.go` | file, db, cfg, err, em, ... |
| `internal/mcp/server.go` | mode, run, args, k, em, ... |
| `internal/reader/pdftext.go` | hexVal, c |

## Connected Communities

- **engine +19 dirs** (25 cross-edges)
- **engine +12 dirs** (22 cross-edges)
- **daemon +15 dirs** (12 cross-edges)
- **engine +13 dirs** (7 cross-edges)
- **eval +1 dirs** (5 cross-edges)
- **cli +13 dirs** (5 cross-edges)
- **engine +7 dirs** (4 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (2 cross-edges)
- **reader +8 dirs** (1 cross-edges)
- **. +1 dirs · repoGoldenAbs** (1 cross-edges)
- **. +1 dirs · fnv64** (1 cross-edges)
- **cli +7 dirs** (1 cross-edges)
- **engine +10 dirs** (1 cross-edges)
- **engine +3 dirs** (1 cross-edges)
- **engine +2 dirs · Get** (1 cross-edges)
- **reader +7 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-230"
smart_context with task: "understand eval +6 dirs", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
