# Tasks: Embedding Model/Dimension Mismatch Validation

**Input**: Design documents from `/specs/005-embedding-dim-validation/`

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅, quickstart.md ✅, constitution.md ✅

**Tests**: INCLUDED. The guard is a **safety invariant** — silent corruption must
become a loud error, and that property MUST be verified (FR-001/FR-003/FR-005/SC-001
are testable guarantees). The constitution mandates `go test ./...` green. Tests
are intrinsic here, not optional.

**Organization**: Grouped by user story (US1/US2/US3 from spec.md); a shared
foundational layer (corpus profile + length guard) blocks the stories.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: Which user story (US1/US2/US3); setup/foundational/polish have NO story label
- All paths project-relative; pure Go, `CGO_ENABLED=0` (Principle III); no new deps, no schema change

---

## Phase 1: Setup (Shared Foundation)

**Purpose**: The shared `CorpusProfile` helper that both the query guard and the
status drift view derive from. No new package — one helper file in `internal/engine`.

- [x] T001 Create the `CorpusProfile` type and derivation helper in `internal/engine/embedding_profile.go`: read Pebble prefix-0x04 Embedding records in their persisted `{Model, Vector}` shape, tally per-model and per-dim (`len(Vector)`) counts, and expose `MajorityModel`, `MajorityDim`, `ModelCounts`, `DimCounts`, `Total`, and `Consistent` (true iff one model + one dim). Pure Go, no network.

**Checkpoint**: Profile derives correctly from records (unit-tested in T003/T010); `go build ./...` green.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The per-vector length guard that stops silent garbage cosine — the
core of H03. Blocks every user story.

**⚠️ CRITICAL**: No user-story work can begin until the length guard lands.

- [x] T002 Add the length guard to `Vector.Query` in `internal/index/vector.go`: when scoring, **skip** any stored vector whose length ≠ the query vector's length and **count** the skips; never call `cosine()` on a mismatched-length pair (no silent `min(len)` truncation to garbage). Expose the skipped count so callers (US3) can report it; corrupted/odd-length entries are skipped, never panic.
- [x] T003 Foundational tests for the length guard in `internal/index/vector_test.go`: (a) mismatched-length stored vectors are skipped + counted, not garbage-scored; (b) equal-length happy path scores normally; (c) a corrupted odd-length entry is skipped without panic.

**Checkpoint**: `cosine()` can no longer produce a score from a length mismatch; the guard is independently tested.

---

## Phase 3: User Story 1 — Refuse a Mismatched Query (Priority: P1) 🎯 MVP

**Goal**: A query whose embedding **model or dimensionality** doesn't match the
corpus majority is refused with a clear error naming expected vs actual — never
plausible-but-wrong results, never a panic.

**Independent Test**: Ingest under model A, switch config to model B (different
dim or name) without re-indexing, query → fails with a mismatch message; happy-path
query (matching model) is unaffected.

### Implementation for User Story 1

- [x] T004 [US1] Add the refuse check to `engine.Query` in `internal/engine/query.go`: derive the `CorpusProfile` (T001), compare the active embedder's `Model()` and dimensionality (use `embedder.Dimensions()`, embedding the query to populate it if 0) against the profile's majority model + dim; on model **or** dim mismatch return a sentinel `ErrEmbeddingMismatch` (carrying expected vs actual) and no hits. Empty corpus → not an error. Define the sentinel + the `errors.Is`-able variable in `internal/engine/`.
- [x] T005 [US1] Surface the sentinel mismatch error across every query transport with identical message text: CLI `go-rag query` prints it + non-zero exit; REST and gRPC return their native error carrying the message; MCP `go_rag_query` returns the JSON-RPC error with the message — in `internal/cli/query.go`, `internal/rest/`, `internal/grpc/`, `internal/mcp/server.go`
- [x] T006 [US1] US1 tests in `internal/engine/query_test.go`: (a) refuse on a **dimensionality** mismatch; (b) refuse on a **same-dimensionality-different-model** mismatch; (c) happy-path matching query is unaffected (no false alarm, normal results); (d) empty-corpus query returns no results without error. Use injected embedders of differing model/dim — no Ollama.
- [x] T007 [US1] Validate US1 via quickstart Scenarios A (refuse dim mismatch), B (refuse same-dim-diff-model), and C (happy path) in `specs/005-embedding-dim-validation/quickstart.md`; confirm `make build && make vet && make test` green.

**Checkpoint**: User Story 1 fully functional — silent embedding mismatch is now a loud, identifiable error across all transports. This is the shippable MVP.

---

## Phase 4: User Story 2 — Status Drift Visibility (Priority: P2)

**Goal**: An operator can see the corpus's stored embedding model + dimensionality
and any inconsistency in status, **without querying**.

**Independent Test**: After a partial migration, `go-rag status` reports the stored
majority model/dim and a drift flag with per-model counts.

### Implementation for User Story 2

- [x] T008 [US2] Extend `Status`/`StatusInfo` in `internal/engine/status.go` and `internal/engine/types.go`: derive from `CorpusProfile` (T001) so `EmbeddingModel` reports the **stored** majority model (not the configured string), `Dimensions` the stored majority dim, and add `EmbeddingDrift` (bool), `ModelCounts`, `DimCounts`. Empty corpus → no model/dim, no drift.
- [x] T009 [US2] Render the drift view across surfaces: CLI `go-rag status` prints majority model/dim and, when `EmbeddingDrift`, a line with per-model counts; MCP `go_rag_status` / REST / gRPC surface the new fields — in `internal/cli/status.go`, `internal/mcp/server.go`, `internal/rest/`, `internal/grpc/`
- [x] T010 [US2] US2 tests in `internal/engine/status_test.go`: (a) consistent corpus → `EmbeddingDrift=false`, single model/dim; (b) mixed corpus → `EmbeddingDrift=true` with correct per-model/dim counts; (c) status reports the **stored** majority, not the configured model.
- [x] T011 [US2] Validate US2 via quickstart Scenario D (status drift) in `specs/005-embedding-dim-validation/quickstart.md`.

**Checkpoint**: User Stories 1 AND 2 both work independently — refusal at query time + proactive drift visibility.

---

## Phase 5: User Story 3 — Graceful Partial Degradation (Priority: P3)

**Goal**: A mixed (mid-migration) corpus queried by the **majority** model scores
only matching vectors (minority skipped+counted) and does not fail; querying under
the **minority** model is refused.

**Independent Test**: 80% of vectors under model A, 20% under model B; query under
A → only A scored, B skipped+warned, no failure; query under B → refused.

### Implementation for User Story 3

- [x] T012 [US3] Wire the partial verdict in `internal/engine/query.go`: when the query matches the majority model+dim but the profile is `!Consistent`, let `Vector.Query` (T002) skip the minority by length, attach the `skipped` count to the result, and log a warning; ensure a query that matches only the **minority** still hits the US1 refuse path (it doesn't match the majority).
- [x] T013 [US3] US3 tests in `internal/engine/query_test.go`: (a) majority query over a mixed corpus → only majority vectors scored, minority skipped with `skipped > 0`, no error; (b) minority-model query over the same corpus → refused (US1 path); (c) a fully-consistent corpus never reports skips.
- [x] T014 [US3] Validate US3 via quickstart Scenario E (graceful mid-migration) in `specs/005-embedding-dim-validation/quickstart.md`.

**Checkpoint**: All three user stories independently functional; the corpus degrades gracefully through a migration instead of all-or-nothing.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Cross-transport parity proof, final validation, and closing the loop
on the audit backlog.

- [x] T015 [P] Cross-transport parity test for the refuse error (quickstart Scenario F): assert the mismatch message text is identical across CLI, REST, gRPC, and MCP for the same mismatched query — in `internal/engine/parity_test.go` (or the existing parity harness).
- [x] T016 Final validation: run `make build && make vet && make test && make lint` green; run the full quickstart (Scenarios A–F); confirm no new third-party deps (`go mod tidy` clean) and happy-path query latency preserved (the guard is O(1) after profile derivation).
- [x] T017 **Tick off H03 completion in `RAG_BOOK_AUDIT_BACKLOG.md`**: mark the H03 row done (reference `specs/005-embedding-dim-validation/`) and update the backlog intro/status to reflect H03 is shipped — the user's explicit closing task.
- [x] T018 [P] Commit under Conventional Commits (`fix(embed): …` for the silent-corruption guard) and confirm `cliff.toml` changelog generation picks it up.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately (T001, the profile helper).
- **Foundational (Phase 2)**: Depends on Setup; **BLOCKS all user stories**. T002→T003 (guard then its tests).
- **User Stories (Phase 3–5)**: All depend on Foundational completion.
  - **US1** is the MVP and must complete first (US3's "partial" verdict builds on US1's refuse check + T002's skip).
  - **US2** depends only on T001 (the profile) — independent of US1.
  - **US3** depends on US1 (refuse check) + T002 (skip) — last.
- **Polish (Phase 6)**: Depends on the stories being complete; T017 (backlog tick-off) runs last.

### User Story Dependencies

- **US1 (P1)**: After Foundational. **MVP.**
- **US2 (P2)**: After Setup (T001) only — can proceed in parallel with US1.
- **US3 (P3)**: After US1 (T004 refuse check) + Foundational (T002 skip).

### Within Each User Story

- Helper/types before the query-path change; query path before transport surfacing; tests after implementation; quickstart validation last.
- Story complete and independently testable before the next priority.

### Parallel Opportunities

- Foundational: T003 (guard tests) can be written alongside/after T002.
- US1: T005 (transport surfacing) fans out across 4 transport files in parallel once T004 lands.
- US2: T009 (status rendering) fans out across surfaces once T008 lands.
- Polish: T015 (parity test) and T018 (commit) run in parallel; T017 is last.

---

## Parallel Example: US1 + US2

```bash
# Once Foundational (T001–T003) is done, US1 and US2 can proceed together:
# US1 chain (query-time refusal):
Task: "Refuse check in engine.Query (T004)"   # then T005 (transports), T006 (tests), T007 (validate)
# US2 chain (status drift) — depends only on T001:
Task: "Extend Status/StatusInfo (T008)"        # then T009 (render), T010 (tests), T011 (validate)

# Within US1, surface the sentinel across transports in parallel:
Task: "CLI refuse error in internal/cli/query.go"      # T005
Task: "REST/gRPC/MCP refuse error in their adapters"   # T005
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 (profile helper) → Phase 2 (length guard + tests).
2. Phase 3 (US1): refuse check → transport surfacing → tests → quickstart A/B/C.
3. **STOP and VALIDATE**: US1 independently — a mismatch is refused loudly across all transports, happy path unaffected. This alone closes the H03 silent-corruption blind spot.
4. Demo / merge as MVP.

### Incremental Delivery

1. Setup + Foundational → length guard in place (no more garbage cosine).
2. + US1 → mismatch refused at query time (MVP — the silent killer is closed).
3. + US2 → drift visible in status (proactive).
4. + US3 → graceful mid-migration (resilient).
5. Polish → parity proof, full quickstart, **tick off H03 in the backlog** (T017).

### Notes

- No schema change: the guard reads already-persisted `{Model, Vector}` provenance.
- The profile derivation rides the existing per-query load scan (no new O(N)); H01's future index cache will host it for free.
- `cosine()`'s `min(len)` body stays only as a never-triggered safety net; the guard guarantees equal lengths on the scored path.
- Commit after each task or logical group (Conventional Commits).
- Stop at any checkpoint to validate the story independently.
