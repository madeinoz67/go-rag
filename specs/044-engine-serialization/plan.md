# Implementation Plan: Engine Write-Operation Serialization

**Branch**: `044-engine-serialization` | **Date**: 2026-07-03 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/044-engine-serialization/spec.md`. Technical design: [`docs/design/engine-serialization.md`](../../docs/design/engine-serialization.md) (per-document lock + non-blocking queue push, 12-agent RedTeam-validated).

## Summary

Two changes ship together: (1) a **per-document lock** (`sync.Map[docID]*sync.Mutex`) that serializes same-docID operations (fixing the concurrent-DeleteDoc / reingestDocs suppression race — spec 043's HIGH adversarial-review finding), and (2) a **non-blocking queue push** (detached sender) in `processFile` that eliminates the blocking `p.queue <- job{...}` send (the forward-progress stall that defeated the broad `opMu` approach). The two are inseparable: the doc-lock covers `ReingestPath`'s full `capture → DeleteDoc → Ingest` critical section, and the non-blocking push ensures the lock-holder never stalls on a full queue.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`), pure Go.

**Primary Dependencies**: existing only — `sync` (Map, Mutex). No new dependencies.

**Storage**: Pebble KV — NO new prefix, NO migration. The `sync.Map` is in-memory only.

**Testing**: `go test -race -cover ./...`. New: a concurrent same-docID test (two goroutines on the same docID → no double event), a full-queue test (fill the queue → processFile returns immediately).

**Target Platform**: cross-platform single binary.

**Project Type**: CLI + multi-transport server. This is an **internal fix** — no new transport surface, no proto change, no CLI change.

**Performance Goals**: `processFile` returns within <10ms even with a full queue (the detached sender offloads the blocking send). Doc-lock contention is per-docID (unrelated docs run concurrent).

**Constraints**: pure Go; no new storage prefix (no migration); no proto change; the lock ordering `docLock → p.mu` must be acyclic; `Close` must not contend for doc-locks.

**Scale/Scope**: **size S** — one new `sync.Map` field + a `docLock` helper + lock acquisitions in 4 methods + a 3-line queue-push change. The design was 3-agent researched + 12-agent RedTeamed.

## Constitution Check

*GATE: Must pass before Phase 0 research.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | In-process mutex + goroutine. No network egress. |
| **II. Content-Addressed Identity** | ✅ PASS | No identity change. The doc-lock is keyed by the existing docID. |
| **III. Pure Go — No CGo** | ✅ PASS | `sync.Map` + `sync.Mutex` — stdlib only. |
| **IV. Async-After-ACK Writes** | ✅ PASS *(enhanced)* | The non-blocking push **strengthens** Principle IV: `processFile` currently blocks on `p.queue <- job{...}` (a blocking send AFTER the ACK). The detached sender makes this non-blocking — `processFile` returns even faster. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | No new operation, no transport change. Internal fix only. |
| **Storage discipline / Schema evolution** | ✅ PASS | No new prefix, no migration, no key-construction change, `migrate.ExpectedVersion` unchanged. |

**Compliance statement**: All five principles pass (IV is enhanced). No schema-version impact. No violations → no Complexity Tracking entries.

## Project Structure

### Documentation (this feature)

```text
specs/044-engine-serialization/
├── plan.md              # this file
├── research.md         # Phase 0 — the 3-agent research + the RedTeam resolution
├── data-model.md       # Phase 1 — docLock, detached sender
├── quickstart.md        # Phase 1 — concurrent-same-doc + full-queue validation
└── tasks.md            # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root — files touched)

```text
internal/pipeline/pipeline.go     # docLocks sync.Map field + the non-blocking queue push
internal/pipeline/reingest.go     # docLock helper + lock in captureReingest/takeReingest callers
internal/pipeline/delete.go       # docLock in DeleteDoc
internal/pipeline/reprocess.go    # docLock around captureReingest+DeleteDoc in Reprocess/ReprocessAll
internal/pipeline/reingest.go     # docLock across ReingestPath's full critical section
```

**Structure Decision**: the `docLock` helper + the `docLocks` map live on the Pipeline struct (they guard pipeline-level state: the reingest map, reingestDocs set, and the capture/delete/ingest flow). The non-blocking push is a 3-line change at the end of `processFile`. No engine-level changes (the serialization is at the pipeline level, where the race actually lives).

## Complexity Tracking

> None — the Constitution Check passes with no violations.
