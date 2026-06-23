# Implementation Plan: Pebble-backed Async FTS Index (H16, pivoted)

**Branch**: `main` | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Pivoted spec from `/specs/018-fts-snapshot/spec.md` (audit backlog item **H16**, P1 — Phase 4, last item). Originally "FTS snapshot"; pivoted to **Pebble-backed async FTS** after MuninnDB research + a benchmark (2026-06-23) showed snapshotting patches an in-memory index that shouldn't be the source of truth.

## Summary

Rewrite go-rag's BM25 FTS from an in-memory `map[string]map[string]float64` to a **durable Pebble-backed inverted index**: postings as keys under `0x05`, queried in place via per-term prefix scans, indexed **asynchronously** (post-ACK, in the existing `processJob`). This **eliminates the cold-start rebuild entirely** (the index IS the durable key space — nothing to reconstruct), moves BM25 indexing off the ACK path (Principle IV compliance — the current sync-FTS in `storeDocument` bends it), and **deletes the snapshot/marker/size machinery** H16 originally proposed. Patterned on MuninnDB's proven `internal/index/fts/`. Benchmark-verified: Pebble prefix-scan BM25 queries measure ~0.3 ms worst-case (vs ~0.24 ms in-memory; both ~170× under the 50 ms budget); durable store 6.7 MB for ~2.9 K chunks. FTS-only (vector map unchanged). No new dependencies.

## Technical Context

**Language/Version**: Go 1.26.4 (`go.mod`). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib (`encoding/binary`, `math`, `sort`, `strings`, `sync`); existing `internal/index`, `internal/storage`, `internal/pipeline`, `internal/engine`, Pebble (already a dep). **No new module dependencies.**

**Storage**: Pebble KV — the FTS range `0x05–0x08` (reserved in `storage.go` for BM25 FTS) is now **assigned specific roles**: `0x05` postings, `0x06` per-term document-frequency, `0x08` global stats (N, avgdl). No new prefix constants beyond what's reserved; no change to chunk/embedding records.

**Testing**: `go test -race -cover ./...`; new tests (Pebble-backed Index/Search/Delete round-trip; prefix-scan query; transparency vs old in-memory; cold-start no-rebuild; migration backfill; no quality regression). H02 eval gate — recall@10 unchanged.

**Project Type**: CLI + multi-transport server. Touches: index (FTS rewrite from map to Pebble adapter), storage (prefix role assignment), pipeline (LoadIndex simplification + storeDocument drops sync FTS + processJob writes Pebble postings + DeleteDoc re-tokenizes for key deletion), and the FTS tests.

**Performance Goals**: keyword query < 5 ms p99 (benchmarked ~0.3 ms worst at ~2.9 K chunks — wide headroom under the 50 ms budget). Cold start: no FTS rebuild (just `NewFTS(db)` + vector reload). Ingest ACK: unchanged (FTS moved off the ACK path).

**Constraints**: Pure Go, no new deps; BM25 math unchanged (k1=1.2, b=0.75, field weights); transparency (identical results); FTS indexing async (Principle IV); FTS-only (vector map unchanged); backward-compat migration for pre-pivot vaults.

**Scale/Scope**: local single-user; postings as Pebble keys (LSM-compressed, ~6.7 MB at ~2.9 K chunks).

## Constitution Check

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | All local (Pebble + in-process); no network, no new dependency. |
| II | Content-Addressed Identity | ✅ Pass | Postings are derived from chunk content (re-tokenizable); chunk identities/ContentHash unchanged. |
| III | Pure Go — No CGo/External Runtime | ✅ Pass | stdlib + Pebble (already a dep). |
| IV | Async-After-ACK Writes | ✅ **Pass (improved)** | FTS postings are written **asynchronously** in `processJob` (post-ACK, alongside vectors). The ACK carries only the durable chunk write. The current sync-FTS-in-`storeDocument` (H01/spec 011) bends IV ("BM25 indexing MUST occur asynchronously"); the pivot **corrects** this — BM25 indexing is now async as the constitution mandates. |
| V | Extension by Interface, MCP-First | ✅ Pass | The `FTS` type's `Index`/`Delete`/`Search` methods remain (the backing changes from a map to Pebble; the interface is stable for callers). No transport/API surface change (transparent). |

**No violations.** (Cleaner than the snapshot design — Principle IV now passes by the letter.)

## Project Structure

### Documentation (this feature)

```text
specs/018-fts-snapshot/
├── plan.md                  # this file
├── research.md              # Phase 0 — D1–D8 decisions resolved
├── data-model.md            # Phase 1 — posting key layout, FTS adapter, stats management
├── contracts/
│   └── fts-pebble-contract.md  # Phase 1 — transparency invariant + async-readiness contract
├── quickstart.md            # Phase 1 — runnable validation scenarios
└── tasks.md                 # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/index/fts.go              # REWRITE: FTS → Pebble-backed adapter (NewFTS(db); Index writes posting
                                   #   batch; Search prefix-scans + BM25; Delete re-tokenizes + batch-deletes;
                                   #   IDF cache + stats). BM25 math unchanged.
internal/storage/storage.go        # EDIT: assign roles in the 0x05–0x08 FTS range (0x05=PrefixFTSPosting,
                                   #   0x06=PrefixFTSTermStat, 0x08=PrefixFTSGlobalStat)
internal/pipeline/load.go          # EDIT: LoadIndex — no FTS rebuild (NewFTS(db) + vector reload only);
                                   #   migration backfill if no global-stats key (one-time)
internal/pipeline/pipeline.go      # EDIT: storeDocument drops the sync fts.Index call (FTS moves to async)
internal/pipeline/workers.go       # EDIT: processJob — fts.Index now writes Pebble postings (was in-memory map)
internal/pipeline/delete.go        # EDIT: DeleteDoc — fts.Delete now re-tokenizes content → batch-deletes keys
```

**Structure Decision**: One cohesive rewrite (the FTS) + three small pipeline edits:

1. **FTS rewrite** (`internal/index/fts.go`) — the in-memory posting map → a Pebble-backed adapter. `IndexEngram`/`IndexChunk` writes posting keys + DF + stats atomically; `Search` prefix-scans per term + accumulates BM25 (same math); `Delete` re-tokenizes + batch-deletes; IDF lazy-cached in memory. Patterned directly on MuninnDB's `fts.go`.
2. **Pipeline edits** — `storeDocument` drops the sync `fts.Index` (the ACK path change); `processJob` keeps calling `fts.Index` but it now writes Pebble (async); `DeleteDoc` passes content to `fts.Delete` for re-tokenization; `LoadIndex` stops re-tokenizing (creates the adapter + reloads vectors; migration backfill on first start).
3. **Storage** — assigns specific prefixes within the reserved `0x05–0x08` range.

**Highest-risk items**: (a) the **FTS rewrite** — many tests touch the FTS (parity, eval, H06 cache, H14 filter, H15 context-window); the BM25 math must be identical (FR-008). (b) the **async visibility window** — tests assuming immediate keyword visibility after `add` must now `waitEmbedded`. (c) the **DeleteDoc signature change** — `fts.Delete` gains a content parameter.

## Complexity Tracking

*(Empty — Constitution Check passes on all five principles, improved over the snapshot design.)*
