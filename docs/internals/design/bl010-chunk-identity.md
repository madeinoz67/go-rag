# BL-010 Design — Chunk identity across document versions (B-simple)

**Status:** design (not yet specced/implemented) · **Date:** 2026-07-02 · **Backlog:** [BL-010](../../docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md#bl-010) (+ related: BL-018)
**Informed by:** a 3-agent design analysis, a 4-facet red team, and a 32-agent RedTeam ParallelAnalysis (22/31 agents independently attacked the original "rewire all indexes" mechanism — the critical weakness that produced this B-simple design).

## 1. The problem (verified)

Chunk IDs are not stable across an edit. Today:

```
cid = GenerateID(s.Text, doc.MimeType, {"doc": docID, "idx": i})   // pipeline.go:329
docID = GenerateID(content, mime, metadata)                          // content-addressed
```

Any document edit changes `docID`, which changes **every** chunk's `cid` — even chunks whose text is byte-identical. Chunks are keyed by `cid` alone under `PrefixChunk` (0x03), and six other prefixes key off `cid` (Embedding 0x04, FTS postings 0x05, FTS-indexed 0x07, poison 0x11, near-dup 0x13, embed-queue 0x14). So a re-ingest of an edited doc today is: delete every old chunk + its 6 indexes, re-create everything from scratch. The bridge re-promotes every chunk to MuninnDB (`BatchRemember` per chunk) even when 90% of the text didn't change.

## 2. The decision: B-simple (not B-full, not A)

Three options were considered:

| Option | Mechanism | Verdict |
|---|---|---|
| **A — drop `docID` from `cid`** | `cid = GenerateID(text, mime, {idx})` | ❌ **Rejected.** Breaks `cid` global uniqueness → cross-doc collisions → silent overwrite + orphaned indexes (7 prefixes affected). The "rescue" (composite keys + revived `PrefixDocChunks 0x0B`) is a 7-prefix refactor. |
| **B-full — rewire all indexes** | Copy Embedding + NearDup + **FTS postings** old-cid→new-cid for UNCHANGED chunks | ❌ **Rejected (RedTeam).** 22/31 agents flagged this. FTS postings are a term→cid inverted index (rewire = per-term scan-and-rewrite, not a KV copy); NearDup siblings are bidirectional across other docs; it races the async embed worker; and `DeleteDoc` runs before `processFile` today (nothing to copy from). |
| **B-simple — preserve the vector only, recompute the rest** ✅ | Copy **only** `PrefixEmbedding` (a direct cid→vector key — single safe KV copy, gated on the model baseline); **recompute FTS + NearDup normally**; skip the Ollama embed for UNCHANGED chunks | **Adopted.** Keeps the real saving (the expensive Ollama call) and drops the flawed inverted-index rewiring entirely. |

## 3. The design

### 3.1 A per-chunk `ContentHash` sidecar

Add to `model.Chunk`:

```go
// ContentHash is SHA-256 of the chunk's REDACTED text (the same input space
// cid hashes, minus {doc, idx}) — a comparison key for BL-010 re-ingest diffing,
// NOT a storage key. A non-identity sidecar (like Poisoning / SectionContext):
// GenerateID folds text+mime+{doc,idx} only, so this never enters identity.
// Cross-document ContentHash collisions are harmless (the diff is per-doc).
ContentHash string `json:"content_hash,omitempty"`
```

**Which text:** the *redacted* chunk text (`s.Text` in processFile, post-redaction). Rationale: (a) it's the same text `cid` already hashes, so the sidecar is consistent with identity; (b) the config-drift gate (§3.4) forces re-embed when the redactor changes, so a redaction-rule change correctly invalidates "UNCHANGED." Hashing *raw* text would diverge from `cid`'s input space and complicate the gate.

Computed in `processFile` next to `cid`; persisted by `storeDocument` (auto, via whole-struct marshal — the established sidecar pattern).

### 3.2 The v2 migration (spec-034 runner)

One new idempotent step (`internal/storage/migrate/v2_content_hash.go`): scan `PrefixChunk`, unmarshal each chunk, set `ContentHash = model.ContentHash([]byte(c.Content))`, re-marshal, write back. Register `Version: 2`, bump `ExpectedVersion` to 2. The runner's per-step fsync + idempotency model handles crash-replay. **Caveat (RedTeam):** re-marshal of a struct with omitempty sidecars must be idempotent — a chunk whose sidecar was nil must round-trip to nil, not a zero-value drift. Verify in the migration test.

### 3.3 The delta — a multiset diff

For a re-ingested source path, diff the OLD chunk set (captured before delete) against the NEW set, keyed by `ContentHash`:

```
countOld[h] = # occurrences of hash h in old
countNew[h] = # occurrences of hash h in new
for each h: u = min(countOld[h], countNew[h]) → u × UNCHANGED
            surplus-old → REMOVED, surplus-new → ADDED
```

**Multiset, not set** — a doc with a paragraph repeated 3× → 2× must report 2 UNCHANGED + 1 REMOVED, not "all unchanged because the text is present." **`hash(text)`, not `hash(text+idx)`** — so identical repeats collapse to the same hash AND a moved paragraph = UNCHANGED (re-embedding it is pure waste). Map UNCHANGED chunks old-cid→new-cid by stable position-within-bucket.

### 3.4 The re-ingest reorder + the embed-skip gate

The re-ingest path (`Reprocess`/`ReprocessAll`) today calls `DeleteDoc` then `Ingest` — the old chunks (and their embeddings) are gone before the new ones exist. Reorder:

1. **Capture before delete.** Factor `DeleteDoc`'s chunk scan (`delete.go:23-30`) into a read-only `chunksOfDoc(docID) []Chunk` helper; ALSO read each old chunk's `PrefixEmbedding` (0x04) record. Call both BEFORE `DeleteDoc`.
2. **Diff + gate in `processFile`.** Thread `oldChunks` (+ their embedding records) into `processFile`. After building the new chunks, compute the multiset diff. For each UNCHANGED chunk:
   - **Embed-skip gate:** if the `CorpusBaseline` (embedding model / dim / convention — spec 017 / `internal/engine/baseline.go`) is **unchanged** since the old chunk was embedded → **copy the old `PrefixEmbedding` to the new `cid`** (a single direct-key KV write, done synchronously in `processFile` before the async worker dequeues the job) and mark the job "embedding present — skip Ollama."
   - If the baseline **changed** → re-embed normally (the old vector is stale; do not copy). This is the gate that prevents serving dim-mismatched vectors and honors the existing `Migrate` feature.
3. **Always recompute FTS + NearDup normally.** The async worker re-indexes FTS postings and re-clusters NearDup under the new `cid` exactly as it does for ADDED chunks; the old `cid`'s postings are deleted by `DeleteDoc`. **No rewiring of inverted/bidirectional indexes** — this is the flawed mechanism B-simple drops.

The saving is the **Ollama embed call** for UNCHANGED+baseline-unchanged chunks. FTS re-index and NearDup re-cluster are local CPU (cheap) and run normally — correctness over marginal extra saving.

### 3.5 The event + the bridge contract

Emit `EventReingested` (the reserved `events.EventReingested` / proto enum 2) carrying:

- `DocumentID` (the new docID), `SourcePath`.
- **The delta:** `repeated ChunkDelta { change_type (ADDED|REMOVED|UNCHANGED); chunk_id (new); prev_chunk_id (old, for UNCHANGED/REMOVED) }` — the BL-010 backlog already specified `prev_chunk_id`; adopt verbatim.
- **The old-docID→new-docID mapping** (or have the bridge key by `source_path`, not docID — recommended, since `document_id` is also content-addressed and flips on edit).

**Event ordering:** `RE_INGESTED` **replaces** the `INGESTED(new)` + `DELETED(old)` pair that spec 040 currently surfaces for a re-ingest, so the bridge doesn't double-count (promote ADDED twice / tag UNCHANGED as superseded).

**The bridge's saving:** promote only ADDED chunks (full `BatchRemember`); for UNCHANGED, `PatchEngram` the stored engram's `chunk_id` ref old→new (cheap metadata patch, MB-004) instead of a full re-promote; for REMOVED, tag superseded. If most edits are localized, full-promotion count drops ~90% — the headline benefit, realized on the **MuninnDB-write** side (the expensive cross-process writes), not the go-rag-local side.

## 4. What is explicitly deferred

- **FTS / NearDup rewiring** (the B-full mechanism) — dropped entirely. Only revisited if measurement (§5) shows the FTS re-index cost dominates AND a safe inverted-index rewrite is designed (it is not, today).
- **Reviving `PrefixDocChunks` (0x0B)** — currently vestigial. `chunksOfDoc` is an O(chunks-in-vault) scan filtered by doc; for large vaults this is quadratic under `ReprocessAll`. Reviving 0x0B (PRD §6.4 specifies it) makes capture O(chunks-in-doc). A perf optimization, not a correctness gate — defer until measurement shows the scan cost matters.
- **The watcher as a re-ingest trigger** — the reorder covers `Reprocess`/`ReprocessAll`; the watcher's MODIFIED path (which also re-ingests) must be wired through the same `oldChunks`-threaded `processFile`. Note in the spec.
- **`document_id` stability** — the deeper instability. B-simple addresses the *chunk* delta but a re-ingest still produces a new `docID`. The bridge should key durable refs by `source_path` (stable) rather than `docID` (content-addressed). A separate decision if the bridge needs doc-level identity continuity.

## 5. Measure before claiming a number

The 90% saving is **unverified** (RedTeam: no benchmark, no corpus distribution). Before speccing BL-010, instrument a real re-ingest over a representative vault and measure:

- The actual UNCHANGED ratio for typical edits (is "most edits are localized" true for Obsidian/markdown vaults?).
- The real cost of the `PrefixEmbedding` copy vs the Ollama call skipped.
- The FTS re-index + NearDup re-cluster cost for UNCHANGED chunks (the recomputed-but-not-skipped overhead).

If the UNCHANGED ratio is low (e.g. 30%) or the recompute cost dominates, the optimization may not be worth the complexity — and BL-010 stays deferred.

## 6. Why this is B-simple (the RedTeam verdict, integrated)

- **22/31 RedTeam agents** independently attacked the original "rewire all indexes" (claim 12). B-simple drops exactly that.
- **The config-drift gate** (§3.4) closes the RedTeam's "skip ≠ embedding-current" hole (a model change leaves ContentHash unchanged but the vector stale).
- **The synchronous PrefixEmbedding copy before the worker** closes the "async race / torn rewire" hole — there is no inverted-index rewrite to tear.
- **The old-cid→new-cid map in the event** (§3.5) closes the "bridge bookmark preservation" blocker.
- **The measurement gate** (§5) retires the "unmeasured 90%" objection — the number is earned before it's claimed.

The strong foundation the RedTeam validated (claims 1, 9, 10, 11, 18 — the verified diagnosis, the capture-before-delete, the additive/reversible sidecar, the embed-skip lever) is preserved. The flawed superstructure (inverted-index rewiring) is removed.
