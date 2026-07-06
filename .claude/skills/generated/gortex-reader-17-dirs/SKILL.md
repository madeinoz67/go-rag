---
name: gortex-reader-17-dirs
description: "Work in the reader +17 dirs area — 599 symbols across 55 files (55% cohesion)"
---

# reader +17 dirs

599 symbols | 55 files | 55% cohesion

## When to Use

Use this skill when working on files in:
- ``
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
| `` | http, Error, HasSuffix, Clean, NewReader, ... |
| `internal/caption/captioner.go` | endpoint, model, apiKey, New, provider |
| `internal/caption/circuit.go` | newBreaker |
| `internal/caption/ollama.go` | Model, client, br, baseURL, model, ... |
| `internal/caption/openai.go` | endpoint, NewOpenAI, apiKey, model |
| `internal/caption/openai_test.go` | t, TestCaption_Factory, ok, ok, ok, ... |
| `internal/cli/mcp.go` | addr, target, addr, out, cmd, ... |
| `internal/daemon/client.go` | tool, client, resp, CallTool, err, ... |
| `internal/daemon/lifecycle.go` | addr, MCPURL |
| `internal/daemon/pid.go` | err, dbPath, dbPath, b, ReadToken, ... |
| `internal/daemon/pid_test.go` | tok, t, TestReadTokenAbsent |
| `internal/daemon/process_windows.go` | isPebbleLockHeld, lockPath |
| `internal/embed/embedder.go` | endpoint, apiKey, New, provider, model |
| `internal/embed/openai.go` | endpoint, apiKey, model, NewOpenAI |
| `internal/engine/config.go` | val, err, path, key, err, ... |
| `internal/engine/engine.go` | Config |
| `internal/engine/helpers.go` | n, preview, s, s |
| `internal/engine/index_cache_test.go` | raw, ok, t, e, path, ... |
| `internal/engine/parity_test.go` | rerankFailingOllama, srv, closure@704, t |
| `internal/engine/query_transform_test.go` | suffix, appendingTransformer, query, Transform |
| `internal/enrich/circuit.go` | newBreaker |
| `internal/enrich/enricher.go` | New, model, apiKey, provider, Enricher, ... |
| `internal/enrich/ollama.go` | br, NewOllama, Model, baseURL, model, ... |
| `internal/enrich/openai.go` | model, apiKey, client, apiKey, endpoint, ... |
| `internal/eval/embedder.go` | flush, closure@80, text, cur, r, ... |
| `internal/index/classify.go` | query, hasComparative, tok |
| `internal/index/fts.go` | s, r, flush, isStopword, s, ... |
| `internal/index/retrieval_seam_test.go` | id, Add, vec |
| `internal/index/transform.go` | s, QueryTransformer, normalizeQuery, NormalizingTransformer, query, ... |
| `internal/index/transform_test.go` | err, TestNormalizeQuery_Idempotent, cases, nt, out, ... |
| `internal/mcp/http.go` | HTTPHandler, closure@20, token, mux, closure@24 |
| `internal/mcp/http_test.go` | t, ts, ts, err, resp, ... |
| `internal/mcp/server.go` | flagged, Server, key, val, res, ... |
| `internal/mcp/server_test.go` | t, t, ok, tool, err, ... |
| `internal/pipeline/caption_cache_test.go` | Caption |
| `internal/pipeline/enrich_test.go` | Enrich, fakeEnricher, Model |
| `internal/pipeline/pipeline.go` | ext, extType |
| `internal/pipeline/reprocess.go` | glob, root, root, ctx, closure@20, ... |
| `internal/pipeline/reprocess_test.go` | got, c, TestIsUnder, t |
| `internal/poison/heuristic.go` | norm, normalize, p, matched, text, ... |
| `internal/reader/image.go` | JPEGReader, Name, SupportedExtensions, SupportedMimeTypes |
| `internal/reader/markdown.go` | closure@268, substituteWikilinks, s |
| `internal/reader/pdf.go` | content, s, m, extractShowText, m |
| `internal/reader/pdf_test.go` | r, s, pdfEscapeString |
| `internal/reader/pdftable.go` | row, collapseSpaces, nCols, c, i, ... |
| `internal/reader/readers_test.go` | body, must, mustBuildDocx, w, err, ... |
| `internal/reader/text.go` | countLines, n, spans, TextReader, SupportedMimeTypes, ... |
| `internal/reader/text_test.go` | r, err, src, md, t, ... |
| `internal/rerank/openai.go` | NewOpenAIReranker, model, Model, endpoint, apiKey, ... |
| `internal/rerank/rerank.go` | OllamaReranker, url, apiKey, endpoint, client, ... |
| `internal/rest/openapi_test.go` | TestOpenAPI_Served, srv, resp, t, err, ... |
| `internal/upgrade/release.go` | body, body, goarch, version, parseChecksumForAsset, ... |
| `internal/upgrade/release_test.go` | TestReleaseAssetURL, t, got, t, body, ... |
| `internal/watcher/watcher.go` | glob, m, matchGlob, name |
| `proto/gen/gorag.pb.go` | GetMimeType, GetFileSize, ContentHash, ProtoMessage, EnrichmentStatus, ... |

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
- **grpc +8 dirs** (4 cross-edges)
- **audit +4 dirs** (3 cross-edges)
- **engine +10 dirs** (3 cross-edges)
- **engine +2 dirs · Get** (2 cross-edges)
- **pipeline +2 dirs** (2 cross-edges)
- **. +2 dirs · callTool** (2 cross-edges)
- **engine +1 dirs · ResetChunk** (2 cross-edges)
- **. +1 dirs · stripInlineEmphasis** (2 cross-edges)
- **engine +2 dirs · Score** (1 cross-edges)
- **embed/modelbundle +3 dirs** (1 cross-edges)
- **. +2 dirs · MigrateFromChunks** (1 cross-edges)
- **daemon +2 dirs** (1 cross-edges)
- **. +2 dirs · TestPDFReader_ThreePageTableCha…** (1 cross-edges)
- **. +3 dirs** (1 cross-edges)
- **pipeline +4 dirs** (1 cross-edges)
- **eval +6 dirs** (1 cross-edges)
- **storage +2 dirs** (1 cross-edges)
- **rest +2 dirs · TestVerifyChecksum** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-95"
smart_context with task: "understand reader +17 dirs", format: "gcx"
find_usages with id: "internal/cli/mcp.go::runMCPProxy", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
