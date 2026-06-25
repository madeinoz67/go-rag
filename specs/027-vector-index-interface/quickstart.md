# Quickstart — Swappable Vector Index (H27, spec 027)

**Phase 1 output.** A validation runbook proving the feature works end-to-end and
— critically, because this is a behaviour-preserving refactor — that it changes
**nothing** observable. Every scenario maps to a Success Criterion in
[spec.md](./spec.md).

> **Smoke rule (repo `CLAUDE.md`):** the default `dbPath` is the global vault
> (`~/.go-rag/vaults/default`). When scripting the daemon, always pass
> `--db-path <tmp>` plus non-default `--mcp-addr/--rest-addr/--grpc-addr`. The
> checks below are library/unit/eval checks — no daemon needed.

---

## Prerequisites

- Go 1.22+, `CGO_ENABLED=0` (PRD §10.4).
- A local Ollama with the project's embedding model reachable (only needed for
  the eval-harness scenario SC-002; the interface/conformance/parity checks need
  no model).
- Baseline recorded: before the change, `make build vet test` is green and
  `make test-eval` (spec 004 harness) shows the current recall.

## Build gate

```bash
make build vet test        # CGO_ENABLED=0 build, go vet, go test -race -cover ./...
```

**Expected:** green. This is SC-003's foundation — the full existing suite passes
unchanged after a pure structural extraction.

---

## SC-001 / SC-004 — The seam is real and the contract holds

The reference `*Vector` honours the three invariants, and Retrieval depends on
the contract (proven by wiring a second implementation).

```bash
go test -race -run 'VectorContract' ./internal/index/   # conformance: *Vector honours invariants 1/2/3
go test -race -run 'Retrieval'     ./internal/index/    # seam: Retrieval + a fake VectorIndex → identical results
```

**Expected (SC-001):** the seam test wires `Retrieval` to a fake `VectorIndex`
holding the same vectors as a real `*Vector`; for the same query, both return
identical ranked `[]Hit` — proving Retrieval depends on the contract, not the
concrete type.

**Expected (SC-004):** the conformance test asserts:
1. a mixed-dimensionality corpus → mismatched vectors are skipped, never scored
   (Invariant 1 / FR-002);
2. repeated identical queries → byte-identical order, ties by chunk-ID (Invariant
   2 / FR-003);
3. concurrent `Add`+`Query` under `-race` → no race (Invariant 3 / FR-004).

## SC-002 — Retrieval quality is unchanged (eval harness)

```bash
make test-eval            # spec 004 retrieval-eval harness
```

**Expected:** recall is **identical** to the pre-feature baseline — the
structural change is quality-neutral (no regression). The brute-force `Query`
body is untouched, so scores and ranking are bit-for-bit the same.

## SC-003 / SC-005 — Cross-transport parity + nothing observable changed

```bash
go test -race ./internal/engine/    # incl. parity_test.go: CLI/REST/gRPC/MCP identical
```

**Expected (SC-003):** cross-transport parity tests pass unchanged — the same
query over CLI/REST/gRPC/MCP returns identical responses.

**Expected (SC-005):** no second backend is shipped; grep confirms
`*index.Vector` remains the only `VectorIndex` implementation in the tree, and
no transport/proto/config file was modified:

```bash
git diff --stat main -- internal/rest internal/grpc internal/mcp internal/cli proto/   # empty
git grep 'VectorIndex' -- '*.go' | grep -v _test          # only the interface def + Retrieval.vec field
```

---

## Summary of expected outcomes

| SC | Check | Command | Pass condition |
|----|-------|---------|----------------|
| SC-001 | seam is real | `go test -run Retrieval ./internal/index` | fake backend == real backend results |
| SC-002 | quality-neutral | `make test-eval` | recall identical to baseline |
| SC-003 | parity unchanged | `go test ./internal/engine` | parity tests green, unchanged |
| SC-004 | contract enforced | `go test -run VectorContract ./internal/index` | 3 invariants hold on `*Vector` |
| SC-005 | no backend shipped, no surface change | `git diff --stat` + `git grep VectorIndex` | only `internal/index` + `Retrieval.vec` touched |

If all five pass, the escape hatch is landed safely: Retrieval depends on the
contract, the contract is enforced, and nothing the user can observe has changed.
