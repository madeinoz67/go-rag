# Quickstart — go-rag UI Console, Documents View (Slice 1 validation guide)

**Spec**: [spec.md](./spec.md) · **Contract**: [contracts/ui-documents.md](./contracts/ui-documents.md)

Runnable validation that the Documents view works end-to-end. Run on an **isolated DB**
with **non-default ports** so you do not collide with the user's live daemon (repo
CLAUDE.md smoke-test rule).

## Prerequisites

- `./bin/go-rag` built (`make build`).
- A throwaway DB path, e.g. `/tmp/gorag-docs-smoke`.
- A small known corpus to ingest (a handful of `.md`/`.txt`/`.pdf` files).
- Real Chrome via the **Interceptor** skill for the browser step.

## 1. Start the daemon with the UI transport

```bash
./bin/go-rag init --db-path /tmp/gorag-docs-smoke   # creates vault + admin (prints admin password ONCE — copy it)
./bin/go-rag add --db-path /tmp/gorag-docs-smoke ~/path/to/sample-corpus   # ingest a known small corpus
./bin/go-rag start \
  --db-path /tmp/gorag-docs-smoke \
  --mcp-addr 127.0.0.1:48788 \
  --rest-addr 127.0.0.1:48789 \
  --grpc-addr 127.0.0.1:48790 \
  --ui-addr  127.0.0.1:47881
```

**Admin password:** `init` prints it once (bcrypt-hashed, never retrievable); rotate via
`GORAG_ADMIN_PASSWORD=<pw> ./bin/go-rag init …`. No `go-rag auth` reset subcommand exists.

**Expected:** the daemon binds four loopback ports; the bound-address log line includes
`UI 127.0.0.1:47881`.

## 2. Authenticate (initialized vault → Bearer required)

```bash
TOK=$(curl -s -X POST http://127.0.0.1:47881/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<admin-pass>"}' | jq -r .token)
echo "${TOK:0:8}..."   # gorags_…
```

**Expected:** a `gorags_` token; **no `Set-Cookie`** (`curl -i …` to confirm).

## 3. Smoke — document list (US1)

```bash
curl -s "http://127.0.0.1:47881/api/documents?page_size=10" -H "Authorization: Bearer $TOK" | jq .
```

**Expected:** `{ "documents": […], "next_page_token": "…" }`; the row count (across pages)
matches `./bin/go-rag status --db-path /tmp/gorag-docs-smoke` document count (FR-013);
each row has `status`, `chunk_count`, and (if enriched) `tags`/`summary`. No Bearer → 401.

Status + tag filters:

```bash
curl -s "http://127.0.0.1:47881/api/documents?status=pending" -H "Authorization: Bearer $TOK" | jq '.documents | length'
curl -s "http://127.0.0.1:47881/api/documents?tag=security"    -H "Authorization: Bearer $TOK" | jq '.documents | length'
```

## 4. Smoke — document detail + chunks (US2)

```bash
DOC=$(curl -s "http://127.0.0.1:47881/api/documents?page_size=1" -H "Authorization: Bearer $TOK" | jq -r '.documents[0].id')
# Detail (source_path resolved here)
curl -s "http://127.0.0.1:47881/api/documents/$DOC" -H "Authorization: Bearer $TOK" | jq .
# Chunks (paginated)
curl -s "http://127.0.0.1:47881/api/documents/$DOC/chunks?page_size=5" -H "Authorization: Bearer $TOK" | jq '.chunks | length'
```

**Expected:** detail returns the full `documentDTO` with `source_path` populated; the
chunks page count ≤ the document's `chunk_count`; each chunk carries `section_context` /
`section_depth` where the source had headings. The total chunks (across pages) == the
document's `chunk_count` == `go-rag status` for that doc (zero drift, FR-013 / SC-004).

## 5. Smoke — content search (US3)

```bash
curl -s "http://127.0.0.1:47881/api/documents?q=<a+term+in+your+corpus>&limit=10" -H "Authorization: Bearer $TOK" | jq '.documents | length'
```

**Expected:** only documents whose chunk content matches the term are returned (ranked); a
term absent from the corpus returns an empty array, not an error.

## 6. Browser verify (Interceptor — mandatory for user-facing artifacts)

Drive the real flow in Chrome at `http://127.0.0.1:47881/`:

1. Login → shell loads; click **Documents** in the sidebar (item 2).
2. **List:** documents render with status badges + tags; counts match `go-rag status`.
3. Sort the current page by name / size / chunks / date — order changes.
4. Paginate — every document reachable once; no duplicates/missing.
5. Click a document → detail: metadata, summary/tags (or empty state), chunk list with
   section breadcrumbs.
6. Paginate chunks; select a chunk → text + section context render.
7. Search a content term → list narrows to matching documents; clear → full list returns.
8. Apply a tag/status filter → list narrows; clear → full list returns.
9. DevTools Network → every call is a `GET` to `/api/documents*` with the Bearer header;
   no `Set-Cookie` anywhere; no create/update/delete call issued (FR-009 / SC-005).

## 7. No-Node + read-only assertions

```bash
test ! -e package.json && echo "no package.json (correct)"
test ! -e node_modules   && echo "no node_modules (correct)"
# Every UI route is GET; grep the served handler registrations for POST/PUT/DELETE under /api/documents
```

**Expected:** both print "correct"; no write verb is registered under `/api/documents`.

## 8. Teardown

```bash
./bin/go-rag stop --db-path /tmp/gorag-docs-smoke
rm -rf /tmp/gorag-docs-smoke
```

## Pass criteria (Slice 1 done)

- List: row count matches `go-rag status`; status/tag filters work; pagination is complete.
- Detail: `source_path` resolved; chunk total == `chunk_count` == `go-rag status`.
- Search: content matches return only matching documents.
- Cross-transport parity: `documentDTO` byte-identical to REST/MCP; chunk shape identical.
- Read-only: every network call is a guarded `GET`; no writes possible.
- No Node/build artifacts; single binary serves everything.
