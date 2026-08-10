# Pebble keyspace registry

**The single most important artifact for preventing a whole class of bug.** go-rag runs
its entire state — sources, documents, chunks, embeddings, BM25 inverted index, quarantine,
near-dup, auth, and the vault registry — over **one shared Pebble database**. Logical data
types are separated by **single-byte key prefixes** (PRD §6.7). If two kinds ever claim the
same byte, their scans silently corrupt each other. This registry is the map that keeps them
disjoint; this doc exists because prefix collisions are a silent-corruption failure class
that is invisible until two kinds' scans interfere.

> **Source of truth: `internal/storage/storage.go`.** The `Prefix*` constants and the
> `VaultScopedKinds` slice there are authoritative. This doc is a **reviewer's reference**,
> not authority. **Where code and doc disagree, code wins** — say so in the review and open
> an issue to fix the doc; do not enforce the stale claim. A confidently-wrong doc is worse
> than none.

`ws` = 8-byte `SipHash(vault name)` prefix. Under the v2.0 unified store (spec 052), a
vault-scoped key has shape `kind(1) | ws(8) | payload`; a global key has shape
`kind(1) | payload`. `ws` is deterministic — a vault name always maps to the same prefix
(see the reuse hazard below).

## Storage prefixes (`internal/storage/storage.go`)

| Prefix | Constant | Scope | Key shape after prefix | Value | Notes |
|---|---|---|---|---|---|
| 0x01 | `PrefixSource` | vault | ws+sourceID | Source record | |
| 0x02 | `PrefixDocument` | vault | ws+docID | Document record | |
| 0x03 | `PrefixChunk` | vault | ws+chunkID | Chunk record | carries the poisoning verdict + near-dup sibling refs (spec 019/026 ride this record) |
| 0x04 | `PrefixEmbedding` | vault | ws+chunkID | Embedding metadata | written async after ACK (Principle IV) |
| 0x05 | `PrefixFTSPosting` | vault | ws+term+chunkID | tf + docLen | BM25 posting (audit H16 / spec 018) |
| **0x06** | — | — | — | — | **RESERVED** — FTS gap; do not allocate |
| 0x07 | `PrefixFTSIndexed` | vault | ws+chunkID | docLen | indexed-chunk set; idempotency guard (spec 018) |
| 0x08 | `PrefixFTSGlobalSt` | vault | ws+"stats" | N + totalLen | global BM25 stats (per-vault under unified store) |
| 0x09 | `PrefixConfig` | **global** | key | value | config key/value store — instance-wide |
| 0x0A | `PrefixSourceDocs` | vault | ws+sourceID+docID | — | Source → Documents secondary index |
| 0x0B | `PrefixDocChunks` | vault | ws+docID+chunkID | — | Document → Chunks ordered index |
| 0x0C | `PrefixPathDoc` | vault | ws+path | docID | file path → Document ID lookup |
| 0x0D | `PrefixContentHash` | vault | ws+contentHash | docID | content-hash dedup (distinct from identity hash — Principle II) |
| 0x0E | `PrefixChangeDetect` | vault | ws+… | — | change-detection state |
| 0x0F | `PrefixIdempotency` | vault | ws+opID | receipt | idempotency receipts |
| 0x10 | `PrefixCorpusMeta` | vault | ws | baseline | corpus baseline metadata (embedding drift) — audit H11 / spec 017 |
| 0x11 | `PrefixPoisonQuar` | vault | ws+chunkID | verdict | quarantine index — O(flagged) listing (audit H04 / spec 019) |
| 0x12 | `PrefixThreatSrc` | vault | ws+… | — | threat-source store (FR-012/013, D12) |
| 0x13 | `PrefixNearDup` | vault | ws+chunkID | uint64 | near-dup SimHash fingerprint (audit H20 / spec 026) |
| 0x14 | `PrefixEmbedQueue` | vault | ws+chunkID | {model,status,attempts} | durable pending-embed work queue (spec 030); written atomically with the chunk, removed on embed |
| 0x15 | `PrefixImageCaption` | vault | ws+imageSHA256 | {caption,model} | cross-doc image-caption cache (spec 031) |
| **0x16** | — | — | — | — | **RESERVED** — BL-011 webhook registry (planned, per `docs/RFC-bridge-muninndb/` backlog); auth takes the bytes above it to avoid a future collision |
| 0x17 | `PrefixAuthAPIKey` | **global** | SHA256(secret)[:16] | APIKey JSON | spec 045 — `gorag_` keys; disabled keys persist as `enabled=false` (audit trail) |
| 0x18 | `PrefixAuthAdmin` | **global** | username | AdminUser JSON | spec 045 — bcrypt cost 12 |
| 0x19 | `PrefixAuthSession` | **global** | SHA256(token)[:16] | Session JSON | spec 045 — opaque `gorags_` Bearer sessions (no cookies) |
| 0x1A | `PrefixVaultMeta` | **global** | ws[8] | vault name | spec 052 — list index (scan to enumerate vaults) |
| 0x1B | `PrefixVaultNameIndex` | **global** | siphash(name)[8] | ws[8] | spec 052 — resolve index (point-get name → prefix) |

## Schema-version meta key (not a `Prefix*` constant)

| Key | Scope | Owner | Value | Notes |
|---|---|---|---|---|
| 0xFF | **global** | `internal/storage/migrate` | schema version int | the on-disk schema version (spec 034). Tracked by the `migrate` package, **not** in the `Prefix*` block in `storage.go` — listed here so a reviewer scanning the byte range does not think 0xFF is free. `migrate.ExpectedVersion` is the binary's current version; a store newer than it is refused, never silently misread. |

## Reserved bytes

- **0x06** — FTS gap (left between the FTS posting family and the FTS stats family).
- **0x16** — BL-011 webhook registry (planned). Auth was placed at 0x17–0x19 **deliberately**, above this reservation, so the webhook prefix cannot collide with auth when it lands.
- **spec 060 (MuninnDB bridge)** — v1 is **stateless**: no go-rag keyspace, no prefix, no migration. Promotion state lives in MuninnDB (the `idempotent_id` UPSERT forward index is the correctness layer). The RFC's planned `0x20`–`0x22` (cursor / engram-record / error-ring) remain free; a future durable promoted-chunk cache or backfill-resume marker would allocate there with a numbered migration + `ExpectedVersion` bump + a row here.

## Free bytes

`0x1C`–`0xFE` are free for new storage prefixes (0x1A/0x1B are allocated to the vault
registry; 0xFF is the schema meta). Auth occupies 0x17–0x19. Prefer allocating the next free
byte above 0x1B for storage kinds, and keep new auth/credential kinds inside the 0x17+ band
(or a freshly reserved band) — never inside 0x01–0x15.

**Rule for any change that adds or changes a Pebble key:** the new prefix MUST be disjoint
from every row above, added as a `Prefix*` constant in `internal/storage/storage.go`, added
to `VaultScopedKinds` if it is vault-scoped, **and** added as a row here. If the change alters
the on-disk layout (a new/retired prefix, a value-encoding change, or key construction), the
constitution's Storage-discipline rule applies: add a numbered idempotent migration in
`internal/storage/migrate`, bump `migrate.ExpectedVersion`, and update PRD §6.7.

## Live hazards a reviewer must know

1. **Vault-scoped vs global is load-bearing.** Under the v2.0 unified store (spec 052), the
   19 kinds in `VaultScopedKinds` carry `ws` immediately after the prefix; the global kinds
   (0x09, 0x17–0x1B, 0xFF) do not. A change that adds a kind to the wrong family, or drops
   one from `VaultScopedKinds`, silently breaks either `ClearVault` (which tombstones each
   vault-scoped range) or the v3→v4 migration (which mechanically prepends `ws` to exactly
   those kinds). Check the slice, not just the constant.

2. **`PrefixScanByte` on a vault-scoped kind scans ALL vaults.** The single-byte prefix scan
   helpers (`PrefixScanByte` / `PrefixScan` in `internal/storage/db.go`) are retained **only
   for global prefixes** (Config 0x09, Auth 0x17–0x19, VaultMeta 0x1A, VaultNameIndex 0x1B).
   Calling them with a vault-scoped kind scans every vault's data for that kind. Vault-scoped
   access MUST go through the `internal/storage/keys` package builders + bare `Set`/`Get`/
   `Delete` + `RangeScan` so `ws` is prepended. A PR that re-introduces a `PrefixScanByte`
   call on 0x01–0x15 is wrong.

3. **Single Pebble instance, single writer.** Exactly one go-rag process may open the
   database at a time (Pebble lock). The daemon's per-transport listeners are all readers/
   adapters over one `internal/engine.Engine`; they do not open their own DB. A change that
   opens a second Pebble handle violates the constitution (Storage discipline: "no second
   database").

4. **Identity hash ≠ content hash.** `0x0D ContentHash` is SHA-256 of raw bytes (change
   detection). The document's *identity* is SHA-256 over content **plus a canonicalized
   metadata map** (Principle II). They MUST stay distinct — a change that conflates them
   breaks re-embed-under-a-new-model (it would look like a duplicate document).

5. **Auth is disabled, not deleted, on revoke.** A revoked `gorag_` key (0x17) stays in the
   store as `enabled=false` for the audit trail; the auth path fails closed on it. A change
   that hard-deletes on revoke loses the audit record — flag it.

6. **The loopback bypass is guarded.** The narrow pre-init bypass (bare vault, no admin) is
   disabled by the presence of an admin user (0x18). A change that widens the bypass, or
   makes it fire on an initialized vault, re-arms unauthenticated access — the highest-
   severity class here. See `TestBypassGuard_BareVaultBypasses_InitializedVaultDoesNot`.

7. **Vault prefixes are name-deterministic and therefore reused on name reuse.** Deleting a
   vault does not scrub every cross-vault global record. Re-creating a vault of the same
   name recomputes the identical SipHash prefix. The correct invariant is "prefixes are
   name-deterministic," not "never reused" — a PR asserting the latter is wrong about this
   codebase.
