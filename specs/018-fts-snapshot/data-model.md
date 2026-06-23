# Data Model — Persistent FTS Index Snapshot (H16)

> Phase 1 output. New persisted records (two keys under one new prefix) + the
> in-process dirty/decision state. No change to chunk/embedding/document records;
> the snapshot is a derived, non-authoritative cache.

## New persisted records (per vault, under `PrefixFTSSnapshot = 0x06`)

Two keys in the vault's Pebble DB:

### `0x06 || "snapshot"` — the FTS snapshot blob

A `gob`-encoded record:

| Field | Go type | Meaning |
|-------|---------|---------|
| `Version` | `uint32` | Snapshot format version (D6); mismatch ⇒ rebuild. Bumped only when the serialized shape changes. |
| `Marker` | `uint64` | The staleness-marker value at write time (D4); must equal the current `0x06/"marker"` on load or the snapshot is stale ⇒ rebuild. |
| `Postings` | `map[string]map[string]float64` | term → chunkID → weighted tf (the FTS `postings` field) |
| `DocLen` | `map[string]int` | chunkID → total weighted term count (the FTS `docLen` field) |
| `TotalLen` | `int` | the FTS `totalLen` |
| `N` | `int` | number of chunks (the FTS `N`) |

(`Postings`/`DocLen`/`TotalLen`/`N` are an **exported** projection of the FTS's unexported working
fields — gob can't reach unexported fields, so `MarshalSnapshot`/`RestoreSnapshot` translate. The FTS's
mutex is not serialized.)

### `0x06 || "marker"` — the staleness counter

A small persisted integer (`uint64`, gob- or string-encoded). Bumped **lazily once per engine session**
on the first chunk mutation, **persisted before the chunk is durable** (D4). Read on every cold start to
gate the snapshot (O(1) `Get`).

## New in-process state (on `*Engine`)

| Field | Type | Meaning |
|-------|------|---------|
| `ftsDirty` | `bool` | set when the FTS is mutated this session (chunk add/delete) OR when `LoadIndex` rebuilt (backfill); gates the Close write. |
| `ftsMarkerBumped` | `bool` | in-memory guard for the lazy-once-per-session marker bump. |
| (mutex) | `sync.Mutex` | guards the dirty/marker-bumped read-modify-write. |

`Engine.Close` writes `0x06/"snapshot"` (serialize `idxFts` + current marker + version) **iff `ftsDirty`**.

## FTS methods added (`internal/index/fts.go`)

| Method | Signature | Notes |
|--------|-----------|-------|
| `MarshalSnapshot` | `() ([]byte, error)` | gob-encodes `{Version, Marker?(0 at marshal time), Postings, DocLen, TotalLen, N}` under the engine's read lock. (Marker/Version are stamped by the engine when writing.) |
| `RestoreSnapshot` | `([]byte) error` | gob-decodes into the FTS fields under the write lock; a decode error ⇒ the caller treats the snapshot as invalid (rebuild). |

`Index`/`Delete`/`Query` are **unchanged**.

## `LoadIndex` decision tree (`internal/pipeline/load.go`)

```
LoadIndex(db) → (*FTS, *Vector, fromSnapshot bool, err):
  marker   = Get(0x06/"marker")          // 0 if absent
  blob, ok = Get(0x06/"snapshot")
  if ok && decode(blob).Version == CurrentVersion && decode(blob).Marker == marker:
      fts = new FTS; fts.RestoreSnapshot(blob)   // FAST
      fromSnapshot = true
  else:
      fts = rebuild from PrefixChunk (today's path)  // SLOW
      fromSnapshot = false
  vec = reload from PrefixEmbedding          // unchanged
  return fts, vec, fromSnapshot
```

The engine seeds via this (H01's `indexes()`), and treats `fromSnapshot == false` as `ftsDirty = true`
so its `Close` re-caches (the backfill). The vector reload is unchanged (FTS-only scope).

## State transitions

```
(no snapshot) --cold start--> rebuild --Close--> snapshot(M0)
snapshot(M0) --mutate (lazy bump M0→M1, dirty)--> in-memory FTS updated --Close--> snapshot(M1)
snapshot(M0) --crash after mutate, before Close--> marker M1 on disk, snapshot M0
                --next cold start--> M0≠M1 ⇒ rebuild ⇒ snapshot(M1)   (self-heal)
snapshot(Mk) --format upgrade (Version bump)--> load: Version≠current ⇒ rebuild ⇒ snapshot(Mk, newVersion)
corrupt blob --load--> decode fails ⇒ treated as absent ⇒ rebuild ⇒ overwrite
```

## Validation rules (testable)

- Loading a valid snapshot yields keyword results byte-identical to a forced rebuild (FR-007).
- After an ingest or delete + Close, a subsequent cold start reflects the change (FR-003).
- A stale marker (out-of-band chunk change / simulated crash) ⇒ rebuild + correct results + fresh
  snapshot (FR-004).
- A corrupt/absent snapshot ⇒ rebuild + overwrite, no error to the caller (FR-005/FR-006).
- A format-version mismatch ⇒ rebuild (D6).
- Bulk ingest wall-time does not regress vs today (the marker bumps once; the snapshot writes once on
  Close — FR-009).

## Relationships

- `Engine` **1—1** cached `*FTS` (H01) — now seeded from the snapshot-or-rebuild; **1—1** snapshot
  blob + marker (per vault).
- `processJob`/`storeDocument`/`DeleteDoc` mutate the FTS (H01) and fire the pre-mutation hook → the
  engine bumps the marker (lazy) + sets dirty.
- No relationship change to `QueryResult`/`Chunk`/`Document`, the query cache (H06), or the corpus
  baseline (H11) — the FTS snapshot is read-only w.r.t. those.
