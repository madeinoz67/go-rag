# Contract: UI Query Transport (`POST /api/query`)

**Feature**: specs/048-ui-query-view | **Date**: 2026-07-11

The HTTP contract for the Query view's single backend call. This is a UI-transport route
(`127.0.0.1:7881`, the spec 046 loopback 4th transport), guarded by the spec 045 Bearer session
via `Server.guard` — the same guard every other `/api/*` route uses. It is field-parallel to the
REST `POST /v1/query` contract (`internal/rest/engine_adapter.go`) but is a distinct adapter
that calls `Engine.Query` in-process; it does not proxy to REST.

Field-level definitions live in [data-model.md](../data-model.md). This contract fixes the wire
shape.

---

## `POST /api/query`

Run hybrid/semantic/keyword retrieval over the active vault.

**Auth**: `Authorization: Bearer <gorags_ session token>` (spec 045). Missing/invalid → 401,
shell routes to login.

**Request body** — `application/json`:

```json
{
  "query": "how is the battery charge deadline computed?",
  "k": 5,
  "mode": "hybrid",
  "no_rerank": false,
  "threshold": 0.0,
  "rrf_k": 0,
  "pool_size": 0,
  "source": "",
  "type": "",
  "tags": ["solar", "tariff"],
  "context_window": 1,
  "no_cache": false,
  "include_quarantined": false,
  "dedup": false
}
```

All fields optional except `query`. Defaults: `k=5`, `mode="hybrid"`, all flags false, all
filters empty. `tags` is a conjunction (intersection). `include_quarantined` defaults false
(quarantine-by-default).

**Response 200** — `application/json`:

```json
{
  "hits": [
    {
      "chunk_id": "c3a1...",
      "document_id": "d9f0...",
      "score": 0.873,
      "content": "The charge controller computes the deficit every 5 minutes...",
      "file_path": "docs/bess-design.md",
      "page": 0,
      "chunk_index": 14,
      "section_context": ["Operations", "Charge Control", "Deficit Calculator"],
      "section_depth": 3,
      "poisoning": null,
      "near_dup": { "siblings": ["c3a2..."], "similarity": 0.91 },
      "wikilinks": ["[[tariff]]"],
      "summary": "BESS charge-deadline controller design notes.",
      "enrichment_status": "enriched",
      "extraction_method": "native",
      "extraction_quality": 1.0,
      "context": [
        { "chunk_index": 13, "content": "...", "is_before": true }
      ]
    }
  ],
  "rerank_failed": false,
  "effective_mode": "hybrid",
  "effective_k": 5,
  "effective_pool": 60
}
```

`hits` is at most `k`, trimmed by `threshold`. `score` is the single fused value (no per-stage
breakdown — see spec OQ1 / research R2). `context` is non-empty only when `context_window > 0`.

**Response 400** — `{"error": "empty query"}` (empty/whitespace `query`), or
`{"error": "invalid mode"}` (unknown `mode`).

**Response 401** — `{"error": "unauthorized"}` (missing/invalid/expired session).

**Response 503 / 400 (engine error)** — via `writeEngineErr`:
- Embedder unreachable (semantic/vector mode, local Ollama down) → 503
  `{"error": "embedder unavailable", "detail": "..."}`; client suggests keyword mode (R10).
- Embedding-dimension mismatch (query model ≠ corpus model) → 400
  `{"error": "embedding mismatch", "detail": "..."}`; client suggests re-embed / switch model.
- Rerank failure is **not** an HTTP error — `rerank_failed: true` on a 200 response; hits are
  valid but in fallback (RRF) order.

---

## Non-goals of this contract

- No streaming / SSE — a query returns one JSON response (the engine's retrieval is
  request/response; streaming is out of scope for this slice).
- No batch query endpoint — one query per request.
- No saved-query / query-history persistence (out of scope per spec).
- No separate "hit detail" route — the hit payload already carries full text + context +
  provenance, so detail is a client-side render.
