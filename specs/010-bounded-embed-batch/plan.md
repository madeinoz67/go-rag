# Implementation Plan: Bounded Embedding Batches (H12)

**Branch**: `main` (single-author repo; Spec Kit work commits directly to `main` per project convention) | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/010-bounded-embed-batch/spec.md` (audit backlog item **H12**).

## Summary

Cap how many texts a single embedding request carries, so a large document (hundreds–thousands of chunks) no longer becomes one oversized request that OOMs or times out. The change lives entirely inside `Ollama.Embed` (`internal/embed/ollama.go`): it splits the input texts into fixed-size batches (default 32), runs the existing retry-with-backoff loop **per batch**, and returns the concatenated vectors in input order. The `Embedder` interface, the pipeline caller, the query path, and every consumer are unchanged — batching is an internal transport detail. Failure semantics are preserved: any batch that fails permanently fails the whole call (the pipeline already marks the document errored and stores no vectors, so no partial index).

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4). Pure Go, built `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib only — `net/http`, `encoding/json`, `context`, `time` (all already imported by `internal/embed/ollama.go`). **No new dependencies.**

**Storage**: Pebble KV — **N/A**. Batching is transport-layer (how texts reach the local embedding service); it does not touch stored keys, the embedding record shape (`storedEmbedding`, prefix 0x04), or identity hashes.

**Testing**: `go test -race -cover ./...` (`make test`). The existing `internal/embed/ollama_test.go` drives `Ollama.Embed` against in-process `httptest` Ollama stand-ins (`TestEmbed_FakeServer`, `TestEmbed_RetriesOn5xxThenSucceeds`, `TestEmbed_EmptyInput`) — the same pattern extends to batch-boundary / multi-request / per-batch-retry / order tests.

**Target Platform**: Local single binary; local Ollama `/api/embed`. No network egress.

**Project Type**: CLI + multi-transport server. This feature is in the `internal/embed` subsystem on the **async ingest path** (background workers, after the write ACK).

**Performance Goals**: Write-ACK < 10ms is **untouched** (embedding is post-ACK async work). Small documents (sub-cap) MUST incur no measurable overhead vs. today (one request). Large documents MUST now complete instead of timing out; per-request time and peak memory MUST be bounded by the cap, not by chunk count.

**Constraints**: Pure Go (no CGo); `Embedder` interface unchanged; existing retry/backoff semantics (3 attempts, exponential backoff, 5xx-retry/4xx-fail-fast, ctx-respecting) preserved and applied per batch; vectors returned in input order; no partial results on failure.

**Scale/Scope**: Local vaults; documents can split into hundreds–thousands of chunks — exactly the regime where the unbounded single request fails today.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | Batching is in-process; calls the same local Ollama `/api/embed`. No new binary, cloud, or egress. |
| II | Content-Addressed Identity | ✅ Pass | No change to identity/change hashes or the embedding record. Embed returns vectors; the pipeline stores them keyed by chunk ID exactly as today. |
| III | Pure Go — No CGo | ✅ Pass | stdlib only; no new dependency of any kind. |
| IV | Async-After-ACK Writes | ✅ Pass (core alignment) | Embedding IS the async-after-ACK work. Bounding it protects the async path from OOM/timeout **without** touching the <10ms durable-write ACK. This is the principle H12 most directly serves. |
| V  | Extension by Interface, MCP-First | ✅ Pass | The `Embedder` interface and every caller (pipeline ingest, query path, tests, future providers) are unchanged. Batching is internal to the Ollama implementation. |

**No violations.** Complexity Tracking table below is empty.

## Project Structure

### Documentation (this feature)

```text
specs/010-bounded-embed-batch/
├── plan.md                 # This file
├── research.md             # Phase 0 — cap value, batching site, retry/failure/order semantics
├── data-model.md           # Phase 1 — embed-batch entity + preserved result contract
├── quickstart.md           # Phase 1 — end-to-end validation scenarios
├── contracts/
│   └── embed-batch.md      # Phase 1 — the preserved Embedder.Embed contract (US3 invariant)
└── tasks.md                # (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/embed/
├── ollama.go               # Embed: split into batches → per-batch retry → concatenate (in order)
└── ollama_test.go          # batch-boundary, per-batch transient retry, permanent fail, order, empty/sub-cap
```

**Structure Decision**: This feature touches **one source file** (`internal/embed/ollama.go`) plus its tests. The pipeline call site (`internal/pipeline/workers.go:48`, `p.embed.Embed(ctx, docTexts)`) is **intentionally unchanged** — batching belongs in the transport layer (the audit says so, and it keeps the pipeline, query path, and every consumer unaffected per FR-009). No new packages, no storage change, no interface change.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

*(Empty — Constitution Check passes cleanly on all five principles. No violations to justify.)*
