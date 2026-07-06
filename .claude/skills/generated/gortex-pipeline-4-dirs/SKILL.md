---
name: gortex-pipeline-4-dirs
description: "Work in the pipeline +4 dirs area — 211 symbols across 11 files (67% cohesion)"
---

# pipeline +4 dirs

211 symbols | 11 files | 67% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/caption/captioner.go`
- `internal/model/model.go`
- `internal/pipeline/caption_cache_test.go`
- `internal/pipeline/caption_section_test.go`
- `internal/pipeline/caption_test.go`
- `internal/pipeline/migrate_test.go`
- `internal/pipeline/pipeline.go`
- `internal/pipeline/workers.go`
- `internal/storage/db.go`
- `internal/storage/near.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | Warn |
| `internal/caption/captioner.go` | Captioner |
| `internal/model/model.go` | ImagePages, GeneratedAt, Model, Status, CaptionInfo |
| `internal/pipeline/caption_cache_test.go` | model, t, closure@62, docID, p, ... |
| `internal/pipeline/caption_section_test.go` | docID, t, spans, TestPipeline_CaptionImages_SectionContext, db, ... |
| `internal/pipeline/caption_test.go` | oc, TestPipeline_CaptionImages, found, hits, oc, ... |
| `internal/pipeline/migrate_test.go` | TestEmbeddingModelRecorded, n, dir, p, t, ... |
| `internal/pipeline/pipeline.go` | redactor, db, wg, Pipeline, captioner, ... |
| `internal/pipeline/workers.go` | docID, best, img, raw, err, ... |
| `internal/storage/db.go` | prefix, key, GetWithPrefix |
| `internal/storage/near.go` | val, GetNearDup, chunkID, ok |

## Connected Communities

- **engine +12 dirs** (24 cross-edges)
- **engine +7 dirs** (19 cross-edges)
- **engine +19 dirs** (18 cross-edges)
- **storage +1 dirs** (10 cross-edges)
- **engine +13 dirs** (4 cross-edges)
- **reader +8 dirs** (3 cross-edges)
- **engine +2 dirs · waitForEpoch** (3 cross-edges)
- **engine +6 dirs** (3 cross-edges)
- **index +1 dirs · Search** (3 cross-edges)
- **pipeline +2 dirs** (2 cross-edges)
- **rest +4 dirs** (2 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (2 cross-edges)
- **near +1 dirs** (2 cross-edges)
- **daemon +5 dirs** (2 cross-edges)
- **cli +13 dirs** (2 cross-edges)
- **pipeline** (1 cross-edges)
- **caption · TestCaption_OpenAI_EmptyImage** (1 cross-edges)
- **model +2 dirs** (1 cross-edges)
- **caption · TestCaption_OpenAI_PermanentErr…** (1 cross-edges)
- **cli +7 dirs** (1 cross-edges)
- **near** (1 cross-edges)
- **reader +7 dirs** (1 cross-edges)
- **engine +1 dirs · ResetChunk** (1 cross-edges)
- **index +2 dirs** (1 cross-edges)
- **engine +2 dirs · Score** (1 cross-edges)
- **index +1 dirs · Delete** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-70"
smart_context with task: "understand pipeline +4 dirs", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
