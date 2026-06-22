---

description: "Task list for H14 — Metadata filtering at retrieval"
---

# Tasks: Metadata Filtering at Retrieval (H14)

**Input**: Design docs from `/specs/014-retrieval-filter/` — plan.md, spec.md, research.md, data-model.md, contracts/query-filter.md, quickstart.md

**Tests**: Constitution mandates green tests; H14's value (scoped queries + parity) is only provable through tests; eval is the no-regression gate (SC-004).

**Organization**: Foundational phase builds the Filter type + Retrieval pre-fusion application + Engine wiring. US1 = scoping correctness, US2 = pre-fusion efficiency, US3 = cross-transport parity.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup

- [x] T001 Confirm green starting state: run `make build && make vet && make test` from repo root

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: No user story work until this phase is complete.

- [x] T002 [P] Create `internal/index/filter.go`: `type Filter struct { Source string; Type string; Tags []string }` with `func (f Filter) Empty() bool` (true when all dimensions unset) and `func (f Filter) Matches(filePath, fileType string, docTags []string) bool` (conjunction: Source = glob via `path.Match` against filePath if non-empty; Type = case-insensitive exact if non-empty; Tags = ALL-present in docTags if non-empty). Independent
- [x] T003 In `internal/index/retrieval.go`: add a `keep func(string) bool` parameter to `Search` and `SearchWithRerank` (nil = no filter); apply it to the FTS candidate list and the Vector candidate list BEFORE `reciprocalRankFusion` — drop any chunkID where `keep != nil && !keep(chunkID)`. In `attemptRerank`, pass `keep` to `Search` so the candidate pool is filtered. Depends T002
- [x] T004 In `internal/engine/types.go` add `Filter *index.Filter` to `QueryRequest`; in `internal/engine/query.go` build a `keep func(string) bool` closure from `req.Filter` (if non-nil/non-empty) that resolves chunkID → `lookupDoc(e.db, c.DocumentID)` → checks `Filter.Matches(d.FilePath, d.FileType, tagsFromMetadata(d.Metadata))`; pass to `r.SearchWithRerank`. If `req.Filter == nil || req.Filter.Empty()` → keep=nil (today's behavior, zero overhead). Depends T002, T003

**Checkpoint**: Foundation ready — filter types + pre-fusion application + engine wiring. `make build && make vet && make test` green (no callers pass a filter yet → keep=nil everywhere → unchanged behavior).

---

## Phase 3: User Story 1 — Scope a query to a document subset (Priority: P1) 🎯 MVP

**Goal**: A filtered query returns only matching docs; matches-nothing → empty; unfiltered → identical to today.

**Independent Test**: Ingest docs with known attributes; query with source/type/tags filter; assert only matching docs appear.

### Tests for User Story 1

- [x] T005 [US1] Add `internal/index/filter_test.go`: unit-test `Filter.Matches` — (a) glob match on filePath (`docs/**` matches `docs/a.md`); (b) exact type match (`.md` matches FileType `markdown`); (c) tag conjunction (ALL tags); (d) `Empty()` when all dimensions unset; (e) empty dimensions ignored; (f) no-match when doc has no tags. Depends T002
- [x] T006 [US1] Add `internal/engine/filter_test.go` (package `engine`, using `NewWithEmbedder` + a fake embedder): ingest docs with distinct source/type/tag attributes; query with (a) source filter → only matching-source docs; (b) type filter → only matching-type docs; (c) tags filter → only tagged docs; (d) filter matching nothing → empty result; (e) no filter → byte-identical to today's behavior. Depends T004

**Checkpoint**: US1 functional — filter scoping + opt-in default proven.

---

## Phase 4: User Story 2 — Efficient pre-fusion pruning (Priority: P2)

**Goal**: Non-matching chunks never reach fusion/collapse/rerank.

**Independent Test**: Verify the candidate lists fed to RRF are filtered.

### Tests for User Story 2

- [x] T007 [US2] Add a structural test in `internal/index/retrieval_test.go` (or `filter_test.go`): call `Search` with a `keep` predicate that rejects specific chunkIDs; assert the rejected chunks do NOT appear in the fused results (they were pruned pre-fusion). Also assert nil `keep` passes all chunks (unchanged behavior). Depends T003

**Checkpoint**: US2 functional — pre-fusion pruning proven structurally.

---

## Phase 5: User Story 3 — Cross-transport parity (Priority: P2)

**Goal**: Same filter over CLI/REST/gRPC/MPC → identical results. CLI `--source` wired (currently dead).

**Independent Test**: Same query+filter on all four transports → identical results.

### Implementation for User Story 3

- [x] T008 [P] [US3] Wire CLI flags in `internal/cli/query.go`: read the existing `--source` flag (currently dead) + add `--type` (string) and `--tags` (comma-separated → `[]string`); build `QueryRequest.Filter` from them when any is non-empty. Depends T004
- [x] T009 [P] [US3] Add filter fields to REST in `internal/rest/types.go` (`Source/Type/Tags` on `queryRequest`) and `internal/rest/engine_adapter.go` (map to `QueryRequest.Filter`). Depends T004
- [x] T010 [P] [US3] Add filter to MCP in `internal/mcp/server.go`: `source`/`type`/`tags` in the `go_rag_query` inputSchema + read `args["source"]`/`args["type"]`/`args["tags"]` in `renderQuery` → `req.Filter`. Depends T004
- [x] T011 [US3] Add filter fields to gRPC proto (`proto/gorag.proto`: `string source = 7; string type = 8; repeated string tags = 9;`) + regenerate `proto/gen/` (protoc command from spec 009) + map in `internal/grpc/engine_adapter.go` (`req.GetSource()`/`req.GetType()`/`req.GetTags()` → `QueryRequest.Filter`). Depends T004
- [x] T012 [US3] Add or extend a cross-transport parity test asserting the same filter produces identical rankings over CLI, REST, gRPC, and MCP. Depends T008–T011

**Checkpoint**: US3 functional — all four transports express the filter; parity proven.

---

## Phase 6: Polish & Cross-Cutting

- [x] T013 Run `make build && make vet && make test && make test-eval`; `golangci-lint`/`govulncheck` (env-note if unavailable)
- [x] T014 Run `quickstart.md` scenarios 1–8 (hermetic; no real Ollama)
- [x] T015 Mark **H14** complete in `RAG_BOOK_AUDIT_BACKLOG.md` (`- [ ]` → `- [x]` + `✅ COMPLETE (spec 014): …` annotation). Depends on T005–T012 green + T013 passing
- [x] T016 Commit to `main` with Conventional Commits (`feat(retrieval): metadata filtering at retrieval (H14)`) + push

---

## Dependencies

- **Phase 2**: T002 (independent) → T003 → T004 (sequential, same chain).
- **US1**: T005 depends T002; T006 depends T004.
- **US2**: T007 depends T003.
- **US3**: T008/T009/T010 [P] depend T004; T011 depends T004; T012 depends T008–T011.
- **Polish**: T015 depends on green stories + gate; T016 last.

### Critical path

`T001 → T002 → T003 → T004 → (US1/US2/US3) → T013 → T015 → T016`

### Parallel

- Phase 5: T008 (CLI), T009 (REST), T010 (MCP) are different files → parallel; T011 (gRPC/proto) is its own chain.
- Phase 3: T005 (`filter_test.go`) and T006 (`filter_test.go` engine) are different packages → parallel.

---

## Notes

- Same-file tasks unmarked `[P]`.
- **`FTS.Search`/`Vector.Query` signatures unchanged** — the filter is applied at the Retrieval layer (pre-fusion), not inside the index primitives.
- **Proto regen** (T011): same `protoc` command as spec 009.
- **`Document` has no `Tags` field** — tags live in `Metadata["tags"]`; the plan's `tagsFromMetadata` helper extracts them. No schema change.
- **CLI `--source` is currently dead** (declared in `query.go` but never threaded to `QueryRequest`) — H14 wires it (T008).
- `golangci-lint`/`govulncheck` may be unavailable — record as env note.
