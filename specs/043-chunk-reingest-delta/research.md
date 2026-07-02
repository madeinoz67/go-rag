# Research: Chunk Change Deltas on Re-Ingest (BL-010)

**Phase 0 output for `/speckit-plan`.** Consolidates the technical design ([`docs/design/bl010-chunk-identity.md`](../../docs/design/bl010-chunk-identity.md)) — itself the product of a 3-agent design analysis, a 4-facet red team, and a 32-agent RedTeam ParallelAnalysis — into the Spec Kit research record. No `NEEDS CLARIFICATION` remains; the forks are resolved below.

## R1 — The chunk-identity approach

**Decision**: **B-simple** — add a per-chunk `ContentHash` sidecar (SHA-256 of the chunk's redacted text) for diffing; leave the `cid` formula unchanged; preserve the embedding for `UNCHANGED` chunks via a direct-key `PrefixEmbedding` copy (gated on the embedding baseline); recompute FTS + NearDup normally.

**Rationale**: chunk IDs flip on every edit because `docID` is folded into the `cid` hash (`pipeline.go:329`). B-simple decouples *identity* (`cid`, untouched, globally unique) from *sameness* (`ContentHash`, a comparison key). It keeps the real saving (the Ollama embed call) and drops the flawed inverted-index rewiring.

**Alternatives rejected**:
- **A (drop `docID` from `cid`)** — breaks `cid` global uniqueness → cross-doc collisions → silent overwrite + orphaned indexes (7 cid-keyed prefixes). The "rescue" (composite keys + revived `PrefixDocChunks 0x0B`) is a 7-prefix refactor with no rollback. *Rejected.*
- **B-full (rewire all indexes)** — copy Embedding + NearDup + **FTS postings** old-cid→new-cid. **22 of 31 RedTeam agents** flagged this: FTS postings are a term→cid inverted index (rewire = per-term scan-and-rewrite); NearDup siblings are bidirectional across other docs; it races the async embed worker; `DeleteDoc` runs before `processFile` (nothing to copy from). *Rejected.*

## R2 — The delta algorithm

**Decision**: a **multiset diff** keyed by `ContentHash` = `hash(text)` (not `hash(text+idx)`): `min(N_old, N_new)` `UNCHANGED`, surplus-old `REMOVED`, surplus-new `ADDED`. Map `UNCHANGED` chunks old-cid→new-cid by stable position-within-bucket.

**Rationale**: (a) repeated text (boilerplate, signatures) — a paragraph 3×→2× must report 2 `UNCHANGED` + 1 `REMOVED`, not "all unchanged because present" (a set diff silently drops a real removal); (b) a moved paragraph (same text, new position) is `UNCHANGED` — re-embedding it is pure waste; `hash(text+idx)` would mark it REMOVED+ADDED.

**Alternatives rejected**: set diff (wrong for repeats); positional/`hash(text+idx)` diff (wrong for moves; defeats the saving for reordered docs).

## R3 — The embed-skip gate (config drift)

**Decision**: skip embedding generation for an `UNCHANGED` chunk **only when** the embedding `CorpusBaseline` (model / dim / convention, spec 017 / `internal/engine/baseline.go`) is unchanged since the chunk was last embedded. When the baseline changed → re-embed every chunk (stale vectors never reused).

**Rationale**: closes the RedTeam's "skip ≠ embedding-current" hole — `ContentHash`-equality does not imply the stored vector is current (a model change leaves the text identical but the embedding stale). The gate reuses the existing corpus-baseline mechanism + honors the existing `Migrate` feature (which re-embeds on model change).

**Alternatives rejected**: always skip (serves stale/dim-mismatched vectors); always re-embed (no saving — defeats the feature).

## R4 — The event / bridge contract

**Decision**: emit `RE_INGESTED` (the reserved `events.EventReingested` / proto enum 2) **replacing** the `INGESTED(new)`+`DELETED(old)` pair a re-ingest surfaces today. It carries: the new `DocumentID` + `SourcePath`; `repeated ChunkDelta { change_type; chunk_id (new); prev_chunk_id (old) }`; the bridge keys durable refs by `source_path` (stable), since `document_id` is also content-addressed + flips on edit.

**Rationale**: the old→new cid map is the bridge's bookmark-preservation need (it stores `metadata["chunk_id"]` on every engram); without it, every re-ingest orphans stored refs. Replacing (not accompanying) the pair prevents double-counting (promote ADDED twice / tag UNCHANGED as superseded). The bridge's saving: promote only ADDED (`BatchRemember`), `PatchEngram` UNCHANGED's cid ref (cheap), tag REMOVED.

**Alternatives rejected**: leave the wire-shape "open" (the bridge can't act on the event — a blocker, per the RedTeam); accompany the pair (double-count).

## R5 — The migration

**Decision**: a v2 step in the spec-034 runner (`internal/storage/migrate/v2_content_hash.go`) — scan `PrefixChunk`, unmarshal, set `ContentHash = model.ContentHash(c.Content)`, re-marshal, write back. Idempotent (unconditional re-write; same value on replay). Register `Version: 2`, bump `ExpectedVersion` 1→2.

**Rationale**: the runner's per-step fsync + idempotency model handles crash-replay; `v1_bootstrap` is the no-op precedent (v2 is the first real transform). **RedTeam caveat**: re-marshal of a struct with omitempty sidecars must round-trip nil→nil (not drift to a zero value) — verify in the migration test.

**Alternatives rejected**: no migration (old chunks lack `ContentHash` → treated as "always changed" on first re-ingest — safe degradation, but loses the optimization for the existing corpus until their next re-ingest; the migration gives instant benefit).

## R6 — The UNCHANGED-ratio measurement (the one open item)

**Decision**: **target ≥80% UNCHANGED for a localized edit (SC-001), UNVALIDATED.** Do NOT claim a saving externally until measured.

**Reasoned estimate**: the splitter chunks by tokens (~512/chunk). A localized edit (one paragraph in a multi-paragraph doc) changes the text of 1–few chunks; the rest are byte-identical → `UNCHANGED`. So for a doc with N chunks where k are edited, the ratio ≈ (N−k)/N — high when k≪N (the common note-edit case), low for wholesale rewrites. The ~80–90% target is plausible for typical Obsidian/markdown edits but **unmeasured**.

**Methodology (implementation-validation task, NOT a plan blocker)**: instrument a re-ingest over a representative vault (the user's `~/.go-rag/vaults/default`, or a fixture corpus) — for each doc, apply a localized edit, re-chunk, compute the multiset diff, record UNCHANGED/ADDED/REMOVED. Report the distribution. Also measure the `PrefixEmbedding`-copy vs Ollama-call cost + the FTS re-index overhead for UNCHANGED chunks.

**Exit criterion**: if the real ratio is ≪80% (e.g. ~30%) or the recompute cost dominates, the optimization's value shrinks — re-scope or defer BL-010. The feature ships the delta event regardless (the bridge's MuninnDB-write saving holds even if go-rag re-embeds); the embed-skip is the go-rag-local saving that the ratio justifies.
