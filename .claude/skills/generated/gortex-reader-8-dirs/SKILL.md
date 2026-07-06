---
name: gortex-reader-8-dirs
description: "Work in the reader +8 dirs area — 371 symbols across 18 files (70% cohesion)"
---

# reader +8 dirs

371 symbols | 18 files | 70% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `external-call::dep:github.com/pdfcpu/pdfcpu/pkg/api`
- `internal/audit/reader.go`
- `internal/cli/dashboard.go`
- `internal/cli/eval.go`
- `internal/embed/modelbundle/bundle.go`
- `internal/engine/drift.go`
- `internal/eval/render.go`
- `internal/mcp/server.go`
- `internal/mcp/server_test.go`
- `internal/reader/image_ref.go`
- `internal/reader/pdf.go`
- `internal/reader/pdf_bookmark_test.go`
- `internal/reader/pdf_continuation_test.go`
- `internal/reader/pdf_edge_test.go`
- `internal/reader/pdf_image_test.go`
- `internal/reader/pdf_table_test.go`
- `internal/reader/pdf_test.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | Fprintf, Contains, Sprintf, Encode, recover |
| `external-call::dep:github.com/pdfcpu/pdfcpu/pkg/api` | github.com/pdfcpu/pdfcpu/pkg/api |
| `internal/audit/reader.go` | events, RenderText, e, s, short |
| `internal/cli/dashboard.go` | b, humanBytes, div, exp, div, ... |
| `internal/cli/eval.go` | prefix, model, ds, err, home, ... |
| `internal/embed/modelbundle/bundle.go` | base, downloadURL |
| `internal/engine/drift.go` | Hard, Verdict, em, LiveConvention, Reasons, ... |
| `internal/eval/render.go` | cmp, FormatRun, target, writeMetric, tolerance, ... |
| `internal/mcp/server.go` | out, out, eng, out, c, ... |
| `internal/mcp/server_test.go` | Dimensions, Model, fakeEmbed |
| `internal/reader/image_ref.go` | Width, FileType, Bytes, ImageRef, Height, ... |
| `internal/reader/pdf.go` | err, spans, rawImgs, img, Name, ... |
| `internal/reader/pdf_bookmark_test.go` | ok, r, pdf, info, TestPDFReader_BookmarkHeadings, ... |
| `internal/reader/pdf_continuation_test.go` | objs, objs, closure@98, y, fontObj, ... |
| `internal/reader/pdf_edge_test.go` | md, in, inputs, content, objs, ... |
| `internal/reader/pdf_image_test.go` | offsets, jpegBytes, writeObj, t, x, ... |
| `internal/reader/pdf_table_test.go` | TestPDFReader_ProseNoTable, r, want, s, xref, ... |
| `internal/reader/pdf_test.go` | ok, pdfBytes, body, stream, got, ... |

## Entry Points

- `internal/mcp/server.go::Server.guide`

## Connected Communities

- **engine +19 dirs** (80 cross-edges)
- **engine +13 dirs** (14 cross-edges)
- **cli +7 dirs** (14 cross-edges)
- **daemon +5 dirs** (6 cross-edges)
- **engine +12 dirs** (6 cross-edges)
- **reader +19 dirs** (5 cross-edges)
- **daemon +15 dirs** (5 cross-edges)
- **engine +7 dirs** (4 cross-edges)
- **eval +6 dirs** (3 cross-edges)
- **. +1 dirs · mustBuildPNG** (2 cross-edges)
- **. +3 dirs** (2 cross-edges)
- **cli +13 dirs** (1 cross-edges)
- **upgrade +1 dirs** (1 cross-edges)
- **config +2 dirs · pipeline** (1 cross-edges)
- **engine · ollamaVersion** (1 cross-edges)
- **engine · checkEmbeddingMismatch** (1 cross-edges)
- **engine +2 dirs · TestMigratePlan_EstimateAndBrea…** (1 cross-edges)
- **reader +7 dirs** (1 cross-edges)
- **embed/modelbundle +3 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-221"
smart_context with task: "understand reader +8 dirs", format: "gcx"
find_usages with id: "internal/mcp/server.go::Server.guide", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
