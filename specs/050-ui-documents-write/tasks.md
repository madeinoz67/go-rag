# Tasks: go-rag Management Console — Documents Write-Actions (Slice 4)

**Input**: Design documents from `/specs/050-ui-documents-write/` — [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/ui-documents-write.md](./contracts/ui-documents-write.md), [quickstart.md](./quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓.

**Tests**: INCLUDED — go-rag is test-gated (`make test -race`, `make lint(0)`) and the constitution enforces "Spec/Test/Evals First". Every story + the new cross-transport operation ships a test task.

**Scope note**: this is the console's **first write surface** and its biggest slice. Add/reingest reuse the already-cross-transport `Engine.Add`/`Reprocess` (UI = 5th adapter, cheap). Remove is a **new operation** shipping cross-transport (engine + CLI + REST + gRPC + MCP + proto), per constitution V / spec 047 precedent — that is the bulk of the foundational phase.

**Organization**: Tasks grouped by user story. Research tags (R1–R10) cross-link to [research.md](./research.md); FR/SC to [spec.md](./spec.md).

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[USx]**: user-story phase tag (Setup/Foundational/Polish carry none)
- Every task names its exact file path + the symbol/seam it touches

## Path conventions

New: `internal/engine/delete.go` (+test), `internal/cli/delete.go`, `internal/rest/delete_document.go`, `internal/grpc/delete_document.go`, `internal/ui/documents_write.go` (+test). Edits: `proto/gorag.proto` (+regen `proto/gen`), `internal/mcp/server.go`, `internal/ui/ui.go`, `internal/ui/web/{static/js/app.js, static/css/components.css, templates/index.html}`.

---

## Phase 1: Setup (proto + UI skeleton)

**Purpose**: land the proto contract for the new DeleteDocument rpc + the UI write-handler skeleton so everything downstream compiles.

- [X] T001 Add the `DeleteDocument` schema to `proto/gorag.proto`: `message DeleteDocumentRequest { string doc_id = 1; }`, `message DeleteDocumentResponse {}`, and `rpc DeleteDocument(DeleteDocumentRequest) returns (DeleteDocumentResponse)` on the `Gorag` service. Regenerate `proto/gen/` via the spec-003 protoc invocation. Build clean. (R3)
- [X] T002 [P] Create `internal/ui/documents_write.go`: package comment + Slice-4 scope note; DTO structs per [data-model.md](./data-model.md) — `addRequestDTO{Path, Glob}`, `ingestSummaryDTO{New, Skipped, Errors, Path}`; empty `handleDocumentAdd` / `handleDocumentRemove` / `handleDocumentReingest` stubs so the package compiles before logic lands. (R1, R4)

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean; proto bindings carry `DeleteDocument`.

---

## Phase 2: Foundational (the new delete operation, cross-transport + UI write handlers)

**Purpose**: ship the new remove operation across every transport (constitution V) + the three UI write handlers every story renders against.

**⚠️ CRITICAL**: US2 (remove) needs `Engine.DeleteDoc`; all three stories need the UI write handlers + routes.

- [X] T003 Implement `Engine.DeleteDoc` — `internal/engine/delete.go`: `func (e *Engine) DeleteDoc(ctx context.Context, docID string) error` — thin wrapper that resolves the pipeline (`e.pipeline()`) and delegates to `Pipeline.DeleteDoc(docID)` (which holds the spec 044 per-doc lock and deletes from Pebble + the live FTS/Vector index). Index-only — never touches the source file. (R3, R6)
- [X] T004 `Engine.DeleteDoc` tests — `internal/engine/delete_test.go`: (a) delete by ID removes the doc + its chunks; (b) a subsequent query returns no hit (live index cleared — no phantoms); (c) unknown ID → `ErrNotFound`/not-found; (d) the source file on disk is unchanged. Mirror the existing `Pipeline.DeleteDoc` tests.
- [X] T005 [P] CLI projection — `internal/cli/delete.go`: `go-rag delete <docID>` subcommand (JSON + text), mirroring `newReprocessCmd`/`newAddCmd`; routes through `engine.NewWithDB` so parity holds. (R3; constitution V: CLI op → also MCP, satisfied by T008)
- [X] T006 [P] REST projection — `internal/rest/delete_document.go`: `DELETE /v1/documents/{id}` → `s.eng.DeleteDoc(ctx, id)`; 204 on success, 404 unknown id, 401 unauth. Register in the REST mux. (R3)
- [X] T007 [P] gRPC projection — `internal/grpc/delete_document.go`: `func (a *Adapter) DeleteDocument(ctx, *goragpb.DeleteDocumentRequest) (*goragpb.DeleteDocumentResponse, error)` delegating to `eng.DeleteDoc` (depends on T001 regen). (R3)
- [X] T008 [P] MCP projection — `internal/mcp/server.go`: add `renderDeleteDocument(eng, args)` + a `go_rag_delete_document` tool registration (arg: `doc_id`). (R3)
- [X] T009 Cross-transport delete parity test — `internal/engine/parity_test.go::TestCrossTransport_DeleteDocumentParity` (pattern of `TestCrossTransport_ListChunksParity`): against one engine, assert engine / CLI / REST / gRPC / MCP delete the same doc identically (doc + chunks gone, query clean). (R3, R10, FR-008)
- [X] T010 Implement the three UI write handlers — `internal/ui/documents_write.go`: `handleDocumentAdd` (decode `addRequestDTO`, validate path → `s.eng.Add(ctx, path, glob)` → `ingestSummaryDTO` 200); `handleDocumentRemove` (`s.eng.DeleteDoc(ctx, id)` → 204; 404 unknown); `handleDocumentReingest` (resolve the doc's source path via the document store/`Engine.GetDocument` → `s.eng.Reprocess(ctx, sourcePath)` → `ingestSummaryDTO` 200; 404 unknown id / vanished source). Errors via `writeEngineErr`/`writeError` (existing helpers, same package). (R1, R2, R4, R5, R6, R9)
- [X] T011 Register the write routes — `internal/ui/ui.go::Server.Handler`: `POST /api/documents` (add), `DELETE /api/documents/{id}` (remove), `POST /api/documents/{id}/reingest` (reingest), all guarded via `s.guard` (// spec 050). (R1)

**Checkpoint**: `Engine.DeleteDoc` live across engine + CLI + REST + gRPC + MCP; parity pinned; UI write routes wired (curl POST/DELETE works, 401 unauth, 404/400 errors). `make build && make vet` clean.

---

## Phase 3: User Story 1 — Add documents by path (Priority: P1) 🎯 MVP

**Goal**: an operator adds a document by server-side path from the browser; it ingests and appears.

**Independent Test**: [quickstart.md](./quickstart.md) §1 — add via the console; doc appears in the list + `go-rag status`; parity with `go-rag add`.

### Implementation

- [X] T012 [US1] Alpine add dialog — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `components.css`): an **Add** button + dialog (path text field + optional glob field, default empty); submits `POST /api/documents`; on 200, refresh the Documents list and show the new row (pending badge until embed completes); disable-on-submit; clear errors plainly. No file upload (path-based). (R4, R8)
- [X] T013 [US1] US1 tests — `internal/ui/documents_write_test.go`: (a) `POST /api/documents` 200 + `ingestSummaryDTO`; (b) parity — same doc ID as `go-rag add <path>`; (c) 400 empty path; (d) 401 without Bearer; (e) idempotent re-add (skipped); (f) directory + glob ingests all matches. (FR-008, SC-003)

**Checkpoint**: US1 independently testable — add from the browser (MVP).

---

## Phase 4: User Story 2 — Remove a document (Priority: P1)

**Goal**: an operator removes a document (index-only, source preserved) with confirmation.

**Independent Test**: [quickstart.md](./quickstart.md) §2 — remove via the console; doc gone from list/status/queries; source file intact.

### Implementation

- [X] T014 [US2] Alpine remove confirm — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html` (+ `components.css`): per-row **Remove** action → confirmation dialog naming the document + stating index-only deletion (source file preserved) → `DELETE /api/documents/{id}` on confirm; 204 → row removed; 404 handled; disable-on-submit. Never auto-proceeds without confirm. (R6, R7)
- [X] T015 [US2] US2 tests — `internal/ui/documents_write_test.go`: (a) `DELETE /api/documents/{id}` 204; (b) doc + chunks gone from list, `go-rag status`, and query results; (c) source file on disk unchanged; (d) 404 unknown id; (e) 401 without Bearer. (FR-002, FR-011, SC-002)

**Checkpoint**: US2 independently testable — removable documents.

---

## Phase 5: User Story 3 — Reingest a document (Priority: P2)

**Goal**: an operator reingests a document (re-derive chunks/embeddings) with confirmation.

**Independent Test**: [quickstart.md](./quickstart.md) §3 — reingest after a source change; chunks reflect new content; parity with `go-rag reprocess`.

### Implementation

- [X] T016 [US3] Alpine reingest confirm — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: per-row **Reingest** action → confirmation dialog naming the source path + stating dedup is bypassed → `POST /api/documents/{id}/reingest` on confirm; 200 → list refresh; 404 (unknown / vanished source) handled; disable-on-submit. (R5, R7)
- [X] T017 [US3] US3 tests — `internal/ui/documents_write_test.go`: (a) `POST /api/documents/{id}/reingest` 200 + summary; (b) parity — re-derived chunks match `go-rag reprocess <sourcePath>`; (c) 404 unknown id; (d) 404/409 source-not-found when the source vanished; (e) 401 without Bearer. (FR-003, FR-008, SC-003)

**Checkpoint**: US3 independently testable — reingestable documents.

---

## Phase 6: User Story 4 — Writes are guarded, confirmed, observable, shell-consistent (Priority: P2)

**Goal**: prove writes are auth-gated, destructive-ops confirmed, observable (audit + Operations), index-only, and no-Node.

**Independent Test**: [quickstart.md](./quickstart.md) §4 — 401/400/404 matrix; audit event + Operations backlog after a write; no Node artifacts; no source-file mutation.

### Implementation / Verification

- [X] T018 [US4] Guard + index-only + observability tests — `internal/ui/documents_write_test.go`: (a) every write route (`POST /api/documents`, `DELETE /api/documents/{id}`, `POST /api/documents/{id}/reingest`) → 401 without Bearer; (b) repo-root scan finds no `package.json`/`node_modules`; (c) after each write, an audit event is logged (spec 021) and the Operations backlog/activity reflects it (spec 049); (d) remove + reingest never modify the source file on disk (index-only). (FR-005, FR-007, FR-010, FR-011, SC-005, SC-006)
- [X] T019 [US4] Error / edge-state rendering — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: deliberate states for empty path, non-existent path, permission-denied, unknown id, vanished source, embedder-unreachable-during-add (write ACKs; pending surfaces in Operations), and session-expired 401 → login. Never a silent failure or partial mutation. (FR-009, FR-012)

**Checkpoint**: US4 independently testable — the invariants are pinned.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T020 [P] Gate hygiene — `make lint` (0 findings), `make vet`, `make test -race` clean across `internal/engine`, `internal/cli`, `internal/rest`, `internal/grpc`, `internal/mcp`, `internal/ui`. Independently re-run by the parent DA: build/vet/lint `0 issues`, `make test -race` all packages `ok` (internal/ui 276s 69.0% cov; cross-transport delete parity test passed).
- [X] T021 [P] quickstart validation — CLI write smoke on an isolated vault (add → new `go-rag delete` → gone, source intact → reprocess) all green; cross-transport delete parity pinned by `TestCrossTransport_DeleteDocumentParity`. **Live UI write-submission DEFERRED**: the pre-existing `serve --db-path <tmp>` isolation quirk (the flag is ignored; a fresh vault served global-scale data) makes a live daemon write-smoke unsafe — it would mutate the operator's real vault. The UI handlers are covered hermetically by `documents_write_test.go`; the Add dialog render was Interceptor-verified (path + glob + no-upload messaging). The isolation quirk is flagged as a separate daemon defect.
- [X] T022 [P] Doc sync — spec 046 Slice Decomposition updated (050 = Documents write-actions, shipped; Vaults moved to 054); `PROJECTS.md` + MuninnDB memory updated to reflect Slice 4 (first write surface) shipped; first-write-surface milestone + cross-transport delete noted.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: no deps. T001 (proto) blocks T007 (gRPC) + T009 (parity uses gRPC); T002 (UI skeleton) blocks T010 (handlers).
- **Foundational (Phase 2)**: T003 (Engine.DeleteDoc) → T004 (test); T003 blocks T005–T009 (transports/parity) + T010 (remove handler). T005–T008 (the four delete projections) are all `[P]` — fully parallel once T003 lands; T009 (parity) runs after them. T010 needs T003 (remove) but not T005–T009. T011 (routes) needs T010's handler symbols.
- **US1 (Phase 3)**: depends on Foundational (T010/T011). MVP gate.
- **US2 (Phase 4)**: depends on Foundational (T003 Engine.DeleteDoc + T010/T011).
- **US3 (Phase 5)**: depends on Foundational (T010/T011).
- **US4 (Phase 6)**: depends on US1–US3 (verifies them) + the cross-transport parity (T009).
- **Polish (Phase 7)**: depends on all stories complete.

### User-story independence
- US1 is the MVP gate — testable alone once the add handler + route land.
- US2 depends on the new `Engine.DeleteDoc` (foundational).
- US3 reuses US1's list-refresh; adds the reingest confirm.
- US4 is cross-cutting verification of US1–US3.

### Parallel opportunities
- Phase 1: T002 parallel with T001.
- Phase 2: T005–T008 (the four delete transport projections) are all `[P]` — fully parallel once T003 lands; T009 (parity) after them. T010 (UI handlers) parallel with the transport projections (different files, depends only on T003 for the remove handler).
- Story test tasks (T013, T015, T017) can run alongside their UI implementation tasks where files differ.
- US1 (T012) → US2 (T014) → US3 (T016) sequenced (all edit `app.js`/`index.html`).

---

## Parallel Example: Phase 2 (the new delete operation across transports)

```bash
Task: "CLI delete        in internal/cli/delete.go"                      # T005
Task: "REST delete       in internal/rest/delete_document.go"             # T006
Task: "gRPC delete       in internal/grpc/delete_document.go"             # T007
Task: "MCP delete        in internal/mcp/server.go (go_rag_delete_document)" # T008
# all parallel once Engine.DeleteDoc (T003) lands; T009 (parity) runs after.
```

---

## Implementation Strategy

### MVP First
1. Complete Phase 1 (Setup) + Phase 2 (Foundational) — `Engine.DeleteDoc` live across all transports; UI write routes wired.
2. Complete Phase 3 (US1 — add). **STOP and VALIDATE**: add via the console matches `go-rag add` (quickstart §1). This is the **MVP gate** — the first working write.
3. Complete Phase 4 (US2 — remove) — the **demo-complete** point: add → remove lifecycle from the browser, plus the cross-transport delete. The euphoric-surprise moment (the console becomes a tool, not a viewer).
4. Phase 5 (US3) + Phase 6 (US4) + Phase 7 (Polish) add reingest + harden + verify.

### Incremental delivery
- Setup → Foundational → US1 (MVP) → US2 (demo) → US3 → US4 → Polish.
- Each checkpoint is independently testable per its Independent Test.

### Single-author note
This repo commits straight to `main` (CLAUDE.md). Commit after each task or logical group; run `make lint && make test -race` before push.

---

## Notes

- Add/reingest are cheap (reuse cross-transport `Engine.Add`/`Reprocess`); remove is the bulk (new op, cross-transport). This is why the slice is 047-sized, not 048/049-sized.
- Cross-transport parity is the proof (FR-008) — add ≡ `go-rag add`, reingest ≡ `go-rag reprocess`, delete pinned across all five transports (T009).
- Confirmation is client-side UX (R7) — the server executes guarded mutations; no server-side confirm token.
- Remove is **index-only** (FR-011) — the source file on disk is never modified or deleted.
- Vendoring (not building) remains the constraint: no Node/Vite/Tailwind, single binary (FR-010).
- No tags on add (`Engine.Add` has no tags param); tags come from enrichment (spec 029).
