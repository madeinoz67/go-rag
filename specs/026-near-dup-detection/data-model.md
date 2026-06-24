# Data Model — Near-Duplicate Chunk Detection (spec 026, audit H20)

> Phase 1 output for `/speckit-plan`. Grounded in `internal/model/model.go`,
> `internal/pipeline/pipeline.go`, `internal/storage/storage.go`,
> `internal/engine/types.go`. Design decisions live in `research.md` (R1–R10).

## Entity chain (unchanged shape)

```
Source 1:N Document 1:N Chunk 1:1 Embedding      (PRD §6, model.go:1-13)
```

This feature touches **one persisted entity** (`Chunk`), adds **one transient
value** produced at ingest (the SimHash), and **one new key-space prefix** (`0x13`).
No new entity, no new database, no new external dependency.

---

## 1. `model.Chunk` — gains one field

`internal/model/model.go:71-89` (Chunk), after `SectionContext` (spec 025).

**New field:**

```go
// NearDup describes this chunk's near-duplicate relationships (audit H20 / spec
// 026): the chunkIDs of chunks within the configured SimHash Hamming distance
// (pairwise siblings) and the closest sibling's similarity. nil for chunks with
// no near-duplicates and for chunks ingested before the feature — treated as
// absent (never an error) at retrieval (FR-008). A non-identity sidecar (like
// Poisoning / SectionContext): it does NOT participate in chunk or document
// identity. Populated async-after-ACK by the ingest worker's clustering pass;
// query-time collapse (opt-in) reads it to keep one representative per group.
NearDup *NearDupInfo `json:"near_dup,omitempty"`
```

**New type** (alongside `PoisonVerdict` in `internal/model`):

```go
// NearDupInfo is the per-chunk near-duplicate verdict (audit H20 / spec 026).
// Siblings are the chunkIDs within the configured Hamming distance (pairwise);
// Similarity is the closest sibling's normalised similarity in [0,1]. nil/empty
// Siblings is never stored — a chunk with no near-dups has NearDup == nil.
type NearDupInfo struct {
	Siblings  []string `json:"siblings,omitempty"`   // chunkIDs within Hamming k
	Similarity float64 `json:"similarity,omitempty"` // closest sibling, [0,1]
}
```

**Invariants / validation rules (from the spec FRs):**

| Rule | Source | How enforced |
|------|--------|--------------|
| Detect near-dup at chunk granularity | FR-001 | Fingerprint computed per `Chunk.Content` (R2/R6) |
| Derived locally, no LLM/network | FR-002 | Pure-Go SimHash (R1) |
| Non-identity sidecar | FR-003 | Not in `cid` (`pipeline.go:252`) or `GenerateID` (R3) |
| Deterministic + configurable threshold | FR-006 | SimHash is pure; `k` from config (R9) |
| Doesn't alter chunk text/size/embeddings | FR-007 | Sidecar only; chunker/embedder untouched (R6/R7) |
| Absent for no-near-dup & pre-feature | FR-008 | nil pointer + `omitempty` (R3) |
| Never flag distinct content | FR-009 | Conservative k=3 + skip short chunks (R9/R10) |

**Why `*NearDupInfo` with `omitempty` (a pointer).** Matches the `Poisoning
*PoisonVerdict` precedent (`model.go:88`): nil serialises to an **absent** JSON
field, so heading-less / pre-feature chunks (US3) and chunks with no near-dups
all serialize identically to the pre-feature shape (FR-008, migration-safe). A
chunk *with* near-dups emits `near_dup: {siblings:[…], similarity:0.97}`.

**Identity — explicitly out of scope.** `pipeline.go:252` is unchanged:
`cid := model.GenerateID(s.Text, doc.MimeType, map[string]any{"doc": docID, "idx": i})`.
`NearDup` is assigned *after* `cid` and after the ACK-durable store; it never
feeds the hash. Document identity is likewise unchanged (the fingerprint is
stored under `0x13`, not in document metadata).

---

## 2. The SimHash fingerprint — transient at compute, persisted under 0x13

The fingerprint is a `uint64` (8 bytes). It is:

1. **Computed** per chunk on the ACK path (`processFile`, R4) — pure function of
   `Chunk.Content`, deterministic.
2. **Persisted** under the new prefix `0x13` for the sibling scan (R5):

```go
// internal/storage/storage.go (after PrefixThreatSrc 0x12)
PrefixNearDup byte = 0x13 // H20/spec 026: chunkID → SimHash fingerprint (sibling scan)
```

Key shape: `0x13 || chunkID → big-endian uint64`. The async clustering pass scans
this prefix (`PrefixScanByte(0x13, …)`), popcount-compares each entry to the new
chunk's fingerprint, and writes `NearDup` onto the chunk record.

The fingerprint is **rebuildable** from the chunk text (Reprocess re-derives it),
so `0x13` is not a durability concern — it's an index, not a source of truth.
The `NearDup` sidecar (the resolved siblings) is the query-time truth, on `0x03`.

---

## 3. Pipeline flow — where each value is set

`internal/pipeline/pipeline.go:206-301` (`processFile`). Annotated with the
feature's additions (★). Nothing in the existing order moves.

```
raw = ReadFile(path)                              # :207
ch  = ContentHash(raw)                            # :211
if ch seen: return SKIPPED                        # :214  (FR-003 exact dedup, unchanged)
content, metadata, _ = reader.Read(raw)           # :223
spans extracted/removed (spec 025)                # H23
docID = GenerateID(content, mime, metadata)       # :228  (unchanged)
if redactor: content, edits = ApplyWithEdits      # :231  (spec 025)
segs = splitter.Split(content)                    # :249  (unchanged)
for i, s := range segs:
    cid = GenerateID(s.Text, mime, {doc, idx})    # :252  (NearDup NOT in cid)
    chunks[i] = Chunk{ ...existing fields..., SectionContext }   # spec 025
  ★ chunks[i].SimHash computed here? NO — see note.
linked-list, poisoning scoring                    # :267, :283
storeDocument(doc, chunks, ch) → one Sync         # :294
  ★ on the ACK path: write each chunk's SimHash to 0x13 (durable w/ the chunk record)
queue job{docID, chunks}                          # :299  (async embed + BM25 + ★ clustering)
```

**Note on the SimHash field.** The SimHash is **not** stored on the `Chunk`
struct (it's an internal fingerprint, not a query-time attribute). It is computed
on the ACK path and written straight to `0x13`; the `NearDup` sidecar (resolved
siblings) is what lives on the chunk record. This keeps `Chunk` lean (no
8-byte implementation detail on every record) and matches the pattern where
indexes (0x05/0x07/0x11/0x12) live separately from the entity record.

**Async clustering (R4)** — in `processJob` (`pipeline/workers.go`), alongside
embedding + BM25 index, for each new chunk:
1. read its fingerprint from `0x13`;
2. `PrefixScanByte(0x13)`, popcount-compare, collect siblings within `k`;
3. if any, `putChunk` with `NearDup = &NearDupInfo{Siblings, Similarity}`
   (`storage.SetWithPrefix(PrefixChunk, …)`, mirroring `engine.putChunk` /
   `RescanPoisoning` at `engine/poison.go:120-168`).

Near-dup info is therefore **eventually consistent** — present once the worker
drains, the same window keyword/vector search have after ACK.

---

## 4. `engine.QueryHit` — gains one field (canonical projection)

`internal/engine/types.go:52-69` (QueryHit), after `SectionContext`.

```go
// NearDup is this hit's near-duplicate context (audit H20 / spec 026): its
// near-dup sibling chunkIDs (pairwise) and closest similarity. nil/absent for
// chunks with no near-dups or pre-feature chunks. Surfaced 1:1 by every
// transport (FR-004); consumed by opt-in query-time collapse (FR-005).
NearDup *model.NearDupInfo
```

Populated in the hit-building loop at `engine/query.go:243-254`, alongside the
existing `Poisoning` / `SectionContext` copies — a one-line `NearDup: c.NearDup`.

The four transport projections + the `dedup` query flag are specified in
`contracts/api.md`.

---

## 5. Query-time collapse (opt-in) — engine.Query, post-ranking

`internal/engine/query.go`, after the threshold filter (`:268-276`) and before
context expansion (`:288`):

- new `QueryRequest.Dedup bool` (FR-005; default false);
- when set, walk the ranked `out` slice; drop a hit if a higher-ranked kept hit
  lists it as a sibling (read via the existing `lookupChunk` at `:235`). Keep the
  highest-scored per near-dup group. Purely subtractive — scores and ranking
  untouched (FR-007).

---

## 6. State transitions & migration

| Chunk state | `NearDup` | Retrieval behaviour |
|-------------|-----------|---------------------|
| Ingested pre-feature | `nil` (absent) | Hit omits `near_dup`; collapse treats as singleton (US3-3) |
| No near-dups, post-feature | `nil` (clustering found none) | Hit omits `near_dup` (FR-008) |
| Has near-dups, post-feature | non-nil `{Siblings, Similarity}` | Hit carries it; collapse (opt-in) keeps the best per group (US1) |

**Migration is additive and lazy**, identical in shape to SectionContext (spec
025): old chunk records deserialize with `NearDup == nil` (US3-3); back-fill needs
`Reprocess` (the SimHash wasn't previously stored — R3). `0x13` is populated only
for newly-ingested chunks; `status` reports counts over what's been clustered so
far (eventually consistent).

**Idempotency (FR-003).** Re-adding an unchanged file short-circuits at the
content-hash gate (`pipeline.go:214`) before the reader runs — no duplicate
document/chunk. `NearDup` is a non-identity sidecar, so neither document nor
chunk IDs change.

---

## 7. What does NOT change

- `Source`, `Document`, `Embedding` entities — untouched.
- Pebble key-space 0x01–0x12 — untouched (only **0x13 added**).
- The chunker (`internal/chunk`), the `FileReader` interface, chunk/document
  identity, the write-ACK ordering, and the embedded text — all unchanged
  (FR-007, Constitution II/IV/V).
- Embeddings / indexed text / BM25 — untouched (near-dup is structural text
  similarity, orthogonal to embeddings — out of scope for semantic near-dup per
  the spec Assumptions).
- Default query behaviour — `dedup` defaults off (US1-scenario-3); collapse is
  purely opt-in.
