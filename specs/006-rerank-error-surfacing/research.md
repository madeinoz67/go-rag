# Research — Reranker Error Surfacing (H09)

> Phase 0 output for `/speckit-plan`. Resolves every open design question before
> Phase 1 contracts. All decisions are grounded in code verified this session
> (`internal/index/retrieval.go`, `internal/engine/{query,types}.go`,
> `internal/rest/types.go`, `proto/gorag.proto`, `internal/config/config.go`) and in
> the spec's three clarifications (boolean flag; retrieval-error in scope;
> no query text in logs).

## D1 — What shape is the failure signal? (FR-001/002, clarification Q1)

**Decision:** A single boolean `RerankFailed` field on `engine.QueryResult`, set `true`
**only** when reranking was *attempted* (reranker non-nil) and failed (error or
score-count mismatch). Succeeded / not-configured / empty-pool cases are all `false` and
are not distinguished on the response.

**Rationale:** The core user story (spec US1) needs one answer — *"did these results
come from the reranker or from the unranked fallback?"* A boolean answers that exactly.
It is the audit's literal prescription ("surface a `RerankFailed` flag") and the smallest
change across three transports, keeping the item at its audited S-effort size.

**Alternatives considered:**
- **Tri-state enum** (`succeeded | skipped | failed`): *rejected* (clarification Q1 → B).
  Distinguishing "reranker not configured" from "reranker worked" adds contract surface
  across all transports for a distinction the operator already knows from config.
- **Sentinel error** (like spec 005's `ErrEmbeddingMismatch`): *rejected* — a sentinel
  error is the right tool for a *hard refusal* (no valid answer exists). Here the answer
  (fallback-ordered results) is valid and useful, so returning an error would violate
  US1 ("results still returned"). See D3 for the contrast.

## D2 — How are retrieval-stage and rerank-stage failures distinguished? (FR-008/009)

**Decision:** Change `Retrieval.SearchWithRerank` from `([]Hit, error)` to
`([]Hit, bool, error)`:

- the **`error`** return is non-nil **only** for a *retrieval-stage* failure
  (the candidate `Search` call) → `engine.Query` already does `if err != nil { return nil, err }`,
  so it propagates as a normal query error, mapped per-transport exactly like any other
  engine error (the established spec-005 mapping path). This closes the `hits, _ :=` swallow
  at `retrieval.go:110`.
- the **`bool`** return (`rerankFailed`) is `true` only for a *reranker* failure
  (error or `len(scores) != len(hits)`) → graceful degradation: return fallback-ordered
  hits **and** set the flag.

**Rationale:** The two failures have different correct responses. Failed retrieval = no
candidates = nothing to degrade to (must error). Failed reranking = valid candidates in
RRF order = degrade + flag. Splitting them across the two return channels makes FR-008
(distinguishability) structural rather than convention-based, and each is independently
testable.

**Alternatives considered:**
- **Wrap both into the bool and always degrade:** *rejected* — a retrieval failure
  silently returning empty/`hits[:k]` results is the very anti-pattern H09 exists to kill.
- **Two separate sentinel errors:** *rejected* — over-engineered; the error channel
  already propagates retrieval failures correctly through `engine.Query`'s existing
  `err != nil` branch.

## D3 — How is the flag surfaced across transports? (FR-004)

**Decision:** Thread `RerankFailed` through the existing adapter pipeline as a plain
field/text, identical in meaning everywhere:

| Transport | Surface | Insertion point |
|---|---|---|
| **engine** | `QueryResult.RerankFailed bool` | `internal/engine/types.go:30` |
| **REST** | `queryResponse.RerankFailed bool` → JSON `"rerank_failed"` | `internal/rest/types.go` + `engine_adapter.go:28` |
| **gRPC** | `QueryResponse.rerank_failed` proto field | `proto/gorag.proto` QueryResponse + `grpc/engine_adapter.go:60` |
| **MCP** | prepended text line `⚠ reranking failed; results are in fallback order` | `internal/mcp/server.go` renderQuery |
| **CLI** | stderr warning line (stdout JSON stays the result array) | `internal/cli/query.go` |

**Rationale:** This mirrors spec 003's parity contract — adapters carry no logic, only
serialization. Contrast with spec 005 (D6), which surfaced a *hard refusal* via a
sentinel error mapped to each transport's native error shape. H09 is graceful
degradation, so it carries a **field**, not an error. Both patterns coexist cleanly: a
retrieval failure (FR-009) uses the spec-005 sentinel-error path; a rerank failure
(FR-001/002) uses the new field path.

**Alternatives considered:**
- **MCP/CLI only (skip REST/gRPC):** *rejected* — recreates the silent-failure problem
  on the other transports and violates Constitution V / spec 003 parity.

## D4 — What goes in the failure log, and via what logger? (FR-003, clarification Q3)

**Decision:** Use the **stdlib `log`** package — the same logger already used for the
"embedding drift" line at `engine/query.go:84` (`log.Printf`). On rerank failure, emit
one line:

```
rerank failed: model=<rerank_model> candidates=<N> scores=<M> err=<error>
```

i.e. **error message + reranker model + candidate count + returned score count only**.
**Never** the query text or candidate content (clarification Q3 → A).

**Rationale:** A rerank failure is diagnosable from the error + counts alone (model
unreachable, malformed response, length mismatch). Keeping user query content out of logs
is the safe default for a security-focused product and sets the precedent the unbuilt
audit-log item (H18) will inherit. Reusing stdlib `log` avoids introducing a logging
framework (Constitution III — no new deps) and matches the existing precedent exactly.

**Alternatives considered:**
- **Hash query text into the log (H18-style):** *rejected for H09* — adds a hashing
  dependency and a policy decision for a line that does not need query correlation.
- **Structured/slog or a new logger:** *rejected* — no existing convention; stdlib `log`
  is the established choice and keeps the diff minimal.

## D5 — How does the optional retry work? (FR-006, US3)

**Decision:** Add `RerankRetryOnFailure bool` to `config.Config` (default `false`). When
enabled and the first rerank fails, re-retrieve candidates with `min(pool*2, 200)` and
re-score once. On retry success → normal reranked results (`RerankFailed=false`); on
second failure → fallback-ordered results + `RerankFailed=true`.

**Rationale:** Off-by-default satisfies "no latency added to the default path." The `pool*2`
heuristic gives the reranker more candidates to recover from a transient/pool-related
failure; the cap (200) bounds cost. The exact multiplier is an implementation knob, not a
contract — captured here as a sane default, final value settled in `tasks.md`.

**Alternatives considered:**
- **Retry the same candidates (no larger pool):** *rejected* — a deterministic length
  mismatch would retry identically and waste a round-trip; a larger pool is the only retry
  that can change the outcome.
- **On by default:** *rejected* — doubles worst-case query latency on a flaky reranker;
  the operator opts in knowingly (US3 priority P3).
- **Per-request retry flag instead of config:** deferred — config is sufficient for the
  operator-controlled MVP; a request flag can layer on later without breaking anything.

## D6 — Is adding the proto field a breaking change? (deferred from clarify)

**Decision:** **No.** Adding `bool rerank_failed = 2;` to `message QueryResponse` is
additive in proto3: field 2 was unused, proto3 fields are optional by default, and older
clients ignore unknown fields. REST JSON gains `"rerank_failed"` additively (extra key).
MCP gains a warning text line additively. CLI gains a stderr line (stdout JSON unchanged).
**No client breaks.**

**Rationale:** Verified against the current `QueryResponse { repeated QueryHit hits = 1; }`
— field number 2 is free. This confirms the deferred backward-compat question from the
clarify coverage summary.

**Alternatives considered:**
- **Bump a major API version / new message:** *rejected* — unnecessary for an additive
  optional field; would violate the project's parity-without-churn intent.

## D7 — How is this verified? (SC-001…SC-006)

**Decision:** Three layers:

1. **Unit (`internal/index`):** rerank error → fallback hits + `rerankFailed=true` +
   no returned error; score-length mismatch → same; retrieval error → returned `error`,
   `rerankFailed=false`; retry enabled → second attempt observed; retry disabled → none.
2. **Engine + parity (`internal/engine/parity_test.go`):** a failing-reranker query
   returns `QueryResult.RerankFailed=true`, and the **same** query over REST, gRPC, and
   MCP exposes the flag/warning identically (extend the existing parity harness).
3. **Regression (`make test-eval`, SC-005):** the spec-004 eval harness shows no
   recall@10 regression on the happy path (reranker healthy), proving the change is
   observability-only when reranking succeeds.

**Rationale:** Unit tests give deterministic coverage of each verdict; the parity test
enforces FR-004 (cross-transport) structurally; the eval harness proves SC-005 (no
quality regression). The retrieval-error propagation (SC-006) is covered by the unit test
asserting a non-nil error reaches `engine.Query`.

## Open questions after research

**None.** All decisions resolved. The two items deferred from `/speckit-clarify` (retry
pool sizing → D5; proto backward-compat → D6) are settled. Proceeding to Phase 1 design.
