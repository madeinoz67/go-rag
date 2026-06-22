# Contract: `Engine.Query` (preserved under caching — H01)

> H01 introduces **no new external interface** and changes no transport surface.
> The query contract every caller (CLI, MCP, REST, gRPC) depends on is
> **unchanged** — only its latency profile on repeated calls improves. This file
> pins that invariant (US3 / FR-008).

## The preserved contract

`Engine.Query(ctx, QueryRequest) (*QueryResult, error)` MUST satisfy, for all
callers, before and after H01:

| Guarantee | Statement |
|-----------|-----------|
| Results | For identical underlying data, the returned ranked hits are identical (same chunk IDs, same order, same scores) to the pre-H01 per-query-rebuild path. |
| Freshness | Reflects all ingested, deleted, migrated, and watcher-changed data by the next query (read-after-write). |
| Latency | Repeated queries on unchanged data reuse the cached index — the Nth query is far faster than the 1st (the 1st pays the one-time seed). |
| Errors | Unchanged error semantics (embedding mismatch guard, invalid query, etc.). |
| Concurrency | Safe under concurrent queries + background writes (each sees a coherent index view). |

## What changes (internal only, invisible to callers)

- `Engine.Query` reads the Engine's shared `(FTS, Vector)` instead of calling
  `pipeline.LoadIndex(e.db)` per query.
- The Engine seeds that shared pair once (via `LoadIndex`) on first access.
- The ingest pipeline, watcher, and migrate mutate the shared pair in place
  (delete via the new `(*Pipeline).DeleteDoc` clears `fts`/`vec` too).

## Parity / regression anchors

- **Results parity**: every existing retrieval/parity/eval test (spec 003
  cross-transport parity; spec 004 eval harness) MUST pass unchanged — proving
  the cache changes latency, not results (FR-008). See [quickstart.md](../quickstart.md)
  scenario 1 (identical results) and scenario 2 (latency ratio).
- **Latency**: a measurable drop on the 2nd+ query is the positive signal
  (SC-001); no drop would mean the cache isn't being reused (a bug).
