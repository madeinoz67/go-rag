# Tasks: Observability — Metrics, Latency & Tracing

**Input**: Design documents from `/specs/020-observability/` (plan.md, spec.md, research.md, data-model.md, contracts/metrics.md, quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅ (US1–US3), research.md ✅ (D1–D7), data-model.md ✅, contracts/ ✅

**Tests**: Included — quickstart.md's done-definition requires a `/metrics` scrape test, a trace test, an **air-gap test**, and an overhead check.

**Organization**: Tasks grouped by user story (US1 P1 = MVP `/metrics`+`status`; US2 P2 traces; US3 P3 air-gap/opt-in). Go project — `internal/<pkg>/` paths per plan.md.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[Story]**: US1/US2/US3 — maps to spec.md user stories
- All paths project-relative; **new dependency family** (OTel, pure-Go Apache-2.0, user-authorized — Constitution III)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Add the OTel dependency family + the config keys every story uses.

- [X] T001 [P] Add OpenTelemetry deps to `go.mod` — `go.opentelemetry.io/otel`, `otel/sdk`, `otel/sdk/metric`, `otel/exporters/prometheus`, `otel/exporters/stdout/stdouttrace`, `otel/exporters/otlp/otlptrace` (+ `otlp/otlptrace/otlptracehttp`); `go mod tidy`; verify `CGO_ENABLED=0 go build ./...` (Constitution III)
- [X] T002 [P] Add observability config keys to `internal/config` — `metrics_enabled` (default `true`), `otel_export` (`none`|`stdout`|`otlp`, default `stdout`), `otel_endpoint` (string, OTLP only); Get/Set/Validate + Load backward-compat (absent ⇒ defaults)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the `internal/observe` package — all OTel wiring in one place (keeps OTel imports localized; engine imports only `observe`'s helper API). ⚠️ No story work until this lands.

- [X] T003 `internal/observe/otel.go` — `Init(cfg)` builds the TracerProvider + MeterProvider + exporters (stdout trace exporter by default; OTLP constructed **only** when `otel_export=otlp`+endpoint); `Shutdown(ctx)` flushes; no exporter dials out otherwise (Constitution I)
- [X] T004 [P] `internal/observe/metrics.go` — instrument definitions (`gorag_query_duration_seconds` histogram, `gorag_ingest_duration_seconds`, `gorag_operations_total`, `gorag_query_results`, `gorag_chunks_indexed_total`, `gorag_poison_flagged_total`, `gorag_cache_hits_total`/`_misses_total`, `gorag_documents`/`gorag_chunks` gauges) with the D3 bucket boundaries; low-cardinality labels
- [X] T005 [P] `internal/observe/spans.go` — tracer accessor + `StartSpan(ctx, name)` helper (returns ctx + span); span-naming constants (`gorag.Query`/`.Ingest`/`.Migrate` + sub-spans)
- [X] T006 [P] `internal/observe/prometheus.go` — the `/metrics` HTTP handler (OTel prometheus exporter's manual reader → Prometheus text); loopback scrape surface
- [X] T007 observe package test — instruments register; `/metrics` scrape returns the `gorag_*` families after a recorded op; race-clean

**Checkpoint**: `internal/observe` ready (provider/exporters/instruments/handler). Engine instrumentation can begin.

---

## Phase 3: User Story 1 — Operator visibility (`/metrics` + `status --metrics`) (Priority: P1) 🎯 MVP

**Goal**: per-op latency (p50/p99), counts, error rate recorded + exposed via `/metrics` and `status --metrics`, scraped (never pushed), local-only.

**Independent Test**: run a few queries against a daemon, then `curl /metrics | grep gorag_` and `status --metrics` → latency/count metrics present.

### Implementation for User Story 1

- [X] T008 [P] [US1] Instrument `Engine.Query`/`Add`/`Scan`/`Reprocess`/`Migrate` in `internal/engine` — record `gorag_*_duration_seconds` + `gorag_operations_total{status}` (latency via a deferred histogram record; status from the returned error)
- [X] T009 [US1] Wire `observe.Init(cfg)` at daemon boot and `observe.Shutdown(ctx)` on stop (in `internal/daemon` / `cmd/go-rag`); one-shot CLI commands init a short-lived provider that emits + exits
- [X] T010 [P] [US1] Mount `GET /metrics` (loopback, unauth, like `/health`) on the REST mux in `internal/rest`; add the route to `openapi.yaml` (the routes/openapi parity test will flag drift)
- [X] T011 [P] [US1] `status --metrics` — read the meter provider's manual reader, render p50/p99 + counts + error rate in `internal/cli` (status cmd) and `internal/mcp` (`renderStatus`); optional metrics block in REST/gRPC status DTOs
- [X] T012 [US1] `/metrics` + `status --metrics` test — after a query, scrape returns `gorag_query_duration_seconds_bucket{mode=...}` + `gorag_operations_total`; `status --metrics` renders matching p50/p99 (single source of truth: the meter provider)

**Checkpoint**: US1 — operator visibility live (`/metrics` scrape + `status --metrics`).

---

## Phase 4: User Story 2 — Localizing slow/failing ops with traces (Priority: P2)

**Goal**: OTel spans around Query/Ingest/Migrate (+ Query sub-spans) to a local sink; no query text on spans.

**Independent Test**: run a query → a `gorag.Query` span with timed `embed`/`retrieve` sub-spans appears in the local trace sink.

### Implementation for User Story 2

- [X] T013 [P] [US2] Add spans around `Engine.Query` in `internal/engine` — top-level `gorag.Query{mode,k}` + sub-spans `embed` (query embedding), `retrieve` (FTS+vector+RRF), `rerank`; H09 RerankFailed recorded as a span event (no query text)
- [X] T014 [P] [US2] Add spans around ingest (`gorag.Ingest{op}` → `read`/`store`/`embed`) and `Migrate` in `internal/engine`/`internal/pipeline`
- [X] T015 [US2] Trace test — a query emits `gorag.Query` + sub-spans to the local (test) exporter; assert span names + that no query text / chunk content is on any span

**Checkpoint**: US2 — traces localize cost; spans carry no sensitive content.

---

## Phase 5: User Story 3 — Opt-in remote export + the air-gap gate (Priority: P3)

**Goal**: OTLP remote export constructed **only** when explicitly configured; zero telemetry egress by default (Constitution I).

**Independent Test**: default config ⇒ zero outbound connections; `otel_export=otlp`+endpoint ⇒ canary receives OTLP; remove config ⇒ stops.

### Implementation for User Story 3

- [X] T016 [US3] OTLP exporter path in `internal/observe/otel.go` — built only when `otel_export=otlp` + `otel_endpoint` are set; trace + metric OTLP exporters to the named endpoint; nothing dials out otherwise
- [X] T017 [US3] Air-gap test — default config ⇒ a canary `httptest.Server` receives **0** requests across Query/Ingest/Migrate/`/metrics` scrape; with `otel_export=otlp`+endpoint ⇒ canary receives OTLP traffic only; remove ⇒ stops (mirrors H04 `TestThreat_Import_URL_AirGap`)

**Checkpoint**: US3 — air-gap verified; remote export is explicit-only.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: tie-in counters, docs, overhead, final gates.

- [X] T018 [P] Wire tie-in counters in `internal/engine` — `gorag_poison_flagged_total{level}` (from H04 verdict writes), `gorag_cache_hits_total`/`_misses_total{cache}` (from H06 cache), `gorag_documents`/`gorag_chunks` gauges (from Status counts); no double-counting (single instrumentation point per event)
- [X] T019 [P] Docs — metric + span inventory + the air-gap boundary in `docs/observability.md` (FR-008); reference the `contracts/metrics.md` contract
- [X] T020 Overhead check — instrumentation microbench <1% (SC-004); `make test-eval` recall@10 unchanged (SC-006); instrumented paths do not breach <500ms hybrid / <10ms ACK
- [X] T021 Final gates — `go build ./...`, `go vet ./...`, `go test -race -cover ./...` green; `go mod tidy` clean under `CGO_ENABLED=0` (Constitution III: OTel pure-Go); run `quickstart.md` scenarios 1–5; openapi + (any) tool-count parity green

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately (T001 dep-add + T002 config parallel)
- **Foundational (Phase 2)**: depends on Phase 1 — `internal/observe` BLOCKS all stories
- **US1 (Phase 3)**: depends on Phase 2 (`observe` package) — the MVP
- **US2 (Phase 4)**: depends on Phase 2 (uses the tracer from `observe`)
- **US3 (Phase 5)**: depends on Phase 2 (the OTLP path is in `observe.Init`)
- **Polish (Phase 6)**: depends on US1+US2 (tie-in counters wire into instrumented ops)

### User Story Dependencies

- **US1 (P1)**: starts after Foundational — no story deps. **MVP.**
- **US2 (P2)**: starts after Foundational — independently testable (traces)
- **US3 (P3)**: starts after Foundational — independently testable (air-gap/opt-in)
- US1/US2/US3 are mutually independent once `observe` exists (different surfaces)

### Within Each User Story

- Instruments/spans before the surface that exposes them
- `/metrics` mount + `status --metrics` after the engine records metrics
- Test last in each story

### Parallel Opportunities

- Phase 1: T001 ∥ T002 (deps vs config)
- Phase 2: T004 ∥ T005 ∥ T006 (metrics/spans/prometheus files) once T003 lands
- US1: T008 ∥ T010 ∥ T011 (engine instrument vs REST mount vs status render — different files)
- US2: T013 ∥ T014 (Query spans vs Ingest/Migrate spans)
- Polish: T018 ∥ T019 (counters vs docs)

---

## Parallel Example: User Story 1

```bash
# After Foundational (T003–T007) + T008 (engine records metrics), fan out:
Task: "Mount GET /metrics on REST mux + openapi (internal/rest, openapi.yaml)"
Task: "status --metrics render — CLI + MCP + REST/gRPC status block (internal/cli, internal/mcp, internal/rest, internal/grpc)"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (Setup: deps + config) → Phase 2 (Foundational: `internal/observe`)
2. Phase 3 (US1): instrument ops → wire daemon lifecycle → mount `/metrics` → `status --metrics` → test
3. **STOP and VALIDATE**: scrape `/metrics`, run `status --metrics` — latency/counts present
4. This alone closes the core ops gap (blind shipping → visible latency/errors)

### Incremental Delivery

1. Setup + Foundational → `observe` package ready
2. + US1 → `/metrics` + `status --metrics` (**MVP — operator visibility**)
3. + US2 → traces localize slow/failing ops
4. + US3 → opt-in remote export (air-gap verified)
5. Polish → tie-in counters (H04/H06), docs, overhead, final gates

---

## Notes

- `[P]` = different files, no deps on incomplete tasks
- `[Story]` maps the task to its user story for traceability
- Every story is independently completable and testable; stop at any checkpoint to validate
- Commit (Conventional Commits, straight to `main`) after each task or logical group
- Constitution gates (plan.md): **I** air-gap (no telemetry egress by default — the air-gap test is mandatory), **III** OTel is pure-Go Apache-2.0 (permitted; first non-trivial new dep — user-authorized), **IV** instrumentation <1% / within latency budgets
- `internal/observe` is the **only** package that imports OTel directly — keeps vendor coupling in one place (the engine imports `observe`'s helper API)
