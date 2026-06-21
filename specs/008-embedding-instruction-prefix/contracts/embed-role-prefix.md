# Contract: Embedding Role / Instruction-Prefix

> The behavioral contract for how a query/document Role and its instruction
> prefix flow through embedding, how the convention is configured, and how a
> convention mismatch is reported. Governs every transport (CLI/REST/gRPC/MCP)
> and the ingest pipeline identically (constitution Principle V, spec FR-009).

## 1. Role semantics

Every text handed to the embedder carries exactly one **Role**:

| Role | Producer | Prefix rule (prefix models) |
|------|----------|------------------------------|
| `query` | `engine.Query` (probe + the `EmbedFunc` passed to `index.NewRetrieval`) | prepend the model's **query** prefix |
| `document` | `pipeline/workers.go` (chunk ingest, async) | prepend the model's **document** prefix |

The Role is applied by a single pure `Prefixer`; no embedder implementation
needs to know about roles (Principle V). For a non-prefix model, both roles
apply **no** prefix (FR-003).

**Invariants:**

- The prefix is prepended to the text fed to the embedder **only**. Stored
  `Chunk.Content`, the FTS index, and document/chunk identity hashes are
  **never** prefixed (FR-007, Principle II).
- Prefixing is **idempotent**: a text already starting with its prefix is not
  re-prefixed (FR-008).

## 2. Convention resolution (config-gated)

Given the configured `embedding_model` and the `embedding_prefix` mode:

| `embedding_prefix` | Query prefix | Document prefix |
|--------------------|--------------|-----------------|
| `auto` (default) | default-map lookup by model; **none** if unknown | same |
| `on` | default-map lookup (errors if model unknown? no — falls back to explicit below) | same |
| `off` | none | none |
| explicit `embedding_query_prefix` / `embedding_doc_prefix` set | the literal strings | the literal strings |

**Default map** (model-substring match):

| Model family | Query | Document |
|--------------|-------|----------|
| `nomic-embed-text` | `search_query:` | `search_document:` |
| `e5-*` / `multilingual-e5` | `query:` | `passage:` |
| `bge-*` | query instruction | *(none — document unprefixed)* |
| anything else | none | none |

**Resolution precedence:** explicit override strings > mode-derived > none.

## 3. Convention-mismatch contract (US3 / FR-006)

The existing `checkEmbeddingMismatch` guard (`engine/query.go`) — which today
compares query model/dim to the corpus `EmbeddingProfile` majority — is extended
to compare the **active convention** to the corpus **`MajorityConvention`**.

| Condition | Behavior |
|-----------|----------|
| query convention == corpus majority | score normally |
| query convention ≠ corpus majority (incl. legacy `""` corpus queried with prefixes on) | refuse the query with a clear error naming both conventions and directing the operator to **re-embed** (same shape as the existing model/dim mismatch error) |
| empty corpus | no error (no majority to compare) |
| mixed-convention corpus, query matches majority | score the matching majority; skip the minority with a logged warning (mirrors the spec-005 graceful-degradation path) |

The error is part of the existing `ErrEmbeddingMismatch` family so every
transport already maps it consistently (spec 003 parity — no per-transport work).

## 4. Cross-transport parity

Because the prefix is applied in `engine.Query` and `pipeline.workers` — the two
single shared paths under the engine — a query gets the same query prefix and an
ingested document the same document prefix **regardless of origin** (CLI, REST,
gRPC, MCP) (FR-009). No transport adapter changes are required.

## 5. Status surface (SC-003)

The standard status view reports, alongside model/dim/drift today:

- the active prefix **mode** and resolved query/document prefixes, and
- the corpus's `MajorityConvention` and a **convention-drift** flag when more
  than one convention is present (mirrors `EmbeddingDrift`).

## 6. Out of contract

- **Learned / per-query prefix tuning** — not supported; the convention is
  configured/declared, not discovered at runtime (spec Assumptions).
- **Auto-detection of a model's convention by probing the model** — out of scope;
  the default map + override is the v1 mechanism.
- **Reranker space** — unaffected; this contract governs the retrieval embedding
  space only (spec Edge Cases).
