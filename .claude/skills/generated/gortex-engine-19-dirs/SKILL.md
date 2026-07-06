---
name: gortex-engine-19-dirs
description: "Work in the engine +19 dirs area — 1268 symbols across 64 files (70% cohesion)"
---

# engine +19 dirs

1268 symbols | 64 files | 70% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/audit/reader.go`
- `internal/caption/openai.go`
- `internal/chunk/chunk.go`
- `internal/chunk/chunk_test.go`
- `internal/chunk/sentences.go`
- `internal/cli/commands_test.go`
- `internal/daemon/bind.go`
- `internal/embed/hugot.go`
- `internal/embed/hugot_offline_test.go`
- `internal/embed/hugot_test.go`
- `internal/embed/ollama.go`
- `internal/embed/ollama_test.go`
- `internal/embed/openai.go`
- `internal/embedproc/processor_test.go`
- `internal/engine/cache_epoch_test.go`
- `internal/engine/embedding_profile.go`
- `internal/engine/engine_test.go`
- `internal/engine/enrich_test.go`
- `internal/engine/get_chunk_context.go`
- `internal/engine/get_chunk_context_test.go`
- `internal/engine/index_cache_test.go`
- `internal/engine/migrate_plan.go`
- `internal/engine/mismatch_test.go`
- `internal/engine/neardup_e2e_test.go`
- `internal/engine/parity_test.go`
- `internal/engine/poison.go`
- `internal/engine/poison_test.go`
- `internal/engine/query.go`
- `internal/engine/query_prefix_test.go`
- `internal/engine/status.go`
- `internal/engine/threat.go`
- `internal/engine/threat_test.go`
- `internal/engine/types.go`
- `internal/engine/wikilinks_test.go`
- `internal/eval/dataset_test.go`
- `internal/eval/embedder.go`
- `internal/eval/embedder_test.go`
- `internal/grpc/server_test.go`
- `internal/index/fts.go`
- `internal/index/retrieval.go`
- `internal/index/retrieval_seam_test.go`
- `internal/index/retrieval_test.go`
- `internal/index/vector.go`
- `internal/mcp/server.go`
- `internal/mcp/server_test.go`
- `internal/pipeline/enrich_test.go`
- `internal/pipeline/pipeline_test.go`
- `internal/pipeline/prefix_wiring_test.go`
- `internal/poison/heuristic.go`
- `internal/reader/markdown.go`
- `internal/reader/markdown_test.go`
- `internal/reader/pdf.go`
- `internal/reader/pdf_continuation_test.go`
- `internal/reader/pdf_image_test.go`
- `internal/reader/pdftable.go`
- `internal/reader/pdftext.go`
- `internal/reader/pdftext_test.go`
- `internal/reader/text.go`
- `internal/reader/text_test.go`
- `internal/redact/redact.go`
- `internal/rerank/rerank_test.go`
- `internal/rest/server_test.go`
- `internal/watcher/watcher_test.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | LoadInt32, atomic, Log, SliceStable, Ints, ... |
| `internal/audit/reader.go` | e, events, a, all, out, ... |
| `internal/caption/openai.go` | b, imageMIME |
| `internal/chunk/chunk.go` | half, end, b, s, text, ... |
| `internal/chunk/chunk_test.go` | para2, s, segs, s, s, ... |
| `internal/chunk/sentences.go` | r, r, word, sentenceEnd, endsSentence |
| `internal/cli/commands_test.go` | indexOf, i, sub, s |
| `internal/daemon/bind.go` | out, out, addrs, enabledBinds, out |
| `internal/embed/hugot.go` | NewHugot, Dimensions |
| `internal/embed/hugot_offline_test.go` | t, err, TestHugotEmbedder_AbsentModelDoesNotFetch, e |
| `internal/embed/hugot_test.go` | err, err, ctx, q, err, ... |
| `internal/embed/ollama.go` | Embeddings, NewOllama, end, out, Dimensions, ... |
| `internal/embed/ollama_test.go` | want, t, srv, srv, vecs70, ... |
| `internal/embed/openai.go` | resp, err, err, i, req, ... |
| `internal/embedproc/processor_test.go` | out, Embed, i, texts |
| `internal/engine/cache_epoch_test.go` | t, err, err, r1, e, ... |
| `internal/engine/embedding_profile.go` | k, majorityInt, keys, k, bestN, ... |
| `internal/engine/engine_test.go` | v, eng, vaults, names, err, ... |
| `internal/engine/enrich_test.go` | TestTagsForDoc_Bridge, t, got, manual, auto, ... |
| `internal/engine/get_chunk_context.go` | chunks, ok, i, raw, cur, ... |
| `internal/engine/get_chunk_context_test.go` | chunkIDs, c, err, res, want, ... |
| `internal/engine/index_cache_test.go` | out, Embed, err, t, err, ... |
| `internal/engine/migrate_plan.go` | dims, models, d, plan, models, ... |
| `internal/engine/mismatch_test.go` | texts, i, Embed, out, v |
| `internal/engine/neardup_e2e_test.go` | q, without, TestNearDup_Collapse_E2E, err, eng, ... |
| `internal/engine/parity_test.go` | out, h, i, t, err, ... |
| `internal/engine/poison.go` | err, closure@35, ListPoisoned |
| `internal/engine/poison_test.go` | res, res, res, err, rescored, ... |
| `internal/engine/query.go` | keys, s, m, tags, tagsForDoc, ... |
| `internal/engine/query_prefix_test.go` | i, out, texts, Embed |
| `internal/engine/status.go` | m, poolUtilization, Dirs, u, d, ... |
| `internal/engine/threat.go` | p, s, seen, in, p, ... |
| `internal/engine/threat_test.go` | e, f, res, TestThreat_AddPhrases_TriggersRescan, t, ... |
| `internal/engine/types.go` | Files, Dir, DirEntry, Chunks |
| `internal/engine/wikilinks_test.go` | err, q, err, res, e, ... |
| `internal/eval/dataset_test.go` | err, p, TestLoadGolden_Valid, gs, t |
| `internal/eval/embedder.go` | sum, tok, x, vectorize, inv, ... |
| `internal/eval/embedder_test.go` | t, sum, b, err, d, ... |
| `internal/grpc/server_test.go` | out, Embed, i, texts |
| `internal/index/fts.go` | chunkIDFromKey, df, n, term, err, ... |
| `internal/index/retrieval.go` | texts, out, query, scored, h, ... |
| `internal/index/retrieval_seam_test.go` | Query, k, i, j, all, ... |
| `internal/index/retrieval_test.go` | Score, n, candidates, i, out |
| `internal/index/vector.go` | bi, a, n, cosine, na, ... |
| `internal/mcp/server.go` | err, renderFiles, lines, eng, files, ... |
| `internal/mcp/server_test.go` | texts, Embed, i, out |
| `internal/pipeline/enrich_test.go` | i, texts, out, Embed |
| `internal/pipeline/pipeline_test.go` | i, texts, Embed, out |
| `internal/pipeline/prefix_wiring_test.go` | i, texts, out, Embed |
| `internal/poison/heuristic.go` | c, norm, c, t, tokens, ... |
| `internal/reader/markdown.go` | Level, Text, HeadingSpan, Offset |
| `internal/reader/markdown_test.go` | spans, TestStripMarkdownSpans_CodeFenceHashExcluded, sp, in, t |
| `internal/reader/pdf.go` | pageText, closure@216, continuesTable, isHeadingText, f, ... |
| `internal/reader/pdf_continuation_test.go` | TestDetectTablesStructured_ProveEqual, t, prose, cands, i, ... |
| `internal/reader/pdf_image_test.go` | md, TestPDFReader_ExtractsEmbeddedImage, b, t, r, ... |
| `internal/reader/pdftable.go` | MedFS, best, i, sizes, detectTablesStructured, ... |
| `internal/reader/pdftext.go` | ty, tlm, tm, tlm, FontSize, ... |
| `internal/reader/pdftext_test.go` | t, frags, stream, f, w, ... |
| `internal/reader/text.go` | spans, i, i, ln, s, ... |
| `internal/reader/text_test.go` | content, i, sp, TestTextReader_HeadingSpans, md, ... |
| `internal/redact/redact.go` | i, f, sum, c, s, ... |
| `internal/rerank/rerank_test.go` | scores, closure@12, rr, TestReranker_Score, srv, ... |
| `internal/rest/server_test.go` | texts, i, Embed, out |
| `internal/watcher/watcher_test.go` | i, Model, fakeEmbed, texts, out, ... |

## Entry Points

- `internal/embed/openai.go::OpenAI.Embed`
- `internal/index/fts.go::FTS.Search`
- `internal/engine/poison_test.go::TestPoison_ManagementSurface`
- `internal/engine/parity_test.go::TestCrossTransport_SectionContextParity`
- `internal/engine/get_chunk_context_test.go::TestGetChunkContext_WindowMatchesLinkedList`

## Connected Communities

- **engine +12 dirs** (62 cross-edges)
- **engine +13 dirs** (45 cross-edges)
- **reader +19 dirs** (17 cross-edges)
- **engine +3 dirs** (16 cross-edges)
- **cli +7 dirs** (13 cross-edges)
- **daemon +15 dirs** (11 cross-edges)
- **reader +7 dirs** (10 cross-edges)
- **engine +7 dirs** (9 cross-edges)
- **reader +8 dirs** (6 cross-edges)
- **engine +10 dirs** (4 cross-edges)
- **daemon +5 dirs** (4 cross-edges)
- **engine · expandContext** (4 cross-edges)
- **eval · TestDeterministicEmbedder_RoleA…** (3 cross-edges)
- **cli +13 dirs** (2 cross-edges)
- **engine · Stats** (2 cross-edges)
- **engine +1 dirs · ResetChunk** (2 cross-edges)
- **engine +2 dirs · Score** (2 cross-edges)
- **index +1 dirs · Delete** (2 cross-edges)
- **grpc +2 dirs** (2 cross-edges)
- **embed/modelbundle +3 dirs** (2 cross-edges)
- **eval +6 dirs** (2 cross-edges)
- **reader · tableStream** (1 cross-edges)
- **. +3 dirs** (1 cross-edges)
- **. +1 dirs · isAllCapsHeading** (1 cross-edges)
- **index +2 dirs** (1 cross-edges)
- **index +1 dirs · Search** (1 cross-edges)
- **. +2 dirs · TestPDFReader_ThreePageTableCha…** (1 cross-edges)
- **engine +1 dirs · QueryHit** (1 cross-edges)
- **pipeline +4 dirs** (1 cross-edges)
- **config +2 dirs · pipeline** (1 cross-edges)
- **. +1 dirs · fnv64** (1 cross-edges)
- **engine · ImportThreatSource** (1 cross-edges)
- **engine +6 dirs** (1 cross-edges)
- **reader · tokenize** (1 cross-edges)
- **eval · writeTemp** (1 cross-edges)
- **grpc +3 dirs** (1 cross-edges)
- **engine +2 dirs · waitForEpoch** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-75"
smart_context with task: "understand engine +19 dirs", format: "gcx"
find_usages with id: "internal/embed/openai.go::OpenAI.Embed", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
