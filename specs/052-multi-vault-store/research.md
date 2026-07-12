# Research — Multi-Vault Unified Store (v2.0 Storage Model)

**Feature**: specs/052-multi-vault-store | **Date**: 2026-07-13

Phase 0 output. The research was done by a 5-agent / 886k-token MuninnDB-extraction workflow
(adversarially verified). This document synthesizes the verified findings into the design
decisions for go-rag. Every decision is grounded in MuninnDB's proven source, adapted to go-rag's
prefix table and architecture.

---

## R1 — Key layout: kind(1) | wsPrefix(8) | payload (leads with kind)

**Decision**: key shape = `kindByte(1) | wsPrefix(8) | payload`. Lead with kind (MuninnDB's
pattern), so each vault's data for a given kind sits in one contiguous range — O(1) prefix scan
per vault per kind.

**Rationale**: matches MuninnDB's proven layout; preserves go-rag's existing kind-byte convention
(prefix constants 0x01–0x19); range scans use the Pebble idiom `[kind|ws, kind|wsPlus)` where
`wsPlus = IncrementWSPrefix(ws)` (BigEndian+1, carry-forward). ClearVault = one range-tombstone
per kind per vault.

**Alternative rejected**: lead with wsPrefix (`wsPrefix(8) | kind(1) | payload`) — better per-vault
locality (all vault's keys in one SSTable range), but breaks go-rag's kind-byte convention and
scatters kinds within a vault. MuninnDB leads with kind; the compaction locality is fine for
single-operator scale.

---

## R2 — wsPrefix = SipHash-2-4 of vault name (8 bytes, deterministic)

**Decision**: `keys.VaultPrefix(name string) [8]byte` = SipHash-2-4 of the name, BigEndian, with
fixed keys (MuninnDB uses sipKey0="somepseu", sipKey1="dorandum"). Pure function, deterministic.

**Rationale**: SipHash is a PRF — 2^64 prefix space, collisions astronomically unlikely (2^-64).
The 8-byte prefix is the vault's identity inside the key space; the name string lives only in the
registry indexes. Deterministic = no coordination needed to compute a vault's prefix.

**Note**: SipHash collision is undetected-by-design (MuninnDB's posture). For go-rag single-
operator, acceptable. A startup BackfillVaultNames check can flag collisions (cheap, MuninnDB
doesn't bother).

---

## R3 — Vault registry: VaultMeta(0x1A) + VaultNameIndex(0x1B)

**Decision**: two GLOBAL prefixes (no wsPrefix in the key):
- `0x1A | wsPrefix[8] → name` (VaultMeta — prefix→name, for listing)
- `0x1B | siphash(name)[8] → wsPrefix[8]` (VaultNameIndex — name→prefix, for resolution)

**CRITICAL**: the 0x1B key is keyed by `siphash(name)`, NOT by wsPrefix. This indirection is
load-bearing — it makes rename a two-key operation (set 0x1A, set 0x1B(new), delete 0x1B(old)),
not a full rehash of every data key. For a never-renamed vault, siphash(name)==wsPrefix (they
coincide); the indirection only diverges after a rename.

**Rationale**: MuninnDB's exact design (0x0E/0x0F). go-rag's next available prefixes are 0x1A/0x1B
(top is 0x19 AuthSession). Confirmed no collision with existing prefixes.

---

## R4 — ResolveVaultPrefix: LRU → Pebble-get → SipHash fallback (cold-only)

**Decision**: `ResolveVaultPrefix(name) [8]byte`:
1. LRU cache (`vaultPrefixCache`, 10k entries) — hot path.
2. `db.Get(0x1B | siphash(name))` — if len==8, cache + return.
3. Fallback: compute `keys.VaultPrefix(name)` and cache — CORRECT ONLY for never-renamed vaults.

**Rename safety**: the LRU MUST be seeded from the persisted 0x1B index at startup (scan 0x1B,
populate the LRU), so the fallback is cold-only. Never call `VaultPrefix(name)` directly on the
hot path — for a renamed vault it returns the WRONG ws (ws is frozen at creation; siphash(newName)
differs). MuninnDB's engine_vault.go documents this explicitly.

---

## R5 — Vault lifecycle: metadata-only rename, range-tombstone clear

**Decision**:
- **RenameVault**: verify 0x1A value==oldName + 0x1B for newName absent; ONE batch {Set 0x1A→
  newName, Set 0x1B(hash(newName))→ws, Delete 0x1B(hash(oldName))}; evict/refresh caches. Zero
  data moves. <1ms.
- **ClearVault**: capture count; delete 0x14|ws counter FIRST (prevent re-seed); ONE batch with a
  range tombstone per vault-scoped kind (`DeleteRange([kind|ws], [kind|wsPlus])`); evict in-memory
  index state for that vault. O(kinds) tombstones, not O(keys).
- **DeleteVault**: ClearVault + DeleteVaultNameOnly (point-delete 0x1A + 0x1B keys).
- **CreateVault**: implicit on first write (WriteVaultName self-registers).
- **BackfillVaultNames** (startup): scan the Document prefix (0x02), extract ws from bytes [1:9]
  of each key, for any ws missing a 0x1A key write placeholder name `vault-%x` of ws[:4].

**ClearVault scope** (the 19 vault-scoped kinds): {0x01 Source, 0x02 Document, 0x03 Chunk, 0x04
Embedding, 0x05 FTSPosting, 0x07 FTSIndexed, 0x08 FTSGlobalSt, 0x0A SourceDocs, 0x0B DocChunks,
0x0C PathDoc, 0x0D ContentHash, 0x0E ChangeDetect, 0x0F Idempotency, 0x10 CorpusMeta, 0x11
PoisonQuar, 0x12 ThreatSrc, 0x13 NearDup, 0x14 EmbedQueue, 0x15 ImageCaption}. NOT cleared
(global): {0x09 Config, 0x17 AuthAPIKey, 0x18 AuthAdmin, 0x19 AuthSession, 0x1A VaultMeta, 0x1B
VaultNameIndex}.

---

## R6 — Engine: one engine, vault param on every method, per-vault index registries

**Decision**: ONE Engine instance serves ALL vaults. Every public method takes `vault string`
(or a VaultRequest struct), resolves wsPrefix once at entry via `store.ResolveVaultPrefix`, and
threads `[8]byte` down to storage + pipeline + indexes. The in-memory FTS/Vector indexes become
**per-vault registries** (map[wsPrefix]*FTS, lazily seeded on first access, evictable on
ClearVault). The embedder, config, caches, and epoch counters are shared-but-scoped (cache keys
include wsPrefix; epoch is per-vault).

**Rationale**: MuninnDB's engine_vault.go pattern exactly. The engine is the boundary where vault
name → wsPrefix resolution happens; storage never sees a vault string. The per-vault index
registry preserves the seed-once-per-vault invariant (H01) — first query for a vault pays
LoadIndex, every subsequent reuses.

---

## R7 — Cross-vault query: fan-out + N-list RRF rank-merge

**Decision**: `QueryRequest.Vaults []string` (empty/nil = single-vault default; non-empty =
cross-vault). Fan out: run the per-vault retrieval (BM25 + vector) for each target vault in
parallel, producing N×2 ranked lists (BM25 + vector per vault). Fuse with a generalized
`reciprocalRankFusion(lists [][]Hit, k) → []Hit` (currently 2-list → extend to N-list). The fused
pool is then reranked (OllamaReranker is vault-agnostic — it scores query+text, not vault).
Threshold + Dedup apply after the merge.

**Why this works**: go-rag's RRF is RANK-based and SCALE-INVARIANT (`score = Σ 1/(k+rank)`). BM25
IDF and vector scores are NOT comparable across vaults (different corpus statistics), but RANKS are.
The reranker (text-based) IS comparable across vaults. This is the exact property MuninnDB's
activation does NOT have (its scores are absolute), which is why MuninnDB gates cross-vault off —
go-rag's rank-fusion makes it clean.

**Budget cap**: N vaults × pool-size = N×K candidates. Cap the post-RRF pre-rerank pool (e.g.,
min(N×K, 2×pool-size)) to keep the rerank budget flat.

---

## R8 — Migration: v3 → v4 key-widening (one-way, aggressive)

**Decision**: a new migration step in `internal/storage/migrate/v4_multi_vault.go`:
1. Detect legacy per-vault DBs at `~/.go-rag/vaults/<name>/data`.
2. For each: `ws = keys.VaultPrefix(name)`.
3. Iterate every key `kind|payload` in the old DB; rewrite as `kind|ws|payload` into the unified
   store (mechanical prepend — values are opaque blobs, no decoding).
4. Write `0x1A|ws→name` + `0x1B|siphash(name)→ws`.
5. After all vaults: archive the old per-vault dirs (rename to `.prev`, don't delete).
6. Bump `ExpectedVersion` 3 → 4. Per-step fsync. Idempotent. Refuse newer.

**Not in production**: no dual-read / compat layer. The migration is one-way and aggressive.
Fresh installs start at v4 with an empty unified store.

---

## R9 — All transports carry vault param (fail-closed, default "default")

**Decision**: every transport carries a vault selector on every operation. Default "default".
Resolution is fail-closed (unrecognised vault → error).
- **CLI**: `--vault <name>` flag on every command (per-call, not a DB-path selector).
- **REST**: `?vault=<name>` query param + optional body field (must agree). VaultAuthMiddleware
  resolves + validates before the handler.
- **gRPC**: `string vault = N;` on every request message. Unary interceptor validates.
- **MCP**: optional `vault` arg on every tool (default "default").
- **UI**: vault picker in the shell (session-scoped); `X-Go-Rag-Vault` header on /api/* requests.

The engine resolves vault name → wsPrefix; transports never see wsPrefix.

---

## R10 — Config/auth scope: global, not per-vault

**Decision**: config (0x09) and auth (0x17/0x18/0x19) stay GLOBAL (flat, no wsPrefix). One Ollama
model, one admin user, one set of API keys/sessions — shared across all vaults. This is consistent
with single-operator go-rag (auth is already instance-wide today). Per-vault embedding-model
overrides are a future concern.

**Idempotency (0x0F)**: vault-scoped (prepend wsPrefix). A receipt for vault A's operation must
not affect vault B. This differs from MuninnDB (global idempotency), but go-rag's idempotency is
per-operation-per-vault (content-hash-scoped), not global.
