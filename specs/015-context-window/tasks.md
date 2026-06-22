---

description: "Task list for H15 — Context window / sibling expansion"
---

# Tasks: Context Window — Sibling-Chunk Expansion (H15)

**Input**: Design docs from `/specs/015-context-window/` — plan.md, spec.md, research.md, data-model.md, contracts/context-window.md, quickstart.md

**Organization**: Foundational phase populates the linked list + adds the engine context-expansion. US1 = expansion correctness, US2 = distinguishability + no-ranking-change, US3 = cross-transport parity.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup

- [x] T001 Confirm green starting state: `make build && make vet && make test` from repo root

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: No user story work until this phase is complete.

- [x] T002 [P] Populate the linked list in `internal/pipeline/pipeline.go`: after the chunk-construction loop (`chunks := make(...)`), add a loop setting `chunks[i].PreviousChunkID = chunks[i-1].ID` for i>0 and `chunks[i].NextChunkID = chunks[i+1].ID` for i<len-1. This runs before `storeDocument` persists the chunks. Independent
- [x] T003 In `internal/engine/types.go`: add `ContextWindow int` to `QueryRequest`; add `type ContextChunk struct { ChunkID string; Content string; Direction string }` and `Context []ContextChunk` field (omitempty) to `QueryHit`. Depends T002
- [x] T004 In `internal/engine/query.go`: after the result-building loop (which produces `out []QueryHit`), if `req.ContextWindow > 0`, iterate each hit and follow the chunk's `PreviousChunkID`/`NextChunkID` chain up to N steps each way, fetching sibling text via `lookupChunk(e.db, siblingID)`; attach as `hit.Context` (Direction "previous"/"next"). Missing siblings (empty IDs, boundary chunks) → skip gracefully. Depends T002, T003

**Checkpoint**: linked list populated + context expansion in the engine. `make build && make vet && make test` green (no callers set ContextWindow yet → 0 → nil context → unchanged behavior).

---

## Phase 3: User Story 1 — Hit includes surrounding context (Priority: P1) 🎯 MVP

**Goal**: Query with ContextWindow=N → each hit augmented with up to N prev + N next siblings. Boundaries handled gracefully.

**Independent Test**: Ingest a multi-chunk doc; query with ContextWindow=1; assert hits include sibling text.

### Tests for User Story 1

- [x] T005 [US1] Add `internal/engine/context_window_test.go` (package `engine`): ingest a multi-chunk document; query with `ContextWindow=1`; assert (a) each hit's `Context` has up to 1 previous + 1 next entry with correct `Direction` and non-empty `Content`; (b) the first chunk's hit has no "previous" context; (c) the last chunk's hit has no "next" context. Also verify `ContextWindow=0` → `Context` is nil (today's behavior). Depends T004

**Checkpoint**: US1 functional — context expansion proven, boundaries handled.

---

## Phase 4: User Story 2 — Context distinguishable + no ranking change (Priority: P2)

**Goal**: Context is a separate field, not ranked hits; top-k count unchanged.

**Independent Test**: Same query with/without ContextWindow; assert hit count identical.

### Tests for User Story 2

- [x] T006 [US2] Add to `internal/engine/context_window_test.go`: query with ContextWindow=2; assert (a) the number of ranked hits (top-k) is identical to the same query with ContextWindow=0; (b) context chunks do NOT appear in the ranked list; (c) context chunks' `ChunkID`s differ from the hit's `ChunkID`. Depends T004

**Checkpoint**: US2 functional — context is additive, not ranking-affecting.

---

## Phase 5: User Story 3 — Cross-transport parity (Priority: P2)

**Goal**: ContextWindow exposed on CLI/REST/gRPC/MCP; same value → identical context.

### Implementation for User Story 3

- [x] T007 [P] [US3] CLI in `internal/cli/query.go`: add `--context-window` flag (int, default 0); pass to `QueryRequest.ContextWindow`. Depends T004
- [x] T008 [P] [US3] REST in `internal/rest/types.go` + `internal/rest/engine_adapter.go`: add `context_window` request field + `context` response field on the hit DTO; map to `QueryRequest.ContextWindow`. Depends T004
- [x] T009 [P] [US3] MCP in `internal/mcp/server.go`: `context_window` in go_rag_query inputSchema + renderQuery; display context in the result text. Depends T004
- [x] T010 [US3] gRPC in `proto/gorag.proto`: `int32 context_window = 10` on QueryRequest + `repeated ContextChunk context = 7` on QueryHit (new message `ContextChunk { string chunk_id, string content, string direction }`); regen `proto/gen/`; map in `internal/grpc/engine_adapter.go`. Depends T004
- [x] T011 [US3] Cross-transport parity test: same query + context_window over CLI/REST/gRPC/MCP → identical context. Depends T007–T010

**Checkpoint**: US3 functional — all four transports express + return context.

---

## Phase 6: Polish

- [x] T012 Run `make build && make vet && make test && make test-eval`; `golangci-lint`/`govulncheck` (env-note)
- [x] T013 Run `quickstart.md` scenarios 1–7
- [x] T014 Mark **H15** complete in `RAG_BOOK_AUDIT_BACKLOG.md` (`- [ ]` → `- [x]` + `✅ COMPLETE (spec 015): …`). Depends on T005–T011 green + T012 passing
- [x] T015 Commit to `main` with Conventional Commits (`feat(retrieval): context window / sibling expansion (H15)`) + push

---

## Dependencies

- **Phase 2**: T002 (independent) → T003 → T004.
- **US1**: T005 depends T004.
- **US2**: T006 depends T004.
- **US3**: T007/T008/T009 [P] + T010 depend T004; T011 depends T007–T010.
- **Polish**: T014 depends on green stories + gate; T015 last.

### Critical path

`T001 → T002 → T004 → (US1/US2/US3) → T012 → T014 → T015`

### Parallel

- Phase 5: T007 (CLI), T008 (REST), T009 (MCP) → different files, parallel. T010 (gRPC/proto) → own chain.
- Phase 3/4: T005 and T006 are both in `context_window_test.go` (same file) → sequential.

---

## Notes

- Same-file tasks unmarked `[P]`.
- **Re-ingestion needed** for existing vaults (linked list was unpopulated; old chunks have empty Previous/Next → context expansion returns nothing gracefully). Document this in the commit message.
- **Proto regen** (T010): same `protoc` command as spec 009. New message `ContextChunk` + `context_window` field 10 + `context` field 7 on QueryHit.
- `golangci-lint`/`govulncheck` may be unavailable — env note.
