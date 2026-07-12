# Implementation Plan: Multi-Vault Unified Store + Cross-Vault Query (v2.0 Storage Model)

**Branch**: `main` (single-author repo; commits straight to `main`) | **Date**: 2026-07-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/052-multi-vault-store/spec.md`

## Summary

The v2.0 storage-model epic. Collapse go-rag's N separate per-vault Pebble DBs into ONE unified
store, keyed `kind(1) | wsPrefix(8) | payload`. One daemon, one engine, one embedder serves ALL
vaults — vault is a per-request parameter (resolved to `wsPrefix` at the engine boundary), not a
process-level DB-path config. Cross-vault query via fan-out + RRF rank-merge. Aggressive one-way
migration (not in production). Grounded in MuninnDB's adversarially-verified pattern (5-agent,
886k-token research workflow).

This is the **biggest slice in the project** — it touches the storage layer (every key widens),
the engine (every method gains a vault param), all five transports (vault selector), the proto
(vault field on every request), the migration runner (v3→v4 key-widening), and the in-memory
indexes (per-vault registries). It fundamentally changes the storage layout.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`); vendored Alpine (UI, unchanged). No Node.

**Primary Dependencies**:
- stdlib (SipHash-2-4 via `hash/siphash` or a vendored implementation — MuninnDB uses
  `github.com/dchest/siphash`; go-rag may use stdlib or the same dep; plan confirms)
- Pebble (one instance, not per-vault)
- `internal/storage` — the key-widening rewrite (every vault-scoped key gains wsPrefix)
- `internal/storage/keys` (NEW package) — pure key-construction functions, `ws [8]byte` first arg
- `internal/storage/vault.go` (NEW) — VaultPrefix, ResolveVaultPrefix, ListVaultNames, registry,
  lifecycle (rename/clear/delete), BackfillVaultNames
- `internal/storage/migrate` — the v3→v4 key-widening migration step
- `internal/engine` — vault param on every method; per-vault index registries (FTS/Vector);
  shared embedder/config
- `internal/cli`, `internal/rest`, `internal/grpc`, `internal/mcp`, `internal/ui`, `proto/` —
  vault selector on every operation

**Storage**: FUNDAMENTAL LAYOUT CHANGE. Every vault-scoped key widens from `kind(1)|payload` to
`kind(1)|wsPrefix(8)|payload`. 19 vault-scoped prefix families rewrite. 6 global families stay
flat (Config 0x09, Auth 0x17/0x18/0x19, VaultMeta 0x1A, VaultNameIndex 0x1B). **Schema version
bumps v3 → v4** (the biggest migration step yet). Legacy per-vault DBs archived (not deleted).

**Testing**: `go test -race`; a key-widening migration test (idempotency + v3→v4 transform); a
vault-isolation test (no cross-vault leakage); a cross-vault query parity test (RRF fusion); an
end-to-end multi-vault smoke (two vaults, one daemon, add/query/cross-query/rename/clear).

**Performance Goals**:
- Single-vault query latency unchanged (the wsPrefix is 8 bytes prepended — Pebble handles it
  natively; the prefix scan `[kind|wsPrefix, kind|wsPrefix+1)` is the same O(1) range scan).
- Cross-vault query ≤ 2× single-vault (parallel fan-out; RRF fusion is O(N×K) where N=vaults,
  K=pool-size; rerank budget capped).
- Vault rename <1ms (two Pebble key writes, zero data moves).
- Migration: O(total-keys) one-way rewrite — bounded by corpus size; per-step fsync.

**Constraints** (hard):
- ONE Pebble instance (one global writer — STRONGER single-writer invariant than today).
- Shared embedder + config (one Ollama model, one engine).
- Vault-scoped isolation (no cross-vault data leakage in single-vault operations).
- Fail-closed vault resolution (unrecognised vault rejected).
- Migration is one-way, idempotent, v3→v4 (refuse newer).
- Pure Go, single binary, no Node.

**Scale/Scope**: ~25–30 files. The biggest slice in the project by far. Touched: `internal/storage`
(keys package, vault.go, db.go, storage.go, migrate v4), `internal/engine` (every method),
`internal/cli`, `internal/rest`, `internal/grpc`, `internal/mcp`, `internal/ui`, `proto/`,
`internal/pipeline`, `internal/index`, `internal/embedproc`. Plan + tasks phase will break this
into implementation phases.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** | One Pebble instance, one binary, no cloud egress. STRONGER than today (one DB instead of N). |
| II | Content-Addressed Identity | **PASS (vault-scoped)** | Identity (SHA-256 content+metadata) becomes **per-vault** — the same content in two vaults is two distinct documents (two wsPrefix-scoped keys). Cross-vault dedup is explicitly DISABLED (correct: each vault is an independent corpus). Ingestion idempotency holds per-vault. |
| III | Pure Go — No CGo, No External Runtime | **PASS** | stdlib + Pebble + SipHash (pure Go). No new non-Go dependency. |
| IV | Async-After-ACK Writes | **PASS (stronger)** | One global single-writer (one Pebble = one lock for ALL vaults). Writes ACK <10ms (Pebble commit); embed/index async on background workers. STRONGER than today (today: per-vault locks; tomorrow: one global lock — fewer race surfaces). |
| V | Extension by Interface, MCP-First | **PASS** | Vault parameter threads through ALL five transports (CLI/MCP/REST/gRPC/UI). Every operation gains a vault param. Cross-transport parity maintained. |

**Storage Discipline** (the CRITICAL gate): the key-space layout FUNDAMENTALLY CHANGES — every
vault-scoped key widens by 8 bytes. This is a **numbered migration (v3 → v4)** in
`internal/storage/migrate`, with `ExpectedVersion` bumped from 3 to 4. The migration step iterates
each legacy per-vault DB, computes the vault's wsPrefix, and rewrites every key as
`kind|wsPrefix|payload` into the unified store. Per-step fsync. Idempotent (restart-safe). Refuse
newer (v5+ stores rejected by a v4 binary). Legacy per-vault directories archived (not deleted).
PRD §6.7 updated with the new key-space layout. **Schema-version impact: v3 → v4.**

**Gate verdict: PASS.** One Complexity Tracking entry (the storage-layout migration is the
tracked item — it's the constitution-sanctioned, migration-gated layout change, not a violation).

## Project Structure

### Documentation (this feature)

```text
specs/052-multi-vault-store/
├── plan.md              # This file
├── research.md          # Phase 0 (R1–R10 design decisions from MuninnDB extraction)
├── data-model.md        # Phase 1 (the key-space layout: vault-scoped vs global, wsPrefix scheme)
├── quickstart.md        # Phase 1 (multi-vault validation guide)
├── contracts/           # Phase 1 (vault-param flow through transports)
│   └── multi-vault.md
└── tasks.md             # Phase 2 (/speckit-tasks — NOT this command)
```

### Source Code (repository root)

```text
internal/storage/
  keys/                   # NEW package — pure key-construction funcs, ws [8]byte first arg.
                          # VaultPrefix(name) [8]byte (SipHash), SourceKey(ws, srcID),
                          # DocumentKey(ws, docID), ChunkKey(ws, chunkID), FTSPostingKey(ws,...),
                          # VaultMetaKey(ws), VaultNameIndexKey(name), IncrementWSPrefix(ws), etc.
  vault.go                # NEW — ResolveVaultPrefix(name), ListVaultNames, WriteVaultName,
                          # VaultNameExists, RenameVault, ClearVault, DeleteVaultNameOnly,
                          # BackfillVaultNames. Ported near-verbatim from MuninnDB (adapted to
                          # go-rag's prefix table).
  db.go                   # EDIT — SetWithPrefix/GetWithPrefix/PrefixScan gain ws-aware variants
                          # (or the keys-package builders replace them); Open opens ONE unified DB.
  storage.go              # EDIT — prefix constants gain vault-scoped/global annotation; add
                          # VaultMeta=0x1A, VaultNameIndex=0x1B.
  migrate/
    migrate.go            # EDIT — ExpectedVersion 3 → 4; add the v4 key-widening step.
    v4_multi_vault.go     # NEW — the migration: iterate legacy per-vault DBs, rewrite keys with
                          # wsPrefix, write registry indexes, archive legacy dirs.

internal/engine/
  engine.go               # EDIT — every public method gains `vault string` first arg (or a
                          # VaultRequest); resolves wsPrefix once at entry via ResolveVaultPrefix;
                          # threads [8]byte to storage + pipeline + indexes. Per-vault index
                          # registries (idxFts/idxVec become maps keyed by wsPrefix, lazily seeded).
                          # Shared embedder (unchanged — one Ollama model). Per-vault epoch counters.
  query.go                # EDIT — Query gains vault param; cross-vault fan-out + N-list RRF when
                          # QueryRequest.Vaults is non-empty.
  ingest.go               # EDIT — Add/Reprocess gain vault param.
  delete.go               # EDIT — DeleteDoc gains vault param.
  ...every engine file    # EDIT — vault param threaded through.

internal/index/
  retrieval.go            # EDIT — reciprocalRankFusion generalised from 2-list to N-list.

internal/cli/             # EDIT — --vault flag on every command (per-call, not DB-path selector).
internal/rest/            # EDIT — ?vault= query param + body field; VaultAuthMiddleware.
internal/grpc/            # EDIT — vault field on every proto request message.
internal/mcp/server.go    # EDIT — vault tool arg on every MCP tool.
internal/ui/              # EDIT — vault picker in the shell; vault header on /api/* requests.
proto/gorag.proto         # EDIT — string vault = N; on every request message.
proto/gen/                # REGEN.

internal/pipeline/        # EDIT — pipeline carries wsPrefix; OnChange/OnEvent callbacks vault-scoped.
internal/embedproc/       # EDIT — embed queue scanner is vault-aware (0x14|wsPrefix|chunkID).
internal/watcher/         # EDIT — watcher targets a vault NAME (resolved to wsPrefix), not a dir.
```

**Structure decision**: no new top-level package. The `internal/storage/keys` package is the one
new package (pure key-construction functions, mirroring MuninnDB's `keys/keys.go`). Everything
else is edits to existing packages. The migration step (`v4_multi_vault.go`) follows spec 034's
established pattern. The engine's per-vault index registries + the cross-vault fan-out are the
main new engine code.

## Complexity Tracking

| Item | Why Needed | Simpler Alternative Rejected Because |
|------|------------|-------------------------------------|
| Key-space layout change (v3 → v4 migration) | The unified store fundamentally widens every vault-scoped key by 8 bytes — this IS the change, not a deviation. The constitution's Storage Discipline rule REQUIRES a numbered migration for any key-space layout change. | No simpler alternative — the layout change is the point. MuninnDB's proven model requires it. The migration is one-way (not in production) and reuses spec 034's runner. |
