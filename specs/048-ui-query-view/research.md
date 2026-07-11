# Research — Query View (Slice 2)

**Feature**: specs/048-ui-query-view | **Date**: 2026-07-11

Phase 0 output. Resolves the six Open Questions in [spec.md](./spec.md) (OQ1–OQ6) and records
the design decisions that follow from source-inspecting the existing retrieval + UI-transport
contracts. Every decision below is grounded in code read this session, not recalled.

---

## Source grounding (what was verified before designing)

- `Engine.Query(ctx, QueryRequest) (*QueryResult, error)` — `internal/engine/query.go:22`. The
  one retrieval path shared by CLI, MCP, REST, gRPC. Already parity-proven.
- `QueryRequest` — `internal/engine/types.go:15`. Params: `Query, K, Mode, NoRerank, Threshold,
  RRFK, PoolSize, Filter, ContextWindow, NoCache, IncludeQuarantined, Dedup`.
- `QueryHit` — `internal/engine/types.go:56`. Per hit: `ChunkID, DocumentID, Score, Content,
  FilePath, Page, ChunkIndex, Preview, Context, Poisoning, SectionContext, SectionLevel,
  ExtractionMethod, ExtractionQuality, NearDup, Summary, EnrichmentStatus, Wikilinks`. **One
  fused `Score`** — no per-stage BM25/vector/rerank breakdown is carried.
- `QueryResult` — `internal/engine/types.go:111`. `Hits, RerankFailed, EffectiveK,
  EffectivePool, EffectiveMode`.
- REST projection — `internal/rest/engine_adapter.go::handleQuery` (POST, JSON body) and
  `toQueryHits`. This is the JSON contract the UI mirrors field-for-field (separate adapter,
  parallel names).
- UI transport — `internal/ui/ui.go::Server.Handler` (Go 1.22 pattern mux), `Server.guard`
  (auth.Validate wrapper), `writeJSON` / `writeError` / `writeEngineErr` helpers (the last
  already in `documents.go`). Documents view (`internal/ui/documents.go`) is the in-process
  engine-call pattern to copy; its `chunkDTO` already carries SectionContext/SectionDepth/
  Poisoning/NearDup/Wikilinks/ExtractionMethod/Quality.

**Headline finding**: unlike spec 047 (which added `ListChunks` across 5 transports + proto),
**spec 048 needs zero engine/RPC work**. `Engine.Query` already exists everywhere. The UI is a
single new view file + route + Alpine/template edits.

---

## R1 — UI calls `Engine.Query` in-process (4th adapter, not a REST proxy)

**Decision**: `internal/ui/query.go` calls `s.eng.Query(...)` directly, exactly as
`dashboard.go` calls `engine.Status()` and `documents.go` calls `engine.ListDocuments`.

**Rationale**: The established console architecture (spec 046 §FR, spec 047 plan) is that the
UI transport is a peer adapter over the engine — not a proxy to REST. Calling in-process avoids
a second HTTP hop, keeps the auth boundary in one place (`Server.guard`), and inherits engine
behaviour (cache, quarantine, mismatch guard) for free.

**Alternatives rejected**: (a) UI proxies to REST `POST /v1/query` — rejected: doubles the HTTP
path, splits auth across two transports, and breaks the "4th adapter" invariant the shell was
built on. (b) A new engine accessor — rejected: unnecessary; `Engine.Query` is exactly the
needed surface.

---

## R2 — Per-stage score breakdown is out of scope (resolves OQ1)

**Decision**: Surface the single fused `Score` per hit, plus result-level `EffectiveMode`,
`EffectiveK`, `EffectivePool`, and `RerankFailed`. Do **not** show BM25 / vector / rerank
contributions.

**Rationale**: `QueryHit.Score` is the engine's fused (RRF, optionally reranked) score. A
per-stage breakdown would require an engine change (new fields on `QueryHit`, computed in
`Engine.Query` before fusion) — that is a separate, independently-specced engine capability,
not a read-only UI concern. This corrects the assumption in the original feature description.

**Alternatives rejected**: (a) Add per-stage fields to `QueryHit` in this slice — rejected:
violates "no new engine capability," breaks the read-only/UI-slice boundary, and would need
parity work across all transports. (b) Reconstruct stages client-side — rejected: impossible;
the engine does not emit them.

**Deferred to**: a future engine spec (e.g. "retrieval explainability") that adds opt-in
per-stage scoring across all transports with parity.

---

## R3 — Route shape: `POST /api/query` with JSON body (resolves the route decision)

**Decision**: One new route, `POST /api/query`, guarded by `Server.guard`. The request is a
JSON body mirroring the REST `queryRequest` field set. Response is the UI query DTO.

**Rationale**: Query has 13 parameters (several floats/ints/bools + a multi-value tag array).
A GET query-string would be unwieldy and length-fragile; POST-with-JSON-body mirrors the REST
`/v1/query` contract (`TestREST_Query_InvalidJSON_BadRequest`, `TestREST_Query_EmptyQuery_…`)
and keeps the two adapters' contracts parallel. `POST` also makes explicit that a query is a
compute action (it can trigger embedding generation), not an idempotent cache lookup — even
though it mutates nothing.

**Alternatives rejected**: (a) `GET /api/query?...` — rejected: param volume + tag array +
length limits. (b) `GET /api/query` for params + `POST` for execution — rejected: needless
two-step; the controls are client-side form state serialized once on submit.

---

## R4 — DTOs local to `query.go`, field-parallel to the REST contract

**Decision**: `query.go` defines `queryRequestDTO`, `queryHitDTO`, `queryResponseDTO` with
field names matching the REST `queryRequest` / `queryHit` / `queryResponse` shapes. The UI
package does **not** import `internal/rest`.

**Rationale**: Each transport owns its DTOs (REST, gRPC, MCP each have their own projections of
`QueryHit`). The UI follows the same rule — own DTOs, parallel names — so the four adapters
stay consistent without coupling. The existing `chunkDTO` (documents.go) already proves the
pattern: it repeats several `QueryHit` fields by name rather than importing a shared type.

**Fields on `queryHitDTO`** (mirrors REST `queryHit` + adds `Context` for sibling chunks):
`ChunkID, DocumentID, Score, Content, FilePath, Page, ChunkIndex, SectionContext, SectionDepth,
Poisoning, NearDup, Wikilinks, Summary, EnrichmentStatus, ExtractionMethod,
ExtractionQuality, Context`. **`queryResponseDTO`**: `Hits, RerankFailed, EffectiveK,
EffectivePool, EffectiveMode`.

**Alternatives rejected**: (a) Import `internal/rest` DTOs — rejected: couples a UI adapter to
the REST adapter; breaks the peer-adapter invariant. (b) Promote a shared DTO to a new package
— rejected: premature; no other consumer needs it and it would touch every transport.

---

## R5 — Result-cache indicator is invisible; only a bypass toggle (resolves OQ2)

**Decision**: Do not surface cache hit/miss (spec 016). Provide a per-query `no_cache` toggle
(default off) that maps to `QueryRequest.NoCache`.

**Rationale**: Cache hit/miss is an engine-internal detail; showing it adds noise without
operator value. The bypass toggle is the one cache control an operator reasonably wants ("force
a fresh result"), matching the CLI `--no-cache` flag.

**Alternatives rejected**: (a) Show a cache-hit badge per query — rejected: noise; not
actionable. (b) No cache control at all — rejected: the CLI exposes `--no-cache`, so the UI
should too for parity of control.

---

## R6 — Adaptive-depth shown via effective indicators + a note when the classifier acted (resolves OQ3)

**Decision**: Always show `EffectiveMode`, `EffectiveK`, `EffectivePool` after a query. When
`EffectiveK` differs from the operator's requested top-k (i.e. the spec 024 classifier
recommended a different depth), show a short note ("adaptive depth: k adjusted from N → M").

**Rationale**: The operator needs to know what actually ran, not just what they asked for. The
effective indicators alone cover the common case; the delta-note covers the surprising one
without a heavyweight callout.

**Alternatives rejected**: (a) A prominent "classifier acted" banner — rejected: over-emphasises
a routine event. (b) Hide effective values — rejected: contradicts spec FR-006 (transparency).

---

## R7 — Result sets bounded by top-k; k clamped to a sane ceiling (resolves OQ4)

**Decision**: Queries return at most top-k hits (the engine already bounds on `K`). No
pagination beyond k. The UI clamps the top-k input to a [1, 50] range (default 5), matching the
engine's sensible operating range and the CLI default.

**Rationale**: Retrieval is a ranked top-k, not a browse; paginating beyond k has no retrieval
semantics. Clamping prevents an operator requesting 10,000 hits and marshalling a huge payload.

**Alternatives rejected**: (a) Paginate results beyond k — rejected: not how ranked retrieval
works; the engine does not support it. (b) No ceiling on k — rejected: DoS-by-self and payload
bloat.

---

## R8 — `include-quarantined` resets to default (false) each query (resolves OQ5)

**Decision**: The quarantine opt-in is a per-query toggle, client-side, **defaulting to false
on every new query**. It does not persist across queries.

**Rationale**: Quarantine-by-default is the safe posture (spec 019, `IncludeQuarantined=false`
engine default). Resetting each query guarantees the resting state is always safe; the operator
must consciously opt in each time they want flagged chunks. This honours the standing
quarantine-management preference at the result level.

**Alternatives rejected**: (a) Persist the toggle across queries — rejected: risks an operator
forgetting they opted in and silently ingesting untrusted flagged text into downstream reading.
(b) No opt-in at all — rejected: the operator must be able to see why a chunk was flagged
(the preference's "see the score breakdown + release false positives" — here, opt-in + verdict).

**Note**: A dedicated quarantine browse/manage view (list all flagged chunks across the corpus,
bulk release) remains a separate open obligation, not this slice.

---

## R9 — Explicit submit only; controls do not auto-re-query (resolves OQ6)

**Decision**: Changing a control (mode, k, threshold, filter, toggle) does **not** automatically
fire a query. The operator clicks Submit (or presses Enter in the query box) to run.

**Rationale**: A query can trigger embedding generation (semantic/vector mode) — non-trivial
compute and latency on local Ollama. Auto-firing on every control tweak would (a) surprise the
operator with cost/latency and (b) race in-flight requests. Explicit submit matches the CLI
mental model (`go-rag query [q] --mode ...` is one shot).

**Alternatives rejected**: (a) Debounced auto-query on control change — rejected: surprise cost
+ request races. (b) Auto-query only on filter/tag change — rejected: inconsistent; still
surprising and still races.

---

## R10 — Error mapping: embedder-unreachable and mismatch errors surface plainly

**Decision**: Engine errors flow through the existing `writeEngineErr` helper (documents.go).
The frontend maps the two operator-actionable failures to clear guidance: embedder-unreachable
(needed for semantic/vector) → "local embedder unavailable; try keyword mode"; embedding-
dimension mismatch → "query model differs from the corpus; re-embed or switch model".

**Rationale**: These are the two retrieval errors an operator can actually do something about.
Surfacing them as plain guidance (not raw engine errors) is the difference between a usable
console and a black box.

**Alternatives rejected**: (a) Show raw error strings — rejected: hostile to non-engine
operators. (b) Swallow errors and show empty results — rejected: violates spec FR-012 (no
silent failures) and the constitution's "no silent failure" stance.

---

## R11 — Empty/whitespace query → 400 Bad Request

**Decision**: `POST /api/query` with an empty or whitespace-only `query` field returns 400
`empty query`. The frontend disables Submit when the query box is empty/whitespace.

**Rationale**: Mirrors the REST contract (`TestREST_Query_EmptyQuery_BadRequest`) and the CLI
(`cobra.MinimumNArgs(1)`). Fail fast on the nonsensical input; do not call the engine.

**Alternatives rejected**: (a) Run the query anyway — rejected: meaningless; wastes an embedding
call. (b) Silent client-side block only — rejected: keep client and server agreed; the server
must reject too for parity with REST.

---

## R12 — Parity test: `/api/query` == REST `/v1/query` == `go-rag query`

**Decision**: Ship a cross-transport parity test (pattern of the Documents view's parity test
and `internal/engine/parity_test.go`) asserting that the same query+params return identical
hits, order, and scores across the UI `/api/query`, REST `/v1/query`, and the engine directly.

**Rationale**: Constitution Principle V (parity) + spec FR-013. Because all three call the same
`Engine.Query`, parity is structural — but the test pins it so a future DTO drift is caught.

**Alternatives rejected**: Trust structural parity without a test — rejected: the repo's
standing pattern is to pin parity with a test on every new transport surface.
