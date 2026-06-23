# Phase 1 — Quickstart: Secret / PII Scanning (H19)

> Runnable validation that redaction works end-to-end. Implementation detail belongs in
> `tasks.md`; this is a run/validate guide. Run on an **isolated** DB (`--db-path <tmp>`)
> — never the live vault.

**Prerequisites**: `make build` succeeds. No Ollama needed for the redaction path (it's a
text transform; a query proving the secret is absent works on keyword mode without
embeddings).

## Scenario 1 — Secrets are redacted, not retrievable (US1, FR-001/002, SC-001)

```bash
VAULT=$(mktemp -d); DB=$VAULT/vault
./bin/go-rag init --db-path "$DB" >/dev/null
# a fixture doc with several secret types
printf 'contact ops@example.com key AKIAIOSFODNN7EXAMPLE token ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789\n' > /tmp/secrets.md
./bin/go-rag add /tmp/secrets.md --redact --db-path "$DB"
./bin/go-rag query "AKIAIOSFODNN7EXAMPLE" --db-path "$DB"   # expect: No results (redacted)
./bin/go-rag query "ops@example.com" --db-path "$DB"        # expect: No results (redacted)
```

**Pass**: neither the AWS key nor the email is retrievable; the chunks carry
`[REDACTED:aws-key]` / `[REDACTED:email]` placeholders.

## Scenario 2 — Findings reported (US2, FR-004, SC-003)

```bash
./bin/go-rag add /tmp/secrets.md --redact --db-path "$DB" 2>&1 | grep -i redact
# expect: a "redacted: N (aws-key=1, email=1, github-token=1)" line in the summary
```

**Pass**: per-type redaction counts in the ingest summary + the audit log.

## Scenario 3 — Identity stable across redact/no-redact (FR-005, SC-004)

```bash
# ingest the same file twice (different isolated DBs), once redacted, once not;
# the DOCUMENT IDs match (identity over original)
RD=$(mktemp -d)/vault; PL=$(mktemp -d)/vault
./bin/go-rag init --db-path "$RD" >/dev/null; ./bin/go-rag init --db-path "$PL" >/dev/null
./bin/go-rag add /tmp/secrets.md --redact --db-path "$RD"
./bin/go-rag add /tmp/secrets.md --db-path "$PL"
./bin/go-rag files --db-path "$RD" | head -1   # compare doc IDs — they match (identity over original)
```

**Pass**: the same file produces the **same document ID** with and without `--redact`.

## Scenario 4 — Default-off: verbatim ingest, no regression (FR-003, SC-002/006)

```bash
./bin/go-rag add /tmp/secrets.md --db-path "$PL"          # no --redact → verbatim
./bin/go-rag query "AKIAIOSFODNN7EXAMPLE" --db-path "$PL" # expect: the secret IS found (verbatim)
make test-eval                                           # recall@10 unchanged (redaction off)
```

**Pass**: without `--redact`, the secret is retrievable (no behavior change); eval green.

## Scenario 5 — Reprocess rescan redacts the back-catalog (FR-007, SC-005)

```bash
# corpus ingested WITHOUT redact (verbatim), then reprocessed WITH redact
./bin/go-rag reprocess /tmp/secrets.md --redact --db-path "$PL"
./bin/go-rag query "AKIAIOSFODNN7EXAMPLE" --db-path "$PL" # expect: No results (now redacted)
```

**Pass**: after reprocess --redact, the previously-verbatim secret is redacted + gone.

## Done definition for this feature

All five scenarios pass + `go build ./...`, `go vet ./...`, `go test -race -cover ./...`
green + a redact-package unit test (each pattern type → correct placeholder) + an
identity-stability test (same docID redact/no-redact) + a default-off no-regression test +
no new go.mod dependency (Constitution III).
