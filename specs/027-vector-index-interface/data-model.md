# Data Model — Swappable Vector Index (H27, spec 027)

**Phase 1 output.** This finding adds **no new persisted entity and no on-disk
change** (FR-006). It adds one in-memory abstraction (`VectorIndex`) and reframes
the existing `Vector` as the reference implementation of that abstraction. All
storage (Pebble prefixes `0x03` chunks, `0x04` embeddings) is unchanged.

---

## 1. `VectorIndex` — the contract (NEW, in-memory only)

The implementation-neutral agreement defining what any nearest-neighbour backend
MUST provide. Lives in `internal/index`. Not persisted; it is a Go interface.

| Member | Signature (conceptual) | Purpose | Used by |
|--------|------------------------|---------|---------|
| `Add` | `Add(id string, vec []float32)` | store/replace a chunk's vector | pipeline embed path; `LoadIndex` cold-start seed |
| `Delete` | `Delete(id string)` | remove a chunk's vector | document deletion |
| `Query` | `Query(vec []float32, k int) []Hit` | top-k nearest neighbours by cosine similarity | `Retrieval.semantic` (the sole query consumer) |

**Invariants (contract obligations, not implementation detail — see
[contracts/vector-index.md](./contracts/vector-index.md)):**

1. **Dimensionality-skip (FR-002).** A stored vector whose length ≠ the query
   vector's length is **excluded** from scoring — never scored over
   `min(len(a),len(b))` dimensions. This is the H03 anti-silent-corruption guard.
2. **Determinism (FR-003).** For an identical `(corpus, query, k)`, `Query`
   returns identical results in identical order; equal-score results resolve by a
   stable tie-break (ascending chunk-ID).
3. **Concurrency-safety (FR-004).** Safe for concurrent `Add`/`Delete` (ingest
   workers) and `Query` (retrieval) without external synchronisation.

**Relationships.** `Retrieval.vec` is typed `VectorIndex` (was `*Vector`).
`*Vector` satisfies `VectorIndex` structurally — it is the reference
implementation.

---

## 2. `Vector` — the reference implementation (UNCHANGED shape)

The existing brute-force, in-memory, goroutine-safe store
(`internal/index/vector.go`). **Its fields, `Add`, `Delete`, `Query`, `Save`,
`Load`, and `cosine` are unchanged.** The only edit is the type doc comment,
updated to state that `Vector` is the reference implementation of `VectorIndex`
(replacing the vaguer "mirrors a chromem-go backend… swapped in later" note).

| Aspect | Value |
|--------|-------|
| Storage | in-memory `map[string][]float32` + `sync.Mutex` (unchanged) |
| Algorithm | linear-scan cosine, sorted desc, truncated to k (unchanged) |
| Dimensionality-skip | enforced in `Query` (the `len(cv) != len(vec)` guard — unchanged) |
| Tie-break | ascending `ChunkID` (unchanged) |
| Persistence | `Save`/`Load` (JSON) — **vestigial**; kept on the concrete type, NOT in the contract (R3). The store is seeded from Pebble `0x04` by `LoadIndex`. |

`Vector` remains the sole shipped `VectorIndex` implementation (FR-005/SC-005).

---

## 3. `Hit` — the result type (UNCHANGED)

`Query` returns `[]Hit`. `Hit{ ChunkID string; Score float64 }` is unchanged — it
is already the boundary type shared by FTS, Vector, and RRF fusion. No new field,
no new type. `Score` from the vector path is cosine similarity in `[-1, 1]`
(unchanged).

---

## 4. Lifecycle / state transitions (UNCHANGED)

The store's lifecycle is untouched by this feature — only the static type at the
Retrieval boundary changes.

```text
Cold start:  Pebble 0x04 (embeddings) ──LoadIndex──▶ vec.Add(...) ──▶ shared *Vector (cached on engine.idxVec)
Ingest:      chunk embedded async ──▶ vec.Add(chunkID, vec)          (pipeline worker)
Delete:      document removed   ──▶ vec.Delete(chunkID)              (DeleteDoc path)
Query:       Retrieval.semantic ──▶ vec.Query(qvec, poolSize) ──▶ []Hit
```

No state machine is added or altered. The "shared seeded index" model (spec
011/H01) and the async-after-ACK write timing (Constitution IV) are preserved
exactly — the pipeline's `Add`/`Delete` call sites keep their existing timing;
only the *declared type* of `Retrieval.vec` changes from `*Vector` to
`VectorIndex`.

---

## 5. Validation rules (from requirements)

- **FR-002 → invariant 1**: conformance test feeds a mixed-dimensionality corpus
  and asserts mismatched vectors are skipped (see
  [contracts/vector-index.md](./contracts/vector-index.md) §Conformance).
- **FR-003 → invariant 2**: conformance test asserts repeated identical queries
  return identical order; ties break by chunk-ID.
- **FR-004 → invariant 3**: conformance test runs concurrent `Add`+`Query` under
  `go test -race`.
- **FR-006 → behaviour-preservation**: the existing retrieval/query/parity suites
  and the eval harness are byte-identical before/after (SC-002/003).
- **FR-008 → identity**: the contract is expressed over string chunk-IDs only;
  no integer-ID path is introduced.
