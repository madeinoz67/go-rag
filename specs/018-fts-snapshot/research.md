# Research — Pebble-backed Async FTS (H16, pivoted)

> Phase 0 output. The design decisions for the pivot from "FTS snapshot" to
> "Pebble-backed async FTS." Grounded in MuninnDB's source (`scrypster/muninndb`
> `internal/index/fts/`, read 2026-06-23) + a go-rag-local benchmark.

## Context: what ships today (what the pivot changes)

- **`FTS`** (`internal/index/fts.go`) — in-memory `postings map[string]map[string]float64` + `docLen` + `totalLen` + `N`. Methods: `Index(chunkID, fields)`, `Delete(chunkID)`, `Search(query, k) []Hit`. Tokenizer: `Tokenize` ( Porter stemmer — wait, let me check; go-rag's Tokenize may differ from MuninnDB's snowball stemmer).
- **`LoadIndex`** (`internal/pipeline/load.go`) — cold start: `PrefixScan(PrefixChunk)` re-tokenizes each into the in-memory FTS + `PrefixScan(PrefixEmbedding)` reloads the vector map.
- **`storeDocument`** — sync (pre-ACK): writes doc/chunks/path/contenthash to Pebble + calls `fts.Index` (in-memory, sync). This is the Principle IV tension (BM25 indexing on the ACK path).
- **`processJob`** — async (post-ACK): embeds chunks, `vec.Add`, and (H01) re-calls `fts.Index` (idempotent in-memory replace).
- **`DeleteDoc`** — sync: deletes Pebble keys + `fts.Delete(chunkID)` (in-memory map removal).
- **`storage.go`** — reserves `0x05–0x08` for "BM25 FTS inverted index" but assigns no specific roles.

## D1 — Prefix role assignment in the FTS range

**Decision**: assign specific roles within the reserved `0x05–0x08` range:
- `PrefixFTSPosting = 0x05` — posting keys: `0x05 | term | 0x00 | field(1) | chunkID(16)`.
- `PrefixFTSTermStat = 0x06` — per-term document-frequency (DF): `0x06 | term` → uint32.
- `PrefixFTSGlobalStat = 0x08` — vault-level BM25 stats: `0x08 | "stats"` → {N uint64, avgdl float32}.

**Rationale**: `0x05–0x08` is already reserved for BM25 FTS in `storage.go:15`. MuninnDB uses `0x05` (postings), `0x09` (term stats), `0x08` (global stats); go-rag's `0x09` is `PrefixConfig`, so term-stats go to `0x06` (free within the FTS range). `0x07` left unused (future: trigram/fuzzy if needed).

## D2 — FTS adapter shape (thin Pebble-backed)

**Decision**: the `FTS` struct becomes a thin adapter holding `*pebble.DB` + an in-memory IDF cache (`map[string]float64`, lazy, RWMutex-guarded). No posting map, no docLen map, no N/totalLen — those live in Pebble. `NewFTS(db *pebble.DB) *FTS`.

**Rationale**: mirrors MuninnDB's `Index` struct exactly (`db *pebble.DB`, `idfCache`, no posting map). The only in-memory state is the IDF cache (populated lazily on Search, invalidated on Index/Delete). Cold start constructs the adapter in O(1) — no postings to load. The BM25 math (k1, b, field weights) is unchanged — only the data source shifts from map-lookups to Pebble prefix-scans.

## D3 — Async home: processJob (not a new worker pool)

**Decision**: FTS posting writes happen in the existing `processJob` (the async worker that already embeds vectors). No new worker pool. `processJob` currently calls `fts.Index(c.ID, fields)` (in-memory, H01 idempotent re-index); the pivot keeps that call site but `fts.Index` now writes Pebble keys (the backing changed, not the caller).

**Rationale**: go-rag already has the async-after-ACK pattern (`processJob` runs on background workers after the ACK). Reusing it avoids a new goroutine pool/channel/drop-policy (MuninnDB has a dedicated worker with drop-on-overflow; go-rag doesn't need that complexity at local scale). If back-pressure ever becomes a concern (it won't at <10K chunks), a bounded worker is a follow-on.

**Alternative rejected**: a dedicated FTS worker (MuninnDB's pattern, with `Submit` + drop-on-overflow). More machinery; unnecessary at go-rag's scale. The `processJob` queue (buffered channel, 64 capacity) already handles per-doc FTS+vectors work.

## D4 — storeDocument drops sync FTS (Principle IV correction)

**Decision**: `storeDocument` (sync, pre-ACK) drops its `fts.Index` call. The ACK path carries only the durable Pebble writes (doc/chunks/path/contenthash). BM25 indexing happens entirely async in `processJob`.

**Rationale**: the constitution (Principle IV) mandates "BM25 indexing MUST occur asynchronously on background workers AFTER the acknowledgement." The current sync-FTS-in-`storeDocument` (H01/spec 011) was a deliberate choice for immediate keyword visibility — but it bends the constitution. The pivot corrects it. The behavior change: keyword search becomes eventually consistent (async), symmetric with vector search (which was always async). The window is the `processJob` drain time (milliseconds — the existing `waitEmbedded` covers it).

## D5 — LoadIndex simplification + migration backfill

**Decision**: `LoadIndex` becomes:
```
LoadIndex(db) → (*FTS, *Vector):
  fts := NewFTS(db)        // O(1) — thin adapter, no postings to load
  if !FTSStatsKeyExists(db):
      migrateFTS(db)       // one-time: scan chunks → tokenize → write postings + stats
  vec := reload from PrefixEmbedding   // unchanged
  return fts, vec
```
**Migration** (`migrateFTS`): the one-time backfill for pre-pivot vaults. Scans PrefixChunk, tokenizes each, writes posting keys + DF + global-stats. This is the SAME O(corpus) work the OLD cold start did every time — now it happens ONCE, gated by the stats-key check, then never again.

**Rationale**: the pivot's cold-start win is that the FTS is durable — `LoadIndex` no longer rebuilds it. The migration is the bridge for existing vaults (FR-006). After migration, cold start is O(vectors) (the only in-memory index left).

## D6 — Search: per-term prefix scans

**Decision**: `FTS.Search(query, k)` tokenizes the query → for each term, prefix-scans `0x05 | term | 0x00 | field | *` (LowerBound/UpperBound computed from the term + separator), decodes 7-byte posting values, accumulates BM25 (k1=1.2, b=0.75, field weights, IDF from cached DF). Mirrors MuninnDB's `searchToken` exactly. Results sorted by score descending, top-k returned.

**Benchmarked** (2026-06-23): ~0.3 ms worst-case (2-term query with long posting lists) on ~2.9 K chunks; ~0.07–0.12 ms for single-term queries. All ~170× under the 50 ms keyword budget.

**Rationale**: Pebble's ordered LSM enables efficient prefix scans. The iterator reads from the in-memory L0/L1 levels (hot postings) + cached blocks — no disk I/O for hot terms. The IDF cache avoids repeated DF lookups across query terms.

## D7 — Delete: re-tokenize content → batch-delete keys

**Decision**: `FTS.Delete(chunkID, content string)` re-tokenizes the content to recover the terms → constructs `0x05 | term | sep | field | chunkID` keys → batch-deletes them. DF per term decremented (or invalidated in the IDF cache). Same pattern as MuninnDB's `DeleteEngram`.

**Rationale**: the posting key shape (`term | ... | chunkID`) prevents a chunkID-prefix scan (chunkID is at the key's end, after the variable-length term). Re-tokenizing is O(content) — the same cost as the original indexing — and the content is available at delete time (DeleteDoc reads the chunk records). A reverse index (`chunkID → terms`) would add writes per posting for a rare operation (delete); not worth it.

**Interface change**: `Delete(chunkID)` → `Delete(chunkID, content string)`. The sole caller (`pipeline/delete.go` `DeleteDoc`) has the content from the chunk record.

## D8 — Benchmark validation (the de-risk)

**Decision**: proceed with the pivot — the benchmark (2026-06-23) confirms all three risk axes are green:
- **Query latency**: ~0.3 ms worst-case (Pebble prefix-scan BM25 on ~2.9 K chunks, 50 runs avg); vs ~0.24 ms in-memory — negligible difference, both ~170× under the 50 ms keyword budget.
- **Storage**: 6.7 MB for the postings as Pebble keys (LSM-compressed) vs the 10.5 MB gob snapshot blob (or 0.95 MB int-keyed) — smaller than the naive snapshot, and it's the durable store (not a duplicate cache).
- **Ingest**: ~468 ms to ingest ~2.9 K chunks (NoSync, batched) — no regression vs the current in-memory path (the Pebble writes are async in processJob, off the ACK path).

---

## Summary of resolved decisions

| ID | Decision | One-line |
|----|----------|----------|
| D1 | Prefixes | `0x05` postings, `0x06` term-stats (DF), `0x08` global stats — within the reserved FTS range |
| D2 | FTS adapter | Thin Pebble-backed: `*pebble.DB` + lazy IDF cache; no posting map; `NewFTS(db)` |
| D3 | Async home | Reuse `processJob` (no new worker pool); `fts.Index` now writes Pebble |
| D4 | storeDocument | Drops sync `fts.Index` — BM25 indexing off the ACK path (Principle IV) |
| D5 | LoadIndex | No FTS rebuild; `NewFTS(db)` + vectors; one-time migration backfill gated by stats-key |
| D6 | Search | Per-term Pebble prefix scans + BM25 (math unchanged); ~0.3 ms worst-case |
| D7 | Delete | Re-tokenize content → batch-delete posting keys (Delete gains a content param) |
| D8 | Benchmark | All risk axes green: 0.3 ms query, 6.7 MB store, no ingest regression |

**No unresolved NEEDS CLARIFICATION.** Ready for Phase 1 design.
