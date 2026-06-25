# Contract — Migration Plan API (H24, spec 028)

**Phase 1 output.** Unlike most go-rag features, H24 **does** add an external
surface: a new read-only **preview operation** exposed on all four transports,
returning a `MigrationPlan`. This document is that contract. (The real `Migrate`
operation is unchanged in shape.)

---

## The `MigrationPlan` payload (transport-neutral)

Every transport returns the same fields, same names, same semantics:

| Field | Type | Meaning |
|-------|------|---------|
| `target_model` | string | model a real migrate would re-embed onto (`cfg.EmbeddingModel`) |
| `total` | int | total tracked embeddings |
| `stale_total` | int | embeddings a real migrate would re-embed |
| `sources` | `[]{model, count, stale}` | one row per stored model |
| `dimensions` | `[]{dim, count}` | stored dimensionality distribution |
| `consistent` | bool | clean (single model+dim) vs mixed corpus |
| `estimate` | `{stale_embeddings, model_change, mixed_corpus, note}` | labelled-approximate cost |

The `estimate.note` always states it is an estimate, not a time guarantee (R4).
Field naming follows the repo's existing `snake_case` wire convention (REST/gRPC)
and is rendered to the CLI/MCP in human-readable form.

---

## Transport contracts

### CLI

```
go-rag migrate --dry-run     # NEW flag: print plan, exit 0, re-embed nothing
go-rag migrate               # unchanged: renders the SAME plan, then proceeds
```

`--dry-run` MUST exit before any embedding. Output renders every plan field
human-readably (target model, per-source counts with `<- stale` markers, dim
distribution, consistency, estimate). Exit 0 in all cases (empty/clean/mixed).

### REST

`POST /v1/migrate/plan` (no body) → `200` with the plan JSON.
- Read-only: MUST NOT touch the corpus/caches/baseline.
- Succeeds with the embedding backend down (metadata-only).

### gRPC

```proto
rpc MigratePlan(MigratePlanRequest) returns (MigrationPlan);

message MigratePlanRequest {}
message MigrationPlan {
  string target_model = 1;
  int32 total = 2;
  int32 stale_total = 3;
  repeated ModelCount sources = 4;
  repeated DimCount dimensions = 5;
  bool consistent = 6;
  Estimate estimate = 7;
}
message ModelCount { string model = 1; int32 count = 2; bool stale = 3; }
message DimCount   { int32 dim = 1; int32 count = 2; }
message Estimate   { int32 stale_embeddings = 1; bool model_change = 2; bool mixed_corpus = 3; string note = 4; }
```

(Exact field numbers assigned at implementation; shape is the contract.)

### MCP

A new `migrate_plan` tool (no args) returning the plan as structured output. The
existing `migrate` tool is unchanged.

---

## Cross-cutting contract clauses

- **Read-only guarantee (FR-003).** On every transport, the preview MUST NOT
  modify embeddings, documents, caches, the corpus baseline, or the index epoch.
  This is structural — the operation reaches only `EmbeddingModelStats` +
  `CorpusProfile` (see [data-model.md](./data-model.md) §3) — not gated by a flag.
- **No-backend guarantee (FR-004).** The preview MUST succeed with the embedding
  backend unreachable; it generates no embedding.
- **Parity (FR-006).** The same corpus yields an identical `MigrationPlan` across
  all four transports.
- **Preview == execution (FR-008).** The `stale_total`/`sources[].stale` the
  preview reports is exactly the set `Migrate` re-embeds (the engine computes both
  from the same `MigratePlan`).

---

## What is explicitly NOT added

- **No change to `Migrate`'s contract** — it still returns `IngestSummary` and
  still re-embeds. The preview is a *separate* operation (R3), not an overload.
- **No time-based estimate** — out of scope (R4); the estimate is an effort proxy.
- **No partial/selective migration, no scheduling, no auto-trigger** — out of
  scope (spec Assumptions).
