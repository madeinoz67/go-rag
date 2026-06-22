# Phase 0 — Research: Configurable RRF Constant (H08)

> Resolves every design unknown needed before Phase 1 contracts. Each item:
> Decision · Rationale · Alternatives rejected. All grounded in code read this
> session (`internal/index/retrieval.go`, `internal/engine/query.go`,
> `internal/engine/types.go`, `internal/config/config.go`, `internal/cli/query.go`,
> `internal/rest/*`, `internal/grpc/engine_adapter.go`, `internal/mcp/server.go`,
> `proto/gorag.proto`, `Makefile`).

## 1. Fusion formula & rank indexing

**Decision**: Standard single-k RRF, `score(d) = Σ 1/(k + rank)` with `rank`
1-based (first hit = rank 1), `k` default **60**.

**Rationale**: User chose Option A (spec FR-007). The retrieval book §6.6 and
audit §1.3/§1.4 prescribe one constant k≈60. The current implementation loops
`for rank, h := range hits` with `rank` 0-based and contributes
`1/(k+rank+1)`; that is *arithmetically identical* to `1/(k + rank₁based)`. So
the new single-k fusion keeps the exact same loop body, replacing the two
per-list constants (`kVec`, `kFTS`) with one `k`. No off-by-one is introduced or
removed — the `+1` *is* the 1-based rank.

**Alternatives rejected**:
- *Retain asymmetric `kVec`/`kFTS` configurable (Option B)* — keeps the very
  opacity the audit flags ("unreviewable"); rejected by user.
- *Derive `k` from `Mode`* — no principled mapping (hybrid needs fusion; keyword
  and semantic don't fuse at all); rejected as magic.

## 2. Effective-k resolution & the zero-value sentinel

**Decision**: `effective = req.RRFK if req.RRFK > 0 else cfg.EffectiveRRFK()`,
where `Config.EffectiveRRFK() = c.RRFK if c.RRFK > 0 else 60`. An **explicit**
value ≤ 0 provided via flag is rejected; an **explicit** negative in config is
rejected by `Validate`; an **absent** key (Go zero-value `0`) resolves to the
default 60.

**Rationale**: This codebase models all config as plain ints with `omitempty`
(see `RerankCandidates`, `ChunkSize`). JSON-unmarshaling an existing config that
omits `rrf_k` yields `0` — which MUST mean "use the default", not "invalid", or
every existing config breaks on load. FR-005 ("constant ≤ 0 MUST be rejected") is
therefore interpreted as applying to **explicitly provided** values; the zero-value
sentinel for an **absent** key/flag is the documented, backward-compatible
exception. The CLI distinguishes "not passed" from "passed 0" with
`cmd.Flags().Changed("rrf-k")`.

**Alternatives rejected**:
- *`*int` pointer field to distinguish unset from 0* — unidiomatic here (no
  precedent in `Config`), doubles the JSON semantics surface. Rejected.
- *Treat config `0` as invalid (strict FR-005)* — breaks every existing config on
  load. Rejected.

## 3. Injection point: per-query setter, no signature threading

**Decision**: Add `func (r *Retrieval) SetRRFK(k int)` (no-op when `k <= 0`,
matching the `EnableRerankRetry()` setter idiom). `Engine.Query` calls
`r.SetRRFK(effective)` right after `index.NewRetrieval(...)` (verified:
`retrieval` is constructed fresh inside `Engine.Query`, not shared across calls).
`Search` / `SearchWithRerank` / `attemptRerank` read `r.rrfK` internally — **no
signature change**, so the existing rerank tests are unaffected by threading.

**Rationale**: `Engine.Query` already builds a new `*Retrieval` per query
(`internal/engine/query.go`) and already configures it per-call
(`r.EnableRerankRetry()`). A setter is the established pattern, avoids touching 4
internal call sites + 6 retrieval tests, and has no concurrency risk (the struct
is never shared between goroutines).

**Alternatives rejected**:
- *Thread `rrfK int` as a `Search` parameter* — churns `Search`,
  `SearchWithRerank`, `attemptRerank`, and 6 tests for zero behavioral benefit.
- *Store `rrfK` on `Engine` and read it inside `reciprocalRankFusion`* — the
  Engine does not call the fuser; `Retrieval` does. Would require exporting state
  downward against the grain of the layering.

## 4. Proto regeneration (biggest task uncertainty)

**Decision**: Adding `int32 rrf_k = 6;` to `proto/gorag.proto` `QueryRequest`
**requires regenerating** `proto/gen/gorag.pb.go` + the gRPC service stub.
**No regen target exists** in `Makefile`, no `buf.yaml`/`buf.gen.yaml`, no
`//go:generate` directive was found. The regen command must be determined in
`/speckit-tasks` (likely `protoc --go_out=. --go-grpc_out=.` with the right
module/paths, or a one-off `buf generate`) by inspecting
`specs/003-rest-grpc-api/` docs and the git history of `proto/gen/`.

**Rationale**: gRPC is one of the four parity transports (FR-003), so the proto
field + regen is mandatory, not optional.

**Risk / mitigation**: If the regen tooling is not installed in the dev env, the
options are (a) `go install` the protoc plugins / `buf` and regen, or (b)
hand-edit `gorag.pb.go` for a single new optional `int32` field (mechanical, low
risk for an additive optional field). (a) is preferred. **The shipped binary
remains pure-Go** — protobuf codegen is build-time only (Constitution Principle
III preserved). Tasks must confirm the regen path before editing the proto.

**Alternatives rejected**:
- *Ship CLI+REST+MCP parity only, defer gRPC* — violates FR-003 cross-transport
  parity. Rejected.
- *Drop the gRPC field, accept REST/MCP/CLI-only* — same parity violation.

## 5. Eval baseline re-capture (H02 gate)

**Decision**: `make test-eval` compares against the committed
`testdata/golden/baseline.json` at tolerance 2.0. Collapsing to `k=60` changes
fused scores and **will breach tolerance** for the current baseline. The baseline
MUST be **re-captured** with the new default (regenerate `baseline.json` with the
new formula, then commit). This is not a regression — it is the *intended*
default change — but the README/changelog must state it. SC-001 additionally
calls for a before/after recall/MRR/NDCG report on the golden + BEIR datasets.

**Rationale**: The eval harness measures ranking; a ranking change moves the
numbers by definition. The `--tolerance 2.0` gate exists to catch *unintended*
drift, so an *intended* change requires an intentional re-baseline.

**Alternatives rejected**:
- *Loosen `--tolerance` to absorb the change* — hides real future regressions.
  Rejected.
- *Keep the old baseline and make the new formula opt-in only* — contradicts the
  spec (collapse is the new default). Rejected.

## 6. Non-hybrid mode is a no-op (not an error)

**Decision**: `rrf_k` affects **only** `ModeHybrid`. In `ModeKeyword` /
`ModeSemantic` only one list is retrieved and `reciprocalRankFusion` is never
called (verified in `Retrieval.Search`). A configured/flagged `rrf_k` MUST be a
silent no-op on single-list queries, **not** an error — do not reject a valid
constant just because the current query doesn't fuse.

**Rationale**: The constant is a global/retrieval setting; query mode varies per
call. Coupling validation of `rrf_k` to the current mode would make a config key
load-fail depending on query type — surprising and wrong.

## 7. `poolSize` is independent and out of scope

**Decision**: `poolSize` (candidate count entering rerank, H09/H22 territory)
stays at its current default (60) and is **not** exposed by this spec. `rrf_k`
feeds only the fusion weighting; `poolSize` feeds candidate counts into
`fts.Search` / `vec.Query`. They are independent knobs.

**Rationale**: The backlog item is exclusively about the RRF constant. Pool-size
tuning is a separate, explicitly-deferred item (H22).
