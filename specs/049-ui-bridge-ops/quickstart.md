# Quickstart — Bridge Ops View (Slice 3)

**Feature**: specs/049-ui-bridge-ops | **Date**: 2026-07-12

A runnable validation guide proving the Bridge Ops view works end-to-end. Validation/run guide
— implementation detail lives in `tasks.md`. Shape references point at [data-model.md](./
data-model.md) and [contracts/ui-bridge-ops.md](./contracts/ui-bridge-ops.md) rather than
duplicating them.

---

## Prerequisites

- Built binary: `make build` → `./bin/go-rag`.
- Local Ollama running with the corpus's embedding model (to produce a real backlog drain).
- `curl` for route smoke; the **Interceptor** skill (real Chrome) for browser render
  verification — mandatory per CLAUDE.md before any "it works" claim.
- An **isolated DB + non-default ports** — use `serve --db-path <tmp>` (which isolates
  correctly; note: `go-rag start --db-path <tmp>` has an isolation quirk, prefer `serve` for
  smoke), never the global vault / default ports.

---

## Setup (isolated daemon)

```sh
TMPDB=$(mktemp -d)/vault
DOC=$(mktemp -d)/smoke.md
echo "# Smoke corpus for Bridge Ops. Solar deficit controller. Charge deadline." > "$DOC"

GORAG_ADMIN_PASSWORD=smoke-admin ./bin/go-rag init --db-path "$TMPDB"
./bin/go-rag add --db-path "$TMPDB" "$DOC"           # indexes FTS sync; embeds async

# Isolated daemon via serve (honours --db-path), non-default ports:
./bin/go-rag serve --db-path "$TMPDB" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880 --ui-addr 127.0.0.1:17881 &
sleep 2

TOK=$(curl -s -X POST http://127.0.0.1:17881/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"smoke-admin"}' | jq -r .token)
```

---

## Scenario 1 — stats: backlog + drift + subsystems + watch (spec US1/US3/US4)

```sh
curl -s http://127.0.0.1:17881/api/bridge-ops/stats -H "Authorization: Bearer $TOK" | jq .
```

**Expected**: `backlog.complete` may be false mid-embed; `drift.verdict` present; every subsystem
tile present (poisoning/enrichment/caches/adaptive); `watch.scan_driven == true`.

**Parity**: the backlog counts + drift verdict + subsystem states match `go-rag status` for the
same vault.

---

## Scenario 2 — activity: recent ingest events (spec US2)

```sh
curl -s "http://127.0.0.1:17881/api/bridge-ops/activity?tail=20&type=ingest" \
  -H "Authorization: Bearer $TOK" | jq .
```

**Expected**: at least one `ingest` event (the `add` above), most-recent first, with
`timestamp` + `summary` + `outcome`. **Parity**: matches `go-rag audit --type ingest --tail 20`
for the same vault.

---

## Scenario 3 — read-only + guard + empty + validation (spec US4 / FR-008/FR-011/FR-012)

```sh
# 401 without Bearer
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:17881/api/bridge-ops/stats

# 400 invalid type
curl -s -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:17881/api/bridge-ops/activity?type=bogus" \
  -H "Authorization: Bearer $TOK"

# read-only: every Bridge Ops route is GET
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:17881/api/bridge-ops/stats \
  -H "Authorization: Bearer $TOK"      # expect 405 (Method Not Allowed)
```

**Expected**: `401`, `400`, `405`. No write verb registered; the query path mutates nothing
(snapshot `go-rag status` counts before/after a stats+activity fetch → identical).

---

## Scenario 4 — browser render (Interceptor, mandatory)

Open `http://127.0.0.1:17881` in real Chrome via the **Interceptor** skill:

1. Login (`admin` / `smoke-admin`).
2. Click the **Bridge Ops** sidebar item → the REAL view renders (health tiles + drift detail +
   subsystem tiles + watch dirs + a recent-activity list), NOT the placeholder panel.
3. Confirm the backlog tile, drift verdict, and at least one subsystem tile render values that
   match the `curl` stats; the activity list shows the ingest event.
4. Hard-refresh → values re-fetch (manual refresh, no streaming).
5. Open the browser console → no 404s, no JS errors; confirm no `package.json`/`node_modules`
   in the repo.

**Expected**: all five pass with no full-page reload. Per CLAUDE.md this Interceptor check is
the only sanctioned verification — `curl` 200 alone does not count.

---

## Teardown

```sh
kill %1 2>/dev/null   # the background serve
rm -rf "$(dirname "$TMPDB")" "$(dirname "$DOC")"
```
