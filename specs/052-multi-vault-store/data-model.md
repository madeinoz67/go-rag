# Data Model — Multi-Vault Unified Store (v2.0)

**Feature**: specs/052-multi-vault-store | **Date**: 2026-07-13

Phase 1 output. The definitive key-space layout for the unified store. This is the contract every
storage caller, every migration step, and every index builder follows.

---

## Key shape

Every vault-scoped key: **`kindByte(1) | wsPrefix(8) | payload`**
Every global key: **`kindByte(1) | payload`** (unchanged from today)

`wsPrefix [8]byte` = SipHash-2-4 of the vault name (BigEndian, fixed keys).

Range scan for a vault + kind: `[kind|ws, kind|wsPlus)` where `wsPlus = IncrementWSPrefix(ws)`
(BigEndian+1, carry-forward, overflow error).

---

## Prefix table (vault-scoped vs global)

| Byte | Prefix | Scope | Key shape (new) | Cleared by ClearVault? |
|------|--------|-------|-----------------|----------------------|
| 0x01 | Source | VAULT | `0x01\|ws\|sourceID` | YES |
| 0x02 | Document | VAULT | `0x02\|ws\|docID` | YES |
| 0x03 | Chunk | VAULT | `0x03\|ws\|chunkID` | YES |
| 0x04 | Embedding | VAULT | `0x04\|ws\|chunkID` | YES |
| 0x05 | FTSPosting | VAULT | `0x05\|ws\|term\|0x00\|field\|chunkID` | YES |
| 0x07 | FTSIndexed | VAULT | `0x07\|ws\|chunkID` | YES |
| 0x08 | FTSGlobalSt | VAULT | `0x08\|ws` (per-vault BM25 stats) | YES |
| 0x09 | Config | GLOBAL | `0x09\|key` (unchanged) | NO |
| 0x0A | SourceDocs | VAULT | `0x0A\|ws\|sourceID\|docID` | YES |
| 0x0B | DocChunks | VAULT | `0x0B\|ws\|docID\|ordinal` | YES |
| 0x0C | PathDoc | VAULT | `0x0C\|ws\|pathHash\|docID` | YES |
| 0x0D | ContentHash | VAULT | `0x0D\|ws\|contentHash` | YES |
| 0x0E | ChangeDetect | VAULT | `0x0E\|ws\|pathHash` | YES |
| 0x0F | Idempotency | VAULT | `0x0F\|ws\|opKey` | YES |
| 0x10 | CorpusMeta | VAULT | `0x10\|ws` (per-vault drift baseline) | YES |
| 0x11 | PoisonQuar | VAULT | `0x11\|ws\|chunkID` | YES |
| 0x12 | ThreatSrc | VAULT | `0x12\|ws\|sourceID` | YES |
| 0x13 | NearDup | VAULT | `0x13\|ws\|chunkID` | YES |
| 0x14 | EmbedQueue | VAULT | `0x14\|ws\|chunkID` | YES |
| 0x15 | ImageCaption | VAULT | `0x15\|ws\|imageHash` | YES |
| 0x17 | AuthAPIKey | GLOBAL | `0x17\|keyHash` (unchanged) | NO |
| 0x18 | AuthAdmin | GLOBAL | `0x18\|username` (unchanged) | NO |
| 0x19 | AuthSession | GLOBAL | `0x19\|tokenHash` (unchanged) | NO |
| **0x1A** | **VaultMeta** (NEW) | GLOBAL | `0x1A\|ws[8]` → name string | NO (registry) |
| **0x1B** | **VaultNameIndex** (NEW) | GLOBAL | `0x1B\|siphash(name)[8]` → ws[8] | NO (registry) |

**19 vault-scoped** (widened with wsPrefix) + **6 global** (flat, unchanged or new).

---

## Vault registry (global, no wsPrefix in key)

**VaultMeta (0x1A)**: `0x1A | wsPrefix[8]` → `name string`. Used by ListVaultNames (scan [0x1A, 0x1B)).

**VaultNameIndex (0x1B)**: `0x1B | siphash(name)[8]` → `wsPrefix[8]`. Used by ResolveVaultPrefix
(point-get). **Keyed by siphash(name), NOT by wsPrefix** — the indirection that enables
metadata-only rename.

---

## Engine interface shape (vault-aware)

Every public engine method gains `vault string` as first arg (or a request struct carrying it):

```
Engine.Add(ctx, vault, path, glob)         // was: Add(ctx, path, glob)
Engine.Query(ctx, vault, req)               // was: Query(ctx, req)
Engine.DeleteDoc(ctx, vault, docID)         // was: DeleteDoc(ctx, docID)
Engine.Reprocess(ctx, vault, path)          // was: Reprocess(ctx, path)
Engine.Status(vault) → StatusInfo           // was: Status()
Engine.ListDocuments(vault, req)            // was: ListDocuments(req)
Engine.ListChunks(vault, docID, req)        // was: ListChunks(docID, req)
Engine.GetDocument(vault, docID)            // etc.
Engine.AuditRead(vault, opts)               // etc.
```

The engine resolves `vault → wsPrefix` once at entry via `store.ResolveVaultPrefix`, threads
`[8]byte` to storage + pipeline + indexes.

**Cross-vault query**: `QueryRequest.Vaults []string` (empty = single-vault default).

---

## Cross-vault query contract

```
QueryRequest {
    Query      string
    Vaults     []string  // NEW — empty/nil = single-vault (vault arg); non-empty = cross-vault fan-out
    K          int
    Mode       string    // hybrid|semantic|keyword
    ...existing params (NoRerank, Threshold, RRFK, PoolSize, Filter, ContextWindow, etc.)
}
```

Fan-out: for each vault in Vaults (or the single vault arg if Vaults is empty), run the per-vault
retrieval (BM25 + vector) → N×2 ranked lists → generalize reciprocalRankFusion to N-list →
rerank (vault-agnostic) → threshold + dedup → QueryResult.

The QueryResult is unchanged (same QueryHit shape). Cross-vault is transparent to the caller — the
hits just come from multiple vaults. (No vault field on QueryHit in this epic — the operator asked
for cross-vault; the results are fused. A per-hit vault field is a future enhancement.)
