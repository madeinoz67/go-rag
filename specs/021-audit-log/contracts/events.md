# Phase 1 — Interface Contract: Audit Event Schema (H18)

> The stable JSONL schema go-rag appends to `<dbpath>/audit/audit.log` (and rotated
> archives `audit-N.log`). One JSON object per line. This is the contract a reader
> (`go-rag audit`) or external tooling (`jq`) parses.

## Common envelope

Every line has `ts` (RFC3339 UTC) + `type`. `type` selects the remaining fields.

## Event types

### `query` — one per `Engine.Query`

```json
{"ts":"2026-06-23T16:25:10Z","type":"query","query_hash":"9f2c...","mode":"hybrid","k":5,"hits":3,"status":"ok"}
```
- `query_hash`: SHA-256 (hex) of the raw query text — **never** the plaintext (privacy,
  FR-002).
- `status`: `ok` | `error` (from the query's returned error).

### `ingest` — one per Add/Scan/Reprocess/Migrate

```json
{"ts":"...","type":"ingest","op":"add","path":"/docs/x.md","new":1,"skipped":0,"errors":0,"status":"ok"}
```
- `op`: `add` | `scan` | `reprocess` | `migrate`.
- counts only — **no** chunk/document content (FR-003).

### `auth-fail` — one per rejected bearer auth (per transport)

```json
{"ts":"...","type":"auth-fail","transport":"rest","detail":"missing or invalid bearer"}
```
- `transport`: `rest` | `grpc` | `mcp`.
- `detail`: short reason; **never** the rejected token (FR-004).

## Privacy + air-gap invariants (Constitution I / book §11.4)

- No query plaintext, no document/chunk content, and no credentials appear on any record.
- The log is LOCAL (`<dbpath>/audit/`) and never transmitted off-host (no SIEM/syslog/
  cloud forwarding). An air-gap test asserts zero egress.

## `go-rag audit` reader

```bash
go-rag audit                      # last 20 events (default tail)
go-rag audit --tail 100           # last 100
go-rag audit --type query         # only query events
go-rag audit --since 1h           # last hour
go-rag audit --all                # include rotated archives (audit-N.log)
go-rag audit -f json              # raw JSONL (pipe to jq)
```

## Rotation

`audit.log` exceeding `audit_log_max_bytes` (default ~16 MiB) → renamed to `audit-1.log`
(older archives shift: `audit-2.log`, …), last N=3 kept, oldest dropped, fresh
`audit.log` started. No record is ever modified (append-only preserved).
