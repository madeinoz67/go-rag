# Feature Specification: Migration Dry-Run

**Feature Branch**: `028-migrate-dry-run`

**Created**: 2026-06-25

**Status**: Draft

**Input**: User description: "work on backlog item H24" — audit finding **H24**
(`RAG_BOOK_AUDIT_BACKLOG.md`, Phase 6, §1.8): *"`migrate` has no dry-run / cost
estimate. `migrate --dry-run` → doc-count + model delta before re-embedding."*
Today the migration command (re-embed the corpus onto the currently configured
embedding model after a model change) **shows a per-model breakdown but then
immediately re-embeds every stale embedding** — a whole-corpus, one-shot,
expensive operation — with no way to inspect the plan and stop, no estimate of
the work, and no parity across the other transports. The book's guidance ("reserve
a reprocessing budget") presupposes knowing the cost *before* committing. This
feature adds a true no-side-effect dry-run that previews the migration plan and
its cost and exits without touching anything — available wherever migrate is.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Preview a migration without triggering it (Priority: P1)

An operator is about to change — or has just changed — the embedding model and
wants to see what a migration would do and how much work it is **without
committing** to a full-corpus re-embed. Today the command prints a per-model
breakdown (which embeddings are on which model, which are stale) but then proceeds
straight to re-embedding every stale embedding; there is no way to look at the
plan and decline. After this feature, a dry-run mode produces the migration plan
— the current/target model, every stored source model with its embedding count,
which are stale, the total stale count — and exits, leaving the corpus, caches,
and baseline completely untouched.

**Why this priority**: The entire user-facing value of H24. A whole-corpus re-embed
is expensive and one-shot; an operator wants to see the bill before paying it. The
preview *information* largely exists today; the gap is a genuine no-side-effect
dry-run path that can stop.

**Independent Test**: Run the dry-run on a corpus containing stale-model
embeddings; confirm it reports the plan and exits **without** re-embedding
(embedding counts and contents unchanged, no embedding generated, no cache or
baseline mutation). Run it again — identical output (idempotent, read-only).

**Acceptance Scenarios**:

1. **Given** a corpus containing embeddings made with a different model than the current one, **When** the operator runs the dry-run, **Then** the plan is shown (target model, every source model with its count, which are stale, the total stale count) and the operation exits **without** re-embedding anything.
2. **Given** the dry-run has run, **When** the operator inspects the corpus, **Then** nothing changed — embedding counts, contents, caches, baseline, and index epoch are identical to before the dry-run.
3. **Given** no live embedding backend is reachable, **When** the operator runs the dry-run, **Then** it still succeeds and reports the full plan — the dry-run reads stored metadata only and generates no embedding.

---

### User Story 2 - The cost estimate is actionable (Priority: P2)

Beyond a bare count, the operator gets enough signal to decide whether to migrate
now or defer: how many embeddings must be re-generated, the source→target model
(and dimensionality) change, and whether the corpus is currently consistent or
already mixed. This is the "reserve reprocessing budget" signal the book calls out
— an effort proxy that frames the count, not a wall-clock guarantee.

**Why this priority**: Turns the preview from merely informational into
decision-useful. A count alone doesn't say "this is minutes or hours"; the
model/dimensionality delta plus a consistency flag give the operator the context to
gauge effort and risk before committing.

**Independent Test**: On a mixed-model corpus, the dry-run reports the stale
count, names the source and target models, reports the dimensionality delta, and
flags the corpus as mixed; on a clean single-model corpus it reports zero stale
and no dimensionality change.

**Acceptance Scenarios**:

1. **Given** a corpus whose embeddings span multiple models or dimensionalities, **When** the dry-run runs, **Then** it reports each source model and its count, distinguishes stale from current, names the target model, reports any dimensionality delta, and flags whether the corpus is consistent or mixed.
2. **Given** the dry-run output, **When** the operator reads it, **Then** the estimated work is expressed as a re-embedding effort proxy (the count of stale embeddings to regenerate, plus the model/dimensionality delta), clearly labeled as an estimate rather than a time guarantee.
3. **Given** a corpus already fully on the target model, **When** the dry-run runs, **Then** it reports zero stale embeddings and no required dimensionality change.

---

### User Story 3 - Dry-run available wherever migrate is, with zero side effects on every transport (Priority: P3)

Migration is exposed on every transport (CLI, REST, gRPC, MCP). The dry-run mode
is available on all of them identically, and on **every** transport a dry-run is
guaranteed to mutate nothing — it is a pure read.

**Why this priority**: Parity and safety. Lower priority than the preview itself
(US1) and the actionable cost (US2), but required so the dry-run contract holds
uniformly and an agent or remote caller can preview safely without side effects.

**Independent Test**: Invoke the dry-run over each transport on the same corpus;
assert the returned plan is identical across transports and that each leaves the
corpus, caches, baseline, and epoch unchanged.

**Acceptance Scenarios**:

1. **Given** the dry-run option on any transport (CLI, REST, gRPC, MCP), **When** invoked on the same corpus, **Then** the returned plan is identical across all transports.
2. **Given** a dry-run on any transport, **When** it completes, **Then** no state changed — the corpus, caches, baseline, and index epoch are untouched.

---

### Edge Cases

- **Empty corpus (no embeddings yet)**: the dry-run reports nothing to migrate and exits cleanly (not an error).
- **All embeddings already on the current model**: the dry-run reports zero stale embeddings and no work.
- **Mixed / mid-migration corpus**: the dry-run shows the full multi-model breakdown, the dimensionality delta, and the consistency flag.
- **Embedding backend unreachable**: the dry-run MUST still succeed — it is metadata-only and generates no embedding. A dry-run that requires the backend defeats its purpose.
- **Repeated dry-runs**: identical output with no cumulative effect (idempotent, strictly read-only).
- **Preview vs execution drift**: the plan the dry-run shows MUST match what a real migration would actually re-embed (no disagreement between preview and execution).
- **Dimensionality change (e.g. 768 → 1024)**: flagged so the operator knows the index shape changes.
- **Cost-estimate honesty**: clearly labeled an estimate; never presented as an exact time.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a dry-run mode for the migration operation that produces the migration plan and exits **without** re-embedding anything.
- **FR-002**: The dry-run plan MUST report: the current/target embedding model, every stored source model with its embedding count, which are stale (≠ target), the total stale count, and whether the corpus is consistent or mixed.
- **FR-003**: The dry-run MUST be strictly read-only — it MUST NOT modify embeddings, documents, caches, the corpus baseline, or the index epoch.
- **FR-004**: The dry-run MUST succeed without a reachable embedding backend; it operates on stored metadata only and generates no embedding.
- **FR-005**: The dry-run MUST include an actionable cost estimate — at minimum the count of embeddings to regenerate and the source→target model (and dimensionality) delta — clearly labeled as an estimate, not a time guarantee.
- **FR-006**: The dry-run MUST be available on every transport where migration is available (CLI, REST, gRPC, MCP), returning an identical plan for the same corpus.
- **FR-007**: The dry-run plan MUST be deterministic and idempotent — repeated dry-runs on an unchanged corpus return identical output.
- **FR-008**: The dry-run plan MUST reflect what a real migration would actually do — the set of embeddings the preview marks stale is exactly the set a real run would re-embed.

### Key Entities *(include if feature involves data)*

- **Migration Plan (Preview)**: the dry-run output. Names the target model, each source model and its count, the stale set and total, the model/dimensionality delta, a consistency flag, and the estimated re-embedding effort. Read-only; produces no change.
- **Embedding Model Stats**: per-model embedding counts over the stored corpus — the input the plan is computed from.
- **Cost Estimate**: an effort proxy for the re-embed (the count of stale embeddings to regenerate plus the model/dimensionality delta), labeled approximate.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a corpus with stale embeddings, the dry-run reports the full plan (target model, per-source-model counts, stale total) and exits without re-embedding — confirmed by identical embedding counts and contents before and after.
- **SC-002**: The dry-run succeeds with **no embedding backend reachable** (metadata-only), proving it previews without standing up the embedder.
- **SC-003**: The cost estimate reports the stale count and the source→target model + dimensionality delta, labeled as an estimate — sufficient to decide whether to migrate now.
- **SC-004**: The dry-run is available and identical across CLI, REST, gRPC, and MCP, and on every transport mutates nothing (corpus, caches, baseline, epoch unchanged).
- **SC-005**: A dry-run followed by a real migration shows the same stale set the real run actually re-embedded — the preview matches the execution.

## Assumptions

- **The cost estimate is an effort proxy** (stale-embedding count plus model/dimensionality delta), **not a wall-clock time prediction**. Predicting real time requires benchmarking the live embedding backend's throughput, which is out of scope and would also require the backend to be up — contradicting FR-004. The estimate is clearly labeled approximate.
- **The dry-run is metadata-only and strictly read-only.** It consults the stored per-model embedding stats (which already exist) and never generates an embedding.
- **The dry-run is surfaced on every transport migration is** (parity with all other operations); the CLI's `--dry-run` flag is the primary human surface.
- **Out of scope for v1**: scheduling or budgeting migrations over time, partial/selective migration (re-embed only some documents), automatic drift-triggered migration, and time-based cost prediction.
- **The real migration (the re-embed) is unchanged** in behaviour — this feature only adds the preview path; it does not alter what a real `migrate` does.
