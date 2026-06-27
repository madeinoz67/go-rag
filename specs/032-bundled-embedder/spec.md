# Feature Specification: Bundled Pure-Go Default Embedder (Hugot GoMLX, no Ollama)

**Feature Branch**: `032-bundled-embedder`

**Created**: 2026-06-27

**Status**: Draft (revised) — pure-Go backend **resolves** the constitution conflict that blocked the prior ONNX draft. Ready for `/speckit-clarify` / `/speckit-plan`.

**Input**: User description: "implement ONNX embed model as default out of box experience so no ollama dependencies." → **Revised 2026-06-27** to use **Hugot's pure-Go GoMLX backend** instead of ONNX Runtime, after research showed a CGo-free local embedder exists.

> **What changed from the prior draft.** The first draft proposed ONNX Runtime,
> which required a **constitution amendment** (Principle III "no CGo / no C
> libraries") because ONNX Runtime is a C++ library (MuninnDB, the reference impl,
> builds with `CGO_ENABLED=1` and bundles `libonnxruntime` per platform). Research
> surfaced **Hugot's GoMLX backend** (`knights-analytics/hugot` + `gomlx/gomlx`,
> both Apache-2.0, pure Go) — a CGo-free feature-extraction pipeline. Switching to
> it **resolves Principle III**. The feature is now constitution-compatible; only a
> PRD scope edit (N9: Ollama-only) and one delivery decision remain.

## Clarifications

### Session 2026-06-27

- Q: How should the bundled embedding model be delivered? → A: **Download-at-runtime (Option A)** — hash-gated fetch (default source: go-rag's own GitHub Releases); the binary carries only the model ID + SHA-256; verify-on-download; stored globally; air-gapped-escapable; re-embed on hash change.
- Q: When should the model download be triggered? → A: **During `go-rag init`** (Option A) — the model is acquired as part of setup; `add`/`query` never initiate a fetch and run fully offline thereafter. `go-rag model install` remains available for explicit re-fetch/upgrade.
- Q: How to handle the pure-Go query-path latency risk before planning? → A: **Benchmark spike first** (Option A) — ~1 hr prototype (GoMLX + bge-small-en-v1.5) to measure short-query embed latency + batch ingest throughput; commit universal-default vs batch-only scope based on the result, before `/speckit-plan`.
- **SPIKE RESULT (2026-06-27):** Hugot GoMLX + bge-small-en-v1.5 (int8), `CGO_ENABLED=0`, darwin/arm64 — warm query **median 73 ms / p95 85 ms** (well within 500 ms hybrid budget); cold start ~91 ms (<1 s); batch **~20 embeds/sec** (acceptable for async background ingest; heavy bulk → Ollama US4); dim 384. `CGO_ENABLED=0 go build` confirmed (FR-009 ✓). **Verdict: universal pure-Go default (incl. interactive query) is viable.** Caveat: re-benchmark on representative low-end hardware during `/speckit-plan`. Note: Hugot v0.7.5 pulls `x/image` v0.41.0 (GO-2026-5061) — go-rag's v0.43.0 wins via MVS; re-run `govulncheck` after adopting Hugot.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Zero-setup first run (Priority: P1)

A new user installs go-rag and runs `go-rag init`, `go-rag add ./docs`, then
`go-rag query "..."`. It works end-to-end — embeddings generated, semantic results
returned — **without installing, configuring, starting, or knowing about Ollama**
(or any other service). Today this flow blocks on "Ollama not found"; after this
feature it just runs.

**Why this priority**: This is the entire reason the feature exists. Requiring a
separately-installed model server is the single largest setup friction in go-rag and
directly undercuts the PRD §1 frictionless thesis.

**Independent Test**: On a clean machine with no Ollama installed, run init → add →
query (plus the one-time model fetch, per Assumption A3). A semantic query returns
relevant results with no error referencing a missing external service.

**Acceptance Scenarios**:

1. **Given** a fresh OS with go-rag installed and no Ollama present, **When** the
   user runs `go-rag init` → `go-rag add ./docs` → `go-rag query "term"`, **Then**
   documents are embedded and relevant results return, with no error referencing a
   missing external service.
2. **Given** the pure-Go embedder is the default, **When** the user runs `go-rag
   status` on a fresh vault, **Then** go-rag reports the embedding provider as the
   bundled local model (not Ollama, not "unconfigured").

---

### User Story 2 — Local / offline operation (Priority: P2)

After the one-time model fetch, embedding and querying work with **no network and no
external service**.

**Why this priority**: Local-first and air-gapped operation is a core go-rag value
(constitution Principle I). The bundled pure-Go embedder makes this true for
embeddings, not just storage.

**Independent Test**: After the model is present locally, disconnect the network
entirely; run add → query. The cycle succeeds.

**Acceptance Scenarios**:

1. **Given** the model is present on disk (fetched once), **When** the machine is
   fully offline, **Then** add → query succeeds with no network call attempted.

---

### User Story 3 — Re-embed without duplication when switching models (Priority: P2)

A user who starts on the bundled default and later switches to Ollama (or vice
versa) can re-embed the whole vault in place. Existing documents are **re-embedded,
not duplicated**, and queries run against the new embedding space.

**Why this priority**: Constitution Principle II (content-addressed identity) makes
identity independent of the embedding model. Without this story, switching the
default would fragment every existing vault.

**Independent Test**: Add docs under the bundled model, switch the configured
provider to Ollama, reprocess; confirm document count unchanged and queries use the
new vectors.

**Acceptance Scenarios**:

1. **Given** a vault embedded with the bundled model, **When** the user switches to
   Ollama and reprocesses, **Then** each existing document is re-embedded in place
   (count unchanged, no duplicates) and queries return results from the new space.

---

### User Story 4 — Bring-your-own provider for higher quality / speed (Priority: P3)

Users who want a larger model or faster inference can configure Ollama (or another
provider) and override the bundled default. The bundled embedder is the **default,
not a mandate** — the provider-extension story stays intact.

**Why this priority**: Keeps constitution Principle V (Extension by Interface) whole.
The pure-Go default is slower than ONNX Runtime / Ollama, so the escape hatch matters
for users with latency or quality requirements.

**Independent Test**: Configure the Ollama provider, re-embed, confirm embeddings
come from Ollama and the bundled model is bypassed.

**Acceptance Scenarios**:

1. **Given** a vault using the bundled default, **When** the user configures Ollama
   and reprocesses, **Then** embeddings are generated by Ollama and the bundled model
   is not used.

---

### Edge Cases

- **Model artifact missing or corrupt** (deleted, interrupted fetch, bad install):
  go-rag MUST fail with a clear, actionable error naming the artifact and how to
  refetch/reinstall it. It MUST NOT silently fall back to a different embedding
  space (that would break cross-query comparability).
- **First-run fetch blocked / offline at install time** (download delivery only):
  clear error telling the user to fetch the model; the CLI MUST still work for
  non-embedding operations (status, config).
- **Unsupported OS/architecture** for the pure-Go runtime: GoMLX is pure Go and
  cross-compiles, so this is far narrower than the ONNX case, but any platform-specific
  SIMD path must degrade or report clearly.
- **Moving a vault between machines**: the model identity recorded on each embedding
  MUST let go-rag detect a model mismatch and prompt re-embedding rather than mix
  embedding spaces silently.
- **Memory pressure during large batch embeds**: MUST respect async-after-ACK
  (Principle IV) — slow pure-Go embedding must never block the <10 ms write ACK.
- **Latency on the query path**: the query string is embedded live; if GoMLX is too
  slow for short queries, the hybrid <500 ms budget is at risk (see Constraint C6).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: By default, go-rag MUST generate embeddings in-process using an
  embedding model executed by a **pure-Go** inference engine, with **no requirement
  for any separately-installed service or process**.
- **FR-002**: A fresh install MUST reach a queryable state (`init` → `add` → `query`)
  with **zero external-service setup steps**. (Network is used once, during `init`,
  to fetch the model — see FR-010.)
- **FR-003**: Embedding quality with the bundled default MUST meet the existing
  retrieval-quality baseline measured by the H02 eval harness — swapping the default
  MUST NOT regress recall@10 beyond the committed tolerance.
- **FR-004**: Users MUST be able to override the default and use Ollama (or another
  configured provider) instead, with the existing `Embedder` provider interface
  unchanged (constitution Principle V).
- **FR-005**: Switching the embedding model (bundled ↔ Ollama ↔ other) MUST re-embed
  existing documents **in place without creating duplicates** — document identity is
  content-addressed and independent of the embedding model (constitution Principle II).
- **FR-006**: If the model is unavailable (missing, corrupt, or fails to load), go-rag
  MUST fail with a clear, actionable error and MUST NOT silently switch to a different
  embedding space.
- **FR-007**: The constitution's latency budgets MUST still hold with the bundled
  embedder: cold start < 1 s, hybrid query < 500 ms, and write ACK < 10 ms independent
  of embedding latency (background embedding MUST stay off the ACK path, Principle IV).
  Because the pure-Go engine is slower than ONNX Runtime/Ollama, **query-path embed
  latency MUST be benchmarked at plan time** (Constraint C6).
- **FR-008**: The bundled embedder is scoped to the **embedding path only**. It MUST
  NOT perform LLM generation or answer synthesis — query-time generation and cloud
  providers remain out of scope (PRD N4 unchanged).
- **FR-009** (pure-Go build gate): The embedder and all its transitive dependencies
  MUST be pure Go and permissively licensed. `CGO_ENABLED=0 go build ./...` MUST
  succeed in CI with the new provider present — no CGo, no C libraries, no `dlopen`'d
  native runtime (constitution Principle III). This is the property that
  distinguishes this design from the rejected ONNX-Runtime approach.
- **FR-010** (model acquisition — Clarification 2026-06-27 Q1): The model is delivered by **download-at-runtime, hash-gated**. The binary carries only the expected model ID + SHA-256 (compile-time constant). When the local model is missing or its hash differs from the pinned expected hash, go-rag MUST download it, verify SHA-256 against the pinned hash, and **reject on mismatch** (never install an unverified model). The fetch is triggered by **`go-rag init`** (setup) or the explicit `go-rag model install` — never by `add`/`query`, which MUST run fully offline once the model is present. On a hash change (upgrade), the new model is fetched and existing docs re-embed in place (FR-005).

### Key Entities *(include if feature involves data)*

- **Embedding Provider** (existing `Embedder` interface): gains a new bundled/local
  implementation (Hugot GoMLX) that becomes the **default**; Ollama becomes an opt-in
  alternative rather than the assumed provider.
- **Bundled Model Artifact**: the embedding model (ONNX-format weights loaded by the
  pure-Go GoMLX engine) and its tokenizer. Each embedding records the model identity
  (name + version + dimensionality) so model switches are detected and trigger correct
  re-embedding — this identity is distinct from document identity (Principle II).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new user can go from install to a successful semantic query in under
  2 minutes on a clean machine, with no service to install or start (one-time model
  fetch permitted).
- **SC-002**: The full `add` → `query` cycle succeeds with no external service and,
  after the model is local, with the network fully disconnected.
- **SC-003**: Default-bundled retrieval quality (recall@10) stays within the existing
  eval tolerance of the Ollama baseline — measured by the H02 harness, no regression.
- **SC-004**: First-run setup steps that require an external service: **zero** (down
  from today's "install Ollama, start it, pull a model").
- **SC-005**: Cold-start and per-query latency continue to meet the constitution's
  performance standards with the bundled embedder active.
- **SC-006**: `CGO_ENABLED=0 go build ./...` succeeds with the bundled embedder — the
  binary stays pure-Go and single-static.

## Constraints

> The pure-Go pivot **resolves the binding constitution conflicts** that blocked the
> prior ONNX draft. C1 and C3 are now satisfied; only a PRD scope edit and one
> delivery decision remain.

- **C1 (constitution Principle III — "Pure Go, No CGo, No External Runtime"):
  RESOLVED.** Hugot's GoMLX backend (`gomlx/gomlx`) is pure Go with zero C
  dependencies; both it and `knights-analytics/hugot` are Apache-2.0 (permissive, per
  the constitution's license rule). This is the decisive difference from the rejected
  ONNX-Runtime design (which needed `libonnxruntime` and `CGO_ENABLED=1`). Verify at
  plan time with the FR-009 build gate.
- **C2 (Performance Standard — binary < 25 MB): RESOLVED.** Delivery is
  download-at-runtime (Clarification 2026-06-27, Q1), so the binary ships only the
  pure-Go engine and the pinned model hash — the model is fetched, not embedded →
  **no conflict; no standard revision needed.**
- **C3 (constitution Principle I — "no runtime dependencies beyond an optional local
  Ollama"): RESOLVED.** A pure-Go embedded or locally-fetched model is not an
  external runtime process or service; it is compiled-in code + local data.
- **C4 (PRD §2.2 / N9 — "Ollama-only for v1"): REVERSAL, PRD EDIT REQUIRED.** This
  feature makes a non-Ollama provider the **default**. N9 must be revised in the PRD.
  This is a product-spec change, not a constitution amendment.
- **C5 (constitution amendment process): NO LONGER REQUIRED.** Because C1/C3 are
  resolved by the pure-Go choice, no Principle amendment or Sync Impact Report is
  needed. (If D1 chooses bundling and the 25 MB standard is revised, that is a smaller
  performance-standard edit, still short of a Principle change.)
- **C6 (latency risk): RESOLVED — SPIKE 2026-06-27 measured 73 ms median query embed (D2).** Pure-Go inference (GoMLX)
  is materially slower than ONNX Runtime / Ollama (research: ~10–50× vs ORT for the
  slowest pure-Go float32; GoMLX is faster than that but still slower than ORT).
  Background batch ingest is fine (async-after-ACK), but **the query path embeds the
  query string live** — plan must benchmark short-query embed latency and confirm it
  fits the <500 ms hybrid budget, or scope the pure-Go default to batch/offline paths.

## Assumptions

- **A1 (engine)**: `knights-analytics/hugot` with the **GoMLX pure-Go backend**
  (`NewGoSession()`), backed by `gomlx/gomlx`. Both Apache-2.0 (constitution
  license-compatible) and pure Go (constitution Principle III-compatible). Hugot
  provides a HuggingFace-compatible `featureExtraction` pipeline (BERT/MiniLM/BGE
  mean-pooling). Exact API surface decided at plan time; the FR-009 build gate is the
  proof.
- **A2 (model)**: **`bge-small-en-v1.5`** (BAAI, 33M params) — the proven small-model
  default (matches MuninnDB and the embedder-research recommendation; community
  consensus 2025–26 is to stop using `all-MiniLM-L6-v2` for new builds). Quantization
  (FP16 vs int8) and dimensionality decided at plan time against the eval baseline.
- **A3 (delivery — DECIDED, Clarification 2026-06-27 Q1)**: the model is delivered
  via **download-at-runtime, hash-gated**. The go-rag binary carries only the expected
  model ID + SHA-256 as a compile-time constant (~64 bytes). On `go-rag init` (or explicit `go-rag model install`), it hashes the local model;
  if missing or hash ≠ expected, it
  downloads the model, **verifies SHA-256 against the pinned hash, and rejects on
  mismatch** (tamper/corruption guard). Source default: the model is a release asset
  on go-rag's own GitHub Releases (same-origin, trustworthy) — sub-decision D1a.
  Stored globally at `~/.go-rag/models/<model-id>/` (shared across vaults). When the
  pinned hash changes on upgrade, the next install fetches the new model and existing
  docs re-embed in place (FR-005). Air-gapped users can place the model file
  out-of-band; go-rag verifies its hash on load.
- **A4**: The pure-Go embedder is the **default**; Ollama becomes an opt-in
  alternative and is **not removed**. The existing `Embedder` interface is unchanged.
- **A5**: Every stored embedding records its model identity (name + version + dim) so
  a model switch is detectable and triggers correct re-embedding (supports FR-005).

## Decisions / Open Questions

- **D1 (delivery — RESOLVED 2026-06-27, Q1)**: **download-at-runtime (Option A)**.
  Keeps the binary pure-Go and < 25 MB, cross-compiles cleanly; the only cost is a
  one-time hash-gated fetch (mechanism in A3 / FR-010).
- **D1a (download source — deferred to `/speckit-plan`)**: default = model shipped as
  a release asset on go-rag's own GitHub Releases (same-origin, fits the release
  pipeline). Alternative: HuggingFace direct (canonical, but third-party runtime
  egress). Confirm at plan.
- **D1b (fetch trigger — RESOLVED 2026-06-27, Q2)**: the model is fetched during
  `go-rag init` (and via explicit `go-rag model install`); `add`/`query` never fetch.
- **D2 (latency gate — RESOLVED 2026-06-27, SPIKE COMPLETE)**: the pure-Go default is
  **universal, including the interactive query path.** Measured Hugot GoMLX +
  bge-small-en-v1.5 (int8), `CGO_ENABLED=0`, darwin/arm64: warm query embed **median
  73 ms / p95 85 ms** (within the <500 ms hybrid budget, with room for vector search +
  rerank); cold start ~91 ms (<1 s); batch ingest **~20 embeds/sec** (acceptable off
  the ACK path; heavy bulk ingest → Ollama, US4). `CGO_ENABLED=0 go build` confirmed
  (FR-009 ✓). Re-benchmark on representative low-end hardware during `/speckit-plan`.
- No constitution amendment is required for this feature (unlike the prior ONNX
  draft). The only spec-level edit needed is PRD N9 (C4), which is the feature's
  purpose.

> *Note: the prior draft's blocking `[NEEDS CLARIFICATION]` (a constitution-amendment
> showstopper) is removed — the pure-Go choice resolved it. The two open items above
> have reasonable defaults and are flagged for clarify/plan, not blockers.*
