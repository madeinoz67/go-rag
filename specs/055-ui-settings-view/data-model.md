# Data Model — Settings View (Slice 0, spec 055)

> No new persisted entity. Slice 0 is a read-only projection of state the engine
> already holds. This file defines the single transfer object the new endpoint
> returns and the existing symbols it is derived from.

## Source symbols (read-only reuse)

| Source | Location | Provides |
|---|---|---|
| `Engine.Status(vault) *StatusInfo` | internal/engine/status.go:34 | embedding model/dim/convention, resolved prefixes, cache stats, pool, reranker |
| `Config` | internal/config/config.go:19 | rrf_k, chunk size/overlap, cache caps/kill-switch, pool, redaction, rerank, near-dup |
| `redact.DefaultPatterns(path)` | internal/redact/patterns.go:68 | active redaction pattern count |

## Transfer object: `SettingsDTO`

Grouped, read-only JSON projected by `Server.handleSettings`. All values are
**effective** (defaults applied where unset).

```
SettingsDTO {
  vault: string                          // active vault (X-Go-Rag-Vault / default)

  retrieval {
    rrf_k:                  int          // EffectiveRRFK() (default 60)
    pool_size:              int          // EffectivePoolSize() (default 60)
    reranker:               string       // StatusInfo.Reranker ("disabled" if unset)
    rerank_candidates:      int          // effective (default when 0)
    rerank_retry_on_failure: bool
    adaptive_depth_enabled: bool
    near_dup_hamming:       int          // effective (default 3)
  }

  embeddings {
    model:                 string        // StatusInfo.EmbeddingModel (cfg or corpus majority)
    dimensions:            int           // StatusInfo.Dimensions (0 if none)
    prefix_mode:           string        // StatusInfo.ConfiguredPrefix (auto|on|off)
    resolved_query_prefix: string        // StatusInfo.QueryPrefix
    resolved_doc_prefix:   string        // StatusInfo.DocPrefix
    stored_convention:     string        // StatusInfo.EmbeddingConvention
    ollama_url:            string        // StatusInfo.OllamaURL
  }

  cache {
    enabled: bool                         // QueryCacheEnabled effective (global kill-switch)
    result:    CacheStats                // StatusInfo.ResultCache
    embedding: CacheStats                // StatusInfo.EmbeddingCache
  }

  chunking {
    chunk_size:      int                 // Config.ChunkSize
    chunk_overlap:   int                 // Config.ChunkOverlap
    boundary_mode:   string              // fixed: "paragraph-sentence-word" (spec 013)
    section_context: bool                // always true (spec 025)
  }

  redaction {
    enabled:              bool           // Config.PIIRedactEnabled
    pattern_count:        int            // len(redact.DefaultPatterns(cfg.PIIPatterns)) when enabled, else 0
    custom_patterns_path: string         // Config.PIIPatterns ("" when none)
  }
}
```

`CacheStats` is the existing type (`{Enabled, Size, Capacity, Hits, Misses}`).

## Validation rules (carried from spec)

- Every field reflects the running daemon's effective value (defaults where unset) — FR-007.
- The endpoint is read-only — the handler performs no mutation — FR-006.
- Degraded states render as values, not errors: cache disabled ⇒ `enabled=false` + zeroed stats; embedder unreachable ⇒ dimensions/convention as reported by `Status` (no live probe beyond what `Status` already does) — FR-009.
- Query default depth/mode/threshold are deliberately absent (research R2): not configurable, surfaced per-query by view 048.
