# Feature Specification: Retrieval-Quality Evaluation Harness

**Feature Branch**: `004-retrieval-eval-harness`

**Created**: 2026-06-21

**Status**: Draft

**Input**: User description: "read the rag-book-audit and fix the top priority item" → the audit (`RAG_BOOK_AUDIT.md`) names its P0 item **H02 — "No retrieval-quality eval (metrics + golden + `eval` cmd + CI gate)"** as the single highest-priority fix (§5 Remediation Order lists it FIRST; §6 Verdict: *"The #1 risk is that go-rag has no way to measure retrieval quality (H02), so several silent failure modes and every future tuning change fly blind. Fix eval first."*).

> **Why this is the top priority.** go-rag has solid test coverage of *mechanics*
> (ordering, collapse, mode-selection, parity) but **zero coverage of retrieval
> *quality***. Every future change the rest of the audit recommends — query
> transformation (H05), embedding prefixes (H07), boundary-aware chunking (H10),
> RRF tunability (H08), metadata filtering (H14), parent-child context (H15) —
> **cannot be proven to help** without a way to measure recall/precision/MRR/NDCG.
> This spec closes that blind spot. It is the prerequisite that makes every
> subsequent retrieval change safe to ship. It also makes the **silent failure
> modes** flagged in §6(a) — embedding-dimension mismatch (H03), reranker error
> swallowing (H09), per-query index rebuild (H01) — *visible*, because their
> quality impact becomes measurable.
>
> **Scope note — what this spec covers and what it defers.** This is
> **retrieval-only evaluation**: recall@k, precision@k, MRR, NDCG@k. It
> deliberately excludes **generation-side metrics** (RAGAS faithfulness, answer
> relevance, hallucination) — go-rag does not generate (audit §1.6 scope
> decision, PRD §2.2), so those belong to the consuming LLM, not the retriever.
> It also defers hosted benchmarks (MS MARCO/BEIR) and A/B serving — a custom
> golden set drawn from go-rag's own test corpus is the right first step for a
> local personal database. The harness is built **on the interfaces that already
> exist** (`Embedder`, `Reranker`, the shared retrieval engine) so it exercises
> real retrieval with no production coupling — exactly the MVP the audit's §1.6
> strengths analysis says is feasible (under one package + one golden file + one
> subcommand).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Measure Retrieval Quality Against a Golden Set (Priority: P1) 🎯 MVP

An engineer or maintainer wants to know, in concrete numbers, how good go-rag's
retrieval actually is — and whether a change they are about to make will help or
hurt. They run a single command that loads a committed, human-labeled set of
queries (each paired with the chunk IDs judged relevant), runs each query through
the same retrieval engine that serves real clients, and prints standard
information-retrieval metrics: **recall@{5,10}, precision@5, MRR, NDCG@10**. The
moment this exists, the #1 risk in the audit — "every change ships blind" — is
closed. Even with no other audit fix applied, this story alone delivers value:
it establishes a **baseline** number the team can defend every future change
against.

**Why this priority**: Without measurement, none of the other P0/P1 retrieval
gaps can be validated. This is the foundational enabler. The book's ch010
position is explicit: *"evaluation isn't optional — it's how you know whether your
changes help or hurt."* An MVP that produces one honest baseline is independently
shippable and immediately useful.

**Independent Test**: Run the eval command against the committed golden file and
assert it exits 0 and prints all four metric families (recall@5, recall@10,
precision@5, MRR, NDCG@10) computed from the labeled pairs — with no network
calls required.

**Acceptance Scenarios**:

1. **Given** a committed golden evaluation dataset (queries + relevant chunk IDs)
   and a built corpus, **When** the maintainer runs the eval command pointing at
   it, **Then** it computes and prints recall@5, recall@10, precision@5, MRR, and
   NDCG@10 averaged across all golden queries.
2. **Given** the same golden dataset, **When** a relevant chunk does not appear
   in the retrieved top-k, **Then** that query contributes the correct "not
   found" penalty (rank treated as beyond the cutoff) rather than silently
   passing.
3. **Given** a query with no relevant chunks at all in the corpus, **When** the
   harness evaluates it, **Then** it is handled gracefully (reported, not
   crashed) and excluded from averages that would divide by zero.
4. **Given** the harness runs on a clean machine with no Ollama reachable,
   **When** deterministic/local embeddings are selected, **Then** evaluation
   completes reproducibly with identical metric values run-to-run.

---

### User Story 2 - Regression Gate: Block Changes That Hurt Retrieval (Priority: P2)

A contributor opens a pull request that touches chunking, embeddings, retrieval,
or reranking. Before it can merge, an automated gate runs the eval harness and
**fails the build if retrieval quality regressed** beyond an agreed tolerance
(e.g., recall@10 dropped more than a few points versus the recorded baseline).
This is the audit's H02/H-regression concern realized: *"the regression that
almost shipped" — a new embedding model broke chunk boundaries, recall dropped
78%→45%, caught only by continuous eval.* With this story, that regression is
caught by the build, not by a user.

**Why this priority**: Measurement (Story 1) only helps if someone remembers to
look. The gate makes measurement **structural** — it converts the baseline into a
living contract. It is the difference between "we can measure" and "we cannot
unknowingly regress."

**Independent Test**: On a branch, make a change known to degrade retrieval
(e.g., truncate retrieved candidates) and assert the eval gate fails; on the
unchanged baseline, assert the same gate passes.

**Acceptance Scenarios**:

1. **Given** a recorded baseline of metrics, **When** a change is introduced that
   lowers recall@10 beyond the configured tolerance, **Then** the eval gate fails
   with a clear message naming the regressed metric and the delta.
2. **Given** a change that does not affect retrieval (e.g., a doc-comment or CLI
   help edit), **When** the eval gate runs, **Then** it passes — no false alarms
   on unrelated changes.
3. **Given** the gate runs in CI, **When** it executes, **Then** it uses the same
   deterministic, offline configuration as Story 1 so results are reproducible
   across machines.

---

### User Story 3 - A Versioned, Growable Golden Dataset (Priority: P3)

The team wants the evaluation dataset itself to be a living artifact: committed
to git, versioned, and growable over time. Maintainers can add new labeled
queries as the corpus evolves, and (optionally) generate candidate query→chunk
pairs from the corpus for human triage rather than writing every query by hand.
The book's §9.6 directive is *"your evaluation dataset is code — store it in git,
tag versions."* This story makes the golden set that, ensuring the harness in
Stories 1–2 never runs against a stale or stale-sized dataset.

**Why this priority**: A frozen 30-pair set goes stale as the corpus and
retrieval logic evolve. Growth tooling keeps measurement trustworthy over time —
but it is not required to ship the first honest baseline, so it sits behind the
core measurement and gate.

**Independent Test**: Add a new labeled query to the golden dataset, re-run eval,
and confirm the dataset is picked up and reflected in the per-query breakdown
without code changes.

**Acceptance Scenarios**:

1. **Given** the golden dataset is a committed, plain-text file, **When** a
   maintainer adds a new query with its relevant chunk IDs, **Then** the next
   eval run includes it automatically and the dataset change is reviewable in the
   pull request diff.
2. **Given** an optional query-generation helper, **When** a maintainer runs it
   against a corpus directory, **Then** it emits candidate query→chunkID pairs for
   human triage (not auto-committed) — humans stay the source of truth for
   relevance labels.

---

### Edge Cases

- A golden query whose relevant chunk is missing from the top-k (must count as a
  miss at that cutoff, not silently pass).
- A golden query with **zero** relevant chunks in the corpus (must be reported
  and safely skipped, not crash a divide-by-zero average).
- **Ties** in retrieved ranking (metrics must define a deterministic tie-break so
  values are reproducible).
- **Empty or not-yet-embedded corpus** (eval must surface a clear error rather
  than report misleadingly high/low numbers).
- **Non-determinism** when a real Ollama reranker is in the loop — the offline
  default pins embeddings and treats reranking as optional so CI is stable.
- A relevant chunk ID in the golden set that no longer exists after a re-ingest
  (stale label) — reported, not silently dropped from the denominator without
  trace.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST compute standard retrieval-quality metrics —
  **recall@5, recall@10, precision@5, MRR, and NDCG@10** — over a set of ranked
  retrieved results compared against human-labeled relevance, averaged across the
  evaluation dataset.
- **FR-002**: The system MUST ship a committed, version-controlled **golden
  evaluation dataset** of queries paired with the chunk IDs judged relevant,
  drawn from go-rag's own test corpus so it is meaningful for a local personal
  database.
- **FR-003**: The system MUST expose evaluation through both a **CLI command** and
  an **MCP tool** (constitution Principle V: every operation is CLI + MCP), so a
  human or an AI agent can run it first-class.
- **FR-004**: The system MUST be able to run **offline and reproducibly** — using
  deterministic/local embeddings so a clean CI machine produces identical metric
  values run-to-run without requiring a live embedding/rerank service.
- **FR-005**: The system MUST emit a **machine-readable summary** (overall metrics
  + per-query breakdown + pass/fail against a configurable tolerance) suitable for
  use as an automated regression gate.
- **FR-006**: Evaluation MUST be **read-only** with respect to the user's live
  vault — it MUST NOT add, modify, or delete the user's documents or indexes; it
  evaluates retrieval against a corpus without side effects.
- **FR-007**: Evaluation MUST exercise the **same shared retrieval engine** that
  REST/gRPC/MCP use (spec 003 cross-transport parity), so measured quality
  reflects what real clients receive — not a parallel retrieval implementation.
- **FR-008**: The harness MUST treat a relevant item absent from the cutoff as a
  miss, and MUST report (not crash on) golden queries with zero relevant items or
  stale labels.
- **FR-009**: A **regression gate** MUST be available (e.g., a make target run in
  CI) that fails when a monitored metric drops beyond a configurable tolerance
  versus the recorded baseline.

### Key Entities *(include if feature involves data)*

- **Golden Query**: a natural-language search query together with the set of chunk
  IDs a human judged relevant for it. The unit of the evaluation dataset.
- **Relevance Judgment**: the mapping from a chunk ID to its relevance for a given
  query. Binary by default (relevant / not relevant); graded relevance is
  supported where the metric (NDCG) benefits from it.
- **Retrieved Ranking**: the ordered list of results the engine returns for a
  query — go-rag's existing `QueryHit` (carrying `ChunkID`, `DocumentID`, `Score`,
  `FilePath`, `Page`). Because `ChunkID` is content-addressed (SHA-256, Principle
  II), it is the stable join key between a Golden Query's labels and a Retrieved
  Ranking.
- **Evaluation Run**: one pass over the whole golden dataset producing the metric
  families above plus a per-query breakdown and an overall pass/fail verdict
  against the configured tolerances.
- **Baseline**: a recorded snapshot of an Evaluation Run's headline metrics,
  stored in version control, against which future runs are compared by the
  regression gate.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After any change to chunking, embedding, retrieval, or reranking, a
  maintainer can run a single command and see numeric before/after retrieval
  quality within seconds — no manual scoring required.
- **SC-002**: A deliberately introduced retrieval regression is **detected and
  fails the gate**, while a change unrelated to retrieval **passes** — the gate
  produces no false alarms on doc/help edits.
- **SC-003**: go-rag ships with a **published baseline** (recall@10, MRR,
  NDCG@10) for its own test corpus, giving every future change a concrete number
  to defend against.
- **SC-004**: Evaluation runs **with no external network calls** by default, so it
  is reproducible on a clean CI machine and consistent with the air-gapped,
  local-first thesis (constitution Principle I).
- **SC-005**: The evaluation harness and golden dataset are small enough to run
  comfortably inside the existing test/CI loop (well under a minute at MVP scale)
  and keep `make build`, `make vet`, and `make test` green.
- **SC-006**: A maintainer can grow the golden dataset (add labeled queries) and,
  optionally, generate candidate pairs for triage — without writing code — so
  measurement stays trustworthy as the corpus evolves.

## Assumptions

- **Retrieval-only metrics.** This harness measures retrieval quality
  (recall/precision/MRR/NDCG). Generation-side metrics (RAGAS faithfulness,
  answer relevance, hallucination) are **out of scope** — go-rag does not
  generate (audit §1.6 scope decision; PRD §2.2); those metrics belong to the
  consuming LLM.
- **MVP golden set size.** ~30–50 hand-labeled query→relevant-chunk pairs drawn
  from go-rag's own test documents — enough for a meaningful baseline,
  expandable over time. (Industry default; the audit cites the book's
  "50–100 expert-annotated queries" as the target, with 30–50 as a viable MVP.)
- **Binary relevance by default.** Labels are relevant/not-relevant; graded
  relevance is supported for NDCG but the MVP ships binary, the simplest thing
  that works.
- **Read-only, side-effect-free.** Evaluation runs against the user's data
  without mutating it; where isolation matters it can operate over a throwaway
  vault built from the golden corpus, consistent with the project's guidance to
  smoke-test on isolated DBs.
- **Built on existing seams.** The harness reuses the interface-backed
  `Embedder` and `Reranker` (and the shared retrieval engine) — no parallel
  retrieval implementation — so it reflects real behavior and stays Ollama-free
  in CI via deterministic test doubles (constitution Principle V, audit §1.6
  strengths).
- **Hosted benchmarks (MS MARCO/BEIR) and A/B serving are deferred.** A custom
  golden set from the local corpus is the right first step for a local-first
  personal database; standard external benchmarks are a poor fit at MVP scale
  (audit §1.6).
- **The remaining audit items (H01, H03–H28) are tracked separately** in
  `RAG_BOOK_AUDIT_BACKLOG.md`, not in this spec — this spec is solely the
  top-priority H02 fix.
