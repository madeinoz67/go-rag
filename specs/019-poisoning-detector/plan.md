# Implementation Plan: Retrieval Poisoning Defense — Ingest-Time Injection Detection

**Branch**: `019-poisoning-detector` *(spec directory; single-author repo — commits directly to `main`)* | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/019-poisoning-detector/spec.md` — backlog item **H04** (last remaining P0).

## Summary

Close the indirect-prompt-injection blind spot (audit §1.8 / book §11.3) by scoring every
ingested chunk for injection-poisoning risk at ingest time, persisting a per-chunk
`PoisoningVerdict`, **quarantining flagged chunks out of query results by default**, and
surfacing the verdict on every transport (CLI/REST/gRPC/MCP) so downstream LLM consumers can
treat retrieved text as untrusted. Detection is a new pure-Go `PoisoningDetector` interface
(default heuristic scorer: repetition + keyword/phrase-stuffing + instruction-phrase match),
wired into the ingest pipeline's synchronous store path (validation-class cost, ≈ the SHA-256
already computed there), with a non-destructive override and a corpus re-scan over the
reprocess path. No new dependency; CGO-free.

## Technical Context

**Language/Version**: Go 1.22+ (pure Go, `CGO_ENABLED=0`).

**Primary Dependencies**: existing only — cobra (CLI), pebble (KV), chromem-go (vectors),
fsnotify (watcher). **No new dependency** (Constitution III) — detection uses stdlib
(`strings`, `unicode`, `regexp`) only.

**Storage**: single Pebble instance, prefix-partitioned key space. Verdict stored **on the
chunk record** (free — rides the existing chunk-store batch/fsync) plus a new secondary
`0x11` quarantine index prefix (key = chunkID) for O(quarantined) listing. Confirm next-free
prefix against `internal/storage` constants in tasks.

**Testing**: `go test -race -cover ./...` + deterministic scorer unit tests (fixed payloads →
fixed verdicts) + `make test-eval` regression gate (recall@10 unchanged — clean golden set is
unaffected by quarantine). Cross-transport parity test for verdict surfacing (spec 006 pattern).

**Target Platform**: single static binary, cross-platform (Linux/macOS/Windows), local-first.

**Project Type**: CLI + multi-transport daemon (MCP :7878 / REST :7879 / gRPC :7880) over one
`internal/engine.Engine`.

**Performance Goals**: <10ms write-ACK preserved (Constitution IV); <5ms/chunk scoring
(SC-003); <500ms hybrid query unaffected.

**Constraints**: pure Go (no CGo); single Pebble writer; loopback-only; <25MB binary; <50MB
idle memory; detection is heuristic defense-in-depth, **not** a guarantee (documented).

**Scale/Scope**: local single-user, <10K docs. Brute-force scoring per chunk is fine at this
scale.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I | Local-First, Single-Binary | ✅ PASS | Detection is in-process pure-Go heuristics; no cloud/network egress; no new runtime dep; one static binary unchanged. |
| II | Content-Addressed Identity | ✅ PASS | Verdict is a pure deterministic function of chunk text → identical content always yields an identical verdict; re-ingest/re-score is a no-op. Identity & content hashes unchanged. |
| III | Pure Go — No CGo, No Runtime | ✅ PASS | Scorer uses stdlib only (`strings`/`unicode`/`regexp`). **Must verify** no new module added in `go.mod` during tasks. |
| IV | Async-After-ACK Writes | ✅ PASS (validation-class) | IV pushes *embedding/BM25/vector indexing* off the ACK path — it explicitly keeps *validation* on it ("validate, commit, ACK"). Heuristic text-scoring is validation-class: pure CPU on already-in-memory text, cost ≈ the SHA-256 content hash already computed synchronously today, no I/O, sub-ms/chunk. Verdict persists in the **same batch** as the chunk (one fsync). No new async obligation. (Async scoring evaluated & rejected — see research.md D2.) |
| V | Extension by Interface, MCP-First | ✅ PASS | `PoisoningDetector` is a new self-registering interface (mirrors `Reranker`/`QueryTransformer`/`FileReader`); verdict surfaced on **all four** transports (CLI/REST/gRPC/MCP) with cross-transport parity. |

**Principle I note (threat import, FR-013/D12)**: network egress occurs ONLY inside the
explicit, user-initiated `threat import <url>` — a discrete maintenance action, never a runtime
dependency of detect/query. Core operations stay air-gapped (the ClamAV/freshclam-split model,
not a live feed). No Constitution I violation.

**No violations → Complexity Tracking table intentionally empty.** Principle IV is the
borderline one; the rationale above keeps it a PASS, not a justified violation.

## Project Structure

### Documentation (this feature)

```text
specs/019-poisoning-detector/
├── plan.md              # This file
├── research.md          # Phase 0 — detection design decisions
├── data-model.md        # Phase 1 — PoisoningVerdict entity + state machine
├── quickstart.md        # Phase 1 — runnable validation scenarios
├── contracts/           # Phase 1 — transport contracts (verdict + quarantine mgmt)
│   └── transports.md
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
internal/
├── poison/              # NEW — PoisoningDetector interface + default HeuristicScorer
│   ├── detector.go      #   interface (Score(chunk) -> Verdict), self-register pattern
│   ├── heuristic.go     #   repetition + stuffing + instruction-phrase signals (stdlib)
│   └── heuristic_test.go
├── pipeline/            # MODIFY — score sync in processFile; persist verdict w/ chunk
├── storage/             # MODIFY — verdict field on chunk record; new 0x11 quarantine index
├── engine/              # MODIFY — quarantine keep-predicate (reuses 014 Filter); verdict read; override op; **background rescan worker (FR-011)**; threat-list merge + change-detect
├── watcher/             # MODIFY — watch poisoning config (phrase-list file + thresholds) → debounce → trigger rescan (FR-011)
├── model/               # MODIFY — Verdict/level/score/breakdown on Chunk; ThreatSource (D12)
├── config/              # MODIFY — poisoning thresholds/enabled; threat-source store + explicit import (D12)
├── config/              # MODIFY — poisoning_thresholds, poisoning_enabled, phrase-list path
├── cli/                 # MODIFY — query --include-quarantined; poison list/override cmds; status
├── rest/                # MODIFY — verdict + quarantine fields on responses; mgmt endpoints
├── grpc/                # MODIFY — proto fields; mgmt RPCs (regen proto/gen)
└── (MCP adapter)        # MODIFY — schema + renderQuery; mgmt tools
proto/gorag.proto        # MODIFY — verdict/quarantine fields + mgmt RPCs
```

**Structure Decision**: One new self-contained package `internal/poison` (interface + default
scorer), wired into the existing pipeline→engine→transport spine. Mirrors the established
`Reranker`/`QueryTransformer` extension pattern (Constitution V) — core stays closed, the
detector is swappable. Quarantine reuses spec 014's pre-fusion `keep`-predicate rather than a
new retrieval mechanism.

## Complexity Tracking

> Empty — no Constitution violations to justify.
