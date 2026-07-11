# Data Model — Query View (Slice 2)

**Feature**: specs/048-ui-query-view | **Date**: 2026-07-11

Phase 1 output. The Query view introduces **no new persistent data** — it is a read-only
projection over `Engine.Query` (which reads existing Pebble prefixes). This document defines
the request/response DTOs the UI transport exposes and the one new route. Field names are
parallel to the REST contract (`internal/rest/engine_adapter.go`) so the two adapters stay
consistent without coupling (R4).

---

## Route table (new)

| Method | Path | Auth | Body / Query | Returns | Maps to |
|--------|------|------|--------------|---------|---------|
| POST | `/api/query` | `Server.guard` (spec 045 Bearer) | `queryRequestDTO` (JSON) | `queryResponseDTO` | `Engine.Query` in-process |

That is the only new route. Hit "detail" is **not** a second round-trip — the hit payload
already carries full text, sibling context, and provenance, so detail is a client-side render
of an already-fetched hit (R7 of spec 047 established the same client-side-detail pattern).

No changes to existing routes. No engine / REST / gRPC / MCP / CLI route changes.

---

## Request DTO — `queryRequestDTO`

Mirrors `engine.QueryRequest` (`internal/engine/types.go:15`) 1:1, with the filter expanded to
flat fields (matching how the REST `queryRequest` and CLI flags present it).

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `query` | string | — (required) | Natural-language query. Empty/whitespace → 400 (R11). |
| `k` | int | 5 | Top-K results. UI clamps to [1, 50] (R7). 0 = engine resolves (adaptive). |
| `mode` | string | `"hybrid"` | `hybrid` \| `semantic` \| `keyword`. |
| `no_rerank` | bool | false | Disable cross-encoder reranking for this query. |
| `threshold` | float64 | 0.0 | Minimum score; hits below are dropped. |
| `rrf_k` | int | 0 | RRF smoothing override (0 = config default 60). |
| `pool_size` | int | 0 | Reranker candidate-pool override (0 = config default / classifier). |
| `source` | string | `""` | Source-file glob filter. |
| `type` | string | `""` | File-type filter (e.g. `markdown`, `pdf`). |
| `tags` | []string | `nil` | Document-tag filter (conjunction / intersection). |
| `context_window` | int | 0 | N sibling chunks each side of a hit (0 = off). |
| `no_cache` | bool | false | Bypass the result cache for this call (R5). |
| `include_quarantined` | bool | false | Include injection-flagged chunks (default false = quarantine-by-default, R8). |
| `dedup` | bool | false | Collapse near-duplicate hits to one per group. |

`source` / `type` / `tags` are composed into an `engine.Filter` via `engine.NewFilter` (as the
REST adapter does). The three combine by intersection.

---

## Response DTO — `queryResponseDTO`

Mirrors `engine.QueryResult` (`internal/engine/types.go:111`).

| Field | Type | Notes |
|-------|------|-------|
| `hits` | []queryHitDTO | Ranked hits, top-K, trimmed by threshold. |
| `rerank_failed` | bool | True iff reranking was attempted and failed (hits valid, fallback/RRF order). |
| `effective_mode` | string | Mode actually used (explicit / default). |
| `effective_k` | int | K actually used (explicit / classifier-recommended / default). |
| `effective_pool` | int | Candidate pool actually used. |

---

## Hit DTO — `queryHitDTO`

Mirrors `engine.QueryHit` (`internal/engine/types.go:56`) and the REST `queryHit`
(`internal/rest/engine_adapter.go::toQueryHits`), adding `context` (sibling chunks, which the
REST projection omits but the UI detail view wants in one payload).

| Field | Type | Notes |
|-------|------|-------|
| `chunk_id` | string | Content-addressed chunk ID. |
| `document_id` | string | Parent document ID. |
| `score` | float64 | Fused relevance score (RRF, optionally reranked). Single value — no per-stage breakdown (R2). |
| `content` | string | Full chunk text. |
| `file_path` | string | Source file path / URL. |
| `page` | int | Page number when paginated (0 = not paginated). |
| `chunk_index` | int | 0-based ordinal within the source document. |
| `section_context` | []string | Heading breadcrumb (top-level → governing heading). |
| `section_depth` | int | Governing (leaf) heading level 1–6; 0 = no heading. |
| `poisoning` | *poisonVerdictDTO | Injection verdict; present when scored, nil when clean/unknown. |
| `near_dup` | *nearDupDTO | Near-duplicate siblings + closest similarity; nil when none. |
| `wikilinks` | []string | Obsidian wikilink targets. |
| `summary` | string | Document auto-summary (spec 029); empty when unenriched. |
| `enrichment_status` | string | `enriched` \| `failed` \| `nothing-to-enrich`; empty when unenriched. |
| `extraction_method` | string | PDF extraction method (spec 042); default `native`. |
| `extraction_quality` | float64 | Extraction confidence (spec 042); default 1.0. |
| `context` | []contextChunkDTO | Sibling chunks when `context_window > 0`; empty otherwise. |

### Sub-DTOs (read-only projections, field-parallel to existing model types)

**poisonVerdictDTO** — `level` (string), `score` (float64), `matched_phrases` ([]string),
`signals` {*repetition*, *stuffing*, *instruction* (float64)}.
(Mirrors `model.PoisonVerdict` / the REST `poisonVerdict` + `poisonSignals`.)

**nearDupDTO** — `siblings` ([]string), `similarity` (float64).
(Mirrors `model.NearDupInfo` / the REST `nearDupInfo`.)

**contextChunkDTO** — `chunk_index` (int), `content` (string), `is_before` (bool).
(Mirrors `engine.ContextChunk`; `is_before` distinguishes left/right sibling for rendering.)

---

## Validation rules (enforced at the UI handler)

- `query` empty/whitespace → 400 `empty query` (R11).
- `k` clamped to [1, 50] client-side; server passes through (engine bounds on `K`).
- `mode` validated against {`hybrid`, `semantic`, `keyword`}; unknown → 400 `invalid mode`
  (parity with engine behaviour).
- `tags` parsed as a conjunction; empty array = no tag filter.
- Engine errors (embedder unreachable, embedding mismatch, internal) → `writeEngineErr`
  (existing helper) → appropriate HTTP code + plain guidance in the response (R10).

---

## State transitions

None. The view is stateless beyond the per-query request/response. The only client-side state
is the form (controls + include-quarantined toggle, the latter resetting to false each query per
R8) and the rendered result set. No persistence, no session-side query history (spec OQ:
saved-query persistence is explicitly out of scope).
