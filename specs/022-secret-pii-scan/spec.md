# Feature Specification: Secret / PII Scanning at Ingest

**Feature Branch**: `022-secret-pii-scan` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-23

**Status**: Draft

**Input**: "next backlog item" → **H19** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 5, last
item): *"No PII/secret scanning at ingest. Optional regex secret/PII scanner in
`internal/reader` with `--redact`."* (Audit §1.8; book §11.2 — *"detect/redact PII
before indexing."*)

**Why this matters**: today raw text lands in Pebble + vectors verbatim. A `.env` swept
into a watched directory, API keys pasted into notes, or a secrets file committed by
mistake becomes **retrievable verbatim** — anyone querying "AKIA" or a token fragment gets
the secret back. H19 adds an opt-in regex scanner that detects + redacts secrets/PII
**before indexing**, so indexed text never contains live credentials.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Redact secrets before indexing (Priority: P1)

Stephen opts into redaction (`--redact` / config) because his watch directory sometimes
catches stray secrets (notes, .env files, exported configs). When enabled, go-rag scans
each ingested document for a curated set of secret/PII patterns (API keys, tokens, private
keys, credit cards, SSNs, emails), replaces matches with a placeholder, and indexes the
redacted text — so a later query for the secret finds nothing. The original file on disk
is untouched; the ingest summary reports what was redacted.

**Why this priority**: the core privacy gap — indexed secrets retrievable verbatim.

**Independent Test**: with `--redact`, ingest a doc containing an AWS key + an email;
query for the key/email → not found; the ingest summary reports 2 redactions.

**Acceptance Scenarios**:

1. **Given** redaction is enabled, **When** a document containing an AWS access key is
   ingested, **Then** the stored/indexed chunks contain a redaction placeholder (e.g.
   `[REDACTED:aws-key]`), not the key.
2. **Given** the same ingest, **When** the operator queries for the secret, **Then** it is
   not found in results.
3. **Given** the same ingest, **Then** the ingest summary reports the redaction count +
   types.
4. **Given** redaction is **disabled** (default), **When** the same document is ingested,
   **Then** it indexes verbatim (no regression — current behavior).

---

### User Story 2 - Finding visibility + a corpus re-scan (Priority: P2)

Stephen wants to see what was redacted (per-type counts in the audit log / summary), tune
the pattern set, and re-scan the **existing** corpus (already-ingested docs that predate
the feature) without re-reading source files.

**Why this priority**: visibility + back-catalog coverage. Builds on US1.

**Independent Test**: ingest (redacted) → the audit log records redaction findings;
reprocess rescan redacts pre-existing docs.

**Acceptance Scenarios**:

1. **Given** a redacted ingest, **When** the operator reads the audit log / ingest
   summary, **Then** per-type redaction counts are visible.
2. **Given** an existing corpus ingested before the feature, **When** the operator runs a
   rescan (reprocess), **Then** the existing docs are redacted without re-reading source.
3. **Given** the operator adds a custom pattern, **When** a doc matching it is ingested,
   **Then** it is redacted (configurable pattern set).

---

### Edge Cases

- **Identity (Constitution II)**: document identity is computed over the **original**
  content; redaction applies to the stored/indexed text only. Ingesting the same file with
  and without `--redact` yields the **same document ID** (idempotent regardless of the
  setting).
- **False positives**: a security writeup that legitimately discusses/quotes API keys gets
  redacted on the redacted terms. The original is preserved (identity over original; the
  file on disk is untouched), so the operator can disable scanning or tune patterns —
  this is the documented redaction trade-off (recall on redacted terms drops, by design).
- **Unicode/CJK PII**: the curated regex set is ASCII-pattern-centric (AWS keys, credit
  cards, tokens are ASCII; emails are ASCII). Unicode PII (e.g. a Chinese ID/phone) is not
  covered by default — a documented limitation, addressable via custom patterns.
- **Performance**: regex scan cost is proportional to text × patterns. **Opt-in** (default
  off ⇒ no ACK-path cost); when on, bounded (regex is fast; the scan is validation-class,
  like the H04 poisoning detector).
- **Air-gap**: the scanner is a local regex pass — no egress (Constitution I).
- **Already-redacted re-ingest**: re-ingesting a redacted doc is a no-op (idempotent).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: When redaction is enabled, the system MUST scan ingested document text for a
  curated set of secret/PII patterns (API keys, bearer tokens, private keys, credit-card
  numbers, SSNs, email addresses) using regex.
- **FR-002**: The system MUST redact detected secrets (replace each match with a typed
  placeholder, e.g. `[REDACTED:aws-key]`) **before** the text is chunked + indexed, so
  secrets are not retrievable verbatim.
- **FR-003**: Redaction MUST be **opt-in** (a config flag / `--redact`); **default off**,
  preserving current (verbatim) behavior when disabled.
- **FR-004**: The system MUST report findings (per-type redaction counts per document) in
  the ingest summary and the audit log, so the operator sees what was redacted.
- **FR-005**: Document identity MUST be computed over the **original** content
  (Constitution II) — redaction applies to stored/indexed text only, so ingestion is
  idempotent regardless of the redact setting.
- **FR-006**: The pattern set MUST be configurable — a built-in curated set plus a
  user-supplied additional/override patterns source.
- **FR-007**: The system MUST provide a re-scan path (over reprocess) so the existing
  corpus can be redacted without re-reading source files.
- **FR-008**: The system MUST document the threat (indexed secrets retrievable verbatim),
  what is detected, the redaction trade-off (recall on redacted terms), and that it is
  opt-in.
- **FR-009**: Redaction MUST stay off the **default** ACK path (opt-in; when on, bounded
  regex cost within the latency budgets — Constitution IV).

### Key Entities *(include if feature involves data)*

- **Redaction**: the transformation applied at ingest — a set of typed placeholders
  replacing matched secrets/PII in the stored/indexed text. Findings (per-type counts) are
  reported; the original content (for identity) is preserved.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With `--redact`, a doc containing an AWS key, a GitHub token, a PEM private
  key, and an email ingests such that **none** of those secrets is retrievable (query for
  each → not found); the chunks carry typed placeholders.
- **SC-002**: With `--redact` off (default), the same doc ingests verbatim — no behavior
  change, no retrieval regression.
- **SC-003**: Redaction findings (per-type counts) appear in the ingest summary + the
  audit log.
- **SC-004**: Ingesting the same file with and without `--redact` yields the **same
  document ID** (identity over original — Constitution II).
- **SC-005**: A reprocess rescan redacts the existing corpus (verifiable on an isolated
  DB).
- **SC-006**: Default-off (no ACK-path cost when off); `go build/vet/test` green; `make
  test-eval` recall@10 unchanged when redaction is off; no new dependency.

## Assumptions

- **Opt-in (default off)**, matching the audit's *"optional … with `--redact`"*. When
  enabled: scan + redact (typed placeholder) + report findings. Default preserves verbatim
  behavior.
- **Curated pattern set**: AWS access keys (`AKIA…`), GitHub tokens (`ghp_`/`gho_`), a
  generic high-entropy secret heuristic, PEM private keys, credit-card numbers (LUHN), SSNs
  (format), email addresses. **ASCII-centric** — Unicode PII not covered by default
  (documented; addressable via custom patterns).
- **Identity (Constitution II)**: document identity over the **original** content;
  redaction applies to stored/indexed text only — idempotent ingestion preserved regardless
  of the redact setting.
- **Redaction trade-off**: redacting text degrades retrieval on the redacted terms (recall
  on secrets drops — which is the point); the original is preserved by identity + on disk.
- **Scope**: opt-in scan + redact + report + reprocess rescan + configurable patterns +
  docs. **Out of scope**: ML/NER-based PII detection, image/PDF OCR redaction, a
  redaction-review UI, redaction of already-redacted text (no-op).
- **Constitution gates**: I (local regex, no egress), II (identity over original; redact
  indexed text), III (stdlib `regexp`, no new dep), IV (opt-in, bounded scan cost off the
  default ACK path).
