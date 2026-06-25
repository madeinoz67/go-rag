# Contract — VectorIndex (H27, spec 027)

**Phase 1 output.** This is an **internal interface contract**, not an external
API. H27 changes no transport, no proto, no CLI, no on-disk shape (FR-006) —
there is nothing new exposed to users or other systems. The deliverable *is* the
contract, so it is documented here in full.

The contract is the obligation any nearest-neighbour backend must meet to be a
valid `VectorIndex`. The brute-force `*Vector` is the reference implementation
that defines correct behaviour.

---

## Interface surface

```go
// VectorIndex is the implementation-neutral vector store contract. Retrieval
// depends on this interface, not on any concrete backend, so the
// nearest-neighbour implementation is substitutable (audit H27). The brute-force
// *Vector is the reference implementation.
//
// Implementations MUST honour the three invariants below — they are correctness
// obligations, not optional. A backend that cannot honour them must be wrapped
// or rejected at construction (FR-009); never silently accepted.
type VectorIndex interface {
    // Add stores (or replaces) the vector for chunk id.
    Add(id string, vec []float32)

    // Delete removes the vector for chunk id (no-op if absent).
    Delete(id string)

    // Query returns the top-k chunks by similarity to vec, most-similar first,
    // as Hit{ChunkID, Score}. Score is similarity in [-1, 1] for the reference
    // cosine backend.
    Query(vec []float32, k int) []Hit
}
```

All members operate over existing types — string chunk-IDs, `[]float32`, and the
shared `Hit` result type. No new type is introduced.

---

## Invariant 1 — Dimensionality-skip (FR-002)

A stored vector whose length differs from the query vector's length MUST be
**excluded** from `Query` results, never scored.

- **Why:** cosine over `min(len(a), len(b))` dimensions returns a
  plausible-but-wrong similarity for a model/dimensionality mismatch — the
  silent-corruption failure mode (audit H03). The guard exists to make a mixed or
  mid-migration corpus fail safe.
- **Reference behaviour:** `Vector.Query` skips via `if len(cv) != len(vec) { continue }`.
- **Conformance:** a corpus containing vectors of two dimensionalities, queried
  with one, returns only same-dimensionality hits.

## Invariant 2 — Determinism + stable tie-break (FR-003)

For an identical `(corpus contents, query vector, k)`, `Query` MUST return
identical results in identical order across calls. Equal-score results MUST
resolve by a stable, deterministic rule.

- **Why:** retrieval-eval (H02) and cross-transport parity (SC-003) assume
  reproducible ranking. Approximate/ANN backends can vary run-to-run — such a
  backend must stabilise its output (e.g. deterministic seed + secondary sort) or
  be rejected.
- **Reference behaviour:** `Vector.Query` sorts by score desc, then `ChunkID` asc.
- **Conformance:** repeated identical queries return byte-identical `[]Hit`; a
  crafted equal-score pair always resolves in chunk-ID order.

## Invariant 3 — Concurrency-safety (FR-004)

The backend MUST be safe for concurrent `Add`/`Delete` (pipeline ingest workers)
and `Query` (retrieval) without external synchronisation.

- **Why:** the pipeline mutates the shared live index from background workers
  while queries read it (Constitution IV's async-after-ACK model).
- **Reference behaviour:** `Vector` guards all access with `sync.Mutex`.
- **Conformance:** `go test -race` over concurrent `Add`+`Query` reports no race.

---

## Conformance rules (FR-009)

A `VectorIndex` implementation is **valid** iff it passes the conformance test
suite (`internal/index/vector_contract_test.go`) — i.e. it honours all three
invariants over the same fixtures the reference `*Vector` does.

| Situation | Required action |
|-----------|-----------------|
| Backend honours all three invariants natively | accept — drop-in |
| Backend misses the dimensionality-skip | **wrap** in a guard decorator that filters `Query` output (or pre-filters on add) |
| Backend is non-deterministic | **wrap** with a stabilising secondary sort, or **reject** |
| Backend cannot be made concurrency-safe | **reject** at construction |

No wrapper or second backend ships in this finding (FR-005). The table is the
policy a future backend integration (separate finding) must follow.

---

## What is explicitly NOT in the contract

- **Persistence (`Save`/`Load`).** Backend-specific; the store is seeded from
  Pebble `0x04` by `LoadIndex`, not from a snapshot (R3, FR-007).
- **A unified FTS+Vector `Index`.** FTS is Pebble-backed with a different shape;
  no shared contract exists (R1).
- **Integer/owned IDs.** The contract is string chunk-IDs only (FR-008).
- **Any transport/proto/CLI surface.** None added — the seam is internal (FR-006).
