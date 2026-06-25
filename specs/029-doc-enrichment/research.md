# Research — Document Auto-Tag & Summary Enrichment (spec 029)

**Phase 0 output.** Resolves the design decisions for doc-level background
enrichment. Grounded in the source-verified MuninnDB enrichment architecture
(`scrypster/muninndb`) and go-rag's existing sidecar patterns (specs 019/025/026).

---

## R1 — Where do tags/summary live? (the identity-safe question)

**Decision.** A **dedicated `Document.Enrichment *EnrichInfo` sidecar** — `{Tags
[]string, Summary string, Model string, GeneratedAt time.Time, Status string}` —
mirroring the per-`Chunk` sidecars (`Poisoning`, `SectionContext`, `NearDup`).
The existing tag filter is served via a **one-line bridge**: the filter's
tag-resolution helper reads `Document.Enrichment.Tags` *in addition to*
`Document.Metadata["tags"]` (manual/frontmatter tags), so auto-tags flow into
`--tags` filtering with no query-surface change.

**Rationale (identity-critical).** `GenerateID(content, mime, metadata)`
(`model.go`) **hashes the metadata map**. If enrichment wrote tags straight into
`Document.Metadata["tags"]` before the ID is computed, the document's
content-addressed identity would change (Constitution II violation) and re-add
would no longer be a no-op. The ID is fixed once at ingest from the *original*
metadata; a dedicated sidecar populated *after* store keeps identity pristine by
construction. This is the same discipline `SectionContext` uses (span data
stripped from metadata before `GenerateID`). The bridge keeps the spec's "tags
land where the filter reads" promise true without mutating the identity input.

**Alternatives considered.**
- *Tags in `Document.Metadata["tags"]` directly (mutate post-store).* Rejected —
  `Metadata` is the identity-input map; mutating it post-store is conceptually
  muddy (it's "what the doc IS," not "what we learned"), and any future code path
  that re-runs `GenerateID` over current metadata would silently re-key the doc.
  The dedicated sidecar is explicit and safe.

---

## R2 — How is enrichment executed? (async model)

**Decision.** An **async enrichment step in the existing ingest worker** — the
same `processJob` background path that already does embed + BM25 indexing + near-dup
clustering (spec 026). The pipeline gains a `SetEnricher(e Enricher)` binding
(mirroring `SetDetector`/`SetRedactor`/`SetNearDupK`); when bound, `processJob`
calls the enricher once per document after its chunks are stored, and writes the
`EnrichInfo` sidecar. Strictly post-ACK (Constitution IV).

**Rationale.** go-rag already has the async-after-ACK worker pattern; near-dup
(spec 026) is the in-house template for "compute a sidecar async and write it
back." MuninnDB's separate `RetroactiveProcessor` (a dedicated goroutine with its
own 3 s poll + scan-by-flag) is more elaborate than go-rag needs at local scale —
the per-document async hook in `processJob` is simpler and reuses the existing
drain-on-`Close` lifecycle. We adopt MuninnDB's *resilience ideas* (circuit
breaker, no-infinite-retry flag — R5) without its heavier processor topology.

**Alternatives considered.**
- *A standalone MuninnDB-style retroactive processor (separate goroutine +
  scan-by-flag queue).* Rejected for v1 — heavier, duplicates the worker
  lifecycle, and go-rag's corpora are small enough that per-doc async-in-processJob
  suffices. Revisit if enrichment backlog/back-fill becomes a concern.

---

## R3 — What is the Enricher, and which provider?

**Decision.** A new **`Enricher` interface** — `Enrich(ctx, doc) (*EnrichInfo,
error)` — conceptually the document-level sibling of `embed.Embedder`, but for
**generation** (produce tags + summary text), not embedding (produce vectors).
The v1 provider is a **local-Ollama generation client** (the model's
`/api/generate` or `/api/chat` endpoint — the *same* Ollama base URL already used
for embeddings, different endpoint). Pure-Go, reusing the existing HTTP client
shape; no new dependency (Constitution III).

**Rationale.** Enrichment is fundamentally a generation task (summarize + tag),
so it needs a generation interface distinct from `Embedder.Embed`. Ollama already
serves go-rag's embeddings on loopback; the marginal cost of a small
tagging/summary model (e.g. a 7–8 B model) is latency, not dollars (local, free).
The specific model is a plan/config decision (`enrichment_model`), not prescribed.

**Alternatives considered.**
- *Reuse the `Embedder` interface.* Rejected — embeddings and generation are
  different call shapes, endpoints, and outputs; overloading `Embedder` would lie.
- *Cloud providers (OpenAI/Anthropic).* Rejected for v1 (Constitution I: local
  only; MuninnDB supports them but go-rag's thesis is air-gapped). The interface
  leaves the door open.

---

## R4 — How is it gated? (opt-in)

**Decision.** A config gate `enrichment_enabled` (default **off**) +
`enrichment_model`, resolved via `EffectiveEnrichmentEnabled()` — exactly the
pattern `EffectivePoisoningEnabled()` uses (spec 019). The engine binds the
enricher to the pipeline only when enabled. Off → zero enrichment, zero model
calls, byte-identical to today.

**Rationale.** Enrichment consumes local model resources (GPU/CPU) and needs a
tagging model pulled, so default-off is the safe v1 posture (FR-006). An operator
opts in by setting the flag + model. The `Effective*` resolver mirrors the
established config pattern (resolved value, not raw flag).

---

## R5 — How is it resilient? (model-down, bad output, no infinite loop)

**Decision.** Three layers, adopted from the source-verified MuninnDB design:
1. **Circuit breaker** around the model call — opens after consecutive failures
   (MuninnDB defaults: 5 fails → 30 s open, half-open probe), so a down/misbehaving
   model fast-fails instead of stalling the worker.
2. **Permanent-fail marking** — an `EnrichInfo.Status` (`enriched` / `failed` /
   `nothing-to-enrich`) so a document whose enrichment permanently fails (bad
   output) is not retried indefinitely.
3. **Graceful absence** — a document with `Enrichment == nil` (unenriched,
   pre-feature, or enrichment-off) queries and loads normally; the filter simply
   has no auto-tags for it (manual/frontmatter tags still work).

**Rationale.** These are the load-bearing robustness properties (FR-007/FR-009).
MuninnDB's design (verified: `circuit.go` 5/30 s defaults; `DigestEnrichFailed`
permanent-fail bit; `ErrNothingToEnrich`) maps cleanly onto go-rag's
sidecar-status field. The worker never blocks ingest (it's async) and never loops
forever (failed docs are marked).

---

## R6 — How is it surfaced and back-filled?

**Decision.**
- **Tags** — via the R1 filter bridge; `--tags` filtering works with no new query
  field.
- **Summary** — a new field surfaced wherever document metadata is shown: on
  `status` (per-doc + aggregate enriched count) and on the hit/document view,
  identically across CLI/REST/gRPC/MCP (FR-010). Omitted when absent.
- **Back-fill** — an explicit re-enrich pass over pre-feature documents (mirrors
  `Reprocess`/`RescanPoisoning` lifecycle), since enrichment is derived at ingest
  time and there's no cheap rescan (consistent with prior sidecar features).

**Rationale.** Tags need no new transport field (they ride the existing filter);
summary is a small additive field on the document/status surface. Back-fill
follows the established re-derive-on-demand pattern.

---

## R7 — The PRD N4 dependency (scope change)

**Decision.** Enrichment is model-based **generation**, which the PRD's "no LLM
inference" non-goal (N4) excludes. This feature revises N4 **narrowly**: "no LLM
inference **except background, local-only document enrichment**." The PRD edit is
a tracked prerequisite for implementation — it does *not* affect the Constitution
Check (local-only honours Principle I; the sidecar honours II; async honours IV;
the interface honours V).

**Rationale.** Stephen directed this scope change after the verified research;
the constitution is the gate, and it passes. The PRD revision is recorded so the
non-goal list stays honest rather than silently violated.

**Alternatives considered.**
- *Skip enrichment entirely (honour N4 as-is).* Rejected — the single biggest
  retrieval-quality lever (metadata filtering) is unreachable without it, and the
  filter plumbing already exists.
- *Broaden N4 to "LLM generation everywhere."* Rejected — keep the revision
  narrow (background local enrichment only); query-time generation / answer
  synthesis stays out of scope (retrieval-only remains the product).
