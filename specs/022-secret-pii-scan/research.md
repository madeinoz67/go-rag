# Phase 0 — Research: Secret / PII Scanning (H19)

> Resolves the design questions (spec 022). The spec had no NEEDS CLARIFICATION (defaults
> grounded in book §11.2 + the audit's "optional … --redact" + Constitution II); this
> fixes the *how*.

## D1 — Package: `internal/redact` (NOT `internal/reader`)

**Decision**: a new self-contained `internal/redact` package (Scanner + `Apply(text) →
(redactedText, Findings)`), stdlib `regexp` only.

**Rationale**: the audit said "in `internal/reader`", but the correct placement is
**pipeline-side** (D3) to preserve identity-over-original (the reader returns original
content; identity is computed from it). The redactor is a pure text transform — a separate
package keeps it testable in isolation and the pipeline thin. Mirrors `internal/poison`.

**Alternatives rejected**: in `internal/reader` (identity would be over redacted text —
breaks Constitution II); a Pebble-backed finding store (over-engineered; findings are
reported, not queried).

## D2 — Pattern set (curated, ASCII-centric, configurable)

**Decision**: a built-in curated regex set + an optional user-supplied additional-patterns
source:

| Type | Pattern shape | Placeholder |
|---|---|---|
| AWS access key | `AKIA[0-9A-Z]{16}` | `[REDACTED:aws-key]` |
| GitHub token | `gh[opsu]_[A-Za-z0-9]{36}` | `[REDACTED:github-token]` |
| Generic API key | high-entropy `[A-Za-z0-9_\-]{32,}` near `key`/`token`/`secret` | `[REDACTED:secret]` |
| PEM private key | `-----BEGIN [A-Z ]*PRIVATE KEY-----…-----END…` (DOTALL) | `[REDACTED:private-key]` |
| Credit card | `\d[ -]?\d{3,}…` + LUHN validation | `[REDACTED:credit-card]` |
| SSN | `\d{3}-\d{2}-\d{4}` | `[REDACTED:ssn]` |
| Email | RFC-ish `[\w.+-]+@[\w-]+\.[\w.-]+` | `[REDACTED:email]` |

**Rationale**: covers the high-signal, commonly-swept-into-a-vault secrets + the obvious
PII (email). Credit cards LUHN-validated to cut false positives. **ASCII-centric** —
Unicode PII (CJK ID/phone) not covered by default; documented + addressable via custom
patterns (D6).

**Alternatives rejected**: ML/NER-based PII detection (Constitution III — heavy dep, + the
book's guidance is regex-based for this layer); a single "secret" pattern (too coarse).

## D3 — Placement: pipeline, post-identity, pre-chunk (Constitution II)

**Decision**: in `pipeline.processFile`, **after** `docID := model.GenerateID(content, …)`
and the content-hash dedup, **before** `segs := p.splitter.Split(content)`:

```text
read → contentHash(raw) → dedup → docID = GenerateID(content) →
  [if redact] content, findings = redact.Apply(content) →
  chunk(content) → store/embed
```

**Rationale (Constitution II)**: identity is computed over the **original** `content`; then
`content` is reassigned to the redacted text, which the chunker splits. So the same file
ingests with/without `--redact` to the **same docID**, and the content-hash dedup is over
raw bytes (unaffected). Redaction is a pipeline transform (like the H07 embedding prefix) —
to re-apply with a changed setting, use `reprocess` (which bypasses dedup). Chunks are
redacted; the original is preserved by identity + untouched on disk.

**Alternatives rejected**: in the reader (identity over redacted text — breaks II); a
post-chunk transform (would redact per-chunk, missing cross-chunk patterns like PEM blocks).

## D4 — Findings + report shape

**Decision**: `redact.Apply` returns `(redactedText, []Finding)` where `Finding{Type, Count}`
aggregates per-type counts. The pipeline forwards these to the engine's `IngestSummary`
(a new `Redactions []Finding` field) and the H18 audit log (a `redaction` audit event with
per-type counts). The CLI ingest summary prints "redacted: N (aws-key=2, email=5)".

**Rationale**: per-type counts (not the redacted text, not the positions) — the operator
sees *what was found* without re-exposing the secret. The audit log already exists (H18);
reuse it.

## D5 — Reprocess is the rescan path

**Decision**: no separate "rescan" command — `reprocess` re-reads source + re-applies the
pipeline (including redaction when enabled). So `go-rag reprocess <path>` with redact on
redacts the existing corpus.

**Rationale**: reprocess already bypasses dedup + re-applies the current pipeline; redaction
rides it. Adding a separate rescan would duplicate the pipeline.

## D6 — Config + custom patterns

**Decision**: config keys `pii_redact_enabled` (default `false`) + `pii_patterns` (path to
an additional/override pattern file). `--redact` CLI flag on add/scan/reprocess forwards to
the pipeline. Default off (matching the audit's "optional").

**Pattern file format**: one pattern per line — `<type>\t<regex>` (tab-separated); `#`
comments. Loaded + merged with the built-in set at pipeline construction.

## D7 — Air-gap (Constitution I)

The scanner is a pure-local regex pass over in-memory text. No egress, no external
pattern feeds (the pattern file is a local path). Same posture as H04/H18.

## Resolved unknowns → spec FR mapping

| Spec item | Resolved by |
|---|---|
| FR-001 scan curated set | D2 |
| FR-002 redact before index | D3 |
| FR-003 opt-in (default off) | D6 |
| FR-004 report findings | D4 |
| FR-005 identity over original | D3 (Constitution II) |
| FR-006 configurable patterns | D6 |
| FR-007 reprocess rescan | D5 |
| FR-008 docs | contracts/patterns.md + docs/redaction.md |
| FR-009 off default ACK path | D3 (opt-in, validation-class) |

**All NEEDS CLARIFICATION resolved (the spec had none).** Ready for Phase 1.
