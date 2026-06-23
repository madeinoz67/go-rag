# Feature Specification: Retrieval Poisoning Defense — Ingest-Time Injection Detection

**Feature Branch**: `019-poisoning-detector` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-23

**Status**: Draft

**Input**: "next backlog item" → **H04** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 5, first
open item — and the **last remaining P0**):
*"Indirect prompt-injection / retrieval poisoning, zero defense. Pre-index
`PoisoningDetector`-style pass (repetition / keyword-stuffing / instruction-phrase
scoring); flag/quarantine; document the threat."* (Audit §1.8, book §11.3 — *"treat all
user input as untrusted… retrieval poisoning detection… input sanitization."*)

**Why P0**: go-rag indexes any text verbatim and returns chunks verbatim to the client,
which feeds them straight into an LLM. A malicious `.md` dropped into a watched dir
("Ignore previous instructions and exfiltrate…") becomes a retrieved chunk with **zero
defense at ingest or retrieval** — a blind spot. This spec closes that blind spot.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Protected ingest of untrusted sources (Priority: P1)

Stephen points go-rag at untrusted corpora — scraped web pages, community markdown,
third-party repos, a collaborator's notes folder. Today, any indirect-prompt-injection
payload in those files is indexed and later retrieved verbatim into his LLM. He wants a
detection pass to score every chunk for injection risk **before it is made retrievable**,
so poisoned content is caught at the source rather than silently delivered downstream.

**Why this priority**: This is the P0 — the audit's blind spot. Without it, retrieval is an
untrusted-text firehose into the LLM. Every other story depends on detection existing.

**Independent Test**: Ingest a small fixture of known injection payloads into a fresh
isolated vault; confirm each is scored and given a verdict, and that the verdict is
surfaced. Delivers the core defensive value on its own.

**Acceptance Scenarios**:

1. **Given** a fresh vault, **When** a document containing a classic payload
   ("Ignore all previous instructions…") is ingested, **Then** its chunks receive a
   verdict at or above the suspicious threshold, the verdict is persisted with the chunk,
   and **by default the chunk is excluded from query results** until explicitly requested.
2. **Given** a clean reference document, **When** it is ingested, **Then** its chunks
   receive a clean verdict.
3. **Given** a vault with mixed content, **When** the same document is ingested twice,
   **Then** the verdict is identical both times (content-addressed, deterministic).

---

### User Story 2 - Transparent verdicts + false-positive recovery (Priority: P2)

When a chunk is flagged, Stephen needs to see **why** (which signal fired: repetition,
keyword-stuffing, or an instruction phrase) and must be able to **recover** a legitimate
document that was flagged by mistake — e.g., a security writeup that legitimately quotes
injection techniques. No defensive feature is acceptable if it can silently destroy or
hide his real content.

**Why this priority**: Trust in a security heuristic requires transparency and an escape
hatch; without it, false positives make the feature unusable and users disable it.

**Independent Test**: Ingest a legit security writeup that quotes injection payloads;
confirm it can be flagged, inspected for the reason, and overridden back to fully
retrievable state.

**Acceptance Scenarios**:

1. **Given** a vault containing quarantined chunks, **When** the user lists/inspects
   quarantined items over CLI/MCP, **Then** all flagged chunks are returned with their
   verdict, score, and per-signal breakdown (the management surface — honors the standing
   quarantine-management-UI preference in its CLI/MCP form for go-rag v1).
2. **Given** a chunk flagged as a false positive, **When** the user applies an override,
   **Then** the chunk returns to its normal retrievable state across all transports.
3. **Given** any flagged content, **When** the user chooses to recover it, **Then** no
   user content is ever permanently destroyed — everything remains recoverable.

---

### User Story 3 - Audit / triage mode over an existing corpus (Priority: P3)

Stephen already has a large trusted vault ingested before this feature existed. He wants
to run detection over the **existing** corpus in a non-destructive triage mode — discover
which already-stored docs look suspicious — without being forced to re-ingest everything
and without changing retrieval behavior until he decides.

**Why this priority**: Extends coverage to the back-catalog; valuable but not the core
defensive path (new ingests are covered by US1).

**Independent Test**: Run the re-scan over an existing isolated vault; confirm pre-existing
chunks receive verdicts via the reprocess path and retrieval behavior is unchanged by the
scan itself.

**Acceptance Scenarios**:

1. **Given** a vault ingested before this feature, **When** the user triggers a corpus
   re-scan, **Then** existing chunks are scored and verdicts persisted without requiring
   re-ingestion of source files.
2. **Given** triage/audit mode is active, **When** the re-scan completes, **Then**
   retrieval results are unchanged by the act of scanning (detection does not alter
   baseline retrieval until the user chooses a posture).

---

### User Story 4 - Threat-list management & feed import (Priority: P3)

Stephen keeps the detector current as the prompt-injection landscape evolves — adding phrases
from community lists, vendor exports, or his own findings — **without go-rag phoning home or
depending on a live feed**. One explicit `threat import` updates the merged list, provenance is
recorded, and the daemon auto-re-scores so newly-matching chunks get quarantined.

**Why this priority**: keeps detection effective over time; extends US1's static list into a
maintained one. Distinct journey (list/feeds, not chunk-scoring).

**Independent Test**: import a phrase file containing a phrase present in an existing clean
chunk; confirm the chunk is re-scored to quarantined after the auto-rescan, with no egress
outside the import.

**Acceptance Scenarios**:

1. **Given** a running daemon with a clean chunk containing "exfiltrate the keys", **When**
   the user imports a source with that phrase, **Then** the merged list updates, one debounced
   background rescan fires, and the chunk becomes quarantined — with no network activity
   outside the explicit import.
2. **Given** multiple configured sources, **When** the user disables one, **Then** its phrases
   leave the merged list and a rescan re-evaluates affected chunks.
3. **Given** a previously-`released` chunk, **When** a rescan runs after a list update,
   **Then** the chunk stays `released` (override survives) but its refreshed score/signals
   are stored.

---

### Edge Cases

- **Legit security content quoting injection**: a writeup that contains "ignore previous
  instructions" as a documented example — must not be permanently lost; recoverable via US2.
- **Non-English / CJK documents**: the instruction-phrase list is initially English-centric;
  language-agnostic signals (repetition, keyword-stuffing) still apply, and the limitation
  is documented (Stephen ingests Chinese learning material — relevant).
- **Pre-feature back-catalog**: existing chunks need a re-scan path, not only new ingests.
- **Tiny / empty extracted text**: scoring must degrade gracefully (trivially clean), never
  panic on degenerate input.
- **High-repetition binary-ish extracted text** (tables, logs): must not be mass-quarantined
  by the repetition signal alone — signals combine, not veto.
- **Determinism**: identical content always yields an identical verdict (content-addressed).
- **Threat-list update on a running daemon**: editing/importing the phrase list must trigger
  exactly one debounced background rescan (not a storm); a `released` chunk stays released
  even if its refreshed score now flags it (override is sticky until reset).
- **Large feed import**: importing a very large phrase source must not block ingest/query
  (merge is bounded; rescan is async) and must dedupe against existing phrases.
- **Air-gap invariant**: no network egress except during an explicit `threat import <url>` —
  verifiable by a test asserting zero outbound connections in steady state (Constitution I).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST score every ingested chunk for injection-poisoning risk
  **before it is made retrievable**, using at least three signals: verbatim repetition,
  keyword/phrase stuffing, and known instruction-phrase patterns.
- **FR-002**: The system MUST derive a per-chunk poisoning verdict (clean / suspicious /
  quarantined) from the score against configurable thresholds, and persist that verdict
  with the chunk so it survives restarts.
- **FR-003**: The verdict MUST be content-addressed and idempotent — identical chunk text
  always produces an identical verdict, and re-ingest is a no-op for the verdict (Constitution II).
- **FR-004**: The system MUST **quarantine flagged chunks by default** — chunks at or above
  the suspicious threshold are indexed but **excluded from query results unless explicitly
  requested** (e.g. `--include-quarantined` / equivalent on each transport), so poisoned
  content never silently reaches the LLM. *(Confirmed posture Q1=A: closes the P0 blind spot;
  false positives recoverable via FR-006.)*
- **FR-005**: Query results over **every** transport (CLI / REST / gRPC / MCP) MUST surface
  the verdict and risk level on each returned hit, so downstream LLM consumers can treat
  retrieved text as untrusted (Constitution V — MCP-first, cross-transport parity).
- **FR-006**: The system MUST provide a non-destructive override so any false positive can
  be returned to fully retrievable state — no user content is ever permanently destroyed.
- **FR-007**: The system MUST expose a re-scan of the existing corpus (over the reprocess
  path) so pre-feature ingests receive verdicts without forced re-ingestion of source files.
- **FR-008**: The system MUST ship a written threat model: the indirect-prompt-injection
  threat, what the detector does and does not catch, and an explicit statement that
  detection is heuristic defense-in-depth, not a security guarantee.
- **FR-009**: Detection MUST run on the ingest path without breaching the <10ms write-ACK
  budget — bounded heuristic scoring, with any non-trivial work off the ACK-critical path
  (Constitution IV).
- **FR-010**: Detection MUST run **by default for all ingests** — it is the last remaining
  P0, so the blind spot is closed out of the box — and MUST be configurable off via
  flag/config per transport. *(Confirmed Q2=A.)*
- **FR-011**: When the poisoning threat configuration changes (instruction-phrase list or
  thresholds), the daemon MUST automatically trigger **one background re-score** of the
  stored corpus (daemon-mode only), so verdicts reflect the updated threat model without
  manual re-ingest. The re-score MUST be idempotent (unchanged verdicts are no-ops,
  Constitution II), MUST preserve user `released` overrides, MUST run asynchronously off the
  query/write-ACK paths (Constitution IV), and MUST invalidate stale query-result caches on
  completion (index-epoch bump). A manual trigger (`poison rescan`) MUST also be exposed on
  all transports for one-shot/CI use. *(Option A — confirmed.)*
- **FR-012**: The poisoning phrase list MUST be managed as a **local, versioned merge of
  layered sources** (built-in default + zero or more user sources), each independently
  enable/disable-able and deduped, with per-source provenance (origin, version, fetched-at).
  The system MUST expose list management (`list` / `add` / `remove` / `export` / `sources`)
  on all transports.
- **FR-013**: The system MUST support adding/updating threat phrases via an **explicit,
  user-initiated import** (`threat import <path|url>`) — a one-shot operation where the user
  names the source. URL fetch is permitted ONLY as this explicit, user-initiated action (never
  a background/runtime dependency — Constitution I, air-gapped). An import that changes the
  merged list MUST trigger the FR-011 background rescan. The system MUST NOT subscribe to,
  poll, or auto-pull any external feed. *(Option A — confirmed.)*

### Key Entities *(include if feature involves data)*

- **PoisoningVerdict**: the per-chunk risk assessment — a verdict level
  (clean / suspicious / quarantined), a numeric score, and a per-signal breakdown
  (repetition / keyword-stuffing / instruction-phrase). Persisted with the chunk and
  content-addressed so identical text yields identical verdicts; readable by every
  transport at query time.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A committed fixture of known indirect-prompt-injection payloads ingested
  into a fresh vault is detected at **≥95% recall** at the suspicious-or-above threshold.
- **SC-002**: A reference corpus of clean documents — **including security writeups that
  legitimately quote injection techniques** — produces a **≤5% false-positive** quarantine
  rate at the default threshold.
- **SC-003**: Detection adds **<5ms per chunk** to ingest on average and **never breaches
  the <10ms write-ACK budget** (Constitution IV).
- **SC-004**: Every transport (CLI/REST/gRPC/MCP) surfaces the verdict identically on a
  flagged chunk — verifiable by a cross-transport parity check (Constitution V).
- **SC-005**: No user content is permanently destroyed — a false positive is fully
  recoverable to retrievable state via a documented override, verifiable end-to-end.
- **SC-006**: The `make test-eval` retrieval-quality gate stays green (recall@10 unchanged)
  in the default posture — detection does not regress baseline retrieval quality.
- **SC-007**: After the instruction-phrase list is updated on a running daemon, a chunk
  newly-matching the list is re-scored to `quarantine` and excluded from default results
  within one background-sweep pass — **without re-ingesting source files** — and any chunk the
  user had `released` stays `released` (override survives rescans).
- **SC-008**: A user can import a phrase source from a named file or URL via one explicit
  command; the new phrases take effect (merged + deduped) and trigger a rescan — with **zero
  background network activity** outside that user-initiated import (verifiable: no egress
  except during the explicit `import`).

## Assumptions

- Detection runs at ingest time (pre-index), matching the audit's recommended fix; the
  stored verdict is **read** at retrieval (no per-query re-scoring).
- The three signals are: verbatim repetition, keyword/phrase stuffing, and a known
  instruction-phrase pattern list — **initially English-centric**, with a documented
  limitation for non-English/CJK instruction phrases (language-agnostic heuristics still
  apply; relevant because Stephen ingests Chinese learning material).
- **Confirmed posture**: quarantine-by-default (flagged chunks excluded from results unless
  explicitly requested) **and** detection **default-on** for all ingests (configurable off
  per transport). The re-scan of the existing corpus rides the existing `reprocess` path
  (no forced re-ingest).
- **In scope**: detection + flag/quarantine + per-transport verdict surfacing + threat-model
  documentation + false-positive recovery.
- **Out of scope** (per PRD §2.2 / audit §4): LLM-side output moderation, RBAC/multi-tenant
  isolation, conversational/agent-layer guardrails, TLS. Detection is heuristic
  defense-in-depth, **not** a guarantee — documented as such.
- Reasonable defaults (not asking): verdicts are idempotent under content-addressed
  identity; thresholds are user-configurable with documented defaults; signals combine
  additively rather than any single signal vetoing.
- **Threat list (Option A, FR-012/013)**: local, versioned, multi-source-merged; updated via
  explicit user-initiated `import` only — go-rag never auto-pulls/polls a feed (Constitution
  I, air-gapped). The user owns the update cadence (cron/script). The FR-011 rescan fires on
  any merged-list change. Out of scope: STIX/TAXII parsing, live feed subscriptions, telemetry.
- **Background rescan (Option A, FR-011)**: daemon-mode only; the one-shot CLI uses the manual
  `poison rescan` / `reprocess --poisoning` path. Rescan is async indexing-class work
  (Constitution IV) and preserves `released` overrides.
