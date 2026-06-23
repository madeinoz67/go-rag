# Phase 1 — Interface Contract: Redaction Pattern Set (H19)

> The curated regex set + placeholder format the `internal/redact` scanner applies when
> redaction is enabled (spec 022 / audit H19). This is the stable contract an operator
> reads to know what's detected + how it's replaced.

## Placeholder format

Each match is replaced with a typed placeholder: `[REDACTED:<type>]`. The type identifies
what was removed without re-exposing it.

## Built-in pattern set (ASCII-centric)

| Type | Pattern (shape) | Placeholder | Notes |
|---|---|---|---|
| `aws-key` | `AKIA[0-9A-Z]{16}` | `[REDACTED:aws-key]` | AWS access key ID |
| `github-token` | `gh[opsu]_[A-Za-z0-9]{36}` | `[REDACTED:github-token]` | GitHub PAT / fine-grained |
| `secret` | high-entropy `[A-Za-z0-9_\-]{32,}` adjacent to `key`/`token`/`secret`/`passwd` | `[REDACTED:secret]` | generic API key / token |
| `private-key` | `-----BEGIN [A-Z ]*PRIVATE KEY----- … -----END …-----` (DOTALL) | `[REDACTED:private-key]` | PEM private key block |
| `credit-card` | digit groups, LUHN-validated | `[REDACTED:credit-card]` | false-positive-guarded by LUHN |
| `ssn` | `\d{3}-\d{2}-\d{4}` | `[REDACTED:ssn]` | US SSN format |
| `email` | `[\w.+-]+@[\w-]+\.[\w.-]+` | `[REDACTED:email]` | PII |

**Limitation**: ASCII-centric. Unicode PII (e.g. a Chinese resident-ID or phone) is not
covered by default — supply a custom pattern (below).

## Custom patterns (config `pii_patterns`)

A local file, one pattern per line, tab-separated: `<type>\t<regex>`. `#` starts a comment.

```text
# custom-secret-pii.txt
cn-id	\d{17}[\dXx]
internal-token	prod_[A-Za-z0-9]{20}
```

Loaded + merged with the built-in set at pipeline construction. Bad regexes fail at boot
(never mid-ingest).

## Privacy + identity invariants

- A redaction pass reports **per-type counts only** — never the matched text or positions.
- Document identity is over the **original** content; redaction applies to the indexed text.
  Ingesting the same file with/without `--redact` yields the **same document ID**
  (Constitution II).
- Redaction is **opt-in** (`pii_redact_enabled` / `--redact`); default off (verbatim).

## Findings report

Per-ingest, surfaced in:
- the ingest summary: `redacted: N (aws-key=2, email=5)` — verifiable via the CLI/engine.
- the H18 audit log: a `redaction` event with per-type counts.
