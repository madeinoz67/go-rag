# Data Model — Pebble-backed Async FTS (H16, pivoted)

> Phase 1 output. The FTS backing changes from an in-memory map to durable
> Pebble keys. No change to chunk/embedding records; the posting keys + stats are
> new derived data in the reserved FTS prefix range.

## Pebble key layout (the FTS range `0x05–0x08`, now assigned)

### `PrefixFTSPosting = 0x05` — posting keys

```
key:   0x05 | term[n] | 0x00 | field(1) | chunkID(16)
value: tf(float32, 4B) | field(uint8, 1B) | docLen(uint16, 2B) = 7 bytes total
```
One key per (term, field, chunk). Prefix-scanned per term on query (LowerBound =
`0x05|term|0x00|0x00|0*`, UpperBound = `0x05|term|0x01`). Field weights:
concept=3.0 (0x01), tags=2.0 (0x02), body=1.0 (0x03), creator=0.5 (0x04) — same
as today's in-memory FTS.

### `PrefixFTSTermStat = 0x06` — per-term document frequency

```
key:   0x06 | term[n]
value: df(uint32, 4B)
```
Document frequency (how many chunks contain the term). Used for IDF:
`idf = log((N - df + 0.5) / (df + 0.5) + 1)`. Updated atomically with postings
in the same Pebble batch.

### `PrefixFTSGlobalStat = 0x08` — vault-level BM25 stats

```
key:   0x08 | "stats"
value: N(uint64, 8B) | avgdl(float32, 4B) = 12 bytes
```
N = total chunk count; avgdl = rolling average doc length. Used for BM25 length
normalization. Updated atomically with postings. **Presence is the migration
gate**: if absent → pre-pivot vault → one-time backfill.

## FTS adapter (`internal/index/fts.go`, rewritten)

```go
type FTS struct {
    db       *pebble.DB
    mu       sync.RWMutex           // guards idfCache
    idfCache map[string]float64     // term → IDF (lazy, invalidated on Index/Delete)
}
func NewFTS(db *pebble.DB) *FTS
```

No posting map, no docLen map, no N/totalLen — those live in Pebble. The only
in-memory state is the IDF cache (populated lazily on Search, invalidated on
Index/Delete). O(1) construction — nothing to load.

### Methods

| Method | Signature | Behavior |
|--------|-----------|----------|
| `Index` | `(chunkID string, fields map[string]string)` | Tokenizes each field → builds one Pebble batch (posting keys + DF updates + global-stats update) → commits (async, NoSync). Same field-weight semantics as today. |
| `Delete` | `(chunkID, content string)` | Re-tokenizes content → batch-deletes `0x05\|term\|...\|chunkID` for each term + field → invalidates IDF cache. **Signature change**: gains `content` param (the caller has it). |
| `Search` | `(query string, k int) []Hit` | Tokenizes query → per-term prefix-scan + BM25 (k1=1.2, b=0.75, field weights, cached IDF) → top-k. **Math unchanged.** |

## `LoadIndex` decision tree (`internal/pipeline/load.go`)

```
LoadIndex(db) → (*FTS, *Vector, error):
  fts := NewFTS(db)                                    // O(1) — thin adapter
  if !exists(db, 0x08|"stats"):
      migrateFTS(db)                                   // ONE-TIME: scan PrefixChunk → tokenize → write
                                                       // postings(0x05) + DF(0x06) + stats(0x08)
  vec := reload from PrefixEmbedding                   // unchanged
  return fts, vec
```

After migration, cold start is O(vectors) only — the FTS is durable.

## Relationships

- `Engine` **1—1** `*FTS` (H01's shared index) — now a Pebble-backed adapter; seeded once via `LoadIndex` (which no longer rebuilds the FTS).
- `processJob` **writes** postings async (calls `fts.Index`, which now hits Pebble).
- `storeDocument` **no longer** calls `fts.Index` (the sync ACK-path call is removed — Principle IV).
- `DeleteDoc` **calls** `fts.Delete(chunkID, content)` (re-tokenizes + batch-deletes).
- `Retrieval.SearchWithRerank` **calls** `fts.Search` (now prefix-scans Pebble) — transparent to the retrieval layer.
- `H06` query-cache epoch: bumps from `processJob` (where postings are written) — unaffected.
- `H11` corpus baseline: unaffected (FTS backing is orthogonal to the drift baseline).

## Validation rules (testable)

- `Search` on the Pebble-backed FTS returns byte-identical hits (chunkIDs + scores + order) to the pre-pivot in-memory FTS (FR-008/SC-001).
- Cold start with the Pebble-backed FTS performs no re-tokenization (no PrefixChunk scan for FTS — only for the one-time migration when the stats key is absent) (FR-005/SC-002).
- After ingest + async drain, a cold start reflects the change (FR-004/SC-003).
- A pre-pivot vault migrates on first start (stats key absent → backfill → subsequent starts skip) (FR-006/SC-004).
- A delete removes postings so a subsequent cold start doesn't return the chunk (FR-004).
- `make test-eval` recall@10 unchanged (FR-010/SC-005).
- Keyword query < 5 ms p99 (benchmarked ~0.3 ms worst; SC-006).
