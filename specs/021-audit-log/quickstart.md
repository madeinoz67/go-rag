# Phase 1 — Quickstart: Structured Audit Log (H18)

> Runnable validation that the audit trail works end-to-end. Implementation detail
> belongs in `tasks.md`; this is a run/validate guide. Run on an **isolated** DB
> (`--db-path <tmp>` + non-default transport addrs) — never the live vault.

**Prerequisites**: `make build` succeeds. The audit log is local JSONL — no Ollama needed
for query/ingest/auth-fail event generation (auth-fail + query-on-empty both log without
embedding; a real ingest needs Ollama, but the event still records).

## Scenario 1 — Query/ingest/auth-fail each produce a typed record (US1, FR-001, SC-001)

```bash
VAULT=$(mktemp -d); DB=$VAULT/vault
./bin/go-rag init --db-path "$DB" >/dev/null
./bin/go-rag start --db-path "$DB" --mcp-addr 127.0.0.1:17878 \
                   --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
sleep 2
# a query (over REST) → query event
curl -s -X POST 127.0.0.1:17879/v1/query -d '{"query":"retrieval","mode":"keyword"}'
# an auth failure (no/wrong token on a guarded endpoint) → auth-fail event
curl -s 127.0.0.1:17879/v1/status   # if token set, this 401s → auth-fail
# read the audit log
cat "$DB/audit/audit.log"
```

**Pass**: the JSONL contains `query`, and `auth-fail` records with the correct types +
fields (ts, transport for auth-fail; query_hash/mode/hits for query).

## Scenario 2 — Query text is hashed, never plaintext (privacy, FR-002, SC-002)

```bash
# run a query with a distinctive string, then assert the string is NOT in the log
curl -s -X POST 127.0.0.1:17879/v1/query -d '{"query":"supersecret-sentinel-12345","mode":"keyword"}'
if grep -q "supersecret-sentinel-12345" "$DB/audit/audit.log"; then
  echo "FAIL: query plaintext leaked into the audit log"
else
  echo "PASS: only the query hash is logged"
fi
grep '"query_hash"' "$DB/audit/audit.log" | tail -1   # hash present
```

**Pass**: the plaintext does not appear; a `query_hash` field does.

## Scenario 3 — Reader filters by type + tail (US2, FR-007, SC-005)

```bash
./bin/go-rag audit --db-path "$DB" --tail 5
./bin/go-rag audit --db-path "$DB" --type query
./bin/go-rag audit --db-path "$DB" --since 1h
```

**Pass**: `--tail 5` shows ≤5 most-recent; `--type query` shows only query records;
`--since 1h` shows only recent records.

## Scenario 4 — Rotation bounds growth (FR-006, SC-004)

```bash
# cap very small, generate events, observe rotation
./bin/go-rag config set audit_log_max_bytes 1024 --db-path "$DB"
for i in $(seq 1 200); do curl -s -X POST 127.0.0.1:17879/v1/query -d '{"query":"x"}' >/dev/null; done
ls "$DB/audit/"   # audit.log + audit-1.log (rotated), each ≤ cap+line
```

**Pass**: a rotated archive (`audit-1.log`) exists; no single file grows unbounded.

## Scenario 5 — Air-gap + budgets (FR-005/008, SC-006)

```bash
# the log never leaves the host — confirm no forwarding config exists
./bin/go-rag config get audit_log_enabled --db-path "$DB"   # true (default-on)
go test -race ./...                                          # race-clean
go test ./internal/eval/                                     # recall@10 unchanged
```

**Pass**: default-on; no egress path; full suite + eval green; per-op cost within budgets.

## Done definition for this feature

All five scenarios pass + `go build ./...`, `go vet ./...`, `go test -race -cover ./...`
green + a privacy unit test (no plaintext) + an append/read/rotate unit test + an
integration test (the three event types) + no new go.mod dependency (Constitution III).
