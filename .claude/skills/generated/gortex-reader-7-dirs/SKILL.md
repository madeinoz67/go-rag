---
name: gortex-reader-7-dirs
description: "Work in the reader +7 dirs area — 232 symbols across 15 files (69% cohesion)"
---

# reader +7 dirs

232 symbols | 15 files | 69% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/engine/threat.go`
- `internal/enrich/ollama.go`
- `internal/enrich/ollama_test.go`
- `internal/index/filter.go`
- `internal/mcp/http.go`
- `internal/reader/docx.go`
- `internal/reader/docx_test.go`
- `internal/reader/docx_zip.go`
- `internal/reader/markdown.go`
- `internal/reader/markdown_test.go`
- `internal/reader/pdf.go`
- `internal/rerank/rerank.go`
- `internal/rerank/rerank_test.go`
- `internal/rest/server.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | TrimLeft, NewDecoder, Match, TrimSpace, ContainsAny, ... |
| `internal/engine/threat.go` | parsePhrases, out, out, s, out, ... |
| `internal/enrich/ollama.go` | t, out, in, t, normalizeTags, ... |
| `internal/enrich/ollama_test.go` | TestNormalizeTags, i, t, got, want |
| `internal/index/filter.go` | sourceMatches, pattern, err, filePath, matched |
| `internal/mcp/http.go` | checkBearer, v, r, token |
| `internal/reader/docx.go` | body, err, md, zr, extractDocxBody, ... |
| `internal/reader/docx_test.go` | t, Text, docxPara, want, TestDocxReader_HeadingSpans, ... |
| `internal/reader/docx_zip.go` | f, name, rc, readZipFile, zr, ... |
| `internal/reader/markdown.go` | stripped, inCode, closure@112, inner, inner, ... |
| `internal/reader/markdown_test.go` | t, spans, spans, in, out, ... |
| `internal/reader/pdf.go` | info, v, md, v, populatePDFMetadata, ... |
| `internal/rerank/rerank.go` | maxVal, i, parseScores, parts, n, ... |
| `internal/rerank/rerank_test.go` | TestParseScores_Fallback, s, TestParseScores_MaxNormalisation, scores, scores, ... |
| `internal/rest/server.go` | checkBearer, v, token, r |

## Connected Communities

- **engine +19 dirs** (36 cross-edges)
- **daemon +15 dirs** (4 cross-edges)
- **. +1 dirs · stripInlineEmphasis** (3 cross-edges)
- **engine +12 dirs** (3 cross-edges)
- **engine +13 dirs** (2 cross-edges)
- **reader +8 dirs** (2 cross-edges)
- **model +2 dirs** (1 cross-edges)
- **reader +19 dirs** (1 cross-edges)
- **engine +2 dirs · Get** (1 cross-edges)
- **. +1 dirs · zipReader** (1 cross-edges)
- **daemon +5 dirs** (1 cross-edges)
- **cli +7 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-234"
smart_context with task: "understand reader +7 dirs", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
