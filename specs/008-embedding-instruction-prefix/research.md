# Phase 0 — Research & Decisions: Embedding Instruction-Prefix

> Resolves every open technical question before Phase 1 design. Each entry:
> **Decision · Rationale · Alternatives considered.** Grounded in code read this
> session: `internal/embed/ollama.go` (the `Embedder` interface + `Ollama.Embed`),
> `internal/pipeline/workers.go:35` (document embed + the persisted 0x04 record),
> `internal/engine/query.go` (`checkEmbeddingMismatch` guard + `NewRetrieval(em.Embed)`),
> `internal/engine/embedding_profile.go` (`CorpusProfile` / `EmbeddingProfile`),
> `internal/engine/status.go` (profile surfacing), `internal/index/retrieval.go`
> (`EmbedFunc` value type), `internal/config/config.go`, `internal/eval/embedder.go`
> (`DeterministicEmbedder`), and `RAG_BOOK_AUDIT.md` §1.2.

---

## D1 — How does the query/document Role reach embedding without breaking the Embedder interface?

**Decision:** Leave `Embedder.Embed(ctx, texts []string)` **unchanged**. Introduce a
small pure **`Prefixer`** that, given a `Role` (`query` | `document`) and the
configured model's convention, prepends the correct prefix to each text. Apply it
at the two boundaries only:

- **Documents (pipeline):** in `pipeline/workers.go`, prepend the document-role
  prefix to each chunk text before `p.embed.Embed(...)`.
- **Query (retrieval):** in `engine.Query`, wrap `em.Embed` into a query-role
  `index.EmbedFunc` (prepend the query prefix) before `index.NewRetrieval(...)`,
  and apply the same query prefix in the `checkEmbeddingMismatch` probe embed.

**Rationale:** Constitution Principle V (extension by interface) — existing
providers (`Ollama`, the eval `DeterministicEmbedder`) are untouched; the prefix
logic is one pure, unit-testable function. The index layer already accepts
`EmbedFunc` as a value (`retrieval.go:31`), so the query prefix is applied by
**wrapping**, not by teaching `index` about roles — the index stays dumb.

**Alternatives considered:**
- **Add a `Role` parameter to `Embed`:** *rejected* — breaks every implementor
  (Ollama + DeterministicEmbedder) **and** widens the `index.EmbedFunc` type, a
  larger blast radius than an S-effort item warrants.
- **New optional `RoleEmbedder` interface method:** *rejected* — forces a
  type-check branch at every call site for no gain over a boundary function; the
  pure Prefixer is simpler.

---

## D2 — Where does the model→prefix map live, and how is it config-gated?

**Decision:** A built-in default convention map, plus a config override:

- **Default map** (model-substring → `{query, document}` prefixes):
  - `nomic-embed-text` → `search_query:` / `search_document:`
  - E5 family (`e5-*`, `multilingual-e5`) → `query:` / `passage:`
  - BGE family (`bge-*`) → query-only instruction, document none
- **Config** (`internal/config/config.go`, following the spec-006 rerank-field
  pattern): an `embedding_prefix` mode — `auto` (default) | `on` | `off` — and
  optional explicit `embedding_query_prefix` / `embedding_doc_prefix` strings.
  `auto` looks up the configured `embedding_model` in the default map; unknown
  models get **no** prefix (FR-003). `on`/explicit strings override the map.

**Rationale:** FR-002 (default-on for nomic, zero manual config), FR-003 (never
corrupt a non-prefix model), FR-004 (operator override). `auto` makes the common
case zero-config while guaranteeing unknown models are left alone.

**Alternatives considered:**
- **Hardcode-only:** *rejected* — no override, violates FR-004.
- **Per-provider declaration via a new interface method:** *viable later* under
  Principle V, but overkill for v1; the map + override covers it, and a future
  provider can still register into the default map without a core change.

---

## D3 — How is the prefix convention recorded, and how is a mismatch detected (US3 / FR-005 / FR-006)?

**Decision:** Extend the **existing** spec-005 machinery rather than build a
parallel store:

- Extend the persisted 0x04 record (`storedEmbedding`, `workers.go:45`) with a
  convention field (e.g. `"nomic"`, `"e5"`, `""` = none/legacy).
- Extend `EmbeddingProfile` (`embedding_profile.go`) with a `MajorityConvention`
  string and `ConventionCounts` map, mirroring `MajorityModel`/`ModelCounts`.
- Extend the existing `checkEmbeddingMismatch` guard in `engine.Query` to also
  compare the query's **active** convention to the corpus **majority**
  convention — refuse/warn on mismatch exactly as it does today for model/dim.

**Rationale:** The convention is literally a **third axis** of the same profile
(alongside model and dimensionality). The guard already runs on every query
across all four transports (spec 003 parity), so FR-009 (cross-transport
consistency) and FR-006 (no silent half-prefixed corpus) come for free.

**Backward compatibility:** old 0x04 records have no convention field → treated
as `""` (legacy unprefixed). A newly-prefixed query against a legacy corpus
yields a convention mismatch → US3 warn/refuse with a "re-embed" hint. This is
the desired behavior, not a migration hazard.

**Alternatives considered:**
- **Separate convention-check pass:** *rejected* — duplicates the guard.
- **Store convention in config only, not per-vector:** *rejected* — cannot detect
  a half-prefixed *legacy* corpus, which is the entire point of US3.

---

## D4 — Idempotency & identity (FR-007)

**Decision:** The prefix is prepended to the text **fed to the embedder only** —
never stored in `Chunk.Content`, never hashed into document/chunk identity. So
toggling prefixes changes **vectors only**, not identity → re-embedding under a
new convention is a no-duplicate operation (Principle II). The Prefixer is
idempotent: it checks whether a text already starts with the prefix before
prepending, so a user query literally beginning with `search_query:` is not
double-prefixed.

**Rationale:** FR-007, FR-008. Principle II ("content can be re-embedded under a
different model without creating duplicate documents") covers re-embedding under
a different *convention* by the same argument.

**Alternatives considered:**
- **Store the prefixed text in `Chunk.Content`:** *rejected* — corrupts content,
  citations, and the FTS index, and breaks content-addressed identity.

---

## D5 — Evaluation-harness interaction (SC-001)

**Decision:** Two-tier verification:

- **Mechanism (CI, deterministic):** the Prefixer is a pure function applied at
  the boundary for **both** the live Ollama path and the eval path identically.
  The eval `DeterministicEmbedder` (`internal/eval/embedder.go`) is made
  role-aware (its vector derivation incorporates the prefix), so the spec-004
  harness can prove in CI that query and document are encoded with the correct,
  distinct prefixes and that cross-transport parity holds.
- **Quality gain (manual):** the actual recall@5/10 and NDCG@10 *improvement*
  from prefixing requires a real `nomic-embed-text` run, documented as a manual
  quickstart step — **not** baked into the committed offline golden dataset.

**Rationale:** SC-001 demands a measurable gain, but spec 004's golden dataset
is deterministic and embedding-model-agnostic **by design** (reproducibility).
Making it real-model-dependent would violate that guarantee. So: mechanism
proven in CI, quality gain proven manually against a real model.

**Alternatives considered:**
- **Make the golden dataset model-dependent:** *rejected* — breaks spec-004
  reproducibility.
- **Skip the harness entirely:** *rejected* — SC-001 unmet.

---

## D6 — Performance & the write-ACK budget (FR-008 / SC-006)

**Decision:** Prefixing is an O(len(texts)) string prepend, applied only in the
async pipeline workers (documents) and at query-embed time (inside the
`EmbedFunc` wrapper and the mismatch probe). It never touches the write path —
ingest still ACKs in `<10ms` and embeds asynchronously afterward (Principle IV).

**Rationale:** FR-008, SC-006, Principle IV. A string prepend is negligible
relative to the network embed call that follows it.

**Alternatives considered:** none — there is no plausible alternative location;
the prefix must precede the embed call, which is already off the write path.

---

## Summary of code touch-points (informs `tasks.md`)

| Area | File(s) | Change |
|------|---------|--------|
| Prefix logic (new) | `internal/embed/prefix.go` (+ test) | pure `Prefixer`, `Role`, default convention map |
| Document embed | `internal/pipeline/workers.go:35` | prepend document prefix; store convention in 0x04 record |
| Stored record | `internal/pipeline/workers.go` (`storedEmbedding`) | add convention field |
| Query embed | `internal/engine/query.go` (probe + `NewRetrieval` wrap) | apply query prefix |
| Profile | `internal/engine/embedding_profile.go` | add `MajorityConvention` + `ConventionCounts` |
| Mismatch guard | `internal/engine/query.go` (`checkEmbeddingMismatch`) | also check convention |
| Status | `internal/engine/status.go` | surface convention + convention-drift |
| Config | `internal/config/config.go` | `embedding_prefix` mode + explicit prefix overrides; `Get`/`Set`/keys |
| Eval | `internal/eval/embedder.go` | role-aware `DeterministicEmbedder` |
| CLI/docs | `cmd`/`README.md` | document the new config keys |

No new dependencies (Principle III). No new Pebble prefix (single-byte key-space
unchanged — convention rides the existing 0x04 record). No `Embedder` interface
change (Principle V).
