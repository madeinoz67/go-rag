# Phase 1 — Data Model: Embedding Mismatch Validation

> Entities the guard introduces or relies on. Implementation-agnostic shape; field
> types are descriptive, not Go-source. Anchored on the existing stored
> `storedEmbedding{Model, Vector}` record (prefix 0x04) and `index.Vector.dims`.

## Entities

### Embedding Provenance  *(existing — now checked)*

The model name and dimensionality recorded for each stored vector.

| Field | Source | Notes |
|-------|--------|-------|
| `model` | stored record `Model` | written at ingest from the embedder's `Model()` (`workers.go:45`). |
| `dim` | `len(stored record Vector)` | derived, not stored separately. |

Before this feature the provenance was stored but never compared; this spec makes
it the basis of every mismatch decision. No schema change.

### Corpus Embedding Profile  *(introduced — derived, not stored)*

The majority model + dimensionality across all stored vectors in a vault, plus the
distribution that reveals drift. Computed from the prefix-0x04 records by a shared
helper; used by both the query guard and status.

| Field | Type | Notes |
|-------|------|-------|
| `majority_model` | string | the model name on the plurality of records; empty if no embeddings. |
| `majority_dim` | int | `len(Vector)` of the plurality; 0 if no embeddings. |
| `model_counts` | map\<model→count\> | enables drift detection + status reporting. |
| `dim_counts` | map\<dim→count\> | enables drift detection + status reporting. |
| `total` | int | number of embedding records scanned. |
| `consistent` | bool | true iff exactly one model and one dim are present (no drift). |

**Derivation rule**: read every prefix-0x04 record; tally `model` and
`len(Vector)`; majority = the entry with the highest count. `consistent` is true
when both maps have a single key. This is computed during the existing per-query
load scan (no new O(N); cached for free once H01 lands).

### Query Embedding Provenance  *(introduced at check time)*

The model + dimensionality of the vector produced for a query, captured before it
reaches the scorer.

| Field | Source | Notes |
|-------|--------|-------|
| `model` | active embedder `Model()` | the configured/injected embedder's model. |
| `dim` | `len(query vector)` | definitive — the actual vector length. |

The engine has both (it constructs the embedder and embeds the query); today it
discards them before scoring. This feature carries them to the guard.

### Mismatch Verdict  *(introduced — the guard's decision)*

The outcome of comparing Query Embedding Provenance to the Corpus Embedding
Profile, plus (for the partial case) how many stored vectors were skipped.

| Verdict | Condition | Action |
|---------|-----------|--------|
| `match` | query model = majority model **and** query dim = majority dim **and** corpus consistent | score normally; O(1). |
| `partial` | query matches majority, **but** corpus not consistent (some stored records differ) | score the matching majority; **skip+count** the mismatched stored vectors; attach `skipped` count. |
| `refuse` | query model ≠ majority model **or** query dim ≠ majority dim | return a clear error naming expected vs actual; **no results**. |
| `empty` | corpus has no embeddings | proceed (returns no results normally); not an error. |

### Status Drift View  *(extends existing StatusInfo — US2)*

| Field | Type | Notes |
|-------|------|-------|
| `stored_model` | string | majority model actually stored (may differ from configured). |
| `dimensions` | int | majority dim actually stored (existing field, now majority not first). |
| `model_drift` | bool | >1 model present. |
| `dim_drift` | bool | >1 dim present. |
| `model_counts` / `dim_counts` | map→count | surfaced for operator visibility. |

## Relationships

```text
stored Embedding records ──derive──> Corpus Embedding Profile
active embedder + query vector ──derive──> Query Embedding Provenance
(Corpus Profile, Query Provenance) ──guard──> Mismatch Verdict {match|partial|refuse|empty}
  match   ──> Vector.Query scores normally
  partial ──> Vector.Query skips mismatched lengths + counts; result carries `skipped`
  refuse  ──> engine.Query returns sentinel error (mapped per transport)
Corpus Embedding Profile ──also feeds──> Status Drift View (US2)
```

## Validation rules (from requirements)

- **FR-001/FR-003**: a query whose dim or model ≠ corpus majority MUST yield
  `refuse` — verified by asserting `engine.Query` returns the mismatch sentinel
  and no hits.
- **FR-005**: a mixed corpus queried by the majority MUST yield `partial` — only
  majority vectors scored, minority skipped, `skipped` count > 0, no error.
- **FR-002**: every stored record's model + dim MUST be readable for the profile
  (already persisted; verified the guard reads the `{Model, Vector}` shape).
- **FR-004**: status MUST reflect the stored majority and set drift flags when
  `model_counts`/`dim_counts` have >1 key.
- **FR-006**: the guard adds no per-query scan beyond the existing load (the
  profile is derived during it); happy path is O(1) after derivation.
- **FR-007**: the refuse error is identical in text across CLI/REST/gRPC/MCP
  (parity test).
