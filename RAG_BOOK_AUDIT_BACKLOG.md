# go-rag Audit — Implementation Backlog (remaining items)

> Companion to `RAG_BOOK_AUDIT.md`. The audit's **top-priority item, H02
> (retrieval-quality eval harness)**, is **✅ COMPLETE** — implemented in
> `specs/004-retrieval-eval-harness/` and shipped: `go-rag eval` + `go_rag_eval`
> MCP tool compute recall@5/10, precision@5, MRR, NDCG@10 over a committed golden
> dataset, offline and reproducibly, with a `make test-eval` regression gate in
> CI. H02 is intentionally **not** listed below. Use that harness to prove each
> remaining retrieval change helps before merging it.
>
> This file is the actionable todo list for **everything else**: H01 and H03–H28.
> Items are grouped by the audit's recommended remediation order (§5), each
> marked so we can pull one off into its own spec when we're ready. Check a box
> when an item is done; move a row to its own `specs/NNN-…` spec when we start it.
>
> **Priority key:** P0 = silent killer/blind-spot · P1 = quality/correctness/latency ·
> P2 = hardening/ops/polish · P3 = minor/future. **Effort:** S ≤1d · M 1–3d · L >3d.
>
> Source of truth for each item's detail: `RAG_BOOK_AUDIT.md` §1–§2.

---

## Phase 1 — Cheap correctness/security quick-wins (do alongside H02) · all S

These are low-effort, high-value, and several become *visible* the moment H02 lands
(the eval harness will show their quality impact).

- [x] **H03** · P0 · S · embeddings — **Embedding dim/model mismatch unvalidated → silent corruption.** ✅ COMPLETE (spec 005): `Vector.Query` skips mismatched-length vectors (no silent garbage cosine); `engine.Query` refuses on model/dim mismatch via `ErrEmbeddingMismatch`; status surfaces stored majority + drift. *(Audit §1.2)*
- [x] **H09** · P0 · S · retrieval — **Reranker errors silently swallowed.** On rerank error or length mismatch: log the error, surface a `RerankFailed` flag, optionally re-run with a larger pool; never discard the error. *(Audit §1.4)* → **spec 006** (implemented: `RerankFailed` flag on QueryResult surfaced across CLI/REST/gRPC/MCP; retrieval-error propagated (FR-009); opt-in retry (FR-006); failure logged w/o query text (FR-003). Gates: build/vet/test/eval green; `golangci-lint` skipped — not installed in env)
- [x] **H13** · P0 · S · security — **Loopback not enforced on bare `serve` (default binds `0.0.0.0`).** Make loopback the default in `config.Default()`; reject non-loopback bind at boot unless `--bind-external` is set. *(Audit §1.7/§1.8)* ✅ COMPLETE (spec 007): `internal/daemon/bind.go` (`IsLoopbackBind`/`ValidateBind`/`ExternalBindWarning`); boot gate in `serve`'s RunE (chokepoint) + fast-fail pre-check in `start`; `--bind-external` on `start`+`serve` forwarded via `Addrs.BindExternal`; exposure warning at boot; `config.Default().MCPAddr` → `127.0.0.1:7878` + regression test; README docs. Gates: build/vet/test green (`-race -cover`); quickstart scenarios 1/2/3/5 verified live; `golangci-lint` skipped — installed but config-version incompatible (pre-existing env issue, not code)
- [x] **H07** · P1 · S · embeddings — **Missing embedding instruction-prefix (nomic/E5).** ✅ COMPLETE (spec 008): a pure `Prefixer` (`internal/embed/prefix.go`) applies role-correct `search_query:` (retrieval) / `search_document:` (pipeline) prefixes via a nomic/E5/BGE default map, config-gated (`embedding_prefix` auto|on|off + per-role overrides); applied at the two embed boundaries (pipeline documents, `engine.Query` query wrap) with `Embedder.Embed` unchanged (Principle V); the 0x04 record gains a `convention` provenance axis extending spec 005's `EmbeddingProfile` + mismatch guard (refuse on convention mismatch or mixed — never silently half-prefix, FR-006); status surfaces the active convention/mode; re-embed is no-duplicate (Principle II). Gates: build/vet/test green (`-race -cover`); real-nomic smoke verified (ingest+query+status with prefixes on `nomic-embed-text`, 768-dim); `golangci-lint` skipped — not installed in env. Full recall@5/10 + NDCG@10 prefix-off-vs-on delta (SC-001) is the remaining manual measurement — the eval CLI has no model/prefix toggle yet, so the baseline run needs `embedding_prefix=off` via config. *(Audit §1.2)*
- [ ] **H08** · P1 · S · retrieval — **RRF weights hardcoded + asymmetric-k formula unreviewable.** Move k to config (or derive from Mode), expose via flag; document the asymmetry as intentional OR collapse to single k=60. *(Audit §1.3/§1.4)*
- [ ] **H12** · P1 · S · embeddings — **Whole-doc embed batch unbounded → OOM/timeout.** Batch texts ~32–64 inside `Ollama.Embed`, concatenate responses, per-batch retry. *(Audit §1.2)*

## Phase 2 — Biggest latency win (validate timing with H02)

- [ ] **H01** · P0 · M · storage/latency — **Per-query full index rebuild (`LoadIndex` every Query).** Cache the loaded `(FTS, *Vector)` on the Engine with a content-hash generation counter; invalidate on ingest/delete/watcher. The single biggest latency win; also makes eval timing realistic. *(Audit §1.3/§1.7)*

## Phase 3 — Retrieval-quality cluster (measure each with H02)

The whole point of building H02 first: each of these can now be proven to help.

- [ ] **H05** · P1 · M · retrieval — **No query transformation.** Lightweight normalization now (case/whitespace); pluggable `QueryTransformer` interface so HyDE/multi-query land behind `internal/index` without Ollama coupling. *(Audit §1.4)*
- [ ] **H10** · P1 · M/S · chunking — **No boundary-aware chunking + doc comment lies about it.** Implement the documented paragraph→sentence→word cascade (M) OR correct the misleading package doc (S). Decide which. *(Audit §1.1)*
- [ ] **H14** · P1 · M · retrieval/storage — **No metadata filtering at retrieval.** Optional `Filter` (source/type/tags) in `Search` — pre-FTS filter plus post-filter on vector hits. *(Audit §1.3/§1.4)*
- [ ] **H15** · P1 · M · retrieval/chunking — **No parent-child / context expansion (plumbing exists, unused).** `ContextWindow` option that fetches sibling chunks via the existing `PreviousChunkID`/`NextChunkID`. *(Audit §1.4)*

## Phase 4 — Perf + robustness

- [ ] **H06** · P1 · M · latency/embed — **No caching (query-result + query-embedding).** LRU keyed on `(query,mode,k,gen)` for results and `(model,query)` for embeddings; flush on `Migrate`. *(Audit §1.7/§1.8)*
- [ ] **H11** · P1 · M · embeddings/ops — **No embedding drift monitoring / version-pinning.** Persist `{model,dim,ollama-version}` corpus-metadata key; on startup compare to live config and refuse query / force reindex on mismatch. *(Audit §1.2)*
- [ ] **H16** · P1 · M · storage — **No persistent index snapshot (cold-start full rebuild).** Persist an FTS postings snapshot under a new prefix (0x06); load-on-start + incremental watcher updates. *(Audit §1.3)*

## Phase 5 — Security/ops hardening

- [ ] **H04** · P0 · M · security — **Indirect prompt-injection / retrieval poisoning, zero defense.** Pre-index `PoisoningDetector`-style pass (repetition / keyword-stuffing / instruction-phrase scoring); flag/quarantine; document the threat. *(Audit §1.8)*
- [ ] **H17** · P2 · M · ops — **No observability/metrics/tracing.** OTel spans around `Engine.Query`/`Ingest`/`Migrate`; expose `/metrics` on loopback; `status --metrics`. *(Audit §1.8)*
- [ ] **H18** · P2 · S · security — **No audit log.** Structured append-only JSONL of query + ingest + auth-fail events; hash query text. *(Audit §1.8)*
- [ ] **H19** · P2 · S · security — **No PII/secret scanning at ingest.** Optional regex secret/PII scanner in `internal/reader` with `--redact`. *(Audit §1.8)*

## Phase 6 — Polish

- [ ] **H20** · P2 · M · data — **Doc-level dedup only (no near-duplicate).** SimHash/shingle-Jaccard near-dup flagging at ingest (brute-force fine at local <10K scale). *(Audit §1.1)*
- [ ] **H21** · P2 · S · gen-boundary — **Score not calibrated + citation contract under-documented.** Normalize scores to [0,1] within a result set; document `chunk_id` as the canonical citation anchor; add a `chunk_index` ordinal within a document. *(Audit §1.5)*
- [ ] **H22** · P2 · S · retrieval — **No adaptive retrieval depth / pool-size tuning.** Optional `QueryClassifier` returning recommended `k`/`Mode` (cheap rule-based first); configurable reranker pool. *(Audit §1.4)*
- [ ] **H23** · P2 · M · chunking — **Markdown structure destroyed before chunking; no chunk-metadata.** Thread the current heading into per-chunk `Metadata` during chunking; populate `section_context` from the reader's extracted headings. *(Audit §1.1)*
- [ ] **H24** · P2 · S · ops — **`migrate` has no dry-run / cost estimate.** `migrate --dry-run` → doc-count + model delta before re-embedding. *(Audit §1.8)*
- [ ] **H25** · P2 · M · latency — **No streaming / no request batching at embed-rerank.** gRPC server-streaming of ranked hits; ≤50ms dynamic batcher in front of embed/rerank. *(Audit §1.7)*

## Phase 7 — Minor / future / scope-adjacent

- [ ] **H26** · P3 · S · chunking — **Token estimate breaks on CJK.** Rune-based token estimate (CJK has no spaces → 0 "words") OR document the failure. *(Audit §1.1)*
- [ ] **H27** · P3 · M · storage — **Brute-force `*Vector` has no `Index` interface (no HNSW escape hatch).** Extract an `Index` interface (`Add/Delete/Query`) before scaling pressure hits. *(Audit §1.3)*
- [ ] **H28** · P3 · S · gen-boundary — **No explicit retrieval-only contract marker in API.** One-line doc comment on the `Query` RPC; optional `context_window_hint` field. *(Audit §1.5)*

---

## Deliberately out of scope (do NOT implement — audit §4)

LLM generation/answer synthesis · RBAC/multi-tenant isolation (until a shared multi-user MCP server exists, then **P0**) · conversational state/memory/query reformulation (agent-layer) · TLS (N/A on loopback; flips to **P0** if anyone binds non-loopback — see H13) · RAFT/fine-tuning · GraphRAG · agentic RAG · SPLADE · hosted rerankers · horizontal scaling/k8s.

---

*When starting any item: create `specs/NNN-<short-name>/` via `/speckit-specify`,
then run `/speckit-plan` → `/speckit-tasks` → `/speckit-implement`. Use H02 as the
measurement harness to prove each retrieval change helps.*
