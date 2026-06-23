# Tasks: Embedding Drift Monitoring + Version Pinning (H11)

**Input**: Design documents from `/specs/017-embedding-drift-monitor/` — [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/drift-status-contract.md](contracts/drift-status-contract.md), [quickstart.md](quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅

**Tests**: INCLUDED — go-rag is test-gated (constitution: `go test -race -cover` + `make test-eval` must stay green). Every story carries its own `_test.go` tasks.

**Organization**: Phases map to the spec's 4 user stories. Shared primitives (corpus baseline persistence, Ollama-version fetch, drift-verdict type, engine verdict cache) live in Phase 2 Foundational because every story depends on them.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US4 (user-story phases only)
- Exact file paths in every description

---

## Phase 1: Setup

**Purpose**: Confirm a clean baseline before touching anything.

- [x] T001 Record clean baseline: run `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...` — all green; run `make test-eval` and note the recall@10 baseline (must not regress, SC-006).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared primitives EVERY user story depends on — the persisted corpus-baseline record, the Ollama-version fetch, the drift-verdict type, and the engine's cached-verdict scaffolding.

**⚠️ CRITICAL**: No user-story work can begin until this phase is complete.

- [x] T002 [P] Add the corpus-baseline persistence in `internal/engine/baseline.go` + the prefix in `internal/storage/storage.go`: `PrefixCorpusMeta byte = 0x10`; `CorpusBaseline{Model, Dim, Convention, OllamaVersion, RecordedAt}` (JSON, RFC3339 timestamp); `LoadBaseline(db) (*CorpusBaseline, bool)` and `SaveBaseline(db, *CorpusBaseline)` over a single fixed key under `0x10`. Unit-test Load/Save round-trip.
- [x] T003 [P] Implement the Ollama-version fetch in `internal/engine/version.go`: `ollamaVersion(ctx, baseURL) (string, error)` — `GET {baseURL}/api/version`, short timeout (mirror `embedderReachable` in `internal/engine/health.go`), parse `{"version":"…"}`; empty `baseURL` → `""` (offline); unreachable/non-200/parse error → `"unknown"` + nil error. Unit-test with an `httptest` server (known version, 404, unreachable).
- [x] T004 [P] Add the `DriftVerdict` type in `internal/engine/drift.go` (per data-model.md: `Verdict`, `Hard`, baseline/live model/dim/convention/version, `Reasons []string`) and the engine cache scaffolding in `internal/engine/engine.go`: `Engine` gains a `driftVerdict DriftVerdict` + `verdictMu sync.RWMutex` + cached `liveOllamaVersion string`; `RefreshDriftVerdict(ctx)` computes and caches the verdict (logic filled by US1/US2; this task leaves a stub returning `n/a`), and a read accessor used by `Health`. `Close()` clears the cache.

**Checkpoint**: Foundation ready — baseline persists, version is fetchable, the verdict type + engine cache exist. US1 can implement the hard-drift comparison against a hand-saved baseline.

---

## Phase 3: User Story 1 — Hard drift detected at boot; daemon degrades (Priority: P1) 🎯 MVP

**Goal**: A model/dim/convention mismatch between the corpus baseline and the live config is detected at boot, logged loudly, and the daemon starts **degraded** — liveness OK, readiness NOT READY — before any query. (Clarification posture A.)

**Independent Test**: quickstart Scenario 1 — build a corpus under model A, reconfigure to B (no migrate), boot; assert the boot log + `/health` body report hard drift with `ready: false`, `ok: true`.

### Implementation for User Story 1

- [x] T005 [US1] Implement the hard-drift comparison in `internal/engine/drift.go` `computeDriftVerdict(ctx)`: load the baseline (T002); compare baseline.Model vs `cfg.EmbeddingModel`, baseline.Dim vs live `embedderOrOllama().Dimensions()` (skip when live dim 0), baseline.Convention vs `cfg.Prefixer().Convention()`; populate `DriftVerdict` with `Hard=true`, `Verdict="hard-drift"`, and human-readable `Reasons` on any mismatch; `clean` when all match. Wire it into `RefreshDriftVerdict` (replacing the T004 stub).
- [x] T006 [US1] Wire the boot check in `internal/cli/serve.go`: after `engine.NewWithDB`, call `eng.RefreshDriftVerdict(ctx)` before serving listeners and log the verdict once (`hard-drift: …` / `clean` / etc.) — the loud-at-startup signal (FR-004).
- [x] T007 [US1] Add readiness to `internal/engine/health.go`: `HealthInfo` gains `Ready bool` (readiness) distinct from `OK bool` (liveness); `Engine.Health` sets `Ready = OK && !cachedVerdict.Hard`.
- [x] T008 [US1] Expose readiness on the health probes: REST `/health` body in `internal/rest/` includes `ready` + `drift_verdict` (HTTP stays 200 — do NOT 503, per D4); the gRPC `Gorag.Health` RPC maps `Health.Ready` onto a new `ready` field in `proto/gorag.proto` `HealthResponse` (regen `proto/gen`) + `internal/grpc/engine_adapter.go`. **Batch this proto regen with T023** (StatusResponse drift fields) — one generate step.
- [x] T009 [US1] Test in `internal/engine/drift_test.go`: build a corpus (save a baseline for model A via `SaveBaseline`), reconfigure `cfg.EmbeddingModel` to B, `RefreshDriftVerdict` → assert `Verdict="hard-drift"`, `Hard=true`, `Reasons` mentions model; `Health().Ready==false && Health().OK==true` (the posture-A gate).
- [x] T010 [US1] Test in `internal/engine/drift_test.go`: a matching baseline (model/dim/convention == live) → `Verdict="clean"`, `Ready==true`.

**Checkpoint**: US1 — hard drift is detected at boot and the daemon honestly reports not-ready while staying up.

---

## Phase 4: User Story 2 — Ollama-version pinning (soft warning) (Priority: P1)

**Goal**: An Ollama-server version change between the baseline and live is detected and warned (queries still served); hard drift wins over soft; unreachable Ollama and offline embedders are handled safely.

**Independent Test**: quickstart Scenario 2 (version warning, queries succeed) + Scenario 5 (Ollama down, boot safe).

### Implementation for User Story 2

- [x] T011 [US2] Add the version comparison to `computeDriftVerdict` in `internal/engine/drift.go`: fetch the live version via `ollamaVersion` (T003); if baseline.OllamaVersion and live are both non-empty/non-`"unknown"` and differ → set `Verdict="version-warning"` (soft, `Hard=false`); **hard wins over soft** (a hard-drift verdict stays hard-drift); skip the comparison when either side is `""`/`"unknown"` (offline/unreachable). Cache the live version on the engine.
- [x] T012 [US2] Test in `internal/engine/drift_test.go`: baseline version `0.1.0`, live `0.5.0`, matching model/dim/convention → `Verdict="version-warning"`, `Ready==true` (soft, serve). Then: model ALSO mismatched → verdict stays `hard-drift` (hard-wins-over-soft), `Ready==false`.
- [x] T013 [US2] Test in `internal/engine/drift_test.go`: Ollama unreachable → `liveOllamaVersion=="unknown"`, `RefreshDriftVerdict` returns no error, boot would succeed, version comparison skipped, model/convention comparison still runs.
- [x] T014 [US2] Test in `internal/engine/drift_test.go`: an injected (offline) embedder (`NewWithEmbedder`) → version fetch returns `""`, comparison skipped (FR-010); a stored baseline still allows the model/convention check.

**Checkpoint**: US1 + US2 — both drift dimensions detected, with the correct hard/soft severity and the safe unreachable/offline paths.

---

## Phase 5: User Story 3 — Persisted baseline lifecycle (write/refresh/backfill) (Priority: P2)

**Goal**: The corpus baseline exists in real flows — written on first embed, refreshed on successful migrate, backfilled on first boot for a pre-H11 corpus.

**Independent Test**: quickstart Scenario 3 (migrate refreshes) + Scenario 4 (pre-H11 backfill).

### Implementation for User Story 3

- [x] T015 [US3] Write the baseline on first embed in `internal/pipeline/workers.go` `processJob`: after a successful embed, if no baseline exists yet, save one from `p.embed.Model()`, the produced vector dim, the active prefixer convention, the engine's cached live Ollama version, and now (UTC). (Needs the engine/version passed into the pipeline — pass the cached live version or a small callback; plan-decides the wiring, keep `internal/pipeline` Ollama-free.)
- [x] T016 [US3] Refresh the baseline + verdict on successful migrate in `internal/engine/ingest.go` `Migrate`: after `ReprocessAll` succeeds (and the corpus is uniform under the new model), re-fetch the live version and `SaveBaseline` with the new profile; call `RefreshDriftVerdict` so the daemon's cached verdict flips to `clean`.
- [x] T017 [US3] Backfill the baseline on first boot in `internal/engine/drift.go` (within `RefreshDriftVerdict`, before the comparison): if no baseline exists but embeddings exist, derive the profile from `CorpusProfile(db)` (H03's majority scan) + cached live version + now, and `SaveBaseline` it (no re-ingest — FR-007). If the corpus is empty, leave the verdict `n/a`.
- [x] T018 [US3] Tests in `internal/engine/baseline_test.go`: (a) first embed writes a baseline (model/dim/convention/version/recorded-at populated); (b) `migrate` to a new model refreshes it (recorded-at advances, model = new); (c) a pre-H11 corpus (embeddings present, no baseline) is backfilled on first `RefreshDriftVerdict` without re-ingesting; (d) empty corpus → no baseline, verdict `n/a`.

**Checkpoint**: US1–US3 — drift is detected AND the baseline it depends on is maintained across the real ingest/migrate/upgrade lifecycle.

---

## Phase 6: User Story 4 — Baseline + drift visible in `status` across transports (Priority: P2)

**Goal**: `go-rag status` (and REST/gRPC/MCP) show the baseline (model/dim/convention/version/recorded-at), the live Ollama version, and the drift verdict — identically on every transport.

**Independent Test**: quickstart Scenario 1/2 `status` output shows the Baseline + Drift sections; parity over the four transports.

### Implementation for User Story 4

- [x] T019 [P] [US4] Add the status fields to `StatusInfo` in `internal/engine/types.go`: `CorpusBaseline{Model,Dim,Convention,OllamaVersion,RecordedAt}`, `LiveOllamaVersion`, `DriftVerdict`, `HardDrift`, `VersionDrift` (per data-model.md).
- [x] T020 [US4] Populate the fields + recompute the live verdict in `Engine.Status` in `internal/engine/status.go` (Status fetches the live Ollama version fresh — the on-demand detailed view, distinct from the cached boot verdict read by `/health`).
- [x] T021 [US4] Render a Baseline + Drift section in the CLI status command `internal/cli/status.go` (the daemon-running path delegates to MCP `go_rag_status`; the stopped path computes locally — both show the baseline from the persisted record).
- [x] T022 [P] [US4] Add the drift/baseline fields to the REST status response in `internal/rest/types.go` + populate in the REST status handler.
- [x] T023 [US4] Add the drift/baseline fields to the gRPC `StatusResponse` in `proto/gorag.proto`; regen `proto/gen`; map them in `internal/grpc/engine_adapter.go`. (**Single batched regen with T008** — the `HealthResponse.ready` field.)
- [x] T024 [P] [US4] Append baseline + drift to the MCP `renderStatus` output in `internal/mcp/server.go` (e.g. `, baseline: model=… dim=… conv=… ollama=…@<ts>, drift: <verdict> (<reasons>)`).
- [x] T025 [US4] Parity test in `internal/engine/parity_test.go` (or a new `internal/engine/drift_parity_test.go`): induce a model mismatch; assert the same baseline + drift fields appear over CLI/REST/gRPC/MCP status.

**Checkpoint**: All four stories complete; drift + baseline are observable on every transport.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Eval-harness safety, end-to-end validation, docs, gates, and backlog closure.

- [x] T026 [P] Confirm the eval path is safe: in `internal/eval/`, verify the offline deterministic embedder yields `ollamaVersion==""` so the version check is skipped (FR-010) and `make test-eval` recall@10 is unchanged from the T001 baseline (SC-006). Adjust the eval engine construction only if needed.
- [x] T027 Run every quickstart.md scenario (1–7) on an isolated DB with non-default transport ports (per CLAUDE.md §Constraints — never bare `go-rag start` against the live vault); capture the observed drift verdicts + that all scenarios pass.
- [x] T028 [P] Update `README.md` — add a "Drift monitoring & version pinning" section (corpus baseline, the boot check, hard vs soft drift, the degraded/readiness posture, how to remediate via `migrate`). Match the existing section style.
- [x] T029 Final gates all green: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test -race -cover ./...`, and `make test-eval`. Record coverage.
- [x] T030 Mark H11 complete in `RAG_BOOK_AUDIT_BACKLOG.md`: change the H11 line checkbox `[ ] → [x]` and append a `✅ COMPLETE (spec 017)` note — following the exact format of the neighbouring H01/H06/H15 completion entries — summarising what shipped (persisted corpus baseline under prefix 0x10, boot drift check, Ollama-version pinning, degraded/readiness posture-A, migrate refresh, backfill, status surface on 4 transports), the gates passed, and any caveat (e.g. `golangci-lint` skipped if the env still lacks a compatible config). This is the explicit "make the backlog item complete when finished" task requested for this spec.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately.
- **Foundational (Phase 2)**: depends on Phase 1 — **BLOCKS all user stories**.
- **US1 (Phase 3)**: depends on Phase 2 (baseline Load/Save + version fetch + verdict type).
- **US2 (Phase 4)**: depends on Phase 2 + US1 (extends `computeDriftVerdict` with the soft comparison; hard-wins logic from US1).
- **US3 (Phase 5)**: depends on Phase 2 (baseline Save) + US1 (verdict cache); writes/refreshes the baseline US1/US2 compare against.
- **US4 (Phase 6)**: depends on Phase 2 + US1/US2 (renders the verdict) + US3 (renders the baseline).
- **Polish (Phase 7)**: depends on all stories complete.

### Within Each User Story

- Types/structs before the logic that reads them.
- `computeDriftVerdict` hard (US1 T005) before soft (US2 T011) — they share the function.
- The `/health` body (US1 T008) and `StatusResponse` (US4 T023) share a proto regen — do them together.

### Parallel Opportunities

- **Phase 2**: T002, T003, T004 independent (different files) → parallel.
- **Phase 6**: T019, T022, T024 independent across files → parallel; T023 is the proto+regen (batch with T008).
- **Phase 7**: T026, T028 independent → parallel.

### Cross-phase coordination note

- **One proto regen** covers T008 (`HealthResponse.ready`) and T023 (`StatusResponse` drift fields) — do them together.
- **T015 wiring**: the pipeline writing the baseline needs the live Ollama version, which lives on the engine. Keep `internal/pipeline` Ollama-free (constitution: index/pipeline stay free of the embedder HTTP layer) — pass the cached version string (or a small `func() string` callback) into the pipeline rather than the engine; plan confirms the exact seam.

---

## Implementation Strategy

### MVP First (Foundation + US1)

1. Phase 1 (baseline) → Phase 2 (foundation: baseline persist + version fetch + verdict type/cache).
2. Phase 3 (US1: hard-drift detection + degraded readiness).
3. **STOP and VALIDATE**: quickstart Scenario 1 passes (boot detects model mismatch; `/health` `ready:false, ok:true`; H03 refuses queries).
4. The headline value (proactive hard-drift detection + honest readiness) is live.

### Incremental Delivery

1. Foundation → US1 (MVP: hard drift at boot, degraded) → validate.
2. + US2 (version pinning, soft warn, safe unreachable/offline) → validate Scenarios 2 & 5.
3. + US3 (baseline lifecycle: write/refresh/backfill) → validate Scenarios 3 & 4.
4. + US4 (status surface across 4 transports) → validate + parity.
5. Polish → T026–T029 green → T030 closes H11 in the backlog.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[USx]` labels map tasks to spec user stories for traceability.
- Every story is independently testable at its checkpoint.
- Commit after each task or logical group (Conventional Commits, straight to `main`).
- Highest-risk item: **T006 + T007 + T009** — the boot degraded-readiness posture (clarification A). If `/health` ever 503s, or the daemon exits, or readiness stays `true` under hard drift, look here first.
- Second-risk: **T015 + T017** — the baseline must exist before it's compared (first-embed write + first-boot backfill); a missing baseline must never crash the boot or the query path.
