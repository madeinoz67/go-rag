# Implementation Plan: Cached Loaded Index — No Per-Query Rebuild (H01)

**Branch**: `main` (single-author repo; Spec Kit work commits directly to `main`) | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/011-index-cache/spec.md` (audit backlog item **H01**, P0).

## Summary

Stop rebuilding the in-memory search index from disk on every query. Today `Engine.Query` calls `pipeline.LoadIndex(e.db)` per call — a full scan of every chunk (0x03) and embedding (0x04), re-parsing JSON and re-tokenizing, repeated identically for back-to-back queries over unchanged data. The fix: the Engine owns **one shared `(FTS, Vector)` pair**, seeded once from `LoadIndex` (the full historical corpus) on first access and reused by every query thereafter; the ingest pipeline, the watcher, and migrate all mutate that same shared pair so it stays current. `FTS` and `Vector` are already goroutine-safe (mutex-protected, documented), so concurrent query reads + background writes are safe with no Engine-level read/write lock. This is the audit's "single biggest latency win" and makes the eval harness measure real retrieval cost.

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: unchanged — stdlib `sync`; the existing `internal/index` (`FTS`, `Vector`), `internal/pipeline` (`LoadIndex`, `Pipeline`, `DeleteDoc`), `internal/engine`. **No new dependencies.**

**Storage**: Pebble KV — **N/A**. This is purely an in-memory caching change. No new key-space prefix, no persisted snapshot (that is H16), no change to the chunk/embedding record shapes (Principle II intact).

**Testing**: `go test -race -cover ./...` (`make test`). The eval harness (`make test-eval`, spec 004) becomes a realistic latency measurement (SC-003). New tests target `internal/engine` (cache reuse + read-after-write + concurrency) and `internal/pipeline` (cache-aware delete).

**Target Platform**: Local single binary; the win is for the long-running daemon (MCP/REST/gRPC serving repeated queries). One-shot CLI queries still cold-start (out of scope — H16).

**Project Type**: CLI + multi-transport server. This feature is in `internal/engine` (the shared core) with a small touch in `internal/pipeline` (cache-aware delete) — no transport adapter changes.

**Performance Goals**: Query latency < 500ms hybrid top-5 (constitution). After this change, repeated queries are flat (O(retrieval), not O(corpus)+O(retrieval)); the first query pays the one-time seed. Write-ACK < 10ms untouched (the seed and all cache updates are post-ACK async/once work, never on the ACK path).

**Constraints**: Pure Go; `FTS`/`Vector` goroutine-safety relied upon (already true); single-writer process model (Pebble lock) — the cache is per-process, no cross-process coherence; query results identical to today (FR-008); no per-write full rebuild (FR-007).

**Scale/Scope**: Local vaults < 100K chunks; the seed is O(corpus) once, then O(1) per query.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | In-process cache over the existing local Pebble + local Ollama. No new binary, cloud, or egress. |
| II | Content-Addressed Identity | ✅ Pass | No change to identity/change hashes or the chunk/embedding records. The cache is an in-memory projection of persisted data, rebuilt identically by `LoadIndex`. |
| III | Pure Go — No CGo | ✅ Pass | stdlib + existing packages only; no new dependency. |
| IV | Async-After-ACK Writes | ✅ Pass (core alignment) | The durable write ACK (<10 ms) is untouched. The one-time seed and all cache mutations happen off the ACK path (seed on first query; incremental adds on the existing background workers; deletes off the ACK path). This is the principle H01 most directly serves — and the latency budget it targets (query < 500 ms). |
| V  | Extension by Interface, MCP-First | ✅ Pass | No external surface change. `Engine.Query`'s contract (input→ranked hits) is unchanged; every transport sees identical results, faster on repeat. The shared index reuses the existing `FTS`/`Vector` types — no new interface. |

**No violations.** Complexity Tracking table below is empty.

## Project Structure

### Documentation (this feature)

```text
specs/011-index-cache/
├── plan.md              # This file
├── research.md          # Phase 0 — shared-live vs snapshot; cache-aware delete; seed-once; concurrency
├── data-model.md        # Phase 1 — the index-cache entity (shared FTS/Vector + generation)
├── quickstart.md        # Phase 1 — latency-ratio, read-after-write, delete-freshness, concurrency
├── contracts/
│   └── query-cache.md   # Phase 1 — the preserved query contract (same results, faster on repeat)
└── tasks.md             # (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/engine/
├── engine.go            # shared (idxFts, idxVec) + lazy e.indexes() seed-once; e.pipeline() passes them
├── query.go             # replace pipeline.LoadIndex(e.db) → e.indexes()
└── (query/ingest tests) # cache-reuse, read-after-write, delete-freshness, concurrency
internal/pipeline/
├── delete.go            # DeleteDoc → method (p *Pipeline) DeleteDoc: also clears p.fts/p.vec
├── reprocess.go         # call p.DeleteDoc (was pipeline.DeleteDoc(p.db, …)) ×2
└── (delete tests)       # cache-aware delete removes from in-memory index
internal/watcher/
└── watcher.go           # call cd.pl.DeleteDoc (was pipeline.DeleteDoc(cd.db, …)) ×2
```

**Structure Decision**: The change centers on `internal/engine` (own + seed + serve the shared index) and a small, mechanical change to `internal/pipeline/delete.go` (make `DeleteDoc` cache-aware) plus its 4 callers (2 in `reprocess.go`, 2 in `watcher.go`). **No transport adapter, CLI, config, or proto change** — `Engine.Query`'s contract is unchanged, only faster on repeat. `pipeline.New` already accepts `fts`/`vec` parameters, so the engine passes its shared pair instead of fresh empty ones — the pipeline package needs no signature change for that.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

*(Empty — Constitution Check passes cleanly on all five principles. No violations to justify.)*
