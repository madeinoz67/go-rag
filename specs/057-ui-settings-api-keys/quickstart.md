# Quickstart — Settings: API Keys (Slice 2a, spec 057)

> Runnable validation. Mirrors the 050/055 shape. Run against an ISOLATED daemon.

## Prerequisites

- Built binary: `make build` → `./bin/go-rag`
- Scratch vault: `export GR=/tmp/gorag-057`; known admin: `export GORAG_ADMIN_PASSWORD=s3cret`

## Setup

```sh
mkdir -p $GR
GORAG_ADMIN_PASSWORD=$GORAG_ADMIN_PASSWORD ./bin/go-rag init --db-path $GR
./bin/go-rag start --db-path $GR \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880 --ui-addr 127.0.0.1:17881
TOK=$(curl -s -X POST http://127.0.0.1:17881/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"s3cret"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
```

## Validate US1 (list — no secret)

1. `curl -s -H "Authorization: Bearer $TOK" http://127.0.0.1:17881/api/settings/auth/api-keys`
   → `[]` (empty list, no `secret` field anywhere).

## Validate US2 (create — secret once)

2. `curl -s -X POST -H "Authorization: Bearer $TOK" http://127.0.0.1:17881/api/settings/auth/api-keys -H 'Content-Type: application/json' -d '{"label":"ci","mode":"read"}' | python3 -m json.tool`
   → `{id, label:"ci", mode:"read", created_at, expires_at:"", enabled:true, secret:"gorag_….<secret>"}`.
3. List again → the new key appears **without** `secret` (SC-002: secret in exactly one place).

## Validate US3 (revoke — immediate)

4. `ID=<the id from step 2>; curl -s -o /dev/null -w "%{http_code}\n" -X DELETE -H "Authorization: Bearer $TOK" http://127.0.0.1:17881/api/settings/auth/api-keys/$ID`
   → `204`. The revoked bearer now fails: `curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $SECRET" http://127.0.0.1:17881/api/settings/auth/api-keys` → `401`.
5. List → the key appears with `enabled:false` (audit trail), still no secret.

## Validate boundary

6. No bearer → `401` on all three routes. Invalid mode (`{"label":"x","mode":"foo"}`) → `400`.
7. Browser (Interceptor): Settings → API Keys — sortable table + create dialog (secret shown once) + revoke confirm.

## Teardown

```sh
./bin/go-rag stop --db-path $GR
for p in 17878 17879 17880 17881; do lsof -ti :$p | xargs kill -9 2>/dev/null; done
rm -rf $GR
```
