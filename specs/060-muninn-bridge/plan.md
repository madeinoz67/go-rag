# Implementation Plan: MuninnDB Bridge + Memory & Graph View

**Branch**: `060-muninn-bridge` | **Date**: 2026-08-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/060-muninn-bridge/spec.md`

## Summary

A bridge module embedded in the go-rag daemon that promotes document chunks into a local MuninnDB vault as content-addressed engrams (via the now-shipped UPSERT — `idempotent_id` + `upsert_mode`), and a console view that reads the resulting memory graph back. The bridge hooks the pipeline worker (`processJob`, the enrichment seam — chunks already in hand) to **non-blockingly enqueue** to a decoupled `bridgeProc` (the `embedProc` pattern), so a slow/down MuninnDB never stalls ingest; the bridgeProc writes to a loopback MuninnDB over gRPC (the repo's first outbound gRPC client), auto-backfills the existing corpus on first enable (storm-limited + pausable), and degrades gracefully when MuninnDB is absent. It is an egress exception to the local-first principle (PRD §2.2 carve-out, opt-in/loopback/never-core) — not a constitution amendment. v1 is **stateless** (no new go-rag keyspace): UPSERT-on-`idempotent_id` is the correctness layer.

**This plan builds on a complete prior architecture** — `docs/RFC-bridge-muninndb/bridge-muninn.md` (2026-06-25, maintainer-reviewed 2026-06-30) — and the research reconciliation in [research.md](research.md). It does not redesign what the RFC already settled; it updates the RFC for (a) the now-shipped UPSERT surface (stateless v1), (b) the auto-on-enable + storm-limit + pause decision, and (c) the new Memory & Graph view.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`).

**Primary Dependencies** (new): `google.golang.org/grpc` (already a dependency via the gRPC *server* — promoted to a client use), generated `muninn_v1` client stub vendored from `scrypster/muninndb/proto/gen/go/muninn/v1` (outbound only; go-rag never serves MuninnDB RPCs). Pure-Go, Apache-2.0, no CGo.

**Storage**: no new go-rag keyspace for v1 (stateless). The bridge reads chunks via the existing `internal/storage` API (`GetDocumentChunks`) and writes state to MuninnDB. An optional local perf cache, if ever needed, would allocate `0x20–0x22` (still free per `docs/internals/keyspace-registry.md`) with a numbered migration — out of scope for v1.

**Testing**: `go test -race ./...` (the repo standard); `golangci-lint(0)`. The cognitive-hygiene property (NFR-002) is a `Read`-before/after test against a fake `MuninnClient` that records calls (and a live-local-MuninnDB smoke in quickstart). Cross-transport parity is N/A (the bridge is one transport — gRPC outbound).

**Target Platform**: single binary, loopback-only egress to a local MuninnDB (`127.0.0.1:8477`). No remote.

**Project Type**: daemon extension — new `internal/bridge/muninn/` package + `internal/ui` view + CLI subcommands. No new binary/entrypoint.

**Performance Goals**: write-ACK `<10ms` unaffected (Principle IV — promotion fires off the event bus, async); promotion lag `<2s` from `EventEmbedded` under normal load (RFC target); backfill storm-capped (configurable max-in-flight + token-bucket, NFR-006).

**Constraints**: opt-in (default OFF); loopback-only enforced at config-validation; `mk_` key via `Authorization: Bearer` metadata, never in a URL/log; daemon shutdown drains/sheds promotion within the existing stop budget (NFR-005, the spec 045 embedproc-drain lesson).

**Scale/Scope**: single operator; vaults up to ~10k chunks. Backfill is resumable and storm-limited so a large vault never saturates MuninnDB.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Researched and verified in [research.md](research.md) R7. **Passes — no violations, no Complexity Tracking entries.**

| Principle | Verdict | Basis |
|---|---|---|
| I. Local-First, Single-Binary | ✅ (PRD §2.2 carve-out, not an amendment) | Bridge is opt-in, background, loopback-only, never a core op. Ingest/index/query never depend on it. Single binary — bridge is goroutines + a gRPC client inside the daemon. |
| II. Content-Addressed Identity | ✅ | `idempotent_id = "chunk:"+chunkID` derives from chunk content identity — this is why the UPSERT no-op works and why changed chunks create rather than evolve. |
| III. Pure Go — No CGo | ✅ | `grpc-go` + generated `muninn_v1` stub, pure Go, Apache-2.0. No C deps. |
| IV. Async-After-ACK Writes | ✅ | Promotion fires off `EventEmbedded` on the event bus; `Publish` is non-blocking and runs after the durable status write-back (confirmed: `engine.go:261` binds `OnEvent → bus.Publish`). `<10ms` ACK budget unaffected. |
| V. Extension by Interface, MCP-First | ✅ | `MuninnClient` is a transport-agnostic interface (testable with a fake); the view is a UI adapter; the bridge is a bus subscriber (decoupled, like enrich). |

**Keyspace / schema-version impact**: **none for v1** (stateless — no new prefix, no migration, no `ExpectedVersion` bump). Documented in research.md R2 + R7. The plan's tasks MUST affirm "no on-disk layout change" per the storage-discipline compliance rule.

**PRD §2.2 revision** (an implementation task, not a gate): add the opt-in loopback-bridge carve-out alongside N4 (enrichment) and N7 (console).

## Project Structure

### Documentation (this feature)

```text
specs/060-muninn-bridge/
├── plan.md              # This file
├── research.md          # Phase 0 — the reconciliation (UPSERT surface, RFC, invariants)
├── data-model.md        # Phase 1 — entities, events consumed, WriteRequest mapping
├── quickstart.md        # Phase 1 — E2E validation runbook
├── contracts/
│   ├── muninn-grpc-client.md  # outbound gRPC contract (Write/BatchWrite/Read/Hello/Link)
│   └── ui-rest.md             # GET /api/memory/graph + bridge status endpoints
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

Adopts the RFC layout (`docs/RFC-bridge-muninndb/bridge-muninn.md` § Architecture), updated for stateless v1.

```text
internal/
├── bridge/
│   └── muninn/
│       ├── client.go        # MuninnClient interface + gRPC impl (Bearer interceptor, loopback dialer, backoff, health) — first outbound gRPC client in the repo
│       ├── processor.go     # bridgeProc: decoupled worker pool, bounded semaphore + token bucket, circuit breaker, bounded drain (embedProc pattern)
│       ├── bridge.go        # Coordinator: Enqueue(ws,docID,vault,chunks,mode) + lifecycle (start/stop/pause), started gated by EffectiveBridgeEnabled()
│       ├── mapper.go        # Chunk + DocumentMeta → muninn_v1.WriteRequest (embedding:nil, stability:30.0, idempotent_id="chunk:"+chunkID, upsert_mode=true)
│       ├── concept.go       # Rule-based concept cascade (section_heading → title → filename → first-60)
│       └── bridge_test.go   # NFR-002 cognitive-hygiene test (Read before/after via fake client), circuit-breaker + drain tests
├── pipeline/workers.go      # EDIT — processJob: 2-line `p.bridge.Enqueue(...)` hook (non-blocking; nil-guarded like enricher)
├── engine/engine.go         # EDIT — pipeline(): bind SetBridge + start bridgeProc; Close(): bridgeProc.Stop() between bus.Close() and embedProc.Stop()
├── config/config.go         # EDIT — flat Bridge* fields + EffectiveBridgeEnabled() + loopback Validate
├── ui/
│   ├── memory_graph.go      # NEW — GET /api/memory-graph/* (Activate-driven browse + Read detail; NOT named bridge* — avoids the bridgeops collision)
│   └── placeholder.go       # EDIT — delete the "memory-graph" entry (retires the last placeholder)
└── cli/
    └── bridge.go            # NEW — `go-rag bridge muninn {init,status,pause,resume}` subcommands

proto/
└── muninn/v1/               # NEW — vendored read-only client stub from scrypster/muninndb (outbound only; verify no transitive CGo)

docs/internals/
└── PRD_RAG_Database.md      # EDIT — §2.2 carve-out (N-bridge: opt-in loopback egress)
```

**Structure Decision**: a new `internal/bridge/muninn/` package. The seam is `Pipeline.processJob` (the enrichment seam — chunks already in hand) enqueueing to a decoupled `bridgeProc` (the `embedProc` pattern, NOT enrich-inline — the bridge calls an external process that may be down, so it must not block the pipeline worker). Config is flat `Bridge*` fields (codebase precedent; no nested object). The view is a new file in `internal/ui` (named `memory_graph.go`, not `bridge*`, to avoid the spec 049 bridgeops collision). The MuninnDB proto is vendored read-only (generated client stub only; go-rag never serves it). No new binary, no second Pebble instance, no new keyspace.

## Complexity Tracking

> None — Constitution Check passes with no violations to justify.

## Open Items for `/speckit-tasks`

Research (research.md R9) resolved the seam/config/UI/gRPC questions. Remaining for tasks:

- Storm-limit defaults (`BridgeMaxInFlight`, `BridgeRatePerSec`) — pick sensible values; defaults in the config layer.
- On-query Hebbian hook: ship v1 with the seam stubbed but disabled (it touches `SearchWithRerank`); full on-query is a follow-up.
- Vendor mechanism for the `muninn_v1` stub: copy + a regen-check vs. `go.mod replace` to a local checkout (the stub is generated, manually maintained upstream per muninndb's drift note).
- Decide `bridgeProc` drain-order in `Engine.Close` (before `pipe.Close()` since it reads `j.chunks`) and the drain-timeout value (mirror embedproc's 5s).
- Loopback validation: `net.ParseIP` at config-time + `grpc.WithContextDialer` refusal at dial (defense vs DNS rebinding).
