# Quickstart — Crash-Safe Background Embedder (spec 030)

**Phase 1 output.** A validation runbook proving the headline benefit (crash-safe
embedding) + the throughput/resilience/compatibility properties. Every scenario maps to a
Success Criterion in [spec.md](./spec.md).

> **Smoke rule (repo `CLAUDE.md`):** use an **isolated DB** (`--db-path <tmp>`) + non-default
> daemon addrs; never touch the live vault. These checks are unit/integration (the
> kill-restart one needs the daemon). Needs a local Ollama with an embedding model for the
> real-embed steps; the structural/unit checks need none.

---

## Prerequisites

- Go 1.22+, `CGO_ENABLED=0`.
- A local Ollama with an embedding model (e.g. `nomic-embed-text`) for the live checks.
- Baseline: `make build vet test` + `make test-eval` green before the change.

## Build gate

```bash
make build vet test
```

**Expected:** green. SC-005's foundation — the refactor is structural; the existing suite
+ eval pass unchanged.

---

## SC-001 — Crash-safe embedding (the headline)

The defining test: a crash mid-embed must not orphan a document.

```bash
# ingest a doc, then KILL the daemon before its embedding lands
go run ./cmd/go-rag start --db-path "$DB" --mcp-addr 127.0.0.1:17878 & DAEMON=$!
sleep 1  # let it ingest + ACK
# add a doc (ACKs fast; embedding is background) then kill BEFORE embed completes
go run ./cmd/go-rag add doc.md --db-path "$DB"
kill -9 $DAEMON         # SIGKILL mid-embed
# restart — the embedder's startup scan must recover the pending embedding
go run ./cmd/go-rag start --db-path "$DB" --mcp-addr 127.0.0.1:17878 &
sleep <embed tick>
# the doc MUST be retrievable by semantic search — no manual re-ingest
go run ./cmd/go-rag query "<phrase>" --db-path "$DB"   # EXPECT: the doc is returned
```

**Pass:** after kill+restart, the doc is returned by semantic search — the background
embedder recovered the pending 0x14 record. (A unit test simulates this without the
daemon: write a chunk + 0x14, run the embedder's recovery scan, assert 0x04 written +
vec.Add + 0x14 removed.)

## SC-002 — <10ms ACK unchanged

```bash
go test ./internal/pipeline/   # asserts the ACK-path write (0x03+0x14 atomic) is <10ms
```

**Pass:** the write-ACK latency is identical to the pre-feature baseline — embedding is
strictly post-ACK.

## SC-003 — Backend down → graceful

```bash
# Ollama unreachable: ingest must still ACK + succeed; the backlog reflects pending
go run ./cmd/go-rag add doc.md --db-path "$DB"   # enrichment/embedding backend down
go run ./cmd/go-rag status --db-path "$DB"        # EXPECT: embed_pending > 0, no errored docs
```

**Pass:** ingestion ACKs + succeeds (docs stored); `embed_pending` reflects the backlog;
the circuit breaker prevents a stall; no infinite retry.

## SC-004 — Cross-document batching

```bash
go test ./internal/embedproc/   # asserts a bulk queue drains in ⌈N/MaxBatchSize⌉ Embed calls, not N
```

**Pass:** N pending chunks embed in ~N/32 calls (one per micro-batch), not one-per-chunk.

## SC-005 — Outputs unchanged (structural refactor)

```bash
make test-eval          # retrieval-eval recall identical to baseline
go test ./...           # full suite green (incl. cross-transport parity)
```

**Pass:** recall@10 identical; embeddings/vectors/identity/query results byte-identical
before vs after — the refactor moved only the embed scheduling + recovery, not its output.

---

## Summary of expected outcomes

| SC | Check | Pass condition |
|----|-------|----------------|
| SC-001 | crash-safe | kill mid-embed + restart → doc retrievable, no re-ingest |
| SC-002 | ACK preserved | <10ms write ACK unchanged |
| SC-003 | backend-down graceful | ingest ACKs; backlog visible; no stall/loop |
| SC-004 | cross-doc batch | N chunks → ~N/32 Embed calls |
| SC-005 | outputs unchanged | recall + parity + identity identical before/after |

If all five pass, embedding is crash-safe, decoupled, batched, and resilient — and the
database's core retrieval guarantees (identity, ACK budget, output parity) are intact.
