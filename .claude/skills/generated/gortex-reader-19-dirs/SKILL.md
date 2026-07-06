---
name: gortex-reader-19-dirs
description: "Work in the reader +19 dirs area — 617 symbols across 57 files (56% cohesion)"
---

# reader +19 dirs

617 symbols | 57 files | 56% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/audit/event.go`
- `internal/caption/captioner.go`
- `internal/caption/circuit.go`
- `internal/caption/ollama.go`
- `internal/caption/openai.go`
- `internal/caption/openai_test.go`
- `internal/cli/mcp.go`
- `internal/daemon/client.go`
- `internal/daemon/lifecycle.go`
- `internal/daemon/pid.go`
- `internal/daemon/pid_test.go`
- `internal/daemon/process_windows.go`
- `internal/embed/embedder.go`
- `internal/embed/openai.go`
- `internal/engine/config.go`
- `internal/engine/engine.go`
- `internal/engine/helpers.go`
- `internal/engine/index_cache_test.go`
- `internal/engine/parity_test.go`
- `internal/engine/query_transform_test.go`
- `internal/enrich/circuit.go`
- `internal/enrich/enricher.go`
- `internal/enrich/ollama.go`
- `internal/enrich/openai.go`
- `internal/eval/embedder.go`
- `internal/grpc/server.go`
- `internal/index/classify.go`
- `internal/index/fts.go`
- `internal/index/retrieval_seam_test.go`
- `internal/index/transform.go`
- `internal/index/transform_test.go`
- `internal/mcp/http.go`
- `internal/mcp/http_test.go`
- `internal/mcp/server.go`
- `internal/mcp/server_test.go`
- `internal/pipeline/caption_cache_test.go`
- `internal/pipeline/enrich_test.go`
- `internal/pipeline/pipeline.go`
- `internal/pipeline/reprocess.go`
- `internal/pipeline/reprocess_test.go`
- `internal/poison/heuristic.go`
- `internal/reader/image.go`
- `internal/reader/markdown.go`
- `internal/reader/pdf.go`
- `internal/reader/pdf_test.go`
- `internal/reader/pdftable.go`
- `internal/reader/readers_test.go`
- `internal/reader/text.go`
- `internal/reader/text_test.go`
- `internal/rerank/openai.go`
- `internal/rerank/rerank.go`
- `internal/rest/openapi_test.go`
- `internal/upgrade/release.go`
- `internal/upgrade/release_test.go`
- `internal/watcher/watcher.go`
- `proto/gen/gorag.pb.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | Error, ToLower, IsLetter, TrimSpace, HasSuffix, ... |
| `internal/audit/event.go` | detail, transport, AuthFailEvent |
| `internal/caption/captioner.go` | apiKey, model, endpoint, New, provider |
| `internal/caption/circuit.go` | newBreaker |
| `internal/caption/ollama.go` | NewOllama, Ollama, baseURL, model, Model, ... |
| `internal/caption/openai.go` | Model, apiKey, apiKey, model, endpoint, ... |
| `internal/caption/openai_test.go` | t, ok, TestCaption_Factory, ok, ok, ... |
| `internal/cli/mcp.go` | code, err, req, closure@25, sessionID, ... |
| `internal/daemon/client.go` | req, body, tool, err, err, ... |
| `internal/daemon/lifecycle.go` | addr, MCPURL |
| `internal/daemon/pid.go` | ReadToken, TokenPath, dbPath, err, b, ... |
| `internal/daemon/pid_test.go` | tok, t, TestReadTokenAbsent |
| `internal/daemon/process_windows.go` | isPebbleLockHeld, lockPath |
| `internal/embed/embedder.go` | apiKey, New, endpoint, model, provider |
| `internal/embed/openai.go` | apiKey, NewOpenAI, model, endpoint |
| `internal/engine/config.go` | val, path, err, SetConfig, err, ... |
| `internal/engine/engine.go` | Config |
| `internal/engine/helpers.go` | s, s, n, preview |
| `internal/engine/index_cache_test.go` | t, docIDForPath, path, ok, e, ... |
| `internal/engine/parity_test.go` | t, rerankFailingOllama, srv, closure@704 |
| `internal/engine/query_transform_test.go` | appendingTransformer, suffix, query, Transform |
| `internal/enrich/circuit.go` | newBreaker |
| `internal/enrich/enricher.go` | provider, model, New, Enricher, apiKey, ... |
| `internal/enrich/ollama.go` | br, model, Ollama, baseURL, Model, ... |
| `internal/enrich/openai.go` | model, apiKey, OpenAI, Model, NewOpenAI, ... |
| `internal/eval/embedder.go` | r, tokenize, closure@80, cur, text, ... |
| `internal/grpc/server.go` | closure@40, token, bearerInterceptor |
| `internal/index/classify.go` | tok, query, hasComparative |
| `internal/index/fts.go` | s, Tokenize, flush, s, isStopword, ... |
| `internal/index/retrieval_seam_test.go` | Add, vec, id |
| `internal/index/transform.go` | s, QueryTransformer, NormalizingTransformer, query, n, ... |
| `internal/index/transform_test.go` | TestNormalizingTransformer_Transform, c, got, twice, cases, ... |
| `internal/mcp/http.go` | closure@24, closure@20, HTTPHandler, mux, token |
| `internal/mcp/http_test.go` | resp, body, err, resp3, t, ... |
| `internal/mcp/server.go` | cfg, initTool, args, eng, err, ... |
| `internal/mcp/server_test.go` | m, mcpCall, TestMCP_ToolsList, err, tc, ... |
| `internal/pipeline/caption_cache_test.go` | Caption |
| `internal/pipeline/enrich_test.go` | Enrich, fakeEnricher, Model |
| `internal/pipeline/pipeline.go` | glob, name, ext, m, extType, ... |
| `internal/pipeline/reprocess.go` | root, path, path, isUnder, Reprocess, ... |
| `internal/pipeline/reprocess_test.go` | TestIsUnder, got, c, t |
| `internal/poison/heuristic.go` | norm, text, matched, instruction, normalize, ... |
| `internal/reader/image.go` | SupportedMimeTypes, Name, SupportedExtensions, JPEGReader |
| `internal/reader/markdown.go` | closure@268, substituteWikilinks, s |
| `internal/reader/pdf.go` | m, s, m, content, extractShowText |
| `internal/reader/pdf_test.go` | pdfEscapeString, r, s |
| `internal/reader/pdftable.go` | nCols, populated, c, closure@212, grid, ... |
| `internal/reader/readers_test.go` | t, err, body, w, must, ... |
| `internal/reader/text.go` | md, countLines, s, r, TextReader, ... |
| `internal/reader/text_test.go` | src, t, v, content, TestTextReader_NoHeadings, ... |
| `internal/rerank/openai.go` | client, model, model, OpenAIReranker, endpoint, ... |
| `internal/rerank/rerank.go` | New, apiKey, endpoint, model, OllamaReranker, ... |
| `internal/rest/openapi_test.go` | srv, resp, err, TestOpenAPI_Served, t, ... |
| `internal/upgrade/release.go` | fields, goarch, version, err, body, ... |
| `internal/upgrade/release_test.go` | t, t, got, got, want, ... |
| `internal/watcher/watcher.go` | m, matchGlob, glob, name |
| `proto/gen/gorag.pb.go` | sizeCache, SourcePath, GetTags, MimeType, GetChunkCount, ... |

## Entry Points

- `internal/cli/mcp.go::runMCPProxy`
- `internal/mcp/http_test.go::TestHTTPBearerAuth`

## Connected Communities

- **engine +19 dirs** (36 cross-edges)
- **engine +12 dirs** (29 cross-edges)
- **reader +8 dirs** (27 cross-edges)
- **reader +7 dirs** (25 cross-edges)
- **daemon +15 dirs** (13 cross-edges)
- **cli +7 dirs** (12 cross-edges)
- **engine +7 dirs** (9 cross-edges)
- **cli +13 dirs** (8 cross-edges)
- **engine +13 dirs** (8 cross-edges)
- **daemon +5 dirs** (5 cross-edges)
- **grpc +8 dirs** (5 cross-edges)
- **audit +4 dirs** (4 cross-edges)
- **engine +2 dirs · Get** (3 cross-edges)
- **engine +10 dirs** (3 cross-edges)
- **engine +1 dirs · ResetChunk** (2 cross-edges)
- **. +2 dirs · callTool** (2 cross-edges)
- **. +1 dirs · stripInlineEmphasis** (2 cross-edges)
- **rest +4 dirs** (1 cross-edges)
- **embed/modelbundle +3 dirs** (1 cross-edges)
- **. +2 dirs · MigrateFromChunks** (1 cross-edges)
- **engine +2 dirs · Score** (1 cross-edges)
- **storage +2 dirs** (1 cross-edges)
- **eval +6 dirs** (1 cross-edges)
- **engine +2 dirs · waitForEpoch** (1 cross-edges)
- **daemon +2 dirs** (1 cross-edges)
- **pipeline +4 dirs** (1 cross-edges)
- **. +3 dirs** (1 cross-edges)
- **pipeline +2 dirs** (1 cross-edges)
- **. +2 dirs · TestPDFReader_ThreePageTableCha…** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-96"
smart_context with task: "understand reader +19 dirs", format: "gcx"
find_usages with id: "internal/cli/mcp.go::runMCPProxy", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
