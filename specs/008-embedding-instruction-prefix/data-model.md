# Phase 1 — Data Model: Embedding Instruction-Prefix

> Entities the feature introduces or extends. Implementation-agnostic shape;
> field types are descriptive, not Go-source. Anchored on the existing stored
> `storedEmbedding{Model, Vector}` record (prefix 0x04, `workers.go:45`) and the
> spec-005 `EmbeddingProfile` (`embedding_profile.go`). **No new Pebble prefix**
> — the convention rides the existing 0x04 record (single-byte key-space
> unchanged, constitution storage discipline).

## Entities

### Embedding Role *(new — conceptual, not persisted)*

The purpose of a text at the moment it is embedded.

| Value | Where it applies | Prefix applied |
|-------|------------------|----------------|
| `query` | retrieval queries (`engine.Query` probe + the `EmbedFunc` passed to `NewRetrieval`) | the model's query-role prefix |
| `document` | corpus chunks (`pipeline/workers.go`) | the model's document-role prefix |

The Role is a transient parameter to the pure `Prefixer`; it is not stored.

### Instruction Prefix *(new — derived)*

The role-specific text prepended before embedding, resolved per the configured
model's convention. Resolved by the `Prefixer` (research D2):

- `nomic-embed-text` → query `search_query:`, document `search_document:`
- E5 family → query `query:`, document `passage:`
- BGE family → query-only instruction, document none
- unknown model → none (FR-003)

Idempotent: a text already beginning with its prefix is not re-prefixed (D4).

### Embedding Provenance *(existing 0x04 record — extended)*

The per-vector record of how a vector was produced. Today `{model, vector}`;
this feature adds the **convention** axis so a half-prefixed corpus is detectable.

| Field | Source | Notes |
|-------|--------|-------|
| `model` | stored record `Model` | unchanged (written from `embedder.Model()`). |
| `dim` | `len(Vector)` | derived, unchanged. |
| `convention` *(new)* | the prefix convention active at embed time | `"nomic"`, `"e5"`, `""` (none/legacy). **Backward compat:** old records with no field read as `""`. |

The convention is **provenance, not identity**: it describes how a vector was
produced, so the corpus profile and mismatch guard can reason about it. It does
not enter the document/chunk identity hash (Principle II).

### Corpus Embedding Profile *(existing `EmbeddingProfile` — extended)*

Derived read-only from the 0x04 records by `CorpusProfile`. Gains a third axis.

| Field | Existing/new | Meaning |
|-------|--------------|---------|
| `MajorityModel` | existing | plurality model name. |
| `MajorityDim` | existing | plurality dimensionality. |
| `MajorityConvention` *(new)* | **new** | plurality convention string. |
| `ModelCounts` | existing | per-model record counts. |
| `DimCounts` | existing | per-dim record counts. |
| `ConventionCounts` *(new)* | **new** | per-convention record counts. |
| `Total` | existing | records scanned. |
| `Consistent` | existing → **widened** | true iff ≤1 model, ≤1 dim, **and ≤1 convention**. |

A query's active convention is compared to `MajorityConvention` by the mismatch
guard (see contracts). An empty corpus remains `Consistent=true` (vacuous).

### Prefix Convention (Config) *(new config fields)*

Extends `config.Config` following the spec-006 rerank-field pattern.

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `embedding_prefix` | enum `auto`\|`on`\|`off` | `auto` | `auto` = derive from `embedding_model`; `on`/`off` = force. |
| `embedding_query_prefix` | string | `""` | explicit override of the query prefix (empty = derive). |
| `embedding_doc_prefix` | string | `""` | explicit override of the document prefix (empty = derive). |

`Get` / `Set` and `knownConfigKeys` (`internal/engine/config.go`) gain the three
keys; `config.Default()` sets `embedding_prefix = "auto"`.

## State transitions

- **Empty corpus → first ingest:** the first document's convention (resolved
  from config) establishes the corpus convention. Subsequent ingests must match;
  a mismatched ingest is still stored (provenance records the actual convention)
  but flips `Consistent=false`.
- **Legacy corpus (convention `""`) + prefixes enabled:** querying yields a
  convention mismatch (US3) → warn/refuse with a "re-embed" hint. Re-embedding
  writes convention-tagged vectors; identity unchanged → **no duplicate
  documents** (FR-007).
- **Re-embed under a new convention:** vectors change, documents/chunks do not.
  `Consistent` returns to true once the re-embed completes.

## Validation rules (from spec FRs)

- FR-001/002: query → query prefix, document → document prefix, for prefix models.
- FR-003: unknown/non-prefix model → no prefix ever.
- FR-004: config override honored verbatim.
- FR-005: convention stored as provenance.
- FR-006: convention mismatch detected before scoring.
- FR-007: re-embed creates no duplicates (identity over content+metadata, not prefix).
- FR-008: idempotent prepend; off the write path.
