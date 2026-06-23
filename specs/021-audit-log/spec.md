# Feature Specification: Structured Audit Log

**Feature Branch**: `021-audit-log` *(spec directory; per project convention this work
commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-23

**Status**: Draft

**Input**: "next backlog item" → **H18** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 5,
next open item after H17): *"No audit log. Structured append-only JSONL of query +
ingest + auth-fail events; hash query text."* (Audit §1.8; book §11.4/§11.5 — *"log
every retrieval (user, query hash, doc IDs), auth events, retention."*)

**Why this matters**: today go-rag records only `stderr`/`go-rag.log`. There is no
durable, structured record of what was queried or ingested, and no trail of failed
auth attempts — so abuse, a bad ingest, or a probe against the daemon is invisible
after the fact. H18 adds a local, append-only, privacy-preserving audit trail.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Security audit trail of operations + auth (Priority: P1)

Stephen wants a durable record of what go-rag did — every query (by hash, not
plaintext), every ingest, and every failed auth attempt — written to a **local**,
append-only, structured log that never leaves his machine. After the fact he can
review who-did-what (operation + auth outcome) without the log containing sensitive
query content.

**Why this priority**: the core security-ops gap — without it there is no
after-the-fact accountability for queries, ingests, or auth probes.

**Independent Test**: run a query, an ingest, and trigger an auth failure; confirm
the audit JSONL contains three correctly-typed records, and that no query plaintext
appears in the file.

**Acceptance Scenarios**:

1. **Given** an audit-enabled daemon, **When** a query runs, **Then** a JSONL record
   is appended carrying the query **hash** (not the plaintext), mode, result count,
   status, and a timestamp.
2. **Given** the same daemon, **When** an ingest runs, **Then** a record is appended
   carrying the path + outcome counts (no chunk content).
3. **Given** a token-guarded transport, **When** a request fails auth, **Then** an
   auth-fail record is appended carrying the transport + timestamp — **without** the
   rejected token.

---

### User Story 2 - Reading + filtering the audit log (Priority: P2)

Stephen wants to triage the audit log — tail recent events, or filter by event type
or time window — without hand-parsing JSONL. A reader command gives him a quick view.

**Why this priority**: a log you can't read conveniently is half a feature; builds on
US1. Distinct, lower-priority than the trail itself.

**Independent Test**: append a few events of mixed types, then filter by type and by
time window; confirm only matching records are returned.

**Acceptance Scenarios**:

1. **Given** a populated audit log, **When** the operator reads it filtered to
   `query` events in the last hour, **Then** only matching query records are shown.
2. **Given** the same log, **When** the operator tails the log, **Then** the most
   recent records stream/are shown in order.

---

### Edge Cases

- **Privacy**: query text MUST be hashed (SHA-256) — the raw query string must never
  appear in the log (book §11.4; queries can carry PII/sensitive content).
- **Append-only**: existing records MUST never be modified or reordered (the app only
  appends; rotation moves the old file aside — it does not rewrite history).
- **Air-gap**: the log is LOCAL (vault directory); it MUST NOT be transmitted off-host
  (Constitution I — no SIEM/cloud/syslog forwarding).
- **Bounded growth**: a long-running daemon must not grow the log unbounded — a size
  cap with rotation (no per-event or per-query cardinality).
- **Concurrent appends**: the async ingest workers + concurrent queries + auth
  attempts must append safely (the appender is single-writer-serialized).
- **Auth-fail detail**: a failed auth records the transport + time, never the rejected
  token value (no secret leakage into the audit trail).
- **Per-op cost**: the append must stay well inside the latency budgets (<500ms query,
  <10ms write-ACK) — Constitution IV.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST append a structured JSONL record for every query, every
  ingest operation (add/scan/reprocess/migrate), and every failed-authentication event.
- **FR-002**: A query record MUST carry a SHA-256 **hash** of the query text — never
  the plaintext — plus mode, requested top-k, hit count, status (ok/error), and a
  timestamp (privacy, book §11.4).
- **FR-003**: An ingest record MUST carry the path and the outcome counts
  (new/skipped/errors), with **no** chunk or document content.
- **FR-004**: An auth-fail record MUST carry the transport and timestamp, and MUST
  NOT carry the rejected credential.
- **FR-005**: The audit log MUST be a LOCAL append-only file under the vault directory;
  it MUST NOT be transmitted off-host (Constitution I).
- **FR-006**: The log MUST be bounded — a configurable size cap with rotation (an
  existing record is never modified; rotation archives the old file).
- **FR-007**: The system MUST provide a reader that filters the log by event type and
  time window (and a tail of recent records).
- **FR-008**: Audit logging MUST be default-on and configurable off; the per-op append
  cost MUST stay within the latency budgets (Constitution IV).
- **FR-009**: The system MUST document the event schema, the privacy posture (query
  hashed; no content), and the air-gap boundary.

### Key Entities *(include if feature involves data)*

- **AuditEvent**: one JSONL record — `{timestamp, type (query|ingest|auth-fail),
  ...event-specific fields}`. Query events carry `query_hash` (not text); ingest
  events carry `path` + counts; auth-fail events carry `transport`. Appended to a
  local file; never mutated.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a query + an ingest + an auth failure, the JSONL contains three
  records of the correct types with the expected fields — verifiable by parsing.
- **SC-002**: No query record contains the raw query string — only its hash
  (verifiable: the plaintext does not appear anywhere in the audit file).
- **SC-003**: The log file lives under the vault directory and is append-only
  (verifiable: existing records are unchanged after later operations).
- **SC-004**: Rotation engages at the configured size cap — verifiable by capping the
  log and observing a rotated archive + bounded growth.
- **SC-005**: The reader filters by event type and time window and tails recent
  records — verifiable end-to-end.
- **SC-006**: Zero egress (the log never leaves the host); default-on; per-op cost
  within the latency budgets; `go build/vet/test` green.

## Assumptions

- **Air-gap (Constitution I, established)**: the audit log is a LOCAL JSONL file in
  the vault directory; no SIEM/cloud/syslog forwarding (out of scope; revisits as P0
  only if go-rag ever backs a shared multi-user server).
- **Privacy (book §11.4)**: query text is hashed (SHA-256); no chunk/document content
  and no query plaintext on any record; rejected credentials are never logged.
- **Always-on (default)**: security audit is on by default (configurable off via
  `audit_log_enabled`), mirroring the H04/H17 default-on posture for security features.
- **Retention**: bounded — a configurable size cap with rotation (default a modest cap);
  age-based retention is a future refinement.
- **Event set**: query, ingest (add/scan/reprocess/migrate), auth-fail (audit §1.8).
  Out of scope: RBAC/named-user identity (single-user local — the "user" dimension is
  the auth outcome, not an identity), remote/syslog forwarding, tamper-evidence beyond
  append-only best-effort.
- **Constitution gates**: I (local, no egress), IV (per-op append cost within budgets),
  V (consistency across transports — auth-fail recorded wherever auth is checked).
