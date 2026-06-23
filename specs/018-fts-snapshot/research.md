# Research — Persistent FTS Index Snapshot (H16)

> Phase 0 output. Resolves the Technical Context unknowns and the spec's deferred
> plan-level decisions (D1–D6) against the live codebase (read 2026-06-23). Each
> decision cites its evidence. The spec is the WHAT/WHY; this is the HOW rationale.

## Context: what already ships (H16 layers on these)

- **`FTS`** (`internal/index/fts.go`) — an in-memory field-weighted BM25 index. Serializable state is
  exactly four fields: `postings map[string]map[string]float64` (term→chunkID→weighted tf), `docLen
  map[string]int`, `totalLen int`, `N int` (the `sync.Mutex` is not serialized). Methods: `Index`
  (add/replace), `Delete`, `Query`. **No Save/Load today.**
- **`LoadIndex`** (`internal/pipeline/load.go`) — cold start: `PrefixScan(PrefixChunk)` re-tokenizing
  each into a fresh FTS + `PrefixScan(PrefixEmbedding)` reloading the vector map. O(corpus) per cold
  start.
- **H01/spec 011** — the engine caches the seeded `(*FTS, *Vector)` once per engine (reused across
  queries); H16 changes the *seed source* (snapshot vs rebuild), not the per-engine reuse.
- **H06/spec 016** — `OnChange` callback (engine `markIndexChanged`) fires after each index mutation;
  the in-memory epoch invalidates the query cache. H16's marker is a *persisted* counter (survives
  restart), orthogonal to the in-memory epoch.
- **`Vector.Save/Load`** (`internal/index/vector.go:83,94`) — exist but are **file-path-based** (write
  to a filesystem path) and unused. Out of scope for H16 (FTS-only, locked clarification); not reused.

## D1 — Snapshot storage: prefix + record shape

**Decision**: a new prefix `PrefixFTSSnapshot = 0x06` holding **two keys**: `"snapshot"` (the serialized
FTS blob) and `"marker"` (the staleness counter). Both per-vault (they live in the vault's Pebble DB).

**Rationale**: `0x06` is within the `0x05`–`0x08` range `storage.go` reserves for the BM25 FTS index
(`storage.go:15` comment), matching the audit's named prefix. Two keys under one prefix keeps the snapshot
blob and its validity marker independent (the marker is read on every cold start; the blob only when
valid) and avoids a combined read/parse when the marker already says "stale." Single blob per vault
(sharding is YAGNI at < 10K chunks).

**Alternatives rejected**: a single combined `{marker, fts}` record (parses the blob just to read the
marker — wasteful on the common stale path); a file-path snapshot (mirroring Vector's unused hooks) —
violates the single-Pebble-DB discipline (constitution: all state in one Pebble instance).

## D2 — Serialization format: gob

**Decision**: `encoding/gob` over an **exported** snapshot struct (`{Postings, DocLen, TotalLen, N}`),
since gob can't reach the FTS's unexported fields.

**Rationale**: the whole point is fast cold start. JSON of a large nested `map[string]map[string]float64`
parses slowly (string escaping, float formatting) — plausibly slower than the re-tokenization H16 exists
to avoid, erasing the win. gob is binary, compact, and fast for nested maps. The cost is a small exported
snapshot struct (the FTS keeps its unexported working fields; `MarshalSnapshot`/`RestoreSnapshot`
translate between them).

**Alternatives rejected**: JSON (consistency with chunk/embedding records — but perf-hostile for a large
index); a custom binary format (premature; gob is enough).

## D3 — Currency strategy: checkpoint-on-Close (write once per session)

**Decision**: the snapshot is written **on engine `Close`** (drain) **iff the session mutated** (a dirty
flag set by the pre-mutation hook). Not per-chunk. Bulk ingest of N chunks ⇒ exactly one snapshot write
(on Close), satisfying FR-009.

**Rationale**: a per-chunk snapshot write would be O(snapshot) per chunk — a perf cliff on bulk ingest
(the exact anti-pattern). Checkpoint-on-Close is the simplest correct + efficient strategy: the live FTS
accumulates mutations; the durable snapshot is refreshed once when the session ends. The lazy-once marker
(D4) guarantees a crash between mutations and Close is detected.

**Alternatives rejected**: per-mutation incremental (O(snapshot)/chunk — rejected); a periodic debounced
writer (more moving parts, same crash-recovery needs); write-on-every-query (wasteful; queries don't
mutate). The one-shot CLI benefits too: a mutating command (`add`/`scan`) writes on its Close; a
read-only `query` doesn't mutate ⇒ no rewrite (but a backfill write happens on the first cold start if
the snapshot was absent — see D5).

## D4 — Staleness marker: persisted counter, lazy-once-per-session, bumped-before-chunk

**Decision**: a persisted counter at `0x06/"marker"`, bumped **lazily once per engine session** on the
**first** chunk mutation (via a pre-mutation callback the engine binds), and persisted **before** the
chunk is durable. The snapshot blob embeds the marker value at write time. On cold start: one `Get` of
the current marker vs the snapshot's embedded marker ⇒ mismatch ⇒ rebuild (O(1) check, FR-008).

**Crash safety** (the highest-risk item): because the marker is bumped+persisted *before* the chunk
write, a crash always leaves `persisted-marker ≥ snapshot-marker`. Windows:
- Crash after marker-bump, before chunk write → marker ahead, chunk absent; reload mismatches ⇒ rebuild
  (scans chunks, chunk not there) ⇒ correct.
- Crash after chunk write, before Close-snapshot-write → marker ahead of snapshot ⇒ mismatch ⇒ rebuild
  ⇒ picks up the chunk ⇒ correct.
- Clean close → snapshot written with the current marker ⇒ next load matches ⇒ fast.

Lazy-once-per-session means only the **first** mutation of a session pays the one Pebble-put marker bump
(sub-ms, once per session — Constitution IV safe); subsequent mutations just set the in-memory dirty
flag. The marker is persisted so it survives the crash the dirty flag can't.

**Alternatives rejected**: bump per-chunk (10K Pebble puts on bulk ingest — avoidable overhead); a chunk
count (O(N) scan to check — defeats the cold-start win, and misses same-count replaces); an in-memory-only
marker (lost on crash ⇒ undetectable staleness); the H06 epoch (in-memory, resets each run — can't gate a
persisted snapshot).

## D5 — `LoadIndex` decision tree + backfill

**Decision**: `LoadIndex` becomes:
```
marker := Get(0x06/"marker")               // 0 if absent
blob, ok := Get(0x06/"snapshot")
if ok && blob.Header.Marker == marker && blob.Header.Version == currentVersion:
    fts := RestoreSnapshot(blob)            // FAST path
    fromSnapshot = true
else:
    fts = rebuild from PrefixChunk          // today's path (SLOW)
    fromSnapshot = false
vec = reload from PrefixEmbedding           // unchanged
return fts, vec, fromSnapshot
```
The engine, after seeding, treats a `fromSnapshot == false` (rebuilt) result as **dirty** so its `Close`
writes a fresh snapshot (the backfill — FR-006: a pre-H16 vault or a stale/corrupt snapshot is rebuilt
then cached for next time). A corrupt/unparseable blob is caught by the decode and treated as `ok=false`
(FR-005). A version mismatch (future format change) is treated as stale ⇒ rebuild (forward-compat).

**Rationale**: keeps `LoadIndex` a pure read (no writes); the engine owns the write (on Close) and the
dirty flag. The backfill is just "a rebuild marks the session dirty ⇒ Close writes it."

## D6 — Forward-compat: format-version stamp

**Decision**: the snapshot blob carries a `Version` field (a constant bumped whenever the serialized FTS
shape or the gob struct changes). On load, a mismatch ⇒ rebuild (treated as stale). So a go-rag upgrade
that changes the format silently rebuilds once, then re-caches — no stale-shape decode, no wrong results.

**Rationale**: the marker (D4) catches *content* drift (chunks changed) but not *format* drift (the
serialization changed). The version stamp catches the latter. Together (marker + version) they make the
snapshot fully self-invalidating. Cheap (one int in the header).

---

## Summary of resolved decisions

| ID | Decision | One-line |
|----|----------|----------|
| D1 | Storage | Prefix `0x06`, two keys (`"snapshot"` blob + `"marker"` counter); per-vault |
| D2 | Format | `gob` over an exported `{postings, docLen, totalLen, N}` struct (fast/compact; JSON rejected for perf) |
| D3 | Currency | Checkpoint-on-Close-if-dirty (one write per session; no per-chunk write) |
| D4 | Marker | Persisted counter, lazy-once-per-session, bumped-before-chunk (crash-safe, O(1) check) |
| D5 | LoadIndex | Load-if-marker+version-valid else rebuild; a rebuild marks dirty ⇒ Close re-caches (backfill) |
| D6 | Forward-compat | Version stamp in the blob; mismatch ⇒ rebuild |

**No unresolved NEEDS CLARIFICATION.** Scope locked (FTS-only, clarification 2026-06-23). Ready for
Phase 1 design.
