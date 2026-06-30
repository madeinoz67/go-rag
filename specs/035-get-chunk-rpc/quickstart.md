# Quickstart — GetChunk RPC (spec 035)

> Phase 1 output of `/speckit-plan`. Runnable validation scenarios that prove
> `GetChunk` works end-to-end across all four transports. Implementation bodies
> live in `tasks.md`; message/field details live in `data-model.md` and
> `contracts/get-chunk.md` — this is a validation/run guide, not a spec.

## Prerequisites

- Built binary: `make build` → `./bin/go-rag` (pure Go, `CGO_ENABLED=0`).
- An **isolated** test vault — never script the daemon against the global default
  (`~/.go-rag/vaults/default`). Use a tmp path + non-default ports (project rule).
- A local Ollama running the configured embedder is **not** required to validate
  `GetChunk` (it is a read), but ingestion needs it.

## Setup — isolated vault with one ingested document

```bash
VAULT=$(mktemp -d)/vault
go-rag add --db-path "$VAULT" path/to/sample.pdf     # ingest → chunks get content-addressed IDs
# Capture a real chunk_id for the scenarios below:
go-rag chunk get --help >/dev/null 2>&1 || true       # (command added by this spec)
CID=$(go-rag --db-path "$VAULT" doc list --json | jq -r '.[0].chunks[0].chunk_id // empty')
# NOTE: exact listing command shape is finalized in tasks.md; the point is to
# obtain one real content-addressed chunk_id from an ingested document.
```

---

## Scenario 1 — Resolve a valid `chunk_id` (Story 1, FR-001/004/005)

**Goal:** a valid `chunk_id` resolves to the full chunk + parent document metadata in one call.

```bash
go-rag --db-path "$VAULT" chunk get "$CID" --json | jq .
```

**Expected:** a JSON object with `chunk` (full content, `chunk_index`, `page_number`,
`section_context`, `poisoning`, `next_chunk_id`, …) **and** `document`
(`file_path`, `file_type`, `status`, `summary`, `enrichment_status`, …). The
`chunk.content` matches the text produced at ingestion. ✅ FR-001, FR-004, FR-005.

---

## Scenario 2 — Missing / stale `chunk_id` → not-found (Story 1, FR-002)

**Goal:** a `chunk_id` that was never ingested returns a clear not-found — never an empty chunk.

```bash
 go-rag --db-path "$VAULT" chunk get "0".repeat(64) --json ; echo "exit=$?"
```

**Expected:** non-zero exit, `chunk not found: <id>` on stderr. No partial chunk. ✅ FR-002.
*(Stale IDs after re-chunking hit the same path — content-addressed identity means
an unchanged chunk keeps its ID; a changed chunk gets a new one, so the old ID
ceases to resolve.)*

---

## Scenario 3 — Cross-vault isolation (Story 1, FR-003)

**Goal:** a `chunk_id` that exists in *another* vault is not disclosed.

```bash
OTHER=$(mktemp -d)/other-vault
# Ingest the same file into a second vault; that chunk_id is valid there.
# From the FIRST vault, that ID must be not-found (it isn't in this store).
CID_OTHER=$(go-rag --db-path "$OTHER" ...)   # a chunk_id valid only in $OTHER
go-rag --db-path "$VAULT" chunk get "$CID_OTHER" --json ; echo "exit=$?"
```

**Expected:** not-found (non-zero exit) — the engine is single-vault-per-process, so
a `chunk_id` absent from the bound vault resolves to not-found. The chunk from the
other vault is never returned. ✅ FR-003. *(No separate check needed — see
`contracts/get-chunk.md` Not-found contract.)*

---

## Scenario 4 — Malformed / empty `chunk_id` → invalid input (FR-009)

```bash
go-rag --db-path "$VAULT" chunk get "" ;           echo "exit=$?"   # empty
go-rag --db-path "$VAULT" chunk get "   " ;        echo "exit=$?"   # whitespace
go-rag --db-path "$VAULT" chunk get "not-a-sha" ;  echo "exit=$?"   # malformed
```

**Expected:** non-zero exit, clear invalid-input error, **no scan** (constant-time
rejection). ✅ FR-009.

---

## Scenario 5 — Document metadata reflects current state (Story 2, FR-005)

**Goal:** after re-ingest or enrichment, the returned `document` reflects the current state.

```bash
# Re-ingest or run enrichment on the same source, then:
go-rag --db-path "$VAULT" chunk get "$CID" --json | jq '.document.enrichment_status, .document.summary'
```

**Expected:** the enrichment status / summary match the document's *current* state
(e.g. `enriched` with a populated `summary` after spec-029 enrichment has run). ✅ FR-005.

---

## Scenario 6 — Cross-transport parity (Story 3, FR-006) — the headline invariant

**Goal:** the same `chunk_id` returns byte-identical chunk + document metadata over gRPC, REST, MCP, and CLI.

Start an isolated daemon (non-default ports):
```bash
go-rag start --db-path "$VAULT" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
```

Fetch the same `$CID` over each transport and compare normalised output:
```bash
# CLI
go-rag --db-path "$VAULT" chunk get "$CID" --json | jq -S . > /tmp/cli.json
# REST
curl -s "http://127.0.0.1:17879/v1/chunks/$CID" | jq -S . > /tmp/rest.json
# gRPC (grpcurl; service/package from proto/gorag.proto)
grpcurl -plaintext -d "{\"chunk_id\":\"$CID\"}" 127.0.0.1:17880 gorag.Gorag/GetChunk | jq -S . > /tmp/grpc.json
# MCP (tools/call go_rag_get_chunk over JSON-RPC to :17878) -> /tmp/mcp.json

diff /tmp/cli.json /tmp/rest.json && diff /tmp/cli.json /tmp/grpc.json && echo "PARITY OK"
```

**Expected:** identical chunk content, identical document metadata, identical
not-found mapping per transport (`404` REST / `NOT_FOUND` gRPC / `-32001` MCP /
non-zero CLI exit). ✅ FR-006, SC-001.

---

## Scenario 7 — Latency is corpus-size-independent (SC-003)

**Goal:** confirm the read is a constant-time point lookup, not a scan.

```bash
# Time a fetch against a small vault, then against a large one (10s of chunks
# vs 100k+). Latency should stay single-digit ms in both.
go-rag --db-path "$VAULT" chunk get "$CID" --json >/dev/null
# A Go test (tasks.md) asserts this directly with a benchmark-style timing loop.
```

**Expected:** flat latency regardless of corpus size (two Pebble point reads, no
scan). ✅ SC-003, FR-007. *(The authoritative check is a `go test` benchmark over
a synthetically-large vault — see `tasks.md`.)*

---

## Teardown

```bash
go-rag stop --db-path "$VAULT" 2>/dev/null || true
rm -rf "$(dirname "$VAULT")" "$(dirname "$OTHER")"
```

## What this does NOT cover

- `GetChunkContext` (BL-002) and `BatchGetChunks` (BL-003) are separate specs;
  `GetChunk` exposes `previous_chunk_id` / `next_chunk_id` but does not traverse them.
- Query-time signals (poisoning/quarantine filtering) do not apply — `GetChunk`
  returns the addressed chunk as stored; the poisoning verdict travels with it as
  metadata (see spec Edge Cases).
- No new stored state, no schema migration — `migrate.ExpectedVersion` stays at 1
  (`research.md` R4).
