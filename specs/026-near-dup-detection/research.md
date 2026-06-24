# Research — Near-Duplicate Chunk Detection (spec 026, audit H20)

> Phase 0 output for `/speckit-plan`. Resolves every design decision against the
> live codebase (read this session via tokensave). Every claim cites `file:line`.
> The companion `data-model.md`, `contracts/api.md`, and `quickstart.md` build on
> these decisions. The feature spec is `spec.md`; the constitution is
> `.specify/memory/constitution.md` v1.0.0.

---

## R1 — Detection algorithm: SimHash (64-bit, Hamming distance)

**Decision.** Compute a **64-bit SimHash** per chunk; two chunks are near-duplicate
when their SimHashes differ in ≤ `k` bits (small **Hamming distance**, default
`k=3`). Sibling lookup at ingest is a scan of the fingerprint index with a
popcount comparison.

**Rationale.**
- SimHash is a **locality-sensitive hash**: similar inputs produce similar
  bit-vectors, so near-dups land at small Hamming distance — exactly the property
  we want, and cheap to test (`bits.OnesCount64(a^b)`, one instruction).
- It is **~40 lines of pure stdlib** (`crypto/sha256` to hash token features,
  `math/bits` for popcount) — no new dependency (Constitution III).
- The fingerprint is **8 bytes** — trivially storable, scannable.
- The audit's own guidance: *"SimHash/shingle-Jaccard … brute-force fine at
  local <10K scale."* SimHash is the audit's first suggestion and the cheapest.

**Alternatives rejected.**
- *Shingle-Jaccard (exact n-gram overlap)* — more precise but O(n²) pairwise set
  operations; better as a **verifier** on SimHash candidates than the primary
  detector. Rejected as primary.
- *MinHash + LSH banding* — sublinear candidate lookup, the right answer at
  web scale, but overkill for a local <10K corpus and more code. Deferred (a
  future swap behind the same fingerprint interface if the corpus grows).
- *Embedding cosine similarity* — semantic near-dup, explicitly **out of scope**
  (spec Assumptions); would also couple this feature to the embedder and change
  with every model swap. Rejected for v1.

---

## R2 — Granularity: chunk-level (not document-level)

**Decision.** Fingerprint and cluster at **chunk** granularity — the retrieval
unit (`model.Chunk`, `model.go:71`).

**Rationale.** Two documents can be mostly different yet share one copy-pasted
section; only that **chunk** is a near-dup, and it's the chunk that pollutes the
top-k. Document-level near-dup misses partial overlaps entirely. The spec
(US2-scenario-2, Assumptions) fixes this; retrieval diversity is won or lost at
chunk granularity.

---

## R3 — Identity: a non-identity sidecar (like Poisoning / SectionContext)

**Decision.** `Chunk.NearDup *NearDupInfo` is a **sidecar** — it does **not**
enter the chunk identity hash (`cid = GenerateID(text, mime, {doc, idx})`,
`pipeline.go:252`) or the document identity (`GenerateID(content, mime, metadata)`,
`model.go:46`). Content-hash dedup (`ContentHash(raw)` at `pipeline.go:211-216`)
is unchanged.

**Rationale.** This is the established pattern — `Chunk.Poisoning` (spec 019,
`model.go:88`) and `Chunk.SectionContext` (spec 025) are both non-identity
sidecars. Making near-dup participate in identity would break idempotent
re-add (FR-003) and the "re-embed without duplicate documents" property
(`model.go:62-63`). FR-003 is a hard requirement.

**Migration.** Pre-feature chunks deserialize with `NearDup == nil` (absent, never
an error — US3-scenario-3). Back-fill is via `Reprocess`, **not** a cheap rescan:
unlike poisoning (whose signal lives in the persisted `Content`), the SimHash is
derived at ingest and not previously stored — consistent with SectionContext
(research R7 there).

---

## R4 — Timing: fingerprint sync on ACK, sibling-clustering async-after-ACK

This is the one genuine subtlety under Constitution IV.

**Finding.** Computing one chunk's SimHash is **microseconds** (one pass over the
chunk's tokens). But **finding its near-dup siblings** requires comparing the
fingerprint against the existing corpus — an O(n) scan of the fingerprint index
per chunk, O(n·m) per ingested document (m = chunks/doc). At <10K chunks this is
sub-second per batch, but on a large ingest it could press the **<10 ms ACK
budget** (Principle IV, `constitution.md`).

**Decision.** Split the work across the two phases the pipeline already has:
1. **SimHash compute — synchronous on the ACK path**, at the same site as
   `SectionContext` resolution (`pipeline.go`, the `processFile` chunk loop). It
   is per-chunk text work (cost ≈ the `EstimateTokens` already computed there),
   rides the chunk record's single `Sync`, zero added fsync. The fingerprint is
   also written to the `0x13` index (R5) on the ACK path.
2. **Sibling clustering — async-after-ACK**, in the existing background worker
   (`pipeline/workers.go processJob`, which already does embed + BM25 index). It
   reads the new chunk's fingerprint, scans the `0x13` index for siblings within
   Hamming `k`, and writes the `NearDup` sidecar back onto the chunk record
   (`storage.SetWithPrefix(PrefixChunk, …)`, like `engine.putChunk`).

**Rationale.** This keeps the ACK path under 10 ms (only the µs fingerprint
on it; the O(n) relational scan is async, symmetric with embedding and BM25
indexing). Near-dup info is **eventually consistent** — available once the
worker drains, exactly like keyword search after ACK (the H16/spec 018 model,
`pipeline.go:322-329`). Collapse (US1) reads the populated sidecar, so it works
once the worker has run — the same window vectors/keyword have.

**Alternatives rejected.**
- *All-sync on ACK* — risks the <10 ms budget on large corpora; rejected.
- *All-async* — workable, but the fingerprint is so cheap there's no reason to
  defer it, and computing it sync lets the `0x13` index be ACK-durable (clustering
  can rebuild from it without re-reading source). Sync-fingerprint is strictly
  better.

---

## R5 — Storage: new Pebble prefix 0x13 (chunkID → SimHash)

**Finding.** The key-space is prefix-partitioned (`storage/storage.go`): 0x01
Source … 0x10 CorpusMeta, 0x11 PoisonQuar, 0x12 ThreatSrc. **0x13 is free.**

**Decision.** Add `PrefixNearDup byte = 0x13 // H20/spec 026: chunkID → SimHash
fingerprint (for near-dup sibling scan)`. One Pebble instance, single-writer
(Constitution IV) — no second database, no sidecar file (PRD §6.7). The
`NearDup` sidecar itself lives on the `0x03` chunk record (like `Poisoning`).

**Key shape.** `0x13 || chunkID → uint64(SimHash)`. The sibling scan is a
`PrefixScanByte(0x13, …)` popcount-comparing each entry to the new chunk's
fingerprint. At <10K entries this is a fast scan; banding/bucketing is a future
optimisation (swap behind the scan, no contract change).

**Rationale.** Consistent with every prior secondary index (quarantine 0x11,
threat-src 0x12). The fingerprint index is rebuildable from the chunk text
(re-derive via Reprocess), so it's not a durability concern.

---

## R6 — Coordinate space: the chunk's retrievable text

**Decision.** Compute the SimHash over the **same text the chunker produces and
the embedder indexes** — i.e. the stripped, post-redaction `Chunk.Content`
(`pipeline.go`: redaction at :231, chunking at :249, content = `s.Text`).

**Rationale.** Near-dup must reflect **what is actually retrieved**. If two
chunks are near-dup in their indexed form, they're redundant in results — that's
the signal we want. Computing on the indexed text (not raw source) means
redaction is respected (a redacted secret doesn't create a spurious near-dup
mismatch) and the comparison is against the real retrieval unit.

---

## R7 — Collapse: query-time, opt-in, post-ranking

**Decision.** De-duplication is an **opt-in query flag** (`dedup`, default off —
FR-005, US1-scenario-3), applied **post-ranking** in `engine.Query`: after the
hits are scored and threshold-filtered (the existing point at `query.go:234-276`),
walk the top-k and drop any hit that is a near-dup sibling of a higher-scoring
hit already kept. Keep the highest-scoring representative per near-dup group.

**Why post-ranking.** Collapse must not change which chunks are *retrieved* or
their *scores* (FR-007) — it only decides which *survive* into the returned
top-k. Doing it after scoring/rerank/threshold preserves all ranking guarantees
and is purely subtractive (like the poisoning quarantine filter at
`query.go:189-215`, which is also post-candidate). O(k²) over the small top-k —
trivial.

**Sibling resolution at query time.** Each hit's `Chunk.NearDup.Siblings`
(chunkIDs) is read via the existing `lookupChunk` (`query.go:235`). A hit H is
dropped iff a higher-ranked kept hit lists H in its siblings (or H lists the kept
hit). Pairwise — no global cluster ID needed (R8).

**Default off.** Silently hiding results by default would be surprising
(US1-scenario-3, Assumptions). The operator opts in per query.

---

## R8 — Sidecar shape: pairwise siblings (no global cluster ID)

**Decision.** `NearDup *NearDupInfo{ Siblings []string; Similarity float }`,
where `Siblings` is the list of chunkIDs within Hamming `k` of this chunk
(pairwise), `Similarity` is the closest sibling's similarity. `nil` when the chunk
has no near-dups or was ingested before the feature.

**Rationale.** Pairwise siblings are sufficient for collapse (R7) and avoid the
**transitivity problem** of global cluster IDs (if A≈B and B≈C but A≉C, a single
cluster ID would wrongly merge A and C). Pairwise is also what the ingest-time
scan naturally produces. A "cluster count" for status is derived on demand (a
union-find over siblings) if wanted; v1 reports the simpler **near-dup chunk
count** (chunks with ≥1 sibling).

---

## R9 — Threshold: Hamming k, configurable, default conservative

**Decision.** Threshold is the Hamming distance `k` (bits differing). Default
**k=3 of 64** (≈ chunks ≥~95% similar in their token-feature distribution) —
conservative, to protect precision (FR-009: don't merge distinct content).
Configurable via `near_dup_hamming` (config) and tuned against the eval harness
(SC-004).

**Rationale.** A 64-bit SimHash with k=3 is the canonical "high-similarity"
setting (Charikar's original analysis). Lower k = stricter (fewer, more-similar
near-dups); higher k = looser (more, less-similar). The default errs toward
precision; the operator widens it if they want aggressive de-duplication.

---

## R10 — Precision guard: skip fingerprinting short chunks

**Decision.** Chunks below a minimum token length (e.g. `MinTokens`, the existing
`chunk.Splitter` floor, `chunk.go:33`) are **not fingerprinted** → `NearDup` stays
nil. Very short text produces unreliable SimHashes (few features → collisions on
distinct short chunks).

**Rationale.** Protects FR-009 (never flag distinct content). Short chunks are
also low-value for retrieval diversity; skipping them costs nothing. Consistent
with the spec edge case "Short or sparse chunks … MUST NOT produce spurious
near-duplicate matches."

---

## Summary of decisions

| # | Decision | Honours |
|---|----------|---------|
| R1 | 64-bit SimHash + Hamming distance (pure stdlib) | Constitution III, Local-First |
| R2 | Chunk-level granularity | FR-001, retrieval diversity |
| R3 | Non-identity sidecar `Chunk.NearDup` (like Poisoning/SectionContext) | FR-003, Constitution II |
| R4 | SimHash sync on ACK; sibling-clustering async-after-ACK | Constitution IV (<10 ms) |
| R5 | New Pebble prefix `0x13` (chunkID → SimHash); sidecar on 0x03 chunk record | PRD §6.7, single DB |
| R6 | Fingerprint over the chunk's indexed (post-redaction) text | FR-007, RAG semantics |
| R7 | Collapse: opt-in, post-ranking, highest-scored per group | FR-005/007, default-off |
| R8 | Pairwise siblings (no global cluster ID) — avoids transitivity | FR-009, simplicity |
| R9 | Hamming k=3 default, configurable, tuned via eval | FR-006/009, SC-004 |
| R10 | Skip fingerprinting short chunks (precision guard) | FR-009 |

No NEEDS CLARIFICATION remains. Constitution Check (next, in `plan.md`): all five
principles PASS — no violations, no Complexity Tracking entries.
