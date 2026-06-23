# Phase 0 — Research: Structured Audit Log (H18)

> Resolves the design questions for the audit log (spec 021). The spec had no
> NEEDS CLARIFICATION (defaults grounded in book §11.4 + Constitution I); this
> fixes the *how*.

## D1 — File vs Pebble key-space

**Decision**: a JSONL **file** under the vault dir (`<dbpath>/audit/audit.log`), NOT a
Pebble prefix.

**Rationale**: the audit log is an append-only **ops artifact** (like `go-rag.log`), not
core document/index data. The constitution's "single Pebble instance for core data" is
about document/chunk/embedding/index state — a sidecar log file doesn't violate it
(appending to a file is not "a second database"). A JSONL file is also the book/audit's
stated format ("structured append-only JSONL") and trivially greppable/jq-able.

**Alternatives rejected**: a Pebble append-only prefix (over-engineered — Pebble is a KV,
not a log; rotation/appends are awkward); syslog (egress/coupling — Constitution I).

## D2 — Appender: async single-writer goroutine

**Decision**: callers do a **non-blocking channel send** of an `AuditEvent`; a single
writer goroutine drains the channel, serializes appends (+ rotation), and flushes
periodically. No per-event fsync.

**Rationale (Constitution IV)**: the append must stay off the caller's path — including
the ingest ACK path (<10ms). A buffered channel → background writer keeps the caller at a
~µs channel-send, never blocking on disk. Single writer = no append races (the daemon is
single-writer for Pebble; the audit log is single-writer for its file). Mirrors the
ingest pipeline's queue pattern.

**Alternatives rejected**: a mutex-guarded sync append on the caller's path (blocks the ACK
on every event — IV risk); per-event fsync (durability over the latency budget — rejected);
a Pebble-backed log (D1).

## D3 — Event schema (JSONL)

**Decision**: one JSON object per line; a common envelope + type-specific fields.

```json
{"ts":"2026-06-23T16:25:10Z","type":"query","query_hash":"<sha256>","mode":"hybrid","k":5,"hits":3,"status":"ok"}
{"ts":"...","type":"ingest","op":"add","path":"/x.md","new":1,"skipped":0,"errors":0,"status":"ok"}
{"ts":"...","type":"auth-fail","transport":"rest","detail":"missing or invalid bearer"}
```

No PII: queries carry `query_hash` (D4); ingest carries counts (no content); auth-fail
carries the transport (never the rejected token). See `contracts/events.md` for the full
contract.

## D4 — Query hashing (privacy, book §11.4)

**Decision**: `query_hash = sha256(query_text)` (hex). The raw query never appears on any
record.

**Rationale**: queries can carry PII/sensitive content; the book §11.4 mandates hashing.
The hash lets an operator correlate repeated identical queries (or match a known query)
without exposing content.

**Alternatives rejected**: truncated plaintext (privacy fail); HMAC keyed hash (no secret
to key it with in a local single-user tool; SHA-256 suffices).

## D5 — Wiring points

**Decision**:
- **query event**: `engine.Query` (alongside the existing observe span/metric record) —
  one per query (hits from `res.Hits`, status from err).
- **ingest event**: `engine.Add/Scan/Reprocess/Migrate` (the IngestSummary has the counts).
- **auth-fail event**: the REST `guard` (server.go:123), the gRPC `bearerInterceptor`
  (server.go:38), and the MCP auth path — wherever a bearer check rejects.

**Rationale**: single instrumentation point per event type → no double-counting. Engine
events are transport-agnostic (one Query/ingest log regardless of CLI/REST/gRPC/MCP);
auth-fail is transport-specific (logged at each transport's check, labeled by transport).

## D6 — Rotation (bounded growth)

**Decision**: size-capped rotation. When `audit.log` exceeds `audit_log_max_bytes`
(default ~16 MiB), rename it to `audit-1.log` (shifting older archives: `audit-2.log`,
…), keep the last N=3, and start a fresh `audit.log`. Hand-rolled (no lumberjack dep —
Constitution III).

**Rationale**: a long-running daemon must not grow the log unbounded; rotation archives
history without rewriting it (append-only preserved). N=3 + 16 MiB ≈ 64 MiB worst case —
well inside the local memory/disk budget.

**Alternatives rejected**: unbounded (operational hazard); age-based rotation (needs a
background ticker; size is simpler + sufficient for local).

## D7 — Reader (`go-rag audit`)

**Decision**: a `go-rag audit` command: `--tail N` (last N events), `--type query|ingest|
auth-fail`, `--since <duration>` (e.g. `--since 1h`). Reads + filters the active log
(archives optional via `--all`).

**Rationale**: a log you can't read conveniently is half a feature. A stdlib reader
(scans lines, parses JSON, filters) is cheap. Operators can also `jq` the file directly.

## Resolved unknowns → spec FR mapping

| Spec item | Resolved by |
|---|---|
| FR-001 append per event | D2 + D5 |
| FR-002 query hashed | D4 |
| FR-003 ingest (counts, no content) | D3 + D5 |
| FR-004 auth-fail (no token) | D3 + D5 |
| FR-005 local file, no egress | D1 (Constitution I) |
| FR-006 bounded rotation | D6 |
| FR-007 reader/filter | D7 |
| FR-008 default-on, off-path | D2 (Constitution IV) |

**All NEEDS CLARIFICATION resolved (the spec had none).** Ready for Phase 1.
