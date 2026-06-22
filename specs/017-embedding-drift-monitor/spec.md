# Feature Specification: Embedding Drift Monitoring + Version Pinning

**Feature Branch**: `017-embedding-drift-monitor` *(commits directly to `main` per project convention.)*

**Created**: 2026-06-22 · **Status**: Draft

**Input**: "next backlog item" → **H11** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 4, second open item):
*"No embedding drift monitoring / version-pinning. Persist `{model,dim,ollama-version}` corpus-metadata
key; on startup compare to live config and refuse query / force reindex on mismatch."*
Source: `RAG_BOOK_AUDIT.md` §1.2 (P1: "Zero drift monitoring or version-pinning… build drift monitoring
'from day one'… `Migrate` reprocesses but never *detects* that a model swap silently corrupted the
index"). Book reference: ch005 §4.6 ("a library update silently changed pooling behavior once, dropping
retrieval accuracy 15%").

**Problem:** go-rag can be silently corrupted by changes *outside its own config* — and only learns about
it reactively, one query at a time. The retrieval book's headline drift caution (§4.6) is that an
embedding-library or **Ollama-server update** can quietly change pooling/quantization, so the same model
name produces *different vectors* after the update, tanking recall with no error. go-rag today records
only the model *name* per chunk; it has no record of the **Ollama server version** the corpus was built
under, no **startup check**, and no **persisted corpus baseline** — so this class of drift is invisible
until an operator notices retrieval quality has collapsed.

This is layered on top of two already-shipped items, and does **not** redo them:
- **H03 / spec 005** added a *query-time* runtime guard — `checkEmbeddingMismatch` refuses a query whose
  model/dim/convention differs from the corpus's stored majority. That is **reactive** (fires on the
  first mismatched query) and **cannot** detect an Ollama-version change (same model + dim, different
  server).
- **H07 / spec 008** recorded the prefix *convention* per embedding record.

H11 adds the **proactive** layer the book demands: a persisted corpus baseline
(`{model, dim, convention, ollama-version, recorded-at}`), a **startup** comparison of the live config +
live Ollama version against that baseline, and **Ollama-version pinning** — surfacing drift at boot
(before any query) and making a server-side change visible.

## Clarifications

### Session 2026-06-22

- Q: Under **hard** drift (model/dim/convention mismatch) detected at startup, what is the daemon's
  externally-observable posture? → **A: start degraded.** The process stays up (liveness OK) so the
  operator can run `status` / `migrate` in place, but **readiness is NOT READY** — REST `/health` and
  the gRPC health RPC report "not ready to serve" so clients/orchestrators do not route query traffic —
  and mismatched queries remain refused by the existing H03 guard. Soft (Ollama-version) drift stays
  warn-only and ready. Rationale: in a hard mismatch H03 refuses *every* query, so a "healthy" probe
  would be dishonest; modeling drift as a **readiness** signal (not liveness) avoids restart loops while
  truthfully reflecting "can't serve queries right now."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Drift is detected at startup, before any query (Priority: P1) 🎯 MVP

The operator starts the daemon (or runs `status`) against a corpus built under a different embedding
model/dimension/convention than the one currently configured. The system detects the mismatch at boot
and surfaces it loudly — in the startup log and in `status` — so the operator learns they must re-index
*before* issuing a query that would silently mis-score.

**Why this priority**: This is the proactive core H11 adds over H03. H03 catches a mismatched query
reactively; H11 catches it at boot, the moment the daemon opens the corpus, so the operator is never
surprised by a query-time refusal they don't understand, and so drift is visible even from `status`
without running a query. The book's "from day one" guidance is about *proactive* monitoring.

**Independent Test**: Build a corpus under model A; reconfigure to model B; start the daemon; assert the
startup log + `status` report the model mismatch (corpus baseline ≠ configured) without any query being
run.

**Acceptance Scenarios**:

1. **Given** a corpus whose persisted baseline records model `nomic-embed-text`, **When** the daemon
   starts with `embedding_model` configured as `mxbai-embed-large`, **Then** the startup output and
   `status` report a model mismatch (baseline ≠ configured) before any query.
2. **Given** a baseline dimension of 768, **When** the configured/live embedding produces 1024-dim
   vectors, **Then** the startup check reports a dimension mismatch.
3. **Given** a baseline convention of `nomic`, **When** the configured prefix convention resolves to
   `e5`, **Then** the startup check reports a convention mismatch.
4. **Given** a healthy corpus (baseline matches configured model/dim/convention), **When** the daemon
   starts, **Then** no drift is reported (clean boot).
5. **Given** a hard model/dim/convention mismatch at startup, **When** a client probes `/health` (REST)
   or the gRPC health RPC, **Then** readiness is NOT READY while liveness stays OK (process up) — so
   query traffic is deflected without the process being restarted, and `status`/`migrate` still work.

---

### User Story 2 - An Ollama-server version change is detected and warned (Priority: P1)

The operator updates Ollama (or restores the vault on a machine with a different Ollama build). The
model name and dimension are unchanged, but the server version differs from the one recorded in the
corpus baseline. The system detects the version change at startup and **warns** (does not refuse) —
because the same model under a new server *may* produce subtly different vectors, the operator should
re-index, but queries still work in the meantime.

**Why this priority**: This is the book's §4.6 headline failure (a library/server update silently
changing pooling). It is the one drift dimension H03 *cannot* detect (same model/dim), so it is H11's
most novel value. Warn-don't-refuse because a version bump must not hard-block retrieval.

**Independent Test**: Build a corpus under Ollama 0.1.x (version recorded in baseline); simulate/start
against an Ollama reporting 0.5.y; assert the startup check reports the version change as a warning and
that queries are still served.

**Acceptance Scenarios**:

1. **Given** a baseline recording Ollama version `0.1.0`, **When** the live Ollama reports `0.5.0`,
   **Then** the startup check emits a version-drift warning (baseline ≠ live) and `status` shows both
   versions.
2. **Given** a version-drift warning, **When** the operator runs a query, **Then** the query still
   succeeds (version drift warns; it does not refuse — only model/dim/convention refuse).
3. **Given** the live Ollama is unreachable at startup, **Then** the version check is skipped (not an
   error) and `status` reports the live version as unknown; the daemon still starts.

---

### User Story 3 - A persisted corpus baseline exists and stays current (Priority: P2)

The corpus carries an explicit metadata record — `{model, dim, convention, ollama-version,
recorded-at}` — capturing the embedding profile it was built under. It is written when the first
embedding lands, refreshed whenever the corpus is re-embedded under a consistent profile (e.g. after
`migrate`), and backfilled on first boot for a pre-H11 corpus (derived from the existing per-embedding
records + the live Ollama version). It is the authoritative baseline the startup check compares against.

**Why this priority**: The baseline is the substrate for both US1 and US2. Persisting it (rather than
re-deriving the majority on every boot) gives a stable, point-in-time snapshot and a home for the
Ollama version, which is not stored anywhere today.

**Independent Test**: Ingest a document; assert a corpus-baseline record exists with the ingesting
model/dim/convention + the live Ollama version + a recorded-at timestamp. Run `migrate` to a new model;
assert the baseline is refreshed to the new profile + current version.

**Acceptance Scenarios**:

1. **Given** an empty corpus, **When** the first document is embedded, **Then** a baseline record is
   written capturing that embedding's model/dim/convention, the live Ollama version, and a timestamp.
2. **Given** a pre-H11 corpus with no baseline, **When** the daemon first boots, **Then** a baseline is
   backfilled from the existing per-embedding majority + live Ollama version (no re-ingestion required).
3. **Given** a completed `migrate` (re-embed under a new model), **When** it finishes, **Then** the
   baseline is refreshed to the new profile + current Ollama version.
4. **Given** a baseline, **When** `status` runs, **Then** it shows the baseline (model/dim/convention/
   ollama-version/recorded-at) alongside the live values.

---

### User Story 4 - Drift state is visible in `status` across transports (Priority: P2)

`go-rag status` (and the REST/gRPC/MCP status surfaces) report the baseline-vs-live comparison:
baseline model/dim/convention/ollama-version, live configured model, live Ollama version, and explicit
drift flags (model/dim/convention drift = hard; ollama-version drift = soft warning). An operator or AI
agent can see the corpus's version health without running a query.

**Why this priority**: Operability — drift you can't see in `status` is drift you can't manage. This
makes US1/US2's detection observable in the canonical health view, on every transport.

**Independent Test**: Induce a model mismatch; run `go-rag status`; assert it shows the baseline, the
live configured model, and a model-drift flag. Repeat over REST/gRPC/MCP for parity.

**Acceptance Scenarios**:

1. **Given** a model mismatch, **When** the operator runs `status`, **Then** it reports the baseline
   model vs the configured model and flags the drift.
2. **Given** an Ollama-version change, **When** the operator runs `status`, **Then** it reports the
   baseline version vs the live version and flags it as a version-drift warning.
3. **Given** the drift state, **When** read over CLI/REST/gRPC/MCP, **Then** the same drift flags and
   baseline fields appear on every transport (parity).

---

### Edge Cases

- **Mid-migration (mixed) corpus**: a corpus partway through a `migrate` has mixed records. The baseline
  reflects the *intended* profile (post-migrate); the existing intra-corpus drift flags (H03/H07
  `EmbeddingDrift`/`ConventionCounts`) still report the mixed state. H11's baseline-vs-config check is
  orthogonal — both are shown.
- **Baseline absent (pre-H11 corpus)**: backfill on first boot from the per-embedding majority + live
  Ollama version (US3). No re-ingestion, no error.
- **Ollama unreachable at startup**: the daemon must still start (a local DB with no Ollama is a valid
  read state). The version check is skipped; live version reported as "unknown"; no drift verdict on
  version until Ollama is reachable. Re-checked when Ollama becomes reachable.
- **Ollama reachable but version endpoint errors**: treat as "unknown" (don't block); log a note.
- **Eval harness / offline embedder**: the deterministic offline embedder has no Ollama; the baseline's
  ollama-version field is "offline"/empty and the version check is skipped (the eval path is hermetic).
- **`migrate` interrupted / partial**: the baseline is refreshed only on *successful* migrate completion
  (not mid-migrate), so a half-migrated corpus keeps the pre-migrate baseline until migrate finishes —
  consistent with the mixed-state handling above.
- **Interaction with the query cache (H06)**: detecting drift does not itself flush the cache; a
  `migrate` (the operator's remedy) already flushes both caches (H06). Startup drift detection runs
  before queries, so no stale cache is served for a mismatched profile (queries are refused by H03
  anyway).
- **Dimension unknown until first embed**: for an empty corpus there is no baseline dim yet; the startup
  check is a no-op until the first embedding lands and writes the baseline.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST persist a **corpus baseline** record capturing the embedding profile the
  corpus was built under: `{model, dimension, prefix-convention, ollama-version, recorded-at}`.
- **FR-002**: The baseline MUST be written when the first embedding lands and refreshed on successful
  `migrate` completion (the corpus is then uniformly under the new profile + current Ollama version).
- **FR-003**: On daemon startup (and on `status`), the system MUST compare the **live configured
  embedding profile** (configured model; resolved convention; live embedding dimension) and the **live
  Ollama server version** against the persisted baseline.
- **FR-004**: On a **model / dimension / convention** mismatch (baseline ≠ configured/live) — a *hard*
  drift signal — the daemon MUST start **degraded**: the process stays up (liveness OK) so `status` /
  `migrate` work, the drift is surfaced loudly (startup log + `status`), and **readiness is NOT READY**
  so clients/orchestrators do not route query traffic. Mismatched queries remain refused by the existing
  H03 guard. (Soft / Ollama-version drift is warn-only and stays ready — FR-005.)
- **FR-005**: On an **Ollama-version** change (baseline version ≠ live version), the system MUST emit a
  *warning* at startup + in `status` — a *soft* drift signal. Queries MUST still be served (version
  drift does not refuse; the operator is advised to re-index).
- **FR-006**: When the live Ollama is unreachable (or its version endpoint errors), the version check
  MUST be skipped (live version reported "unknown"); the daemon MUST still start, and model/dim/
  convention checks still run.
- **FR-007**: A pre-H11 corpus (no baseline) MUST be backfilled on first boot from the existing
  per-embedding majority profile + the live Ollama version — no re-ingestion, no error.
- **FR-008**: `status` MUST surface the baseline (model/dim/convention/ollama-version/recorded-at), the
  live configured model, the live Ollama version, and explicit drift flags — model/dim/convention drift
  (hard) and ollama-version drift (soft).
- **FR-009**: The drift state and baseline fields MUST be exposed identically on CLI/REST/gRPC/MCP
  status (cross-transport parity).
- **FR-010**: The baseline's ollama-version field MUST be empty/"offline" for the deterministic offline
  embedder (eval harness), and the version check MUST be skipped on that path.
- **FR-011**: The readiness signal (REST `/health`, gRPC health RPC) MUST distinguish **liveness**
  (process alive, storage open — always OK while the daemon runs) from **readiness** (can it usefully
  serve queries): hard drift (FR-004) makes readiness NOT READY while leaving liveness OK, so the
  process is not restarted but query traffic is deflected. The existing `HealthInfo.OK` (= storage open)
  is the liveness signal; a separate readiness verdict reflects drift.

### Key Entities

- **Corpus baseline**: a persisted, corpus-level metadata record — `{model, dim, convention,
  ollama-version, recorded-at}` — the authoritative snapshot of the profile the corpus was built under.
  Distinct from the *derived* per-embedding majority scan (H03) because it is point-in-time, persisted,
  and carries the Ollama version.
- **Drift verdict**: the startup comparison result — `clean` | `hard-drift` (model/dim/convention
  mismatch) | `version-warning` (ollama-version change) | `unknown` (Ollama unreachable / no baseline
  yet).
- **Live Ollama version**: the version reported by the running Ollama server at boot/status time,
  compared against the baseline's recorded version.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Starting the daemon against a corpus whose baseline model ≠ the configured model reports
  the mismatch at boot (verifiable in the startup log + `status`, with no query run) **and** marks
  readiness NOT READY (liveness stays OK).
- **SC-002**: An Ollama-version change between baseline and live is reported as a warning at boot +
  `status`, while queries still succeed (verifiable: query returns results despite the version warning).
- **SC-003**: A corpus baseline record exists after the first embedding, is refreshed after `migrate`,
  and is backfilled on first boot for a pre-H11 corpus (verifiable via `status` showing the baseline
  fields + recorded-at).
- **SC-004**: `status` shows the baseline-vs-live comparison and drift flags identically over
  CLI/REST/gRPC/MCP (parity).
- **SC-005**: With Ollama unreachable, the daemon starts, the version check is skipped (live version
  "unknown"), and model/dim/convention checks still run (verifiable: boot succeeds, status reports
  "unknown" version).
- **SC-006**: The H02 eval harness (offline deterministic embedder) is unaffected — recall@10 unchanged
  (the version check is skipped on the offline path; FR-010), no quality regression.

## Assumptions

- **Layered on H03/H07, not replacing them**: the query-time mismatch guard (H03/spec 005) and the
  per-embedding convention provenance (H07/spec 008) remain as-is. H11 adds the proactive startup layer
  + ollama-version pinning + a persisted corpus baseline.
- **"Refuse query / force reindex" (audit wording) is realized as**: on hard drift the daemon starts
  **degraded** (liveness OK, readiness NOT READY — FR-004/FR-011), mismatched queries are refused by
  the existing H03 guard, and the operator runs `migrate` (which H11 makes aware of the drift and which
  refreshes the baseline). The daemon is NOT crashed/exited, so `status`/`migrate` work in place; H11
  does NOT auto-reindex (auto-reembed is surprising; the operator decides). A future `--auto-migrate` is
  out of scope.
- **Severity split**: model/dim/convention mismatch = HARD (loud + readiness NOT READY, queries refused
  by H03); ollama-version change = SOFT (warn, stays ready, queries allowed). A version bump must not
  hard-block retrieval; a model swap is a real config error the operator must fix.
- **Liveness vs readiness**: the existing `HealthInfo.OK` (= storage open) is the **liveness** signal
  and stays OK whenever the daemon runs; drift affects a separate **readiness** verdict so the process
  is not restarted (no systemd loop) while query traffic is still deflected.
- **Ollama version source**: the Ollama `/api/version` endpoint. If absent/erroring, treat as "unknown."
- **Baseline storage**: a new persisted corpus-metadata record (single Pebble key under a new prefix).
  The exact prefix is a plan decision (the constitution's fixed single-byte prefix space; plan verifies
  no collision with 0x01–0x06). NOT per-embedding (that's H07's 0x04 record) — one corpus-level header.
- **Backfill is lazy + non-destructive**: a pre-H11 corpus gets a baseline on first boot (majority +
  live version); no re-ingestion, no write to existing records.
- **`recorded-at`**: an absolute timestamp of when the baseline was last refreshed (first-embed or
  migrate-complete).
- **Startup check runs once at boot** (and the same comparison is available on-demand via `status`);
  it does not poll continuously. A version change mid-run is caught on next boot/status.
- **Dimension source**: the live dimension comes from the embedder's reported `Dimensions()` (definitive
  for deterministic embedders; populated for Ollama after its first response), mirroring H03.
- **Transport exposure**: the baseline + drift flags extend `StatusInfo` and surface on all four
  transports' status (the CLI delegates to MCP `go_rag_status`; REST/gRPC status carry the fields per
  the established projection — plan decides proto expansion vs the minimal-projection precedent).
- **Out of scope**: continuous/background drift *scoring* (a drift metric over time), automatic
  re-indexing, multi-provider version pinning (Ollama-only per PRD), and persistent-index snapshot
  (H16). Also out of scope: changing H03's query-time guard behavior.
