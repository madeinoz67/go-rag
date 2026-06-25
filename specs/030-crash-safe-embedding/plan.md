# Implementation Plan: Crash-Safe Background Embedder

**Branch**: `030-crash-safe-embedding` (commits to `main` directly — single-author repo; see `CLAUDE.md`) | **Date**: 2026-06-25 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/030-crash-safe-embedding/spec.md` (the MuninnDB approach). Adopts MuninnDB's embedder-as-retroactive-processor model: the embed step moves off the per-ingest `processJob` to a self-healing background processor with a durable pending-embed queue — closing go-rag's crash-recovery gap (a SIGKILL mid-embed no longer orphans documents) and adding cross-document batching + circuit-breaker resilience.

## Summary

Today embedding is async-after-ACK (Constitution IV) but **coupled to the per-ingest
`processJob`** via an in-memory queue (`chan job`). A crash between ACK and the embedding
landing loses the queued job — the chunk is durable (0x03) but vector-less (no 0x04),
silently invisible to semantic search until manual re-ingest. This feature adopts
MuninnDB's model (`cmd/muninn/server.go` → `NewRetroactiveProcessor(store, embedPlugin,
DigestEmbed)`):

1. **A durable pending-embed queue** under a new prefix **`0x14`** (`PrefixEmbedQueue`):
   `processFile` writes the chunk (0x03) + a pending record (0x14) **in one atomic batch**,
   then ACKs — "the DB is the queue" (research R1).
2. **A background embedder** (`internal/embedproc`) — the **sole writer of 0x04**. On
   `Start()` it runs an **initial 0x14 scan** (crash recovery), then drains the queue on
   Notify + a 3s poll, **micro-batching across documents** (≤ MaxBatchSize), guarded by a
   **circuit breaker** (5/30s, the spec-029 primitive). It writes 0x04
   `{model,convention,vector}`, `vec.Add`s, bumps the **index epoch** (H06), and removes
   the 0x14 record (research R3/R4).
3. **`processJob` loses its embed role** (keeps FTS/near-dup/enrich/status) — the embedder
   is now the only embedder (research R2, full-decoupled).
4. **Status** surfaces `embed_pending` + `embed_failed` (the 0x14 backlog) — the only
   external-visible change (research R6).

The refactor is **structural** (FR-008): the 0x04 record shape, `vec.Add`, epoch bump,
H07 prefix, and H03 guard are identical to today — only the embed scheduling + crash-
recovery move. Delivered in two task-phases: **A** = queue + embedder + crash-safety
(US1); **B** = cross-doc batching + circuit breaker (US2).

Full design rationale: [research.md](./research.md) (R1–R6). Entity/lifecycle detail:
[data-model.md](./data-model.md). Design contract: [contracts/embedder.md](./contracts/embedder.md).
Validation runbook: [quickstart.md](./quickstart.md).

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/madeinoz67/go-rag`; `CGO_ENABLED=0`, PRD §10.4).

**Primary Dependencies**: none added. Reuses the existing embedder/Ollama client, the
spec-029 circuit-breaker primitive, and Pebble. Pure-Go (Constitution III).

**Storage**: one new prefix **`0x14`** (`PrefixEmbedQueue`) — the durable pending-embed
queue. Existing prefixes (0x03 chunks, 0x04 embeddings) unchanged; 0x04 remains the
authoritative embedding store.

**Testing**: `go test -race -cover ./...` (`make test`). New: `internal/embedproc`
(processor unit tests: recovery scan, batching, circuit breaker, idempotency), a
kill-restart integration test (SC-001), and the spec-004 retrieval-eval harness
(no-regression — SC-005, recall identical).

**Target Platform**: single statically-linked binary, local-first (Constitution I/III).

**Project Type**: single-binary local RAG database + multi-transport daemon (CLI/MCP/REST/gRPC).

**Performance Goals** (Constitution): write ACK <10ms — the atomic 0x03+0x14 write
replaces today's 0x03 write and is equally fast (FR-002). Embedding latency is unchanged
(the embedder Notify-makes it as prompt as processJob today); cross-doc batching improves
bulk-ingest throughput.

**Constraints**: crash-safe (FR-001 — structural: atomic 0x03+0x14 + startup scan),
async-after-ACK (FR-002), decoupled (FR-003), circuit-breaker (FR-004), cross-doc batch
(FR-005), idempotent (FR-006), output-identical (FR-008).

**Scale/Scope**: one new prefix + queue type, one new `internal/embedproc` package
(processor + circuit breaker reuse), the `processFile` atomic-write change, removing the
embed role from `processJob`, daemon/CLI startup wiring, two status fields (4 transports).
Touches `internal/{storage,pipeline,engine,embedproc,daemon,cli,rest,grpc,mcp}`. The
biggest core-path refactor in the audit — phased (A: crash-safety, B: throughput).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.0.0 (five principles). All five
PASS — no violations, so the Complexity Tracking table is empty.

| # | Principle | Verdict | Justification (grounded) |
|---|-----------|---------|--------------------------|
| I | Local-First, Single-Binary | ✅ PASS | Reuses the local Ollama embedder + Pebble; no network/cloud. Single `CGO_ENABLED=0` binary, no new dependency. |
| II | Content-Addressed Identity | ✅ PASS | The 0x14 queue record is a work-item pointer (chunkID → status), not identity. Chunk/document identity (GenerateID, ContentHash) is untouched; re-embedding is idempotent (FR-006). |
| III | Pure Go — No CGo | ✅ PASS | stdlib + existing deps only; the processor + circuit breaker are pure Go. |
| IV | Async-After-ACK Writes | ✅ PASS | **Honoured, not weakened** — the atomic 0x03+0x14 write is the ACK-time work (<10ms); embedding stays strictly background. This feature *strengthens* IV: it removes the crash-window where an ACK'd doc could lose its embedding. |
| V | Extension by Interface, MCP-First | ✅ PASS | The embedder is a modular `internal/embedproc` package (mirrors `internal/enrich`); no new MCP surface (the two status fields ride the existing status tool). The embedder interface reuse keeps the core closed. |

**Post-design re-check** (after `data-model.md` / `contracts/embedder.md`): one new
prefix (0x14), no identity change, no new dependency, ACK budget preserved, output
shape identical. The five verdicts are unchanged. **Gate: PASS.**

## Project Structure

### Documentation (this feature)

```text
specs/030-crash-safe-embedding/
├── spec.md              # Feature spec
├── plan.md              # This file
├── research.md          # Phase 0 — R1–R6 (mechanism, decoupled model, processor, breaker, migrate, status)
├── data-model.md        # Phase 1 — 0x14 queue + embedder lifecycle
├── quickstart.md        # Phase 1 — SC-001..005 (kill-restart etc.)
├── contracts/
│   └── embedder.md      # Phase 1 — internal processor contract + the 2 status fields
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

A new `internal/embedproc` package + the queue prefix + the processFile/processJob split.
No new `main`:

```text
internal/
├── storage/storage.go       # PrefixEmbedQueue = 0x14 + Put/Get/ScanEmbedQueue helpers (R1)
├── embedproc/               # NEW: the background embedder (R3) — processor + circuit-breaker reuse + cross-doc batch (R4)
├── pipeline/
│   ├── pipeline.go          # processFile: atomic 0x03+0x14 write + Notify(embedder) (R1)
│   └── workers.go           # processJob: REMOVE the embed role (keep FTS/near-dup/enrich/status) (R2)
├── engine/                  # constructs + starts the embedder (daemon + CLI one-shots); binds OnChange/index handles; status fields (R3/R6)
├── daemon/                  # starts the embedder for the daemon lifetime + drain on shutdown (R3)
├── rest/ ├── grpc/ ├── mcp/ ├── cli/   # embed_pending + embed_failed on status (4 transports) (R6)
```

**Structure Decision.** Every directory maps 1:1 to a PRD subsystem. The embedder lives
in a new `internal/embedproc` package (keeps the pipeline an ingest orchestrator —
Principle V; the processor owns embed scheduling/recovery, mirroring how `internal/enrich`
owns enrichment). The 0x14 prefix follows the single-Pebble prefix-partitioned rule.
`processJob`'s embed logic (prefixing, 0x04 shape, epoch bump) **moves** to the embedder
verbatim (FR-008) — not duplicated. No new `main`, single binary entrypoint untouched.

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

*No violations.* The Constitution Check gate PASSES on all five principles (and
strengthens IV — the crash-window an ACK'd doc could lose its embedding is removed).
This table is intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
