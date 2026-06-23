# Secret / PII Redaction at Ingest (H19 / spec 022)

go-rag can **opt-in** to redact secrets and PII from ingested text *before indexing*
(book §11.2), so a `.env` swept into a watched dir or API keys in notes never become
retrievable verbatim. Default **off** (verbatim); enabled via `--redact`.

## What's redacted

| Type | Example | Placeholder |
|---|---|---|
| AWS access key | `AKIAIOSFODNN7EXAMPLE` | `[REDACTED:aws-key]` |
| GitHub token | `ghp_aBcDeFgH…` | `[REDACTED:github-token]` |
| PEM private key | `-----BEGIN RSA PRIVATE KEY-----` | `[REDACTED:private-key]` |
| Credit card (LUHN-validated) | `4111 1111 1111 1111` | `[REDACTED:credit-card]` |
| SSN | `123-45-6789` | `[REDACTED:ssn]` |
| Email | `ops@example.com` | `[REDACTED:email]` |

Credit cards are **LUHN-validated** before redaction (cuts false positives). Custom
patterns via a local file (`pii_patterns` config key): one per line, `<type>\t<regex>`,
`#` comments.

## Using it

```bash
go-rag add /path/to/docs --redact           # redact at ingest
go-rag add /path/to/docs                     # default off — verbatim
go-rag config set pii_redact_enabled true    # enable in config (daemon path)
go-rag config set pii_patterns /path/to/custom-patterns.txt
```

## Identity (Constitution II)

Document identity is computed over the **original** content; redaction applies to the
indexed text only. The same file ingests with/without `--redact` to the **same document
ID** — ingestion is idempotent regardless of the redact setting. The original file on disk
is **never touched**.

## Trade-off

Redacting text degrades retrieval on the redacted terms (a query for the secret finds
nothing — which is the point). The original content is preserved by identity + on disk, so
re-ingesting without `--redact` (or reprocessing) restores verbatim text.

## Limitations

- ASCII-centric: the built-in patterns target ASCII secrets/PII. Unicode PII (e.g. a
  Chinese resident-ID) is not covered by default — supply a custom pattern.
- No ML/NER: this is a regex-based heuristic layer, not a semantic PII detector.

## Architecture

`internal/redact` is a pure-stdlib (`regexp`) package. The pipeline calls `Apply(text)`
**between** identity computation and chunking — so the docID is over the original content
and the chunks carry redacted text. Stdlib only, no new dependency (Constitution III).
