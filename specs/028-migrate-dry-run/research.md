# Research — Migration Dry-Run (H24, spec 028)

**Phase 0 output.** Resolves the design decisions for a no-side-effect migration
preview. Each decision is grounded in the current `migrate` / `Migrate` source.

---

## R1 — Where does the plan computation live?

**Decision.** Add a new read-only engine method `Engine.MigratePlan(ctx)
(*MigrationPlan, error)` that computes the migration plan from the two existing
metadata-only readers — `pipeline.EmbeddingModelStats(db)` (per-model counts) and
`engine.CorpusProfile(db)` (dimensionality distribution + consistency). It does
NOT embed, flush caches, reprocess, or refresh the baseline. Then **refactor
`Engine.Migrate` to call `MigratePlan` first** and proceed only when `StaleTotal
> 0` — so the preview and the execution share one code path (FR-008, free).

**Rationale.** The dry-run computation is exactly the first half of today's
`Engine.Migrate` (`internal/engine/ingest.go:124`): it already reads
`EmbeddingModelStats`, sums stale, and branches on `stale == 0`. Extracting that
read into `MigratePlan` centralises the logic, removes the duplication between
`Migrate` (engine) and the inline preview in `newMigrateCmd` (CLI), and makes the
preview available to every transport through one method. The CLI's hand-rolled
per-model print becomes a render of the shared plan.

**Alternatives considered.**
- *Compute the plan only in the CLI.* Rejected — breaks parity (REST/gRPC/MCP
  couldn't preview) and duplicates the logic a third time.
- *A standalone helper package.* Rejected — it needs `e.cfg.EmbeddingModel` and
  the engine's `*storage.DB`; it belongs on the `Engine` alongside `Migrate`.

---

## R2 — What does "dimensionality delta" mean when no backend is available?

**Decision.** The dry-run reports the **stored** dimensionality distribution
(`CorpusProfile.DimCounts` → which dims are present and how many embeddings each)
plus a **consistency flag** (`CorpusProfile.Consistent` — single model+dim vs
mixed). It does **NOT** predict the *target* model's dimensionality.

**Rationale.** The target model's embedding dimension is only known by generating
an embedding with it — which requires the live backend. FR-004 requires the
dry-run to succeed with no backend reachable, so a target-dim prediction is
impossible by construction. The honest, backend-free cost signal is: "your
corpus currently holds dims {768: 980, 1024: 20} → mixed; majority 768" plus the
stale count. That tells the operator the corpus is mixed and roughly how much
re-embedding is pending — enough to decide, without overstating precision. (This
refines the spec's FR-005 wording from "source→target dimensionality delta" to
"stored dimensionality distribution + consistency flag"; the target dim is a
post-migration fact, not a preview fact.)

**Alternatives considered.**
- *Probe the target model's dim by embedding a sentinel.* Rejected — needs the
  backend, violating FR-004, and adds latency/failure modes to a preview.

---

## R3 — How is the dry-run surfaced across transports?

**Decision.** A **distinct preview operation** on each transport —
`MigratePlan` — rather than a `dry_run` flag overloading the existing `Migrate`.
Concretely:
- **CLI**: `go-rag migrate --dry-run` → calls `Engine.MigratePlan`, renders the
  plan, exits 0 without proceeding. (Plain `migrate` also renders the same plan
  first, then proceeds — replacing the current inline print.)
- **gRPC**: a new `rpc MigratePlan(MigratePlanRequest) returns (MigrationPlan)`.
- **REST**: a new `GET /v1/migrate/plan` (or `POST` mirroring the no-body
  `Migrate`) returning the plan JSON.
- **MCP**: a new `migrate_plan` tool.

**Rationale.** `Migrate` returns `IngestSummary` (a mutation result); a preview
returns a fundamentally different shape (`MigrationPlan`). Overloading `Migrate`'s
return type conditionally is awkward in gRPC (one return type per RPC) and
muddies "this call mutates" vs "this call is read-only." A separate operation has
clean return types, clean read-only semantics, and matches the codebase's
one-engine-method-per-RPC/MCP/REST pattern (every other op works this way). The
CLI keeps the `--dry-run` *flag* affordance humans expect, but it routes to the
same plan path.

**Alternatives considered.**
- *`dry_run: bool` on `MigrateRequest`, return a union response.* Rejected —
  union return types are messy across transports and hide the read/mutate
  distinction that FR-003 depends on.

---

## R4 — What is the cost estimate, precisely?

**Decision.** The cost estimate is an **effort proxy**, not a time prediction:
`StaleEmbeddings` (the count that a real migrate would re-embed) + the stored
dimensionality distribution + the consistency flag + the source→target model
name. It is rendered with an explicit "estimate" label.

**Rationale.** A wall-clock prediction needs benchmarking the live embedding
backend's throughput (model, batch size, GPU/CPU, concurrency) — which (a)
requires the backend up (FR-004) and (b) is inherently approximate anyway. The
book's "reserve a reprocessing budget" guidance is served by knowing *how much*
work (the stale count) and *what kind* (model/dim change, mixed or clean) — that
is the decision signal. Time prediction is explicitly out of scope (Assumptions).

**Alternatives considered.**
- *A rough time estimate (stale_count × observed_embed_ms).* Rejected — needs a
  prior benchmark (where stored? how refreshed?) and the backend reachable;
  introduces a new failure mode and a false-precision risk.

---

## R5 — What guarantees the dry-run is strictly read-only?

**Decision.** `MigratePlan` touches only the two read functions
(`EmbeddingModelStats`, `CorpusProfile`) — both are `PrefixScan` reads over
Pebble that never write. It must NOT call `flushCaches`, `ReprocessAll`,
`refreshBaselineAfterMigrate`, or anything that bumps the index epoch. The
no-backend guarantee (FR-004) follows automatically: neither reader constructs an
`Embedder` or touches the network.

**Rationale.** FR-003 (read-only) and FR-004 (no backend) are the load-bearing
correctness properties of a dry-run. They are structural — they hold because the
method calls only metadata readers — rather than behavioural (something a flag
gates). The conformance test (see quickstart.md SC-001/SC-002) asserts both by
measuring corpus/cache/baseline state before/after and by running with the
backend unreachable.

**Alternatives considered.**
- *Gate the side effects behind a `dryRun bool` inside `Migrate`.* Rejected — a
  flag is a behavioural guard that can be bypassed by a future edit; a separate
  method that physically cannot reach the mutation code is structurally safe.
