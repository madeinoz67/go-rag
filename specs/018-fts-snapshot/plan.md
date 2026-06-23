# Implementation Plan: Persistent FTS Index Snapshot (Fast Cold Start) (H16)

**Branch**: `main` | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/018-fts-snapshot/spec.md` (audit backlog item **H16**, P1 — Phase 4, last item).

## Summary

Persist the built BM25 FTS postings to Pebble so a cold start (daemon boot / one-shot CLI) **loads the
snapshot** instead of re-tokenizing every chunk. New prefix `PrefixFTSSnapshot = 0x06` (within the
reserved FTS range) holds two keys: a serialized FTS blob (`gob` over `{postings, docLen, totalLen, N}`)
+ a staleness marker. A `LoadIndex` decision tree loads the snapshot when its embedded marker matches the
current marker (O(1) check) and the format version is current; otherwise it rebuilds from chunks (today's
path). The snapshot is written on engine `Close` when the session mutated (checkpoint), and the marker is
bumped **lazily once per session** on the first chunk mutation (cheap, crash-safe — a crash always leaves
the marker ahead of the snapshot ⇒ next cold start rebuilds). **FTS only** (locked clarification); the
vector-map reload is unchanged. Transparent — identical results; pure latency win. No new dependencies.

## Technical Context

**Language/Version**: Go 1.26.4 (`go.mod`). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib (`encoding/gob`, `encoding/json`, `sync`); existing `internal/index`
(FTS), `internal/storage`, `internal/pipeline`, `internal/engine`. **No new module dependencies.**

**Storage**: Pebble KV — **one new prefix**, `PrefixFTSSnapshot = 0x06` (within the `0x05`–`0x08` range
`storage.go` reserves for the BM25 FTS index), holding two keys: `"snapshot"` (the serialized FTS blob)
and `"marker"` (the staleness counter). No change to chunk/embedding records; the snapshot is a derived,
non-authoritative cache.

**Testing**: `go test -race -cover ./...`; new tests (snapshot load = rebuild results [transparency],
cold-start faster [timing], currency after ingest/delete, stale/absent/corrupt → rebuild + rewrite,
backward-compat backfill, format-version invalidation). H02 eval gate — recall@10 unchanged (FR-010).

**Project Type**: CLI + multi-transport server. Touches: index (FTS Save/Load), storage (new prefix),
pipeline (LoadIndex decision tree + the pre-mutation marker-bump callback), engine (snapshot write on
Close, dirty tracking).

**Performance Goals**: a cold start with a valid snapshot loads the FTS in a `gob` decode (≪ the
re-tokenization it replaces); target ≥5× faster than the rebuild on a multi-thousand-chunk corpus
(SC-002). Bulk ingest does **not** regress — the marker bumps once per session and the snapshot writes
once on Close (no per-chunk snapshot write, FR-009).

**Constraints**: Pure Go, no new deps; cold start must never return wrong results from a stale/corrupt
snapshot (rebuild fallback); the staleness check is O(1) (one Get) so the speed win survives; chunks
remain authoritative (snapshot loss = latency, not data loss).

**Scale/Scope**: local single-user; one snapshot blob per vault.

## Constitution Check

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | All local (Pebble + in-process gob); no network, no new dependency. Single binary unchanged. |
| II | Content-Addressed Identity | ✅ Pass | The snapshot is derived FTS state, non-authoritative; document/chunk identities + content hashes are unchanged. Loss ⇒ rebuild from chunks (latency, not data loss). |
| III | Pure Go — No CGo/External Runtime | ✅ Pass | stdlib only (`encoding/gob`). gob chosen over JSON for snapshot compactness/speed (the whole point is fast load); JSON would parse slower than the re-tokenization being avoided. |
| IV | Async-After-ACK Writes | ✅ Pass | The snapshot is written on engine `Close` (drain), never on the ACK path. The marker bump is **lazy-once-per-session** (only the first chunk mutation of a session does one extra Pebble put on the sync storeDocument path — sub-ms, once per session; well inside the < 10 ms budget). Subsequent mutations don't re-bump. |
| V | Extension by Interface, MCP-First | ✅ Pass | The FTS gains `Save`/`Load` methods (serialization); the `Embedder`/`FileReader` interfaces are unchanged. The snapshot is internal and transparent — no transport/API surface change (no new CLI/REST/gRPC/MCP contract). |

**No violations.** (Re-check after Phase 1 design — still clean: one new Pebble prefix for two derived
keys, no new process/dependency, no public-surface change. The lazy-once marker bump keeps IV intact.)

## Project Structure

### Documentation (this feature)

```text
specs/018-fts-snapshot/
├── plan.md                  # this file
├── research.md              # Phase 0 — D1–D6 decisions resolved
├── data-model.md            # Phase 1 — FTS snapshot record, marker, decision tree
├── contracts/
│   └── fts-snapshot-contract.md  # Phase 1 — internal contract (no transport surface; transparency + escape)
├── quickstart.md            # Phase 1 — runnable validation scenarios
└── tasks.md                 # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/storage/storage.go        # EDIT: PrefixFTSSnapshot byte = 0x06 (new prefix; within reserved FTS range)
internal/index/fts.go              # EDIT: add MarshalSnapshot()/RestoreSnapshot() (gob over the 4 FTS fields); no change to Index/Delete/Query
internal/pipeline/load.go          # EDIT: LoadIndex decision tree — load snapshot if marker+version valid, else rebuild; returns whether it rebuilt
internal/pipeline/pipeline.go      # EDIT: a pre-mutation hook (OnChunkMutation) fired at the start of storeDocument + DeleteDoc for the marker bump
internal/pipeline/delete.go        # EDIT: fire OnChunkMutation at the start of DeleteDoc
internal/engine/snapshot.go        # NEW: writeSnapshotIfDirty (serialize idxFts → 0x06/"snapshot" with marker+version); loadMarker/bumpMarker (0x06/"marker", lazy-once-per-session)
internal/engine/engine.go          # EDIT: track dirty (FTS mutated); bump marker via OnChunkMutation; Close writes the snapshot if dirty
```

**Structure Decision**: Three cohesive, independently-testable layers:

1. **FTS serialization** (`internal/index/fts.go`) — `MarshalSnapshot`/`RestoreSnapshot` over the 4
   serializable fields (`postings`, `docLen`, `totalLen`, `N`). Pure, no storage coupling. Unit-testable
   round-trip (index → marshal → restore → same query results).
2. **Snapshot store + marker** (`internal/engine/snapshot.go` + `internal/storage` prefix) — two keys
   under `0x06`: the blob + the marker counter; lazy-once-per-session bump; write-on-Close-if-dirty.
3. **Cold-start wiring** (`internal/pipeline/load.go` decision tree + `internal/engine/engine.go`) —
   LoadIndex loads-or-rebuilds; the engine writes the snapshot on Close when the session mutated, and
   bumps the marker lazily via the pipeline's pre-mutation hook.

**Highest-risk correctness item**: the **marker ordering + crash safety** (D4/FR-004/FR-008). The marker
must be bumped+persisted before the chunk is durable, so a crash always leaves the marker ≥ the snapshot
⇒ next cold start rebuilds (never serves a stale snapshot). A regression test for each crash window
(stale marker, corrupt blob, absent snapshot, out-of-band chunk change) gates it.

## Complexity Tracking

*(Empty — Constitution Check passes on all five principles. No violations to justify.)*
