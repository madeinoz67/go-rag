# go-rag vs RAG Book — Full Findings & Recommendations

> **Adversarial gap audit** of the go-rag implementation against *Building RAG Systems That Actually Work* (`rag-book-md/`, 17 chapters).
>
> **Method:** RedTeam ParallelAnalysis — 8 domain-adversary agents (data/chunking, embeddings, storage/indexing, retrieval/reranking, generation-boundary, evaluation, hybrid/local/latency, security/ops). Each agent read its book chapter(s) and verified go-rag code via tokensave, then produced independent gaps/strengths/scope-decisions. This document is the complete, unabridged synthesis.
>
> **Date:** 2026-06-21 · **SUT:** go-rag (single-binary, local-first, pure-Go, Pebble KV, retrieval-only RAG database; PRD §2.2 excludes cloud, multi-user/RBAC, LLM generation, audio/video, web UI, plugins, non-Ollama embedders)

## Contents

1. [Full Domain Findings](#1-full-domain-findings) — every gap, strength, scope-decision per domain
2. [Consolidated Prioritized Hitlist](#2-consolidated-prioritized-hitlist) — P0→P3, actionable
3. [What go-rag Does Better Than the Book](#3-what-go-rag-does-better-than-the-book)
4. [Scope Decisions — Intentional Exclusions (do NOT fix)](#4-scope-decisions--intentional-exclusions-do-not-fix)
5. [Recommendations & Remediation Order](#5-recommendations--remediation-order)
6. [Verdict](#6-verdict)

**Priority key:** P0 = silent killer/blind-spot · P1 = real quality/correctness/latency · P2 = hardening/ops/polish · P3 = minor/future. **Effort:** S ≤1d · M 1–3d · L >3d.

---

## 1. Full Domain Findings

### 1.1 Data Curation & Chunking  ·  *ch003 (Data Curation) + ch004 (Chunking)*

**GAPS**
- **[P0] Chunker has zero boundary awareness — and its doc comment lies about it.** Book §3.2 (the chapter's headline failure mode): "fixed-size chunking without boundary awareness destroys meaning." go-rag `chunk.Split` (`internal/chunk/chunk.go`) is a pure word-window (`step = perChunk − overlapWords`) — no sentence/paragraph/structure detection. The package doc *claims* "a paragraph→sentence→word cascade with a ~50-token minimum" — **that cascade does not exist**. Fix: implement the documented cascade (greedy-fill sentences to Size, flush at paragraph breaks) OR correct the misleading doc. (Doc fix = S; cascade = M.)
- **[P1] Document-level dedup only; no near-duplicate or chunk-level dedup.** Book §2.3: exact-hash dedup is "the tip of the iceberg"; near-dup (MinHash/SimHash) needed or retrieval is "crowded out." go-rag dedups only on exact `ContentHash(raw)`. Fix: SimHash/shingle-Jaccard near-dup flagging at ingest (brute-force is fine at local <10K scale).
- **[P1] No chunk-metadata enrichment** (hypothetical questions, summary, section context). Book §3.6: enrichment "improves retrieval recall by 15–25%." `Chunk` carries position/index/page only; `Metadata` exists only on `Document`. Fix: per-chunk `Metadata` + populate `section_context` from the markdown reader's already-extracted headings (no LLM required).
- **[P2] Markdown structure destroyed before chunking.** `stripMarkdown` flattens headings/lists/emphasis to plain text *before* the splitter sees it; headings are captured into `metadata["headings"]` as a flat list but never re-attached to the chunk they govern. Fix: thread the current heading into per-chunk metadata during chunking.
- **[P2] No freshness/staleness signal despite capturing timestamps.** Book §2.5: stale docs "actively mislead." `Document.IngestedAt`/`UpdatedAt` exist but no staleness flag, no downranking, no temporal query intent. Fix: expose `updated_at` as a retrievable filter + simple recency boost.
- **[P3] Token estimate is a 1.3×-words heuristic, not tokens.** `EstimateTokens = ceil(words × 1.3)`. CJK (no spaces) collapses to ~0 "words," mis-sizing chunks. Fix: rune-based estimate or document the CJK failure.

**SCOPE DECISIONS (intentional exclusions)**
- No OCR / multi-modal chunking (§3.8): image readers extract dimensions only, flagged `"ocr":"deferred"`. Correct for v1.
- No LLM-driven agentic/proposition/late chunking (§3.3–3.5): PRD excludes LLM generation. Reasonable.
- No multilingual routing (§2.6): local-first, Ollama-only; a model-choice concern, not preprocessing.

**STRENGTHS**
- **Idempotent SHA-256 dedup split into two hashes** — `ContentHash(raw bytes)` for change-detection vs `GenerateID(content+mime+metadata)` for identity. Re-embedding under a new model does **not** duplicate documents. Better than most reference impls.
- **Chunk linked list** (`PreviousChunkID`/`NextChunkID`) — directly enables parent-child / neighbor-context retrieval (§3.7) without storing duplicate parents.
- **Per-chunk `PageNumber` propagation** — satisfies §"extract text position" for PDFs; citation granularity many systems lack.
- **Sync store / async embed split** — `processFile` returns <10ms, workers do embed+index. Matches the book's pipeline architecture.
- **MinTokens tail-merge** — handles the "tiny fragment" failure mode even within the naive splitter.

---

### 1.2 Embeddings  ·  *ch005 (Embedding Models)*

**GAPS**
- **[P0] Query embedding dimension never validated against stored vectors.** Book §4.6: "a library update silently changed pooling behavior once, dropping retrieval accuracy 15%"; dimensionality is a core invariant (§4.1). go-rag `Vector.Query` runs `cosine(vec, cv)` with **no length check**; changing `EmbeddingModel` mid-corpus → query dim-N vs stored dim-M → garbage scores or index-panic, **no error**. `embed.Ollama.setDims` never cross-checks against the loaded index. Fix: length-check in `Vector.Query` (skip+log on mismatch); persist `{model,dim}` per chunk (already stored as `storedEmbedding.Model`) and reject query embeddings whose model/dim differ from the corpus majority.
- **[P1] No instruction-prefix / query-passage asymmetric encoding.** Book §4.2: E5 requires `"query:"`/`"passage:"`, BGE uses instruction prefixes; **default model nomic-embed-text documents asymmetric `search_query:`/`search_document:` prefixes**. go-rag embeds query and chunks identically, unprefixed. Fix: add a `Role` param to `Embed` (or wrap the embed func) with `search_query:` (retrieval) / `search_document:` (pipeline) when model is nomic (config-gated).
- **[P1] Zero drift monitoring or version-pinning.** Book §4.6: build drift monitoring "from day one." go-rag: no baseline, no drift score, no version record beyond the `Model` string. `Migrate` reprocesses but never *detects* that a model swap silently corrupted the index. Fix: persist `{model,dim,ollama-version}` corpus-metadata key; on startup compare to live config and refuse query / force reindex on mismatch.
- **[P1] Whole-document batch with no size cap.** `processJob` sends **all** chunks of a doc in one `Embed` call — a 2000-chunk PDF becomes one Ollama POST (client timeout 60s) → OOM/timeout. Fix: batch texts into ~32–64 inside `Ollama.Embed`, concatenate, per-batch retry.
- **[P2] No query-embedding cache.** Book §4.7: "query embedding caching… hit rates 30–50%." go-rag constructs a fresh `NewOllama` per query and re-embeds identical queries. Fix: LRU keyed on `(model, normalized query)`.
- **[P2] No quantitative model-selection / eval harness.** Book §4.7: "test 3–5 models on 50–100 labeled queries." Model is a static config string. (Out of scope for v1; the `Embedder` interface already makes swapping trivial — document as a known gap.)

**SCOPE DECISIONS**
- SPLADE / sparse-learned (§4.3): scope is Ollama dense + BM25 via RRF (hybrid present). Caveat: no learned sparse expansion, so "authentication" won't expand to "token/login" — BM25-only lexical side.
- Fine-tuning (§4.4): no training infra. Correct.
- MRL / ColBERT / ColPali / quantization (§4.5): defer (complexity).
- Multi-provider embeddings: PRD Ollama-only; interface leaves the door open.

**STRENGTHS**
- **Retry-with-backoff on the embed call** (3 attempts, exponential 50ms×n, 5xx-retry/4xx-fail-fast, respects ctx) — matches book's resilience guidance, exceeds typical hobby impls.
- **`Embedder` interface properly abstracted** — every consumer depends on the interface, enabling the test fakes the book's eval/swap guidance requires.
- **Response integrity check** — `len(embeddings) != len(texts)` guard catches partial/truncated batches (a drift-mode failure); returns explicit error rather than indexing misaligned vectors.
- **Thread-safe dim discovery** — `setDims` mutex-guarded, set-once from first response; concurrent workers share one `*Ollama` safely.
- **Hybrid retrieval via RRF** (§4.3 sparse+dense fusion) + **cross-encoder rerank hook** (§4.7 bi-then-cross-encoder) — both implemented.

---

### 1.3 Storage & Indexing  ·  *ch006 (Vector Databases & Storage)*

**GAPS**
- **[P0] Per-query full index rebuild.** Book: indexes are built once (HNSW incremental / batch IVF) and persisted; "streaming inserts, no rebuilds" is a named HNSW advantage. go-rag `pipeline.LoadIndex` does a full `PrefixScanByte(PrefixChunk)` + `PrefixScanByte(PrefixEmbedding)` rebuild, reconstructing BM25 FTS + the in-memory vector map from JSON on **every query** — O(N) disk reads + unmarshal + re-tokenization per request (strictly worse than the book's "numpy + for loop" baseline, which at least keeps vectors resident). Fix: cache the loaded `(FTS, *Vector)` on the Engine with a content-hash generation; invalidate on ingest/delete/watcher. `Vector.Save/Load` JSON hooks already exist but are unused.
- **[P0] No persistent index — recovery is a full re-scan.** Book §5.3: "always back up your index metadata separately." go-rag: BM25 postings + any graph live only in RAM and vanish on restart. The embedding→chunk mapping is reconstructable (dodges catastrophe), but every cold start pays the full rebuild. Fix: persist an FTS postings snapshot under a new prefix (0x06); load-on-start + incremental watcher updates.
- **[P1] Brute-force ceiling with no documented escape hatch.** Book: FLAT acceptable <100K vectors w/ memory (the scenario go-rag falls into). `Vector.Query` iterates the entire map with a Go cosine loop; the doc claims a "chromem-go (HNSW) backend that can be swapped in later" but **there is no interface** — `*Vector` is a concrete struct used directly. Fix: extract an `Index` interface (`Add/Delete/Query`) before scaling pressure hits.
- **[P1] Hybrid fusion uses per-list RRF k, not standard single-k RRF.** Book §6.6: RRF uses one constant k (≈60): `1/(k+rank)`. go-rag uses `kVec=40`,`kFTS=60` as separate dampeners (`1/(kVec+rank+1)`); the off-by-one vs `1/(k+rank)` is defensible, but asymmetric K silently up-weights the FTS list. Fix: document the asymmetry as intentional or collapse to single K=60.
- **[P2] No metadata filtering at the vector layer.** `Vector.Query` takes only `(vec, k)`; the only "filter" is post-fusion `collapseByDoc`. Fix: add an optional predicate to `Query`.
- **[P3] Pebble treated as opaque KV, not a search engine.** Book: backends co-locate vectors + indexes. go-rag uses Pebble only as a serialization layer (vectors live in a Go map). Acceptable for v1; flag for v2.

**SCOPE DECISIONS**
- No ANN (HNSW/IVF/PQ): correct for <100K local vaults per the book's own table. **Caveat:** the threshold is *vectors* (chunks), not documents — chunked corpora hit 100K fast. No telemetry/guardrail warns when crossed.
- No multi-tenancy: local single-user. **Caveat:** a "vault" abstraction exists but storage has a single keyspace with no tenant byte — isolation is process-level, not key-level.
- No sharding/replication: legitimately out of scope.

**STRENGTHS**
- **Pebble + local-first is a legitimate strength, not an apology.** The book's cost analysis concludes self-hosted wins at scale and "you might not need a vector database at all" under 100K. go-rag's pure-Go, CGO-free, embedded Pebble (LSM, WAL, `pebble.Sync` durability) is the book's "numpy array" tier hardened with a real storage engine — crash-safe, zero-ops, single binary. Correct architecture for the target.
- **Single-writer lock via Pebble file lock** — addresses the book's concurrency caveat (races/memory corruption from naive parallelism) at the storage layer; stronger than the numpy-baseline warning.
- **RRF hybrid + reranker hook + per-document collapse** — §5.4 hybrid arch met, not deferred; `collapseByDoc` is a recall/precision feature the book doesn't even cover.
- **Idempotent SHA-256 content addressing** — vectors regenerable from source, so the absent persistent index is a latency cost, not a data-loss risk.

---

### 1.4 Retrieval & Reranking  ·  *ch007 (Retrieval Strategies) — the core quality domain*

**GAPS**
- **[P0] Reranker-error fallback returns possibly-wrong results, unlogged.** Book §6.6: rerank failure should trigger fallback retrieval or flag for review. go-rag: on `err != nil OR len(scores) != len(hits)`, silently returns `hits[:k]` in RRF order with the error discarded. Fix: log the error, surface a `RerankFailed` flag, optionally re-run with a larger pool; never swallow.
- **[P0] No query transformation whatsoever.** Book §6.1–6.2 (acronym expansion, normalization, HyDE, multi-query, conversational rewriting, decomposition) is a baseline quality lever — the chapter's opening war story (40%→85%) is exactly a synonym-expansion win. go-rag passes the raw query into both FTS and vector. Fix: at minimum lightweight normalization (case/whitespace); add a pluggable `QueryTransformer` interface so HyDE/multi-query land behind `internal/index` without Ollama coupling.
- **[P1] RRF weight constants hardcoded and un-tunable.** Book §6.6: fusion weights tunable per corpus. go-rag `kVec=40`,`kFTS=60` constants; the asymmetric k implicitly weights FTS higher, opaquely. Fix: move to config (or derive from Mode), expose via flag, document.
- **[P1] No metadata filtering pre- or post-retrieval.** Book §6.1/§6.2. `Search` has no filter param. Fix: optional `Filter` (source/type/tags) inside FTS + post-filter on vector hits.
- **[P1] No parent-child / context expansion.** Book §6.4 (full section): small chunks retrieve well but lack context — return parent/sentence-window. go-rag returns raw chunk text; **but chunks already store `PreviousChunkID`/`NextChunkID`** — the plumbing exists, unused. Fix: `ContextWindow` option that fetches sibling chunks around the hit.
- **[P2] No adaptive retrieval depth.** Book §6.5: query-type-aware k (factoid k=3, comparative k=10) → 40% latency win. Fix: optional `QueryClassifier` returning recommended `k`/`Mode` (cheap rule-based first).
- **[P2] No reranker pool-size / candidate-count tuning.** Book §6.3. `poolSize=60` constant; no way to grow for low-recall or shrink for latency. Fix: configurable pool + log utilization.
- **[P3] No ensemble/multi-route or GraphRAG.** (Scope — see below.)

**SCOPE DECISIONS**
- GraphRAG (§6.6): correctly out of scope (no LLM, local-only). Book author calls it "often applied prematurely." Caveat: even lite "entity boosting" is absent if multi-hop queries appear.
- FLARE / Self-RAG / iterative retrieval: require an LLM in the loop; PRD excludes. **Caveat:** rule-level conversational query rewriting is *not* LLM-dependent and is still missing (P0 above).
- LLM-as-reranker / Cohere / hosted rerankers: out of scope (Ollama-only). Cross-encoder via Ollama is the right local substitute.
- Multi-stage cross-encoder→LLM rerank: excluded by no-LLM constraint.

**STRENGTHS**
- **Hybrid RRF is the book's recommended baseline** — §6.6: "Start with hybrid (BM25 + vectors) + cross-encoder re-ranking." go-rag ships exactly this as default `ModeHybrid`.
- **Cross-encoder rerank via Ollama is architecturally sound** — the `Reranker` interface keeps `internal/index` Ollama-free; `internal/rerank` is the sole adapter. Clean dependency inversion, matches the book's "extension by interface."
- **Rerank-then-truncate ordering is correct** — retrieves `pool`, reranks the full pool, *then* takes top-K. Many impls rerank only top-K of RRF and lose recall; go-rag avoids this.
- **`collapseByDoc` (top-1 per document)** — sensible dedup post-processing (§6.1); improves diversity for multi-doc corpora.
- **Three explicit modes (hybrid/semantic/keyword)** — manual query-type routing the book advocates in §6.2; the seam exists even if not automatic.

---

### 1.5 Generation Boundary & Conversational  ·  *ch008 (Generation) + ch009 (Conversational)*

**GAPS** (note: go-rag is retrieval-only by design — gaps here are *retrieval-layer affordances for grounded generation*, not "add an LLM")
- **[P1] Hit payload omits stable citation identifiers clients need for grounded attribution.** Book §7.3: "require the model to cite sources… verify those citations"; context formatting should "include source metadata." go-rag returns `chunk_id` (opaque hash), `document_id`, `file_path`, `page` — enough to *display* provenance but the citation contract is undocumented; multi-chunk citations ("Doc 3, ¶2") can't resolve deterministically. Fix: document `chunk_id` as the canonical citation anchor (it's already content-addressed); optionally add `chunk_index` ordinal within a document.
- **[P2] Raw fused score not calibrated for grounding decisions.** Book §8.2/§7.3: ambiguity detection and confidence-based escalation treat score as a confidence proxy. go-rag's RRF-fused score is dimensionless and uncomparable across queries/modes — a client can't set a `threshold` or judge "is hit #1 actually relevant" from the number. Fix: normalize scores to [0,1] within a result set; document that `threshold` is relative-within-result, not absolute-confidence.
- **[P2] No per-hit retrieval provenance for hybrid mode.** Book §7.3: citation verification needs to know *which* source supported a claim. go-rag collapses to top-1 per doc and discards which leg (keyword vs semantic) matched. Fix: optionally emit `match_mode` (keyword/semantic/both) per hit.
- **[P3] No explicit "retrieval-only / no generation" contract marker in the API.** Fix: one-line doc comment on the `Query` RPC; optional `context_window_hint` field.

**SCOPE DECISIONS (correct exclusions — with caveats)**
- **No LLM generation / answer synthesis** — correct. Book treats retrieval DB and generation as separable (ch008 assumes an external LLM consuming retrieved context). PRD §2.2 is architecturally sound. Caveat: faithfulness/hallucination scoring *cannot* live in a retrieval-only DB — correctly deferred to the consumer. **Document this boundary.**
- **No conversational state / memory / query reformulation** — correct v1. §8.1–8.5 are agent-layer concerns requiring an LLM. A stateless retrieval DB is the right primitive. Caveat: go-rag could cheaply expose a `context_window_hint` and per-query embedding endpoint so clients implement §8.3 vector memory on top.
- **No multi-LLM routing / cascading / caching (§7.1/§7.4/§7.6)** — correct; all require generation.

**STRENGTHS**
- **Clean retrieval/generation separation** — directly matches the book's implicit architecture (ch008 treats retrieval as input to generation, never conflated). `QueryHit` carries `content + file_path + page` so any client can format context with the headers/separators §7.2 prescribes.
- **`chunk_id` is content-addressed (SHA-256)** — gives clients a stable, idempotent citation handle out of the box; `[Source: X]` verification maps straight to `chunk_id`.
- **Document-level collapse (`collapseByDoc`)** — anticipates §8.4's dedup-by-doc-id pattern; prevents one doc flooding top-k.
- **Cross-transport parity** (REST DTO drops engine-only `Preview` to match proto 1:1) — `QueryHit` is the single source of truth, so attribution metadata is consistent regardless of consumer.

---

### 1.6 Evaluation & QA  ·  *ch010 (Evaluation) + ch017 App. C — typically the weakest area*

**GAPS**
- **[P0] No retrieval-quality metrics (recall@k, MRR, NDCG, precision@k).** Book ch010/App.C: Precision@5 > 0.70, Recall@10 > 0.80, MRR > 0.60, NDCG@10 > 0.75; "evaluation isn't optional — it's how you know whether your changes help or hurt." go-rag: nothing — tests are mechanical (ordering, collapse, mode-selection), not quality. Every chunking/embedding/weight/reranker change ships blind. Fix: `internal/eval/metrics.go` implementing recall@{5,10}, precision@5, MRR, NDCG@10 over `[]Hit` vs `map[chunkID]relevance` (pure Go; formulas in App.C). **The single biggest quality gap.**
- **[P0] No golden/evaluation dataset or harness.** Book §9.1: "start with 50–100 expert-annotated queries"; §9.6: "your evaluation dataset is code — store in git, tag versions." go-rag: no `testdata/eval/`, no Q→relevant-chunk pairs, no harness CLI. Fix: (1) commit `testdata/golden/v1.jsonl` (30–50 hand-labeled pairs from the repo's own test docs); (2) `go-rag eval --golden ...` printing recall/MRR/NDCG; (3) `make test-eval` CI gate. Under 200 LOC for the MVP.
- **[P1] No regression gate on retrieval changes.** Book §9.6 "the regression that almost shipped" — new embedding model broke chunk boundaries, 78%→45%, caught only by continuous eval. Fix: run `eval` in CI on every change touching `internal/chunk`/`internal/index`/`internal/rerank`/hybrid weights; fail PR on >5pt recall@10 regression vs main.
- **[P1] No synthetic query generator for eval-set growth.** Book §9.1. Fix: `go-rag eval-gen --corpus <dir> --n 100` reusing the existing Ollama embedder/generator to emit candidate Q→chunkID pairs for human triage.
- **[P2] No latency/quality monitoring surface.** Book App.C: p50 < 1s, p99 < 3s, error < 1%; §9.5 production monitor. go-rag: `bench_test.go` measures ns/op in isolation; no per-query timing from the engine. Fix: `Engine.Query` records `latency_ms + mode + k` to a Pebble-prefixed metrics key; expose via `go-rag status --metrics`.
- **[P3] RAGAS / faithfulness / hallucination scoring (§9.2–9.4).** Correctly N/A — go-rag doesn't generate. Flag only because a reviewer will ask.

**SCOPE DECISIONS**
- Generation-side metrics (RAGAS faithfulness, answer relevance, hallucination) are **out of scope** — go-rag doesn't generate; the only meaningful eval surface is **retrieval quality**. Concentrate all effort on recall/MRR/NDCG.
- A/B testing (§9.5) and continuous production sampling: deferred — no multi-user serving layer.
- Standard benchmarks (MS MARCO, NQ, BEIR): P3 deferred — poor fit for a local personal corpus; the custom golden set is the right first step.

**STRENGTHS**
- **Test discipline on mechanics is solid** — parity, concurrency, reprocess, mode-selection, same-document collapse are well-covered; only *quality* invariants are absent.
- **Deterministic embeddings in tests** (`staticEmbed`) — the `EmbedFunc` interface is already injectable, so a golden-dataset harness can pin embeddings for reproducibility without network calls (exactly what an offline recall@k harness needs).
- **Existing interfaces ease the fix** — `Reranker` and `Embedder` are interface-backed, so `internal/eval` can construct a real retrieval pipeline against a golden set with no production coupling. The MVP harness is ~one package + one golden file + one subcommand, <500 LOC.

---

### 1.7 Hybrid/Advanced, Local/Air-gapped, Latency  ·  *ch011 — go-rag's strongest domain*

**GAPS**
- **[P0] Per-query full index rebuild** (dup of §1.3) — `Engine.Query` → `LoadIndex` on every call. **Single biggest latency win available.** Fix: cache `(FTS, *Vector)` with generation-counter invalidation.
- **[P1] No query/result cache.** Book §10.3: "response caching… hit rates 40–60% on technical docs." go-rag: zero caching — identical queries re-embed (Ollama round-trip) and re-retrieve. Fix: LRU keyed on `(query, mode, k, threshold, index-generation)` storing `[]QueryHit`; generation-invalidated. Exact-match cache covers the documented win without a second embedder.
- **[P1] Loopback not enforced on the bare `serve` path.** Book §10.2: air-gapped = no external surface. `config.Default()` sets `MCPAddr: ":7878"` (**all interfaces**); only `daemon.Start()` rewrites to `127.0.0.1:7878`. Running `go-rag serve` directly or loading a saved `config.json` binds MCP to `0.0.0.0`. Fix: make loopback the default in `config.Default()`; reject non-loopback at serve boot unless `--bind-external` is set.
- **[P2] No streaming of results.** Book §10.3. Full `QueryResult` returns after rerank completes; large-k/slow-reranker users wait on the tail. Fix: stream ranked hits over gRPC server-streaming.
- **[P2] No request batching at the embed/rerank boundary.** Book §10.3: "request batching… 3–5× throughput." Each query embeds alone; concurrent queries hit loopback Ollama serially. Fix: ≤50ms dynamic batcher in front of `embed.NewOllama`/`rerank.New`.
- **[P3] No speculative/prefetch of embeddings on watch.** Out of scope for v1.

**SCOPE DECISIONS**
- **RAFT/fine-tuning integration (§10.1): correctly out of scope.** Retrieval-only; RAFT is a generation-layer concern. No gap.
- **ColPali/ColBERT late-interaction (§10.2): reasonably deferred.** Would need a GPU local model + different index shape. Current BM25+vector+rerank covers §10.4 multi-stage adequately for v1.
- **Agentic RAG / MEGA-RAG / KG-RAG (§10.4): correctly out of scope.** Application-layer patterns that *consume* a retriever like go-rag, not features of the retriever.

**STRENGTHS** — *this is go-rag's strongest domain; arguably a cleaner reference impl of §10.2 than the book's own diagrams:*
- **Pure air-gapped by construction.** Single static `CGO_ENABLED=0` binary, Pebble on local disk, loopback Ollama, no telemetry egress, no cloud calls. The book lists "Embedding model / Vector DB / LLM / Document pipeline" as components that "must be local" — go-rag makes every one local by default, zero config. No Chroma/Milvus dependency.
- **Async-after-ACK writes done right (§10.3 pattern).** Lazy pipeline; ACK on durable Pebble store; embed/index on background workers; `Pipeline.Close()` = `close(queue); wg.Wait()`; `Engine.Close()` drains under `pipeMu` with correct LIFO ordering vs `db.Close()`. The part most implementations get wrong.
- **Multi-stage retrieval already implemented (§10.4).** `SearchWithRerank`: fast hybrid BM25+vector → optional cross-encoder rerank over `poolSize` candidates with graceful fallback. The exact 10×-candidate-then-rerank shape the book recommends.
- **Hybrid retrieval is the default, not an option.** BM25 + dense + RRF — the book's "no single retrieval method is best" thesis is the system's baseline.
- **Multi-transport over one engine.** REST/gRPC/MCP from one `Engine` with cross-transport parity tests — the book's "API Layer" done as thin fan-out over identical retrieval, eliminating a class of drift bugs.
- **Single-instance enforcement.** PID guard + Pebble `LOCK` detection prevents "two writers corrupt the KV."

---

### 1.8 Security, RBAC & Scaling/Ops  ·  *ch012 (Security) + ch013 (Scaling/Ops)*

**GAPS**
- **[P0] Indirect prompt-injection via ingested docs.** Book §11.3: "treat all user input as untrusted… retrieval poisoning detection… input sanitization." go-rag: zero defense at ingest or retrieval — `scan`/`reprocess`/`add` index any text, `Query` returns chunks verbatim to the client (which feeds an LLM). A malicious `.md` ("Ignore previous instructions…") becomes a retrieved chunk. Fix: `PoisoningDetector`-style pre-index pass (repetition/keyword-stuffing/instruction-phrase scoring); flag/quarantine; document the threat.
- **[P0 if non-loopback; P2 as shipped] No TLS / no bind enforcement.** Book §11.5 mTLS checklist. go-rag: plaintext HTTP on `127.0.0.1:7878` default, but `Start` accepts arbitrary `--rest-addr`/`--grpc-addr` with no TLS wrap. Fix: hard-error if bind is non-loopback without TLS, or document loudly. (See §1.7 loopback-default gap.)
- **[P1] No PII handling at ingest.** Book §11.2: detect/redact PII before indexing. go-rag: raw text lands in Pebble + vectors. Indexed secrets (a `.env` swept into a watched dir, API keys in notes) become retrievable verbatim. Fix: optional regex secret/PII scanner in `internal/reader` with `--redact`.
- **[P1] No audit log.** Book §11.4/§11.5: log every retrieval (user, query hash, doc IDs), auth events, retention. go-rag: only stderr + `go-rag.log`. Fix: structured append-only JSONL of query + ingest + auth-fail; hash query text.
- **[P1] No observability / distributed tracing.** Book §12.4: OTel spans on embed→retrieve→generate, p50/p99, retrieval-precision, error-rate alerts. go-rag: no metrics/traces/dashboard beyond `status`. Fix: emit OTel spans around `Engine.Query`/`Ingest`/`Migrate`; expose `/metrics` on loopback.
- **[P1] No query/result caching (dup of §1.7).** Book §12.3: multi-layer cache cuts cost/latency 60–80%, with **cache-invalidation on embedding-model change**. go-rag: every query re-embeds/re-searches; `Migrate` invalidates nothing. Fix: L1 in-process exact-match cache keyed by `hash(query)+embedding_model_version`; flush on `Migrate`.
- **[P2] No rate limiting.** Book §11.3. Low severity for loopback single-user, but a misbehaving local client can hammer Ollama. Fix: token-bucket on the gRPC/REST interceptors.
- **[P2] No cost tracking / reprocessing budget.** Book §12.1: "reserve 1 month indexing budget for reprocessing." `Migrate` re-embeds everything with no estimate/dry-run. Fix: `migrate --dry-run` → doc-count + model delta.

**SCOPE DECISIONS (deliberate, defensible)**
- **RBAC / per-user ACL / multi-tenant isolation (§11.1/§11.4):** out of scope — single-user local tool; "vault" dir is the de-facto tenant boundary. **Caveat:** if go-rag ever backs a shared MCP server for multiple agents/users, RBAC becomes **P0**; vault is a namespace, not a security boundary.
- **SSO/SAML/OIDC, MFA, zero-trust segmentation (§11.5):** out of scope for loopback; revisit if bound non-loopback.
- **Output moderation / watermarking (§11.6):** legitimately N/A — retrieval-only, no generation.
- **Horizontal/auto-scaling, load balancing, k8s (§12.2/§12.6):** out of scope — single static binary, single-instance guarded by PID + Pebble lock. Correct call.

**STRENGTHS**
- **Embedding-model versioning/migration (§12.5) — genuine match.** `Engine.Migrate` calls `EmbeddingModelStats`, counts chunks whose model ≠ current, and re-embeds only when stale > 0. Model name is the version key — same pattern as the book's `EmbeddingVersionManager`. Missing the dual-write + validation gate before cutover, but atomic re-embed is acceptable for single-user local.
- **Single-instance enforcement** — `Start` checks PID-alive, clears stale PID, then double-guards with `isPebbleLockHeld` (fcntl). Two-layer guard, stronger than the book implies.
- **Crash recovery** — Pebble WAL gives durability without go-rag writing its own.
- **Loopback default + fail-closed auth** — `127.0.0.1:7878` default; `bearerInterceptor` returns `Unauthenticated` when a token is set and missing. (Empty-token dev mode is the one footgun.)

---

## 2. Consolidated Prioritized Hitlist

### P0 — silent killers / blind-spots (fix first)

| ID | Gap | Fix | Effort | Domain |
|----|-----|-----|--------|--------|
| **H01** | Per-query full index rebuild (`LoadIndex` every Query) | Cache `(FTS,Vector)` on Engine w/ generation invalidation | M | storage/latency |
| **H02** | No retrieval-quality eval (metrics + golden + `eval` cmd + CI gate) | `internal/eval` + `testdata/golden/v1.jsonl` + `go-rag eval` + `make test-eval` | M | eval |
| **H03** | Embedding dim/model mismatch unvalidated → silent corruption | Length-check in `Vector.Query`; persist+check `{model,dim}` | S | embeddings |
| **H04** | Indirect prompt-injection / retrieval poisoning, zero defense | Pre-index `PoisoningDetector`; flag/quarantine; document | M | security |

### P1 — retrieval-quality / correctness / latency

| ID | Gap | Fix | Effort | Domain |
|----|-----|-----|--------|--------|
| **H05** | No query transformation | Normalization now; pluggable `QueryTransformer` (HyDE/multi-query later) | M | retrieval |
| **H06** | No caching (query-result + query-embedding) | LRU keyed on `(query,mode,k,gen)` + `(model,query)`; flush on `Migrate` | M | latency/embed |
| **H07** | Missing embedding instruction-prefix (nomic/E5) | `Role` param; `search_query:`/`search_document:` (config-gated) | S | embeddings |
| **H08** | RRF weights hardcoded + asymmetric-k formula unreviewable | Move k to config; document asymmetry or unify k=60 | S | retrieval |
| **H09** | Reranker errors silently swallowed | Log + `RerankFailed` flag + optional retry | S | retrieval |
| **H10** | No boundary-aware chunking + doc comment lies about it | Implement cascade OR fix the misleading doc | M / S | chunking |
| **H11** | No embedding drift monitoring / version-pinning | Persist `{model,dim,ollama-version}`; startup mismatch check | M | embeddings/ops |
| **H12** | Whole-doc embed batch unbounded → OOM/timeout | Batch 32–64 inside `Ollama.Embed` | S | embeddings |
| **H13** | Loopback not enforced on bare `serve` (default binds `0.0.0.0`) | Loopback default in `config.Default()`; reject non-loopback w/o `--bind-external` | S | security |
| **H14** | No metadata filtering at retrieval | Optional `Filter` in `Search` (pre-FTS + post-vector) | M | retrieval/storage |
| **H15** | No parent-child / context expansion (plumbing exists, unused) | `ContextWindow` using `Previous/NextChunkID` | M | retrieval/chunking |
| **H16** | No persistent index snapshot (cold-start full rebuild) | Persist FTS postings snapshot (prefix 0x06); incremental watcher updates | M | storage |

### P2 — hardening / ops / polish

| ID | Gap | Fix | Effort | Domain |
|----|-----|-----|--------|--------|
| **H17** | No observability/metrics/tracing | OTel spans + `/metrics` + `status --metrics` | M | ops |
| **H18** | No audit log | Append-only JSONL (query/ingest/auth-fail) | S | security |
| **H19** | No PII/secret scanning at ingest | Regex scanner in `internal/reader` + `--redact` | S | security |
| **H20** | Doc-level dedup only (no near-duplicate) | SimHash/shingle-Jaccard at ingest | M | data |
| **H21** | Score not calibrated + citation contract under-documented | Normalize scores [0,1]; document `chunk_id` as citation anchor; add chunk ordinal | S | gen-boundary |
| **H22** | No adaptive retrieval depth / pool-size tuning | `QueryClassifier` → k/Mode; configurable pool | S | retrieval |
| **H23** | Markdown structure destroyed before chunking; no chunk-metadata | Thread heading into per-chunk `Metadata` | M | chunking |
| **H24** | `migrate` has no dry-run / cost estimate | `migrate --dry-run` → doc-count + model delta | S | ops |
| **H25** | No streaming / no request batching at embed-rerank | gRPC server-streaming; ≤50ms batcher | M | latency |

### P3 — minor / future / scope-adjacent

| ID | Gap | Fix | Effort | Domain |
|----|-----|-----|--------|--------|
| **H26** | Token estimate breaks on CJK | Rune-based estimate or document the failure | S | chunking |
| **H27** | Brute-force `*Vector` has no `Index` interface (no HNSW escape) | Extract `Index` interface before scaling pressure | M | storage |
| **H28** | No explicit retrieval-only contract marker in API | Doc comment on `Query` RPC; optional `context_window_hint` | S | gen-boundary |

---

## 3. What go-rag Does Better Than the Book

- **S1 — Local-first / air-gapped done right (§10.2).** Single static `CGO_ENABLED=0` binary, Pebble on local disk, loopback Ollama, zero telemetry egress — a cleaner reference implementation of the book's air-gapped ideal than the book's own diagrams. No external vector DB dependency.
- **S2 — Async-after-ACK with correct shutdown ordering (§10.3).** Lazy pipeline; writes ACK on durable Pebble store (<10ms); embed/index on background workers; `Engine.Close()` drains under a mutex with correct LIFO ordering vs `db.Close()`. The part most implementations get wrong.
- **S3 — Embedding-model migration genuinely implemented (§12.5).** `Engine.Migrate` counts stale-model chunks via `EmbeddingModelStats` and re-embeds only when needed — the book's `EmbeddingVersionManager` pattern, for real.
- **S4 — SHA-256 dual-hash dedup (better than most).** `ContentHash(raw bytes)` for change-detection vs `GenerateID(content+mime+metadata)` for identity — re-embedding under a new model does **not** duplicate documents.
- **S5 — Chunk linked-list + page numbers.** `PreviousChunkID`/`NextChunkID` + `PageNumber` enable parent-child context and citation granularity many systems lack.
- **S6 — Hybrid RRF + cross-encoder rerank = the book's recommended baseline, done right.** Rerank-then-truncate (reranks the full pool, then top-K). `Reranker` interface keeps `internal/index` Ollama-free.
- **S7 — Single-instance enforcement (PID + Pebble `fcntl` lock).** Two-layer guard against "two writers corrupt the KV."
- **S8 — Cross-transport parity.** REST/gRPC/MCP over one `Engine` with byte-identical-hit parity tests — eliminates a class of drift bugs.
- **S9 — Embed resilience.** Retry-with-backoff (3×, exponential, 5xx-only) + response-integrity check + thread-safe dim discovery.
- **S10 — Clean retrieval/generation separation.** Matches the book's separable-layers architecture; `QueryHit` carries `content+file_path+page` for grounded context.

---

## 4. Scope Decisions — Intentional Exclusions (do NOT fix)

- **LLM generation / answer synthesis (ch008)** — PRD §2.2; book treats retrieval & generation as separable. Correct. → faithfulness/hallucination scoring belongs to the consumer.
- **RBAC / multi-tenant isolation (§11.1/§11.4)** — single-user local; "vault" is a namespace, **not** a security boundary. → becomes **P0** if go-rag ever backs a shared multi-user MCP server.
- **Conversational state / memory / query reformulation (ch009)** — agent-layer, requires an LLM. Stateless retrieval DB is the right primitive.
- **TLS** — N/A on loopback; → flips to **P0** if anyone binds non-loopback (H13).
- **RAFT/fine-tuning, GraphRAG, agentic RAG, SPLADE, hosted rerankers, horizontal scaling/k8s** — out of scope by design (local, Ollama-only, single binary).

---

## 5. Recommendations & Remediation Order

**Principle:** each step must make the next measurable. Fix eval first so every subsequent retrieval change can be proven to help.

1. **H02 (eval harness)** — FIRST. Without it, H05/H07/H08/H10/H14/H15 cannot be validated. Unblocks safe iteration on everything else.
2. **H03 (embedding-dim validation) + H13 (loopback default) + H09 (reranker-error log)** — cheap (S) correctness/security quick-wins.
3. **H01 (index cache)** — the single biggest latency win; also makes eval timing realistic.
4. **Retrieval-quality cluster** (measure each with H02): H07 (prefix) → H05 (query transform) → H10 (boundary chunking) → H08 (RRF tunability) → H14 (metadata filter) → H15 (parent-child).
5. **H06 (cache), H11/H12 (embedding integrity/batching), H16 (persist index)** — perf + robustness.
6. **H04 (prompt-injection) + H17/H18/H19 (observability/audit/PII)** — security/ops hardening.
7. **P2/P3** as capacity allows.

---

## 6. Verdict

go-rag is **architecturally excellent for its local-first retrieval-DB scope** — arguably a reference implementation of the book's air-gapped ideal, with genuine strengths (migration, dual-hash dedup, async-ACK, cross-transport parity). The gaps are **not architectural**; they cluster in:

- **(a) silent failure modes** hidden by the absence of evaluation/observability (H01, H03, H09, H11);
- **(b) standard retrieval-quality levers** not yet wired (H05, H07, H08, H10, H14, H15);
- **(c) hardening/polish** (H16–H28).

**The #1 risk is not any single gap — it is that go-rag has no way to measure retrieval quality (H02), so several silent failure modes and every future tuning change fly blind.** Fix eval first.
