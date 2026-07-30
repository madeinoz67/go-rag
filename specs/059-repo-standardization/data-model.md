# Data Model — Repository Governance Standardization

**Date**: 2026-07-30

This feature has no runtime data model — it introduces no structs, tables, or on-disk layout. The two "models" below are the *documented* entities the artifacts describe, not entities the feature creates.

## Entity 1 — Key-space Prefix (documented by the registry)

The registry's primary row entity. Source of truth: `internal/storage/storage.go` (`Prefix*` constants + `VaultScopedKinds`).

| Field | Meaning | Example |
|---|---|---|
| `byte` | The single-byte prefix allocating the keyspace slice | `0x03` |
| `constant` | The Go constant name in `storage.go` | `PrefixChunk` |
| `scope` | `vault-scoped` (key shape `kind \| wsPrefix(8) \| payload`) or `global` (`kind \| payload`) | `vault-scoped` |
| `keyShape` | The payload bytes after the prefix | `ws+chunkID` |
| `value` | What the value encodes | chunk record |
| `note` | One-line hazard / allocation source | spec 018; primary record |

**Validation rule (FR-001/SC-002)**: every `Prefix*` constant in `storage.go` MUST appear in the registry with a matching `byte`. Verified by reading both files, not by a test.

### Scope classification (from `VaultScopedKinds`)

- **Vault-scoped (0x01–0x15, excluding reserved)**: `0x01 Source, 0x02 Document, 0x03 Chunk, 0x04 Embedding, 0x05 FTSPosting, 0x07 FTSIndexed, 0x08 FTSGlobalSt, 0x0A SourceDocs, 0x0B DocChunks, 0x0C PathDoc, 0x0D ContentHash, 0x0E ChangeDetect, 0x0F Idempotency, 0x10 CorpusMeta, 0x11 PoisonQuar, 0x12 ThreatSrc, 0x13 NearDup, 0x14 EmbedQueue, 0x15 ImageCaption`.
- **Global**: `0x09 Config`, `0x17 AuthAPIKey`, `0x18 AuthAdmin`, `0x19 AuthSession`, `0x1A VaultMeta`, `0x1B VaultNameIndex`, `0xFF` schema-version meta.
- **Reserved**: `0x06` (FTS gap), `0x16` (BL-011 webhook, per bridge backlog).

## Entity 2 — Reviewer Invariant Set (documented by the agent)

The routing unit the `code-reviewer` agent applies when a diff touches its files. Each set is advisory prose, not code.

| Field | Meaning |
|---|---|
| `name` | The set label (e.g. "storage/keyspace/migration") |
| `triggerPaths` | The directories/files that activate the set |
| `invariants` | The go-rag-specific checks applied |
| `crossSurface` | The "you changed X, also update Y" obligations |

The six sets (Decision 6): retrieval/hybrid, storage/keyspace/migration, auth/tokens, transport parity, enrichment/embed, cross-surface drift. Full detail lives in the agent body and `contracts/agent-contract.md`.

## State transitions

None. The prefix table is append-only over time (new prefixes added in `storage.go`, then a registry row); the agent's invariant sets grow as subsystems do. Neither has a runtime lifecycle.
