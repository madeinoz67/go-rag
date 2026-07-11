# Quickstart — Query View (Slice 2)

**Feature**: specs/048-ui-query-view | **Date**: 2026-07-11

A runnable validation guide proving the Query view works end-to-end. This is a validation/run
guide — implementation detail lives in `tasks.md`. Field/shape references point at
[data-model.md](./data-model.md) and [contracts/ui-query.md](./contracts/ui-query.md) rather
than duplicating them.

---

## Prerequisites

- Built binary: `make build` → `./bin/go-rag`.
- Local Ollama running with the corpus's embedding model (for semantic/hybrid mode). Keyword
  mode needs no embedder.
- `curl` for route smoke; the **Interceptor** skill (real Chrome) for browser render
  verification — mandatory per CLAUDE.md before any "it works" claim.
- An **isolated DB + non-default ports** for the daemon — never smoke against the global vault
  (`~/.go-rag/vaults/default`) or the default ports (CLAUDE.md standing rule).

---

## Setup (isolated daemon)

```sh
# 1. Isolated vault + non-default transport ports
TMPDB=$(mktemp -d)/vault
./bin/go-rag init --db-path "$TMPDB"
GORAG_ADMIN_PASSWORD=test-admin ./bin/go-rag init --db-path "$TMPDB"   # set a known admin pw

# 2. Ingest a small known corpus (distinct content + a tag)
./bin/go-rag --db-path "$TMPDB" add ./docs/samples/ --tags solar,tariff

# 3. Start the daemon on non-default ports (UI on 17881)
./bin/go-rag start --db-path "$TMPDB" \
  --mcp-addr 127.0.0.1:17878 \
  --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880 \
  --ui-addr 127.0.0.1:17881
./bin/go-rag status --db-path "$TMPDB"   # running, non-zero doc/chunk counts
```

---

## Scenario 1 — login + run a query (happy path)

```sh
# Login → get a Bearer session token
TOK=$(curl -s -X POST http://127.0.0.1:17881/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"test-admin"}' | jq -r .token)

# Run a query (see contracts/ui-query.md for the full body)
curl -s -X POST http://127.0.0.1:17881/api/query \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"query":"charge deadline","k":5,"mode":"hybrid"}' | jq .
```

**Expected**: HTTP 200; `hits[]` non-empty, each with `score`, `file_path`, `chunk_index`,
`section_context`; `effective_mode=="hybrid"`, `effective_k==5`; `rerank_failed==false`.

---

## Scenario 2 — cross-transport parity (spec FR-013)

Same query, same params, identical hits/order/scores across all three:

```sh
# UI transport
curl -s -X POST http://127.0.0.1:17881/api/query -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"query":"charge deadline","k":5,"mode":"hybrid"}' | jq '.hits[] | {c:.chunk_id,s:.score}'

# REST transport (same daemon, port 17879)
curl -s -X POST http://127.0.0.1:17879/v1/query -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"query":"charge deadline","k":5,"mode":"hybrid"}' | jq '.hits[] | {c:.chunk_id,s:.score}'

# CLI
./bin/go-rag --db-path "$TMPDB" query "charge deadline" -k 5 --mode hybrid --format json \
  | jq '.[] | {c:.chunk,s:.score}'
```

**Expected**: the three `chunk_id` / `score` lists are byte-for-byte identical. (Pinned by the
R12 parity test; this is the manual confirmation.)

---

## Scenario 3 — quarantine-by-default (spec FR-007)

```sh
# Ingest a chunk that will be flagged (injection-style content), then:
# Default query → flagged chunk absent:
curl -s -X POST http://127.0.0.1:17881/api/query -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d '{"query":"<flagged-term>"}' | jq '.hits|length'
# Opt-in → flagged chunk appears, WITH its poisoning verdict:
curl -s -X POST http://127.0.0.1:17881/api/query -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"query":"<flagged-term>","include_quarantined":true}' \
  | jq '.hits[] | {c:.chunk_id, verdict:.poisoning}'
```

**Expected**: default excludes the flagged chunk; opt-in returns it with a non-null `poisoning`
verdict (level/score/signals). The opt-in does **not** persist — a subsequent default query
excludes it again (R8).

---

## Scenario 4 — transparency + controls (spec FR-004/FR-005/FR-006)

```sh
# Threshold trims low-score hits; tag filter narrows; effective values echo back:
curl -s -X POST http://127.0.0.1:17881/api/query -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"query":"tariff","k":5,"threshold":0.5,"tags":["tariff"],"mode":"keyword"}' \
  | jq '{n:(.hits|length), mode:.effective_mode, k:.effective_k, all_above:( [.hits[].score]|all(. >= 0.5))}'
```

**Expected**: every returned hit has `score >= 0.5`; every hit's document carries the `tariff`
tag; `effective_mode=="keyword"`; rerank is off for keyword mode.

---

## Scenario 5 — error states (spec FR-012)

```sh
# Empty query → 400
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:17881/api/query \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"query":"   "}'

# Unauthorized → 401
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:17881/api/query \
  -H "Authorization: Bearer deadbeef" -H 'Content-Type: application/json' -d '{"query":"x"}'

# Embedder down (stop Ollama, then semantic query) → 503 with guidance
```

**Expected**: `400`, `401`, and (with Ollama stopped) `503 embedder unavailable`. No silent
empty-result on any error.

---

## Scenario 6 — browser render verification (Interceptor, mandatory)

Open the running daemon's console in real Chrome via the **Interceptor** skill:

1. `http://127.0.0.1:17881` → login with `admin` / `test-admin`.
2. Click the **Query** sidebar item → the Query view renders (not the placeholder).
3. Type a query, submit → ranked results render with score, citation, section breadcrumb.
4. Click a hit → detail opens with full text, section path, provenance signals.
5. Toggle mode/threshold/tags, resubmit → results + effective-state indicators update.
6. Open browser console → no 404s, no JS errors; confirm no `package.json`/`node_modules`
   exists in the repo (`make build` still yields one binary).

**Expected**: all six pass with no full-page reload (client-side nav inside `goragApp`). Per
CLAUDE.md this Interceptor check is the only sanctioned verification — `curl` 200 alone does
not count as "the view works."

---

## Teardown

```sh
./bin/go-rag stop --db-path "$TMPDB"
rm -rf "$(dirname "$TMPDB")"
```
