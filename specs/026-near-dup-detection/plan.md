# Implementation Plan: Near-Duplicate Chunk Detection

**Branch**: `026-near-dup-detection` (commits to `main` directly — single-author repo; see `CLAUDE.md`) | **Date**: 2026-06-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/026-near-dup-detection/spec.md` (audit finding **H20**, Phase 6 §1.1). Adds fuzzy near-duplicate detection at chunk granularity so retrieval results can be de-duplicated of revisions / copy-pastes / editor re-saves that exact content-hash dedup misses.

## Summary

Detect **near-duplicate chunks** (high text similarity, not byte-identical) at
ingest and surface the relationships so query results can collapse redundant
passages. Today identity is exact (`ContentHash = SHA-256(raw)`,
`pipeline.go:211-216`) — a typo-fix revision, a copy-pasted section, or an editor
re-save is ingested as independent content, polluting the top-k with the same
passage repeated.

The fix is surgical and follows the established sidecar pattern (`Poisoning`
spec 019, `SectionContext` spec 025):

1. **SimHash fingerprint per chunk** (64-bit, pure stdlib — `crypto/sha256` +
   `math/bits`), computed over the chunk's indexed text on the ACK path and
   stored under a new Pebble prefix **`0x13`** (chunkID → fingerprint).
2. **Pairwise sibling clustering async-after-ACK** (in the existing
   `processJob` worker, alongside embed + BM25): scan `0x13`, find chunks within
   Hamming distance `k`, write a `NearDup *NearDupInfo` sidecar onto the chunk
   record — a **non-identity sidecar** that never enters `cid`/`GenerateID`
   (FR-003).
3. **Opt-in query-time collapse** (`dedup` flag, default off): post-ranking in
   `engine.Query`, keep the highest-scored representative per near-dup group.
4. **Surface on all transports**: `near_dup` on `QueryHit` (CLI/REST/gRPC/MCP),
   `near_dup_chunks` on status — identical everywhere (FR-004).

The chunker, embeddings, identity, and default query behaviour are all unchanged
(FR-007); collapse is purely opt-in and subtractive.

Full design rationale: [research.md](./research.md) (R1–R10). Entity/field
detail: [data-model.md](./data-model.md). Wire contract:
[contracts/api.md](./contracts/api.md). Validation runbook:
[quickstart.md](./quickstart.md).

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/madeinoz67/go-rag`; `CGO_ENABLED=0`, PRD §10.4).

**Primary Dependencies**: cobra, pebble, chromem-go, grpc-go, protobuf — all
pure-Go (Constitution III). **No new dependencies** (SimHash is stdlib
`crypto/sha256` + `math/bits`).

**Storage**: single Pebble instance, prefix-partitioned (`storage/storage.go`).
Chunks under `0x03`; new fingerprint index under **`0x13`** (`PrefixNearDup`).
No second database, no sidecar file (PRD §6.7).

**Testing**: `go test -race -cover ./...` (`make test`). Key suites this feature
extends: `internal/pipeline/*_test.go` (fingerprint + clustering), a new
`internal/near` package for the SimHash/clustering unit tests,
`internal/engine/parity_test.go` (cross-transport `near_dup` + `dedup` parity),
and the spec-004 retrieval-eval harness (no-regression with `dedup` off,
redundancy-drop with `dedup` on — SC-004).

**Target Platform**: single statically-linked binary, local-first (Constitution
I/III). No platform change.

**Project Type**: single-binary local RAG database + multi-transport daemon
(CLI/MCP/REST/gRPC).

**Performance Goals** (Constitution, unchanged): write ACK <10 ms; query <500 ms
hybrid / <50 ms keyword. The SimHash is µs/chunk on the ACK path; clustering is
O(n) per chunk **async** (R4) — the <10 ms budget is preserved. Collapse is
O(k²) over the small top-k at query time — negligible.

**Constraints**: Local-First (no network/LLM — FR-002), Pure-Go/No-CGo,
idempotent ingestion (FR-003), chunk/embedding geometry frozen (FR-007),
collapse must be opt-in + non-destructive to ranking (FR-005/007).

**Scale/Scope**: one new sidecar field (`Chunk.NearDup`) + type, one new `0x13`
prefix + fingerprint, one SimHash/clustering helper package, one collapse pass
in `engine.Query`, `dedup` flag across 4 transports, status count. Touches
`internal/{storage,model,pipeline,engine,rest,grpc,mcp,cli}`, `proto/`. Targets
local <10K-doc corpora (brute-force fingerprint scan is fine — audit §1.1).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.0.0 (five principles). All
five PASS — no violations, so the Complexity Tracking table is empty.

| # | Principle | Verdict | Justification (grounded) |
|---|-----------|---------|--------------------------|
| I | Local-First, Single-Binary | ✅ PASS | SimHash is pure-Go stdlib (`crypto/sha256` + `math/bits`); no LLM, no network, no cloud (FR-002). Single `CGO_ENABLED=0` binary, no new runtime dep. |
| II | Content-Addressed Identity | ✅ PASS | `NearDup` is a **non-identity sidecar** (like `Poisoning`/`SectionContext`); it does not enter chunk `cid` (`pipeline.go:252`) or document `GenerateID` (`model.go:46`). The fingerprint lives under `0x13`, not in document metadata. Content-hash dedup (`pipeline.go:214`) is untouched → re-add stays a no-op (FR-003). |
| III | Pure Go — No CGo | ✅ PASS | SimHash + popcount are stdlib; no C deps, no new modules. |
| IV | Async-After-ACK Writes | ✅ PASS | The µs-per-chunk SimHash runs on the ACK path (rides the chunk record's existing `Sync`, zero added fsync); the O(n) **sibling-clustering scan runs async** in `processJob` (R4) alongside embedding/BM25 — symmetric with the existing async-after-ACK model. <10 ms ACK preserved. Near-dup info is eventually consistent (same window as keyword/vector search). |
| V | Extension by Interface, MCP-First | ✅ PASS | No `FileReader` or `Embedder` interface change. `near_dup` + `dedup` surfaced on MCP like every other hit field/query flag (R6). The pure-Go detector is a new internal package, no core interface churn. |

**Post-design re-check** (after `data-model.md` / `contracts/api.md`): no new
entity, only one new `0x13` prefix, no interface-signature change, no identity
change, no ACK-path reordering (clustering is async). The five verdicts are
unchanged. **Gate: PASS.**

## Project Structure

### Documentation (this feature)

```text
specs/026-near-dup-detection/
├── spec.md              # Feature spec (/speckit-specify)
├── plan.md              # This file (/speckit-plan)
├── research.md          # Phase 0 — design decisions R1–R10
├── data-model.md        # Phase 1 — Chunk.NearDup, 0x13 fingerprint, flow
├── quickstart.md        # Phase 1 — validation runbook (SC-001..005)
├── contracts/
│   └── api.md           # Phase 1 — near_dup + dedup wire contract, 4 transports
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

Additive edits to existing packages + one new internal package; no new `main`:

```text
internal/
├── near/                    # NEW: pure-Go SimHash + clustering (R1/R8)
│   ├── simhash.go           #   SimHash(text) uint64; HammingNear(a,b,k) bool
│   └── simhash_test.go      #   near-dup on revisions/copy-paste; distinct not flagged; short-chunk skip (R10)
├── storage/
│   └── storage.go           # PrefixNearDup = 0x13 (R5); Put/Get/ScanNearDup helpers (mirror quarantine)
├── model/
│   └── model.go             # NearDupInfo type + Chunk.NearDup *NearDupInfo (non-identity sidecar, R3)
├── pipeline/
│   ├── pipeline.go          # compute SimHash on ACK path, write 0x13 (R4/R5)
│   └── workers.go           # async sibling-clustering in processJob → putChunk NearDup (R4)
├── engine/
│   ├── types.go             # QueryHit.NearDup; QueryRequest.Dedup; StatusInfo.NearDupChunks
│   ├── query.go             # copy c.NearDup on hits; opt-in post-ranking collapse when Dedup (R7)
│   └── status.go            # near_dup_chunks count
├── rest/
│   └── types.go             # queryHit.near_dup; queryRequest.dedup; statusResponse.near_dup_chunks
├── grpc/
│   └── engine_adapter.go    # map NearDup + Dedup to proto
├── mcp/
│   └── server.go            # render near_dup; pass dedup
├── cli/
│   └── query.go             # --dedup flag; near_dup render line/field
proto/
├── gorag.proto              # QueryHit.near_dup = 10; NearDup message; QueryRequest.dedup = 14; StatusResponse count
└── gen/                     # regenerated
```

**Structure Decision.** Every directory maps 1:1 to a PRD subsystem (per
`CLAUDE.md`). The feature is a cross-cutting additive change along the existing
ingest → store → query → transport path — the same path `Poisoning` (spec 019)
and `SectionContext` (spec 025) followed. The only new package is `internal/near`
(the pure-Go SimHash/clustering helper), keeping the detector testable in
isolation and the pipeline/engine free of fingerprint math (Principle V — the
pipeline orchestrates, the helper owns the algorithm). No new `main`, single
binary entrypoint (`cmd/go-rag`) untouched.

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

*No violations.* The Constitution Check gate PASSES on all five principles. This
table is intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
