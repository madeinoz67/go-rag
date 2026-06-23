# Implementation Plan: Structured Audit Log

**Branch**: `021-audit-log` *(single-author repo — commits directly to `main`)* | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature spec from `/specs/021-audit-log/spec.md` — backlog item **H18** (P2, S).

## Summary

A local, append-only, privacy-preserving JSONL audit trail (book §11.4/§11.5): every
query (by SHA-256 **hash**, never plaintext), every ingest, and every failed auth is
appended to a vault-local file by a single-writer background goroutine (off the ACK
path — Constitution IV). Bounded by size-capped rotation; readable/filterable via
`go-rag audit`. Pure stdlib, no new dependency; never transmitted off-host (Constitution I).

## Technical Context

**Language/Version**: Go 1.22+ (pure Go, `CGO_ENABLED=0`).

**Primary Dependencies**: existing only — stdlib (`crypto/sha256`, `encoding/json`, `os`,
`sync`, `time`). **No new dependency** (Constitution III).

**Storage**: a JSONL **file** under the vault directory (`<dbpath>/audit/audit.log`),
NOT a Pebble key-space. The constitution's single-Pebble rule is for core document/
index data; an append-only ops log is a sidecar file (like `go-rag.log`). Rotated at a
size cap; archived files kept (last N).

**Testing**: `go test -race -cover ./...`; an audit-package test (append → read → rotate)
+ a privacy test (no query plaintext in the log) + an integration test (query/ingest/
auth-fail each produce a correctly-typed record).

**Target Platform**: single static binary, local-first.

**Project Type**: CLI + multi-transport daemon (MCP/REST/gRPC) over one Engine.

**Performance Goals**: per-event append off the caller's path (buffered channel → writer
goroutine); no per-event fsync; stays inside <500ms query / <10ms ACK (Constitution IV).

**Constraints**: air-gap (no forwarding — Constitution I); append-only + query-hashed;
bounded growth; default-on.

**Scale/Scope**: local single-user; low event volume; concurrent appends serialized by
the single writer.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I | Local-First, Single-Binary | ✅ PASS | The audit log is a LOCAL JSONL file in the vault dir; no SIEM/cloud/syslog forwarding. Nothing transmitted off-host. |
| II | Content-Addressed Identity | ✅ PASS (N/A) | The audit log is orthogonal to document identity; no change to SHA-256 identity/change-detection. Query events carry a query **hash**, not content. |
| III | Pure Go — No CGo, No Runtime | ✅ PASS | Stdlib only (`crypto/sha256`, `encoding/json`, `os`, `sync`, `time`). **No new dependency.** |
| IV | Async-After-ACK Writes | ✅ PASS | Events go to a **buffered channel** drained by a single writer goroutine — the append is OFF the caller's path (incl. the ingest ACK path); no per-event fsync. The <10ms ACK budget is untouched. |
| V | Extension by Interface, MCP-First | ✅ PASS | New self-contained `internal/audit` package; auth-fail recorded consistently wherever auth is checked (REST guard / gRPC interceptor / MCP handler). |

**No violations → Complexity Tracking table intentionally empty.**

## Project Structure

### Documentation (this feature)

```text
specs/021-audit-log/
├── plan.md              # This file
├── research.md          # Phase 0 — appender/schema/rotation/wiring decisions
├── data-model.md        # Phase 1 — AuditEvent schema + state
├── quickstart.md        # Phase 1 — runnable validation (append, read, rotate, privacy)
├── contracts/
│   └── events.md        # Phase 1 — the JSONL event schema (the stable contract)
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
internal/
├── audit/               # NEW — append-only JSONL logger (the only audit-importing pkg)
│   ├── audit.go         #   Appender (single-writer goroutine + buffered channel) + Init/Close
│   ├── event.go         #   AuditEvent types + JSON marshaling + QueryHash
│   ├── rotate.go        #   size-capped rotation (rename + archive; hand-rolled, no dep)
│   └── reader.go        #   read/filter (by type/time, tail) — backs the CLI
├── engine/              # MODIFY — emit query + ingest events (Query/Add/Scan/Reprocess/Migrate)
├── rest/                # MODIFY — emit auth-fail in the bearer guard
├── grpc/                # MODIFY — emit auth-fail in the bearer interceptor
├── mcp/                 # MODIFY — emit auth-fail in the MCP auth path (if/where present)
├── config/              # MODIFY — audit_log_enabled (default true), audit_log_max_bytes, audit_path
├── cli/                 # MODIFY — `go-rag audit` reader command; daemon Init/Close of the appender
└── daemon/ (cmd)        # MODIFY — start the appender at boot, drain on stop
```

**Structure Decision**: one new self-contained `internal/audit` package owns the appender
+ reader (stdlib only). The engine + transports call `audit.Log(event)` (a non-blocking
channel send) — keeping audit coupling in one place and the call sites one-liners. The log
is a sidecar JSONL file (not Pebble — it's an ops artifact, not core data). Mirrors the
`internal/observe` / `internal/poison` isolation pattern.

## Complexity Tracking

> Empty — no Constitution violations to justify.
