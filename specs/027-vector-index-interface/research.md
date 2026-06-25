# Research — Swappable Vector Index (H27, spec 027)

**Phase 0 output.** Resolves the design decisions for promoting the brute-force
`*index.Vector` store's implicit behaviour into an explicit, implementation-neutral
contract. Each decision is grounded in the current source.

---

## R1 — What is the interface, and where does it live?

**Decision.** Define a `VectorIndex` interface in `internal/index` (alongside the
existing `Reranker` and `EmbedFunc` abstractions in `retrieval.go` / the package)
covering the actual live surface:

- `Add(id string, vec []float32)` — store/replace a chunk's vector.
- `Delete(id string)` — remove a chunk's vector.
- `Query(vec []float32, k int) []Hit` — top-k nearest neighbours by similarity.

Expressed entirely over existing types (`Hit`, `[]float32`, string chunk-ID). No
generics, no capabilities object, no new result type.

**Rationale.** The live surface of `*Vector` outside its own file is exactly these
three methods: `Query` is the sole call from `Retrieval.semantic`
(`retrieval.go`); `Add` is called by `pipeline.LoadIndex` (cold-start seed from
Pebble `0x04`) and the pipeline embed path; `Delete` is called on document
deletion. The audit's own wording ("`Add/Delete/Query`") matches this surface
exactly. Putting it in `internal/index` mirrors the established
`Reranker`/`EmbedFunc` precedent — the package already hides concrete
Ollama-backed implementations behind interfaces and stays dependency-free.

**Alternatives considered.**
- *A wider "Index" interface covering both FTS and Vector.* Rejected — FTS is
  already Pebble-backed (spec 018/H16) with a different shape (`Search`, no
  `Add`/`Delete` on the query type). Forcing a unified `Index` would lie about a
  shared contract that doesn't exist. Two backends, two contracts.
- *A generic `Index[T]`.* Rejected — YAGNI; the concrete types (`Hit`,
  `[]float32`) already exist and are stable. Generics add no value here and
  complicate the conformance test.

---

## R2 — Where does the seam go (minimal blast radius)?

**Decision.** `Retrieval.vec` changes from `*index.Vector` to `VectorIndex`, and
`NewRetrieval(fts, vec, embed)` takes `VectorIndex` for `vec`. The engine
(`engine.go`) keeps holding concrete `*index.Vector` on `idxVec` and in
`indexes()`'s return — `*Vector` satisfies `VectorIndex` structurally, so it is
passed unchanged where the interface is expected. No engine field-type change,
no `LoadIndex` signature change, no pipeline call-site change.

**Rationale.** The single consumer that benefits from substitutability is
`Retrieval` — it is the only call site that should not care *which* backend is
active. The pipeline and `LoadIndex` *construct and mutate* the store; they
legitimately depend on the concrete type (they build the reference
implementation). Making them depend on the interface would gain nothing and would
force a constructor/factory abstraction. This is the smallest change that
delivers US1 (Retrieval depends on contract, not implementation) and it keeps
FR-006 (zero behavioural change) trivially true.

**Alternatives considered.**
- *Also abstract the engine holder (`idxVec VectorIndex`).* Rejected as
  unnecessary for this finding — the engine always instantiates the brute-force
  reference impl; there is no second impl to select between. If/when a backend
  choice exists, the construction site (`LoadIndex`/engine init) is the natural
  switch point and can be abstracted then. Keeping the engine concrete now is
  strictly less code touched.

---

## R3 — Is persistence (Save/Load) part of the contract?

**Decision.** No. `Save`/`Load` stay on the concrete `*Vector` and are **not** in
`VectorIndex`. The interface covers only the retrieval-facing surface
(`Add`/`Delete`/`Query`).

**Rationale.** The vector store is seeded from the durable embeddings store
(Pebble prefix `0x04`) by `pipeline.LoadIndex` (`internal/pipeline/load.go`):
`PrefixScanByte(PrefixEmbedding, …) → vec.Add(...)`. It is **not** seeded from
`Vector.Load`. With the shared seeded index (spec 011/H01) and durable FTS (spec
018/H16), the JSON-file `Vector.Save`/`Vector.Load` methods are vestigial — the
source of truth is Pebble. (Confirm no remaining production caller during
implement; the search surfaced only the definitions and unrelated `Save`/`Load`
on `Baseline`/`Config`/`eval`.) An eventual ANN backend would persist its own
graph format, so baking `Save`/`Load` into the contract would force every backend
to fake a JSON shape it doesn't use. Persistence is backend-specific → out of the
core contract (FR-007).

---

## R4 — How are the three invariants enforced (not just documented)?

**Decision.** Two layers:
1. **Contract documentation** — the `VectorIndex` doc comment states the three
   invariants as obligations (FR-002 dimensionality-skip, FR-003 determinism +
   stable tie-break, FR-004 concurrency-safety).
2. **Conformance test** — `internal/index/vector_contract_test.go` asserts the
   reference `*Vector` honours all three (mixed-dimensionality corpus →
   mismatched vectors skipped, never scored; repeated identical query → identical
   order; concurrent Add + Query → no race under `-race`). This test is the
   "second implementation" of SC-001 run against the reference impl, and it is
   the bar any future backend must pass.

A future backend that cannot honour an invariant is **wrapped** (a guard
decorator that re-implements the length-skip / sort-stabilisation around the inner
`Query`) or **rejected** at construction (FR-009) — never silently accepted. No
wrapper is shipped in this finding; the policy + conformance test are.

**Rationale.** The H03 guard, the mutex, and the tie-break currently live inside
`Vector.Query`/`Vector` as implementation detail. A typical ANN library scores
every indexed vector (no length-skip), uses approximate search (non-deterministic
order), and has its own locking. Without enforcement, "extract an interface"
silently reintroduces the H03 silent-corruption bug on a mixed corpus — the exact
failure the guard exists to prevent. The conformance test makes the contract
*executable*, not aspirational.

**Alternatives considered.**
- *Ship a guard wrapper now.* Rejected — there is no second backend to wrap, so
  it would be dead code. The wrapper is specified (FR-009) for the future
  integration; it lands with that backend.
- *Documentation only.* Rejected — unenforced invariants rot. The conformance
  test is what makes the escape hatch safe.

---

## R5 — What constraints apply to a future backend?

**Decision.** Any future `VectorIndex` implementation must be: **local**
(Constitution I — no network/cloud), **pure-Go / no CGo** (Constitution III), and
must accept the system's **string chunk-IDs** (FR-008). The anticipated shape is a
pure-Go HNSW-style library; chromem-go (already in the PRD §9.2 allowed list and
named in `vector.go`'s existing doc comment + `index.go`'s deferred-T027 note) is
the expected candidate. The specific library is a decision for that future
finding, not this one.

**Rationale.** These are constitutional + identity constraints, not preferences.
An integer-ID or C-based backend would violate Constitution III or FR-008 and is
out of scope. Naming the constraints now means the contract is ready for the
eventual backend without renegotiating the rules.

---

## R6 — Why interface-only now, and not "wait until we need HNSW" (YAGNI)?

**Decision.** Extract the interface now; ship no backend. Retire the stale
`index.go` "task T027" / `vector.go` "swapped in later" promises.

**Rationale (temporal-cost argument).** The cost of extracting the seam grows
monotonically with the number of call sites that take `*Vector` directly. Today
that surface is tiny (Retrieval + pipeline + engine holder). Every future feature
that passes the concrete store widens the retrofit. Landing the seam while one
implementation exists is the cheapest moment, and it is exactly what the audit
asks ("…before scaling pressure hits"). It is also insurance priced at near-zero:
one interface, one type change, one conformance test — a one-day, fully
reversible, behaviour-preserving change. The realistic trigger for an actual ANN
backend is the same multi-user-MCP event that flips RBAC and other scale items to
P0; until then brute-force is adequate below ~10K docs (audit §1.3). So: seam now,
backend later, TODO retired.

**Alternatives considered.**
- *Defer entirely (strict YAGNI).* Rejected — the retrofit cost only rises, the
  TODO is already stale across ~10 specs, and the invariants are currently
  undocumented (a latent correctness landmine). The insurance is too cheap to skip.
- *Ship chromem-go now.* Rejected — out of scope for v1 (no scale pressure), adds
  a dependency + a persistence migration for zero current benefit, and bundles two
  risks (the seam + the algorithm) that should land separately.
