# Phase 0 — Research: Bounded Embedding Batches (H12)

> Resolves every design decision before Phase 1. Each item: Decision · Rationale ·
> Alternatives rejected. Grounded in code read this session:
> `internal/embed/ollama.go` (`Embed` single-POST + 3× backoff + `setDims` +
> `len(embeddings)!=len(texts)` guard), `internal/pipeline/workers.go:48`
> (`p.embed.Embed(ctx, docTexts)` — the unbounded call site; on error the pipeline
> sets `StatusError` and stores no vectors), `internal/embed/ollama_test.go`
> (httptest stand-in pattern).

## 1. Batch cap value: 32

**Decision**: Fixed internal constant `embedBatchSize = 32`.

**Rationale**: Within the audit's "~32–64" range. 32 is the conservative end —
smaller batches mean lower per-request memory and a shorter per-request timeout
budget (the existing 60s client timeout now covers ≤32 texts, not the whole
document), at the cost of more round-trips. For a local Ollama over loopback,
round-trip cost is negligible; memory/time safety is the whole point. 32 matches
common Ollama `/api/embed` guidance for modest-sized texts.

**Alternatives rejected**:
- *64* — fewer requests but larger per-request memory; less safety margin against
  the exact OOM/timeout failure H12 targets. Rejected as the default (could be
  revisited if benchmarks on real corpora favor it).
- *Configurable (`config embed_batch_size`)* — scope creep. H12 is an S-effort
  robustness fix; the audit frames the cap as an internal constant, not a user
  knob. Per-request tuning belongs to a separate item. Rejected for this spec;
  the constant is trivially revisitable later with no contract change.

## 2. Batching site: inside `Ollama.Embed`, not the pipeline

**Decision**: Split `texts` into batches inside `Ollama.Embed`
(`internal/embed/ollama.go`). The pipeline call site
(`internal/pipeline/workers.go:48`) is unchanged.

**Rationale**: The audit explicitly says "batch texts … inside `Ollama.Embed`."
Putting it in the transport layer (a) benefits **every** caller — the ingest
pipeline *and* the query path (`engine/query.go:44,149`) *and* any future
provider wrapper — not just ingest; (b) keeps the pipeline ignorant of transport
concerns (FR-009, contract preservation); (c) is the natural seam — `Embed`'s
contract is "vector per text, in order," and how it satisfies that is its own
affair.

**Alternatives rejected**:
- *Batch in the pipeline (`processJob`)* — would leave the query-path Embed calls
  unbounded (a query batch is tiny today, but the abstraction leaks), and couples
  transport batching to pipeline logic. Rejected.
- *A new batching wrapper type around `Embedder`* — adds a type + indirection for
  a single provider. Rejected; YAGNI until a second provider exists.

## 3. Per-batch retry: reuse the existing 3× backoff, applied independently per batch

**Decision**: Extract the current retry loop (3 attempts, `backoff(attempt)`
exponential, 5xx/network → retry, 4xx → fail fast, ctx-respecting) into a
per-batch helper. Each batch gets its own fresh 3-attempt budget.

**Rationale**: The existing retry is proven (audit §1.2 calls it out as a
strength). Applying it per batch isolates a transient blip to one batch (US2
acceptance 1) without inventing new resilience policy. A fresh budget per batch
is simpler to reason about than a shared budget and recovers each batch
independently.

**Alternatives rejected**:
- *Shared retry budget across all batches* — complex, couples batch failures,
  and a single bad batch could exhaust the budget for later healthy batches.
  Rejected.
- *No per-batch retry (one attempt per batch)* — loses the existing resilience
  the moment batching multiplies the number of requests. Rejected.

## 4. Failure semantics: any permanently-failed batch fails the whole call (no partial result)

**Decision**: If a batch fails after its 3 retries (or fails fast on 4xx), `Embed`
returns the error immediately and returns **no** vectors — not a partial set.

**Rationale**: The pipeline already treats an `Embed` error as document-failed
(`workers.go`: `status = StatusError`, and the vector-store loop is skipped), so
no partial vector set is ever committed (FR-006). Returning partial vectors would
violate that and create silent index gaps. All-or-nothing is both the existing
caller contract and the integrity-safe choice.

**Alternatives rejected**:
- *Return partial vectors + a "best-effort" error* — silent index corruption
  (some chunks embedded, others missing → skewed retrieval). Explicitly rejected
  by FR-006.
- *Skip the failed batch, embed the rest* — same corruption risk. Rejected.

## 5. Order preservation + per-batch integrity check

**Decision**: Process batches in input order and concatenate their result slices
in order, so `out[i]` is the vector for `texts[i]`. The existing
`len(response.Embeddings) != len(batchTexts)` guard runs **per batch**; a
mismatch on any batch fails the whole call (FR-005).

**Rationale**: The pipeline stores `vecs[i]` for chunk `i` (`workers.go`), so
input-order alignment is a hard contract (US3). The per-batch count guard
prevents a truncated/misaligned response from one batch silently shifting all
later vectors.

**Alternatives rejected**:
- *A single integrity check over the concatenated total only* — a short batch
  response would shift every subsequent vector silently until the total mismatch
  (which might still sum correctly by accident across uneven batches). Per-batch
  is strictly safer. Rejected the total-only check.

## 6. Dimensionality discovery: set-once, from the first batch

**Decision**: Keep `setDims` exactly as-is (mutex-guarded, set-once). The first
successful batch sets the dimensionality; subsequent batches' first embeddings
are no-ops on `setDims` (already set).

**Rationale**: Preserves the existing thread-safe dim-discovery invariant (audit
§1.2 strength). Batching changes nothing about it — the first batch is just the
first response instead of the only response.

## 7. Context cancellation between batches

**Decision**: Rely on the per-request ctx binding (each batch's request is built
with `http.NewRequestWithContext(ctx, …)`, as today). When ctx is cancelled, the
next batch's `client.Do` returns the ctx error immediately, failing the call
promptly. Additionally check `ctx.Err()` before starting each subsequent batch
for a prompt inter-batch exit (cheap, belt-and-suspenders).

**Rationale**: Honors FR-008 (cancelled ingest returns promptly) without adding
complexity. The explicit inter-batch check avoids issuing one extra request after
cancellation.

## 8. Sequential batches within a call (no intra-call parallelism)

**Decision**: Process batches sequentially within one `Embed` call.

**Rationale**: The pipeline already parallelizes **across** documents and
background workers; parallelizing batches **within** one call would spike local
Ollama load and add concurrency complexity for no benefit at local single-user
scale. Out of scope; the cap (not parallelism) is what bounds memory.
