# Phase 0 — Research & Decisions: Embedding Mismatch Validation

> Resolves every open technical question before Phase 1 design. Each entry:
> **Decision · Rationale · Alternatives considered.** Grounded in code read this
> session (`internal/index/{retrieval,vector}.go`, `internal/engine/{query,status}.go`,
> `internal/pipeline/workers.go`, `internal/storage`) and `RAG_BOOK_AUDIT.md` §1.2.

---

## D1 — Where does the refuse check live?

**Decision:** In **`engine.Query`**, before/around the retrieval call.

**Rationale:** `engine.Query` is the single shared path every transport calls
(spec 003 cross-transport parity), and it is the one place that holds **both** the
active embedder (`e.embedderOrOllama()` → `.Model()` / `.Dimensions()`) **and**
the database (from which the corpus profile is derived). Putting the guard here
makes the refuse behavior identical for CLI/REST/gRPC/MCP by construction
(FR-007). `index.Retrieval` only receives an `EmbedFunc` (which returns bare
`[][]float32`, dropping model/dim provenance), so it cannot perform the *model*
check on its own — and pushing the responsibility down into the index package
would require widening `EmbedFunc`, a broader change than needed.

**Alternatives considered:**
- **In `index.Retrieval.semantic` / `Vector.Query`:** *rejected* for the model
  check — the `EmbedFunc` signature loses the model; only the length (dim) is
  recoverable there. (The *length* guard does live in `Vector.Query` — see D3.)
- **A new middleware/wrapper embedder:** *rejected* — adds indirection and a new
  type for a check that the orchestrating engine can do directly.

---

## D2 — Where does the corpus profile (majority model + dim) come from?

**Decision:** Derived from the **stored Embedding records** (Pebble prefix 0x04),
read as the persisted `{Model, Vector}` shape (the pipeline writes
`storedEmbedding{Model: embed.Model(), Vector: vec}` at `workers.go:45`). The
profile = {majority Model, majority dim = `len(Vector)`, per-model counts, total}.
A shared helper (`internal/engine/embedding_profile.go`) computes it and is used
by **both** `engine.Query` (refuse check) and `Status` (drift view) so the two
never disagree.

**Rationale:** The provenance is **already stored** — the audit's "persist
{model,dim} per chunk" is effectively done; this spec makes it *checked*. Reading
it from the records (not a new store) means no schema change, no new prefix
(constitution: one Pebble instance, fixed prefixes). The scan cost is paid during
the **existing** per-query index load (which already scans prefix 0x04 in
`pipeline.LoadIndex`), so the profile adds **no new O(N) scan** — and it is cached
for free once H01 (index cache) lands.

**Alternatives considered:**
- **Store a single corpus-level `{model,dim}` key:** *rejected* — duplicates
  truth already present per-record, and a single key can't represent the mixed
  (mid-migration) state the guard must detect (US2/US3). Per-record derivation
  sees drift; a single key hides it.
- **Track model inside `index.Vector`:** *rejected* — couples a generic float
  store to embedding semantics; the records already hold it.

---

## D3 — How is the silent `cosine()` truncation fixed?

**Decision:** Add a **length guard in `Vector.Query`**: when scoring, **skip**
(any) stored vector whose length ≠ the query vector's length, and **count** the
skips; never call `cosine()` on a mismatched-length pair. `cosine()` itself keeps
its `min(len)` body only as a never-triggered safety net (the guard guarantees
equal lengths on the happy path).

**Rationale:** Today `cosine()` (vector.go:106) silently uses `min(len(a),
len(b))`, so a dim-N query vs dim-M stored vector returns a garbage score with no
error — the exact silent-corruption failure mode. The guard makes mismatched
lengths **visible** (counted, skippable) instead of silently mis-scored. This is
also what enables US3 (graceful mid-migration: the majority matches the query
length and is scored; the minority of a different length is skipped+counted).

**Alternatives considered:**
- **Make `cosine()` return an error on length mismatch:** *rejected* — `cosine`
  is a leaf math func returning a float; erroring there ripples signature changes.
  The guard belongs at the iteration site (`Query`), which can skip+count.
- **Truncate but flag:** *rejected* — a truncated cosine is *defined* garbage for
  retrieval; skipping is the only correct action.

---

## D4 — Refuse-the-query vs skip-the-vector: the core semantics

**Decision:** Two distinct outcomes, decided by comparing the **query** to the
**corpus majority**:

- **Refuse** (US1): the query's model ≠ corpus majority model, **or** the query
  vector length ≠ corpus majority dim → return a clear error naming expected vs
  actual; no results. (The query is wrong for this corpus.)
- **Partial / graceful** (US3): the query matches the majority model+dim, **but**
  some stored records differ (mid-migration) → score the matching majority,
  **skip+count** the mismatched minority, attach a "skipped N" note to the result.
- **Match** (happy path): query matches majority and all stored vectors agree →
  score normally; O(1) check, no skips.

**Rationale:** Refusing a query that doesn't match the majority prevents scoring
against the wrong semantic space. Skipping (not refusing) a *minority* of stored
vectors keeps the corpus usable mid-migration instead of all-or-nothing. The
distinction is exactly the spec's US1 vs US3 and is the behavior to test.

**Alternatives considered:**
- **Always refuse on any inconsistency:** *rejected* — makes a 99%-migrated
  corpus unqueryable; the book's guidance is degrade gracefully, not hard-stop.
- **Always skip, never refuse:** *rejected* — a query in a totally different
  space would silently return results from a tiny matching minority as if whole.

---

## D5 — How does status surface drift? (US2)

**Decision:** Extend `Status`/`StatusInfo` to report the **stored** majority model
+ dim (not just the configured model) and a **drift flag** with per-model/dim
counts, computed by the same `CorpusProfile` helper (D2). Today `Status` reports
`EmbeddingModel = cfg.EmbeddingModel` (the *configured* model) and a single dim
from the first record — it cannot see mixed models. After this change, status
shows what's actually stored and flags when >1 model or dim is present.

**Rationale:** An operator should see drift *before* querying (US2). Reporting the
stored majority (vs the config string) also makes a config/model mismatch visible
at status time, complementing the query-time refusal.

**Alternatives considered:**
- **A separate `go-rag doctor`/health command:** *rejected for MVP* — surfaces the
  same data in a new place; status is where operators already look. (A dedicated
  health view remains a reasonable future addition, e.g. under H17 observability.)

---

## D6 — How is the refuse error surfaced across transports? (FR-007)

**Decision:** `engine.Query` returns a **sentinel error** (e.g.
`ErrEmbeddingMismatch`) carrying expected-vs-actual model/dim. Each transport maps
it to its native error shape but preserves the same message text: CLI prints it
and exits non-zero; REST returns the matching HTTP status with the message; gRPC
returns the coded status; MCP returns the JSON-RPC error with the message. Because
the guard lives in the one shared `engine.Query`, the *decision* is identical
everywhere; only the wire encoding differs.

**Rationale:** Cross-transport parity (spec 003, Principle V) requires the same
query to be refused identically regardless of origin. A sentinel error from the
single engine path guarantees that; per-transport mapping is a thin render.

**Alternatives considered:**
- **Return empty results silently on mismatch:** *rejected* — that's the current
  silent-corruption behavior restated; the whole point is to be loud.

---

## D7 — How is this verified? (SC-005)

**Decision:** Primarily via direct unit tests in `internal/engine` + `internal/index`
(refuse on model mismatch, refuse on dim mismatch, graceful skip of a minority,
status drift flag, no-garbage-cosine). Additionally, the **spec-004 evaluation
harness** can be extended with a mismatch scenario (ingest under model A, query
under model B → assert the eval run reports the refusal rather than scoring
garbage) — this is a verification extension, not part of the deliverable, and
demonstrates the two specs compose.

**Rationale:** Unit tests give deterministic, fast coverage of each verdict; the
harness proves the guard is visible to a real retrieval-quality workflow.

---

## Open questions after research

**None.** All decisions resolved. The persistence of `Model` + `len(Vector)` on
prefix-0x04 records is verified in code (`workers.go:45`), confirming D2.
Proceeding to Phase 1 design.
