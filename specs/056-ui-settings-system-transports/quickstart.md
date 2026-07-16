# Quickstart — Settings: System & Transports (Slice 1, spec 056)

> Runnable validation that the slice works end-to-end. Mirrors the 054/055 shape.
> Run against an ISOLATED daemon (not the global vault).

## Prerequisites

- Built binary: `make build` → `./bin/go-rag`
- Scratch vault: `export GR=/tmp/gorag-056`
- A known admin password: `export GORAG_ADMIN_PASSWORD=s3cret`

## Setup

```sh
mkdir -p $GR
GORAG_ADMIN_PASSWORD=$GORAG_ADMIN_PASSWORD ./bin/go-rag init --db-path $GR
./bin/go-rag start --db-path $GR \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880 --ui-addr 127.0.0.1:17881
# login → capture bearer token
TOK=$(curl -s -X POST http://127.0.0.1:17881/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"s3cret"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
```

## Validate US1 + US2 (system identity + transports — no egress)

1. `curl -s -H "Authorization: Bearer $TOK" http://127.0.0.1:17881/api/settings/system | jq`
   → returns `version`, `pid`, `uptime_seconds`, `schema{on_disk,expected,unified_store}`, `transports[]`, `bind_warning`.
2. `./bin/go-rag version` matches `systemStatusDTO.version`; the daemon PID matches `pgrep -f 'go-rag.*serve'`; `transports[].address` match the listening ports (`lsof`).
3. All transports show `loopback: true` + `bind_warning: ""` (loopback bind).

## Validate US3 (update-check — operator-initiated egress)

4. `curl -s -X POST -H "Authorization: Bearer $TOK" http://127.0.0.1:17881/api/settings/updates/check | jq`
   → `{current, latest, newer_available, checked_at}`. `current` == `./bin/go-rag version`.
5. Confirm NO network call happens on `GET /api/settings/system` (US1/US2) — the check is the only egress (SC-003).

## Validate boundary (US read-only + guard)

6. `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:17881/api/settings/system` (no bearer) → `401`.
7. Browser (Interceptor): Settings → System & Transports renders the identity/transports/schema panel; "Check for updates" button fires only on click.

## Teardown

```sh
./bin/go-rag stop --db-path $GR
for p in 17878 17879 17880 17881; do lsof -ti :$p | xargs kill -9 2>/dev/null; done
rm -rf $GR
```
