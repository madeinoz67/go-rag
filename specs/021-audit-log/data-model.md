# Phase 1 — Data Model: Structured Audit Log (H18)

> One entity (`AuditEvent`), serialized as one JSONL line. Stored in a vault-local
> append-only file (not Pebble — it's an ops artifact). Rotated, never mutated.

## Entity

### AuditEvent  *(one JSONL line)*

A single audited operation or auth outcome. Appended (never modified); rotated to an
archive when the file exceeds the size cap.

| Field | Type | Present | Notes |
|-------|------|---------|-------|
| `ts` | string (RFC3339, UTC) | always | event timestamp |
| `type` | enum `query \| ingest \| auth-fail` | always | discriminator |
| `query_hash` | string (hex) | query | SHA-256 of the raw query text — **never** the plaintext |
| `mode` | string | query | hybrid \| semantic \| keyword |
| `k` | int | query | requested top-k |
| `hits` | int | query | results returned |
| `status` | string | query, ingest | ok \| error |
| `op` | string | ingest | add \| scan \| reprocess \| migrate |
| `path` | string | ingest | the ingested path |
| `new` / `skipped` / `errors` | int | ingest | outcome counts (no content) |
| `transport` | string | auth-fail | rest \| grpc \| mcp |
| `detail` | string | auth-fail | short reason (e.g. "missing or invalid bearer"); **never** the rejected token |

**Validation rules (from requirements)**:
- V1: every record MUST have `ts` + `type`.
- V2: a `query` record MUST carry `query_hash`; the raw query string MUST NOT appear
  anywhere in the record (privacy, FR-002).
- V3: an `ingest` record carries counts only — no chunk/document content.
- V4: an `auth-fail` record MUST NOT carry the rejected credential.
- V5: records are append-only — once written, never modified (rotation renames the file;
  it does not rewrite lines).

## State transitions

None per event (an event is an immutable fact). The **log file** has a simple lifecycle:
`active (audit.log)` → on size cap → `rotated (audit-1.log, shifting older)`, with the
oldest archive (beyond N=3) dropped. No record is ever edited or deleted; only whole
archive files age out.

## Relationships

- `AuditEvent` is independent of the document/chunk model (orthogonal to Constitution II
  identity). It references paths (ingest) and query hashes, never content.
- One event per operation (Query/Add/Scan/Reprocess/Migrate) and one per rejected auth —
  single instrumentation point each (no double-counting).

## Storage layout

```text
<dbpath>/audit/
├── audit.log        # active append-only log (JSONL)
├── audit-1.log      # most-recent archive (when rotated)
├── audit-2.log
└── audit-3.log      # oldest kept (older archives are dropped)
```
