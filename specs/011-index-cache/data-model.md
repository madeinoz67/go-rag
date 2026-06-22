# Data Model: Cached Loaded Index (H01)

> No storage entities change — this is purely an in-memory caching change. The
> chunk (0x03) and embedding (0x04) records, identity hashes, and key-space
> prefixes are untouched (Constitution Principle II). This file describes the
> **in-memory entity** introduced and the **lifecycle** that keeps it correct.

## Entities

### Index cache (in-memory, per-Engine process)

The shared full-text + vector index pair, held by the Engine and reused across
queries. Built once from the persisted corpus, kept current by every write path.

| Aspect | Value |
|--------|-------|
| Holds | one `*index.FTS` + one `*index.Vector` (the existing goroutine-safe types) |
| Seeded | once, lazily, by `LoadIndex` (full scan of chunks + embeddings) on first access |
| Lifetime | the Engine process; rebuilt from scratch on cold start (per-process; no persistence — H16) |
| Readers | `Engine.Query` (RRF fusion + rerank read `fts`/`vec`) |
| Writers | `processJob` (ingest/watcher re-ingest: `fts.Index` + `vec.Add`); `(*Pipeline).DeleteDoc` (`fts.Delete` + `vec.Delete`); migrate via reprocess |
| Concurrency | safe — `FTS` and `Vector` are individually mutex-protected; the seed-once is guarded by `idxMu`; shared pointers are stable once set |

### Index generation (conceptual — NOT a persisted counter)

A monotonic "version of the indexed data." With the **live-index** design chosen
here, it is implicit (the index always reflects the current data) rather than an
explicit counter. It exists as a *concept* to describe freshness: the cache is
fresh by construction because writes mutate it in place. (An explicit counter is
the rejected snapshot design — see research.md §1.)

## Lifecycle / state transitions

```text
                    ┌─── first Query ───┐
 Engine created ───►│  (or first write) │──► e.indexes() seeds via LoadIndex ──► SHARED (fts, vec)
                    └───────────────────┘            (idxMu, once)                  │
                                                                                    │
   ┌──────────────────────────────────────────────────────────────────────────────┘
   │   (stable shared pointers; both pipeline and query use them)
   ▼
 ┌─────────────────────────────────┐    ┌──────────────────────────────────────┐
 │ processJob (ingest / watcher)   │    │ (*Pipeline).DeleteDoc (migrate/watcher)│
 │   fts.Index(c.ID, …)            │    │   fts.Delete(cid)  +  vec.Delete(cid)  │
 │   vec.Add(c.ID, vec)            │    │   for each deleted chunk               │
 │   (mutex-safe, concurrent)      │    │   (mutex-safe)                         │
 └─────────────────────────────────┘    └────────────────────────────────────────┘
                 │                                       │
                 └─────────────► live, always-current shared index ◄─────────────┘
                                          │
                                          ▼
                              Engine.Query reads (no LoadIndex)
```

## Validation rules (from requirements)

- **FR-001**: the index is built at most once per process per cold start, reused across queries.
- **FR-002**: every index-affecting path (ingest `processJob`, `DeleteDoc`, migrate/reprocess, watcher) updates the shared index.
- **FR-003**: read-after-write — ingested docs appear, deleted docs disappear, on the next query.
- **FR-004**: no snapshot is pinned; the live index reflects embedding completion naturally.
- **FR-005/006**: per-index mutexes make concurrent reads safe; the seed is once (no thundering herd).
- **FR-007**: writes are incremental (`Index`/`Add`/`Delete`), not full rebuilds.
- **FR-008**: identical data → identical results to today's per-query rebuild.

No persisted state machine; no migration; no new key-space.
