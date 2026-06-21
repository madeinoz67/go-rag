# Data Model — Reranker Error Surfacing (H09)

> Phase 1 output. This feature adds **one boolean field** to an existing struct, **one
> boolean config knob**, and a **log-line shape**. It introduces no new persisted
> entities and no storage changes — the change is entirely on the read/query path.
> Field types are Go-level (engine layer); transport wire shapes are in
> [contracts/query-response.md](contracts/query-response.md).

## Entity: `QueryResult` (modified — `internal/engine/types.go`)

The structured query result returned by `engine.Query` and serialized by every adapter.

| Field | Type | Existing? | Rule |
|---|---|---|---|
| `Hits` | `[]QueryHit` | existing | ranked hits (unchanged); on rerank failure these are in **fallback (RRF) order**, not reranked order |
| `RerankFailed` | `bool` | **new** | `true` **iff** reranking was attempted (reranker configured for the query) and failed (rerank error OR `len(scores) != len(candidates)`). `false` for: rerank succeeded, reranking not configured, empty candidate pool. |

**Validation rules (from FR-001/002/007/008):**
- `RerankFailed=true` ⇒ `Hits` is non-empty whenever candidates existed (degrade ranking, not completeness).
- `RerankFailed=true` ⇒ a failure log line was emitted (FR-003/FR-005).
- A retrieval-stage failure never sets `RerankFailed`; it returns a non-nil `error` from `engine.Query` instead (FR-008/009).

**Unchanged:** `QueryHit` (chunk_id, document_id, score, content, file_path, page) — no new fields.

## Entity: `Config` (modified — `internal/config/config.go`)

| Field | Type | Existing? | Default | Rule |
|---|---|---|---|---|
| `RerankModel` | `string` | existing | `""` | empty ⇒ reranking disabled (`RerankFailed` stays `false`) |
| `RerankCandidates` | `int` | existing | `20` | candidate pool size fed to the reranker |
| `RerankRetryOnFailure` | `bool` | **new** | `false` | when `true`, a failed rerank retried once with `min(pool*2, 200)` candidates before falling back (FR-006). Off ⇒ failure degrades straight to fallback. |

**Note:** `RerankRetryOnFailure` is operator-configured (no per-request field in v1 — see research.md D5).

## Entity: `RerankFailureRecord` (log shape — not persisted)

A single `log.Printf` line emitted on rerank failure (FR-003). **Not** a stored entity;
captured here so the log contract is fixed.

```
rerank failed: model=<RerankModel> candidates=<N> scores=<M> err=<error>
```

| Component | Source | Allowed content |
|---|---|---|
| `model` | `cfg.RerankModel` | model name only |
| `candidates` | `len(hits)` at rerank time | integer count |
| `scores` | `len(scores)` returned by `Reranker.Score` | integer count (diagnoses length mismatch) |
| `err` | the rerank error | error message text |
| query text / candidate content | — | **NEVER logged** (clarification Q3 → A) |

**State transitions — rerank outcome per query:**

```text
                       ┌─ reranker == nil OR NoRerank ──→ SKIPPED (RerankFailed=false)
query with candidates ─┤
                       │   ┌─ err==nil && len ok ─────────→ SUCCEEDED (RerankFailed=false)
                       └───┤
                           │   ┌─ retry off ───────────────→ FAILED (RerankFailed=true, log)
                           └───┤
                               │   ┌─ retry succeeds ───────→ SUCCEEDED (RerankFailed=false)
                               └───┤
                                   └─ retry fails ──────────→ FAILED (RerankFailed=true, log)
```

Separately, **candidate-retrieval failure** (FR-009) is not a state on this machine —
it short-circuits to a non-nil `error` returned from `engine.Query` (no `QueryResult`).

## What is NOT in the data model

- No new persisted records, no new Pebble key prefix, no index/migration changes.
- No change to `Reranker.Score`'s signature (`([]float64, error)`) — the bug is in the
  caller (`retrieval.go`) that discards it, not the adapter.
- Embedding/model drift (H11) and reranker pool-size tuning (H22) are deliberately out
  of scope (spec Assumptions).
