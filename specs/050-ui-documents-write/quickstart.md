# Quickstart — Documents Write-Actions (Slice 4)

**Feature**: specs/050-ui-documents-write | **Date**: 2026-07-12

A runnable validation guide proving the three write actions work end-to-end. Validation/run
guide — implementation detail lives in `tasks.md`. Shape references point at [data-model.md]
(./data-model.md) and [contracts/ui-documents-write.md](./contracts/ui-documents-write.md).

---

## Prerequisites

- Built binary: `make build` → `./bin/go-rag`.
- Local Ollama with the corpus's embedding model (for the async embed to complete).
- `curl`; the **Interceptor** skill for browser verification (mandatory per CLAUDE.md).
- An **isolated DB via `serve --db-path <tmp>`** on non-default ports (the `start --db-path`
  isolation quirk noted in 048/049). Never the global vault / default ports.

---

## Setup (isolated daemon)

```sh
TMPDB=$(mktemp -d)/vault
DOC=$(mktemp -d)/write-smoke.md
echo "# Write smoke. Solar deficit. Charge deadline tariff." > "$DOC"

GORAG_ADMIN_PASSWORD=smoke-admin ./bin/go-rag init --db-path "$TMPDB" >/dev/null 2>&1
nohup ./bin/go-rag serve --db-path "$TMPDB" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880 --ui-addr 127.0.0.1:17881 >/tmp/wserve.log 2>&1 &
sleep 2
TOK=$(curl -s -X POST http://127.0.0.1:17881/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"smoke-admin"}' | jq -r .token)
```

---

## Scenario 1 — add a document (spec US1)

```sh
curl -s -X POST http://127.0.0.1:17881/api/documents -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d "{\"path\":\"$DOC\"}" | jq .
# expect {new:1, skipped:0, errors:0, path:...}
```
**Parity**: `go-rag add --db-path "$TMPDB" "$DOC"` ingests the same document (same doc ID).
The doc appears in `GET /api/documents` and in `go-rag status`; the Operations backlog rises
then drains as embedding completes.

---

## Scenario 2 — remove a document (spec US2)

```sh
DOCID=$(curl -s "http://127.0.0.1:17881/api/documents" -H "Authorization: Bearer $TOK" \
  | jq -r '.documents[0].id')
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE \
  "http://127.0.0.1:17881/api/documents/$DOCID" -H "Authorization: Bearer $TOK"
# expect 204
```
**Expected**: 204; the doc is gone from `GET /api/documents`, from `go-rag status`, and from
query results. **The source file `$DOC` still exists on disk** (index-only removal). Unknown ID
→ 404.

---

## Scenario 3 — reingest a document (spec US3)

```sh
# re-add (scenario 1), change the source, then reingest:
curl -s -X POST http://127.0.0.1:17881/api/documents -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d "{\"path\":\"$DOC\"}" >/dev/null
echo "# Changed content. New tariff structure." > "$DOC"
DOCID=$(curl -s "http://127.0.0.1:17881/api/documents" -H "Authorization: Bearer $TOK" | jq -r '.documents[0].id')
curl -s -X POST "http://127.0.0.1:17881/api/documents/$DOCID/reingest" -H "Authorization: Bearer $TOK" | jq .
# expect {new:1, skipped:0, errors:0, path:...}
```
**Parity**: the reingested chunks match `go-rag reprocess --db-path "$TMPDB" "$DOC"`. Reingest
of a doc whose source vanished → 404 `source not found`.

---

## Scenario 4 — guard, errors, observability (spec US4)

```sh
# 401 without Bearer
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:17881/api/documents \
  -H 'Content-Type: application/json' -d '{"path":"/x"}'                      # 401
# 400 empty path
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:17881/api/documents \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"path":"   "}'   # 400
# 404 unknown doc
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE http://127.0.0.1:17881/api/documents/deadbeef \
  -H "Authorization: Bearer $TOK"                                            # 404
# after a write, an audit event + Operations backlog change are observable
curl -s "http://127.0.0.1:17881/api/operations/activity?type=ingest" -H "Authorization: Bearer $TOK" | jq .count
```
**Expected**: 401, 400, 404, and a non-zero ingest activity count after the add. No write route
reachable unauthenticated; no partial mutation.

---

## Scenario 5 — cross-transport delete parity (constitution V)

```sh
# engine ≡ CLI ≡ REST ≡ gRPC ≡ MCP for delete (the new operation)
curl -s -X POST http://127.0.0.1:17881/api/documents -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d "{\"path\":\"$DOC\"}" >/dev/null
DOCID=$(curl -s "http://127.0.0.1:17881/api/documents" -H "Authorization: Bearer $TOK" | jq -r '.documents[0].id')
# UI delete
curl -s -o /dev/null -w 'UI: %{http_code}\n' -X DELETE "http://127.0.0.1:17881/api/documents/$DOCID" -H "Authorization: Bearer $TOK"
# re-add, then CLI delete
curl -s -X POST http://127.0.0.1:17881/api/documents -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d "{\"path\":\"$DOC\"}" >/dev/null
./bin/go-rag delete --db-path "$TMPDB" "$DOCID"   # (stop the daemon first if lock conflicts)
```
**Expected**: delete removes the doc identically across every transport (pinned by the
cross-transport parity test, R10).

---

## Scenario 6 — browser render (Interceptor, mandatory)

Open `http://127.0.0.1:17881` in real Chrome via **Interceptor**:
1. Login (`admin` / `smoke-admin`).
2. Documents view → an **Add** button + dialog (path + optional glob); per-row **Remove** and
   **Reingest** actions.
3. Add `$DOC` → it appears in the list (pending → embedded).
4. Remove a row → confirm dialog → row disappears; source file intact.
5. Reingest a row → confirm dialog → re-derives.
6. Console → no 404s/JS errors; no `package.json`/`node_modules`.

**Expected**: all pass, no full-page reload. Per CLAUDE.md, Interceptor is the only sanctioned
verification — `curl` 200 alone does not count.

---

## Teardown

```sh
kill %1 2>/dev/null; rm -rf "$(dirname "$TMPDB")" "$(dirname "$DOC")" /tmp/wserve.log
```
