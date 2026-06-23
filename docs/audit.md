# Structured Audit Log (H18 / spec 021)

go-rag records a **local, append-only, privacy-preserving** audit trail of operations
and failed authentication (book §11.4/§11.5). Every query, ingest, and auth failure is
appended to a vault-local JSONL file; the log never leaves the host (Constitution I).

## What's logged

| Event | Fields | Notes |
|---|---|---|
| `query` | `ts`, `query_hash`, `mode`, `k`, `hits`, `status` | query is **SHA-256 hashed** — never plaintext |
| `ingest` | `ts`, `op` (add/scan/reprocess/migrate), `path`, `new`/`skipped`/`errors`, `status` | counts only — **no content** |
| `auth-fail` | `ts`, `transport` (rest/grpc/mcp), `detail` | short reason; **never the rejected token** |

See [`specs/021-audit-log/contracts/events.md`](../specs/021-audit-log/contracts/events.md)
for the full JSONL schema.

## Privacy + air-gap (Constitution I / book §11.4)

- Query text is **hashed** (SHA-256); no query plaintext, no document/chunk content,
  and no credentials appear on any record.
- The log is **LOCAL** (`<dbpath>/audit/audit.log`); never forwarded to a SIEM/syslog/cloud.

## File-growth (rotation)

Size-capped: when `audit.log` exceeds `audit_log_max_bytes` (default ~16 MiB) it's renamed
to `audit-1.log` (older archives shift up; beyond the last **3**, dropped). Worst case
≈ 64 MiB. Append-only preserved (rotation renames whole files; no record is rewritten).

## Configuration

| Key | Default | Meaning |
|-----|---------|---------|
| `audit_log_enabled` | `true` | audit on by default |
| `audit_log_max_bytes` | `~16 MiB` | rotation size cap |
| `audit_path` | (vault default) | override the audit log path |

## Reading the log

```bash
go-rag audit                    # last 20 events
go-rag audit --tail 100         # last 100
go-rag audit --type query       # only query events
go-rag audit --since 1h         # last hour
go-rag audit --all              # include rotated archives
go-rag audit -f json | jq .     # raw JSONL → jq
```

## Architecture

`internal/audit` is the only audit-importing package. Callers (engine + transports) send
events via `audit.Log` — a non-blocking channel send drained by a single writer goroutine
(off the caller's path, incl. the <10ms ACK — Constitution IV; race-free single writer).
Stdlib only, no new dependency (Constitution III).
