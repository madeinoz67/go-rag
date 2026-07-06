---
name: gortex-pipeline-2-dirs
description: "Work in the pipeline +2 dirs area — 164 symbols across 10 files (65% cohesion)"
---

# pipeline +2 dirs

164 symbols | 10 files | 65% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/pipeline/concurrent_test.go`
- `internal/pipeline/neardup_test.go`
- `internal/pipeline/pipeline.go`
- `internal/pipeline/pipeline_test.go`
- `internal/pipeline/progress_test.go`
- `internal/pipeline/reprocess.go`
- `internal/pipeline/reprocess_test.go`
- `internal/pipeline/section_test.go`
- `internal/reader/text.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | Base |
| `internal/pipeline/concurrent_test.go` | t, err, res, cleanup, p, ... |
| `internal/pipeline/neardup_test.go` | dir, got, p, err, err, ... |
| `internal/pipeline/pipeline.go` | glob, n, total, Errors, root, ... |
| `internal/pipeline/pipeline_test.go` | t, dir, t, TestIngest_UnsupportedExtensionSkipped, path, ... |
| `internal/pipeline/progress_test.go` | p, TestIngest_Progress, cleanup, res, dir, ... |
| `internal/pipeline/reprocess.go` | e, res, r, entry, saved, ... |
| `internal/pipeline/reprocess_test.go` | n, err, t, p, cleanup, ... |
| `internal/pipeline/section_test.go` | cleanup, closure@128, TestIngest_SectionContext_Attached, err, closure@219, ... |
| `internal/reader/text.go` | Name |

## Connected Communities

- **engine +12 dirs** (23 cross-edges)
- **cli +13 dirs** (22 cross-edges)
- **engine +13 dirs** (20 cross-edges)
- **engine +19 dirs** (12 cross-edges)
- **engine +7 dirs** (9 cross-edges)
- **reader +19 dirs** (6 cross-edges)
- **watcher +3 dirs** (5 cross-edges)
- **pipeline +4 dirs** (4 cross-edges)
- **engine +6 dirs** (2 cross-edges)
- **reader +8 dirs** (2 cross-edges)
- **pipeline** (1 cross-edges)
- **audit +4 dirs** (1 cross-edges)
- **model +2 dirs** (1 cross-edges)
- **engine +2 dirs · waitForEpoch** (1 cross-edges)
- **reader · TestRegistry_DispatchAndUnknown** (1 cross-edges)
- **storage +2 dirs** (1 cross-edges)
- **daemon +15 dirs** (1 cross-edges)
- **index +1 dirs · Search** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-71"
smart_context with task: "understand pipeline +2 dirs", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
