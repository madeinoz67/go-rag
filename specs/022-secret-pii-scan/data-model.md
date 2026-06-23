# Phase 1 — Data Model: Secret / PII Scanning (H19)

> One entity (`Finding`), produced by the redactor and surfaced in the ingest summary +
> audit log. No new persisted storage — redaction is an in-memory text transform; findings
> are reported (per-type counts), not stored as queryable records.

## Entity

### Finding  *(per redaction pass)*

A per-type aggregate of what a single document's redaction pass detected + replaced.

| Field | Type | Notes |
|-------|------|-------|
| `Type` | string | the pattern type (aws-key, github-token, secret, private-key, credit-card, ssn, email, or a custom type) |
| `Count` | int | how many matches of that type were redacted |

A redaction pass returns `[]Finding` (one entry per type with Count > 0), sorted by Type.

### Redaction (the transform, not stored)

`Apply(text) → (redactedText, []Finding)` — replaces each match with a typed placeholder
(`[REDACTED:<type>]`) and aggregates per-type counts. Pure function of (text, patterns);
deterministic for a fixed pattern set. The redacted text replaces the pipeline's `content`
var post-identity; the original is preserved by identity (Constitution II).

## Validation rules (from requirements)

- V1: every pattern MUST compile (validated at scanner construction; a bad pattern fails
  fast at boot, not mid-ingest).
- V2: credit-card matches MUST pass LUHN validation (cuts false positives).
- V3: a redaction pass MUST return per-type counts only — never the matched secret text and
  never match positions (privacy: findings don't re-expose secrets).
- V4: redaction is a no-op when disabled (default off → verbatim ingest).

## State transitions

None — redaction is a stateless text transform. The pipeline applies it once per ingest
(when enabled); `reprocess` re-applies it to existing docs. No persisted finding state
(findings are reported via the ingest summary + the H18 audit log).

## Relationships

- `Finding` is produced by `redact.Apply` and consumed by the pipeline → engine
  (`IngestSummary.Redactions`) → audit log (a `redaction` event). It references pattern
  **types**, never content.
- The document identity (docID + content-hash) is over the **original** content; redaction
  is orthogonal to identity (Constitution II). The stored chunks carry redacted text.
