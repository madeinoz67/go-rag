---
description: "Task list for spec 036 — Chunk Wikilink Metadata (BL-004)"
---

# Tasks: Chunk Wikilink Metadata (BL-004)

**Input**: Design documents from `/specs/036-chunk-wikilink-metadata/` — `spec.md` (3 user stories, FR-001..FR-014), `plan.md`, `research.md` (R1–R7), `data-model.md`, `contracts/api.md`, `quickstart.md`.

**Prerequisites**: `plan.md` (required), `spec.md` (required), `research.md`, `data-model.md`, `contracts/api.md` all present and reconciled (post-`/speckit-clarify`, 2026-06-30).

**Tests**: INCLUDED — not optional for this project. The go-rag constitution mandates `go build`, `go vet`, `go test` all green on every change ("the repository is never left red"), and the design docs name the test surfaces (`markdown_test.go` grammar, `parity_test.go` cross-transport). Tests are interleaved per story.

**Workflow**: single-author repo, commits to `main` (no branches/PRs). After each checkpoint: `make build && make vet && make test && make lint`, Conventional Commit, push.

**Organization**: grouped by user story (US1 P1 = MVP, US2 P2 = chunk-scoped/deterministic, US3 P3 = all transports/formats). Foundational phase holds the shared data-model + contract changes every story needs.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable — different file, no dependency on an incomplete task in the same phase.
- **[USx]**: user-story phase label.
- Every task names an exact file path.

---

## Phase 1: Setup

**Purpose**: confirm a green baseline before the feature lands. No new project structure, dependencies, or packages (go-rag exists; BL-004 adds no deps — Constitution Principle III).

- [X] T001 Verify baseline `make build && make vet && make test` is green on `main` before starting; fix any pre-existing failure first so the repo is never red on this feature's account.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the shared data-model + wire-contract changes that EVERY user story depends on. **No story work begins until this phase is green.**

- [X] T002 [P] Add the `Wikilinks []string` non-identity sidecar field to `Chunk` in `internal/model/model.go` — `json:"wikilinks,omitempty"`, with a doc comment mirroring `SectionContext` (states: non-identity, absent for no-link/pre-feature, surfaced on `QueryHit`/`GetChunk` across all transports). [FR-008, FR-010, FR-012; R2]
- [X] T003 [P] Add the transient `WikilinkSpan{Target string; Offset int}` type in `internal/reader/markdown.go` — doc comment noting it is transient (consumed + dropped by the pipeline before identity/store, mirroring `HeadingSpan`). [R3]
- [X] T004 [P] Add two additive gRPC fields in `proto/gorag.proto`: `repeated string wikilinks = 17;` on `message Chunk` (after `created_at = 16`) and `repeated string wikilinks = 13;` on `message QueryHit` (after `enrichment_status = 12`), each with a `spec 036 / BL-004` comment. [FR-009; R7; contracts/api.md]
- [X] T005 Regenerate `proto/gen/gorag.pb.go` from the updated `proto/gorag.proto` (the repo's `go generate` / protoc step). (depends T004)
- [X] T006 Wire model↔proto mapping for `Wikilinks` in `internal/grpc` — project `Chunk.Wikilinks` ↔ proto `Chunk.wikilinks` and `engine.QueryHit.Wikilinks` ↔ proto `QueryHit.wikilinks`, beside the existing `section_context` mapping. (depends T002, T005)

**Checkpoint**: Foundation ready — the field exists on the model, the proto carries it, gRPC maps it. User-story work can begin.

---

## Phase 3: User Story 1 — A bridge client reads a chunk's wikilinks (Priority: P1) 🎯 MVP

**Goal**: a client reads a chunk and receives its canonical `[[wikilink]]` target set, with the grammar from the clarifications applied.

**Independent Test**: ingest a markdown doc with `[[authentication]]` and `[[JWT tokens]]`; fetch a chunk via `GetChunk` (REST) and assert `Wikilinks` contains both canonical targets. A no-link chunk returns the field absent/empty. (`quickstart.md` Scenario 1.)

### Implementation for User Story 1

> **Implementation-time refinement (found executing T007–T008):** `WikilinkSpan.Offset` MUST be in the reader's **stripped-text** space (same as `HeadingSpan`) so the pipeline's `redact.TranslateOffset` applies. Because `normalizeObsidian` substitutes `[[…]]`→display text *before* `stripMarkdownSpans` runs, detection **cannot stay in `normalizeObsidian`**. Refined approach: move wikilink detection+substitution into the code-fence-aware `stripMarkdownSpans` pass — reuses `inCode` for FR-014 (code-context exclusion) for free, records `WikilinkSpan.Offset` in stripped space as `b.Len()` advances, and preserves byte-identity (unchanged `chunk_id` for every existing chunk) by applying the *identical* `linkDisplay` substitution. **Ordering constraint:** extract `linkTarget` *before* `stripInlineEmphasis` so emphasis markers inside a target aren't dropped. This supersedes the "in the `reObsidianLink` callback" wording of T007/T008 — detection lives in `stripMarkdownSpans`, `normalizeObsidian` keeps only the `![[…]]` embed pass. Verify against `TestMarkdownReader_ObsidianWikilinks` + the spec-025 byte-identity tests.

- [ ] T007 [US1] Add plain-wikilink collection to `normalizeObsidian` in `internal/reader/markdown.go`: in the `reObsidianLink` callback, capture `linkTarget(inner)` + the match's byte offset into a `[]WikilinkSpan`. Reuse `linkTarget()` — do NOT introduce a second parser. Grammar via `linkTarget`: alias `[[a|b]]`→`a`, anchor `[[a#h]]`/`[[a#^id]]`→`a`, path `[[concepts/auth]]`→`concepts/auth` (preserved), dangling `[[phantom]]`→included, malformed `[[]]`/`[[a|]`→non-empty targets only, de-dup first-occurrence. Embeds `![[…]]` and note-transclusions stay excluded (handled by the separate `reObsidianEmbed` pass). [FR-001, FR-002, FR-003, FR-004, FR-013; Q1, Q4, Q5]
- [ ] T008 [US1] Make the wikilink collection code-fence-aware in `internal/reader/markdown.go`: exclude `[[…]]` inside fenced code blocks and inline code by tracking `inCode` state (reuse the `stripMarkdownSpans` pattern); emit the collected spans as `md["wikilink_spans"]` from `Read`. [FR-014; Q3] (depends T007, same file)
- [ ] T009 [US1] Populate `Chunk.Wikilinks` in the ingest pipeline (`internal/pipeline`): resolve `wikilink_spans` to each chunk by **offset containment** (span belongs to the chunk whose `[StartCharIdx, EndCharIdx)` contains `span.Offset`; `Offset` = index of the opening `[`), de-duplicated first-occurrence; **delete `wikilink_spans` from document metadata before `GenerateID`/store** (identity safety — mirror the `heading_spans` drop). [FR-001, FR-005, FR-006, FR-010, FR-012; R3] (depends T003)
- [ ] T010 [P] [US1] Add `Wikilinks []string` to `engine.QueryHit` in `internal/engine/types.go` and copy `chunk.Wikilinks → hit.Wikilinks` in `internal/engine/query.go` beside the `SectionContext` copy. [FR-009] (depends T002)
- [ ] T011 [US1] Surface `Wikilinks` on the spec 035 `GetChunk` result in `internal/engine/get_chunk.go` — the chunk projection already carries every `Chunk` field; verify it flows and plumb if needed. [FR-009] (depends T002)
- [ ] T012 [P] [US1] Render `Wikilinks` on REST in `internal/rest/types.go` — add `Wikilinks []string \`json:"wikilinks,omitempty"\`` to `queryHit` and to the `GET /v1/chunks/{id}` chunk response shape. [FR-008, FR-009] (depends T002, T005)

### Tests for User Story 1

- [ ] T013 [P] [US1] Reader grammar tests in `internal/reader/markdown_test.go`: `[[a|b]]`→`a`; `[[a#h]]`→`a`; `[[a#^id]]`→`a`; `[[concepts/auth]]`→`concepts/auth`; `![[image.png]]` excluded; `![[Note]]` transclusion excluded (stays in `transcludes`); `[[phantom]]` included verbatim; `[[]]`/`[[a|]` produce no empty target; `[[x]]` inside a fenced block and inside inline code excluded; `[[a]]…[[a]]` de-duped to one. [FR-001..FR-004, FR-013, FR-014; Q1, Q3, Q4, Q5] (depends T008)
- [ ] T014 [US1] GetChunk integration test (`internal/engine` or `internal/rest`): ingest a markdown doc with `[[authentication]]`/`[[JWT tokens]]`, fetch a covering chunk via `GetChunk`, assert `Wikilinks` contains both; ingest a no-link markdown chunk, assert `Wikilinks` absent/empty. [US1 acceptance #1/#5; FR-008] (depends T011, T012)

**Checkpoint**: US1 MVP delivers — a chunk read returns correct, grammar-conformant wikilinks. `make test` green.

---

## Phase 4: User Story 2 — Chunk-scoped and deterministic (Priority: P2)

**Goal**: each edge originates from the exact chunk carrying the link; re-ingest is byte-identical; chunk IDs are unchanged. (US1's resolution already does offset-containment; this story hardens + proves the qualities.)

**Independent Test**: a doc with `[[alpha]]` only in paragraph 1 and `[[beta]]` only in paragraph 2 → per-chunk attribution; re-ingest the same file → byte-identical `Wikilinks`; chunk IDs unchanged. (`quickstart.md` Scenarios 4 & 7.)

### Implementation + Tests for User Story 2

- [ ] T015 [US2] Harden determinism in `internal/pipeline`: guarantee `Wikilinks` is a pure function of chunk text + span order — stable first-occurrence ordering, no map-iteration nondeterminism in the resolution path. [FR-006] (depends T009)
- [ ] T016 [US2] Chunk-scope + determinism + identity tests (in `internal/pipeline`/`internal/engine` test files): (a) multi-paragraph doc → paragraph-1 chunks list `alpha` not `beta`, vice versa; (b) same file ingested twice → byte-identical `Wikilinks` for matching chunk IDs; (c) re-ingested chunks keep identical `chunk_id` (identity safety — `wikilink_spans` dropped before `GenerateID`). [FR-005, FR-006, FR-010; US2 acceptance #1/#2] (depends T015)
- [ ] T017 [US2] Boundary test: a wikilink whose text sits at a chunk boundary attributes to exactly the chunk containing its opening `[` and is never silently dropped. [US2 acceptance #3; FR-005] (depends T015)

**Checkpoint**: US1 + US2 together — wikilinks are correct, chunk-scoped, deterministic, and identity-safe.

---

## Phase 5: User Story 3 — Every transport, every format (Priority: P3)

**Goal**: identical `Wikilinks` value across gRPC, REST, MCP, and CLI; non-markdown sources absent/empty.

**Independent Test**: fetch the same markdown chunk over all four transports, assert byte-identical `wikilinks`; fetch PDF/docx/txt chunks, assert absent/empty. (`quickstart.md` Scenarios 2, 3, 5.)

### Implementation + Tests for User Story 3

- [ ] T018 [P] [US3] Render `Wikilinks` on CLI in `internal/cli/query.go` (`renderResults`) and the spec 035 `chunk get` command — render the list per hit/chunk, omit the line when absent. [FR-009]
- [ ] T019 [P] [US3] Include `Wikilinks` in MCP in `internal/mcp/server.go` — the query-hit payload and the `go_rag_get_chunk` tool result. [FR-009]
- [ ] T020 [US3] Extend `internal/engine/parity_test.go`: assert `wikilinks` is byte-identical across CLI, REST, gRPC, and MCP for the same chunk (mirror the `section_context` parity assertion; SC-002). [FR-009; SC-001] (depends T018, T019, T006)
- [ ] T021 [US3] Non-markdown absent test: ingest PDF, docx, and txt sources; assert `Wikilinks` is absent/empty on the resulting chunks across transports (the markdown reader is the only populator). [FR-007, FR-008] (depends T020)

**Checkpoint**: all three stories done — the field is correct, chunk-scoped, deterministic, identity-safe, and byte-identical across every transport and format.

---

## Phase 6: Polish & Cross-Cutting

- [ ] T022 [P] Update docs: if `CLAUDE.md`'s architecture map or `README.md` enumerates `Chunk` fields / response shapes, add `Wikilinks`; cross-link BL-004 in `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md` (mark BL-004 resolved by spec 036, mirroring how BL-001 references spec 035).
- [ ] T023 Run `make lint` (golangci-lint — the `ci.yml` gate, strictly stricter than `go vet`) and resolve every finding.
- [ ] T024 Run `quickstart.md` validation end-to-end on an **isolated** DB (Scenarios 1–7; non-default `--db-path`/ports per project CLAUDE.md) — confirm the bridge-consumer contract holds.
- [ ] T025 Constitution compliance check + commit affirmation: Principles I–V pass; **no on-disk key-space layout change, no migration, `migrate.ExpectedVersion` unchanged** (additive `omitempty` field on prefix `0x03`); pure Go, `CGO_ENABLED=0`, no new deps. State this in the commit/PR body.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (T001)**: none — start immediately.
- **Foundational (T002–T006)**: T005 depends on T004; T006 depends on T002 + T005; T002/T003/T004 are mutually parallel. **Blocks all stories.**
- **US1 (T007–T014)**: depends on Foundational. T008 depends on T007 (same file); T009 depends on T003; T011 depends on T002; T012 depends on T002 + T005; T013 depends on T008; T014 depends on T011 + T012.
- **US2 (T015–T017)**: depends on US1's T009 (resolution must exist to harden/test).
- **US3 (T018–T021)**: depends on US1 (field + REST rendering must exist). T020 depends on T018 + T019 + T006; T021 depends on T020.
- **Polish (T022–T025)**: after all stories.

### Story completion order (single-author, sequential)
Foundational → US1 (MVP, stop+validate) → US2 → US3 → Polish. Stories are independently testable at each checkpoint.

### Parallel opportunities (within phase, different files)
- Foundational: T002 (`model.go`), T003 (`markdown.go` type only), T004 (`gorag.proto`) — parallel.
- US1: T010 (`engine/types.go`+`query.go`), T012 (`rest/types.go`) — parallel once T002 lands; T013 (test) parallel with T010/T012.
- US3: T018 (`cli`), T019 (`mcp`) — parallel.

---

## Parallel Example: Foundational + US1

```text
# Foundational (parallel — different files):
Task: "T002 add Chunk.Wikilinks in internal/model/model.go"
Task: "T003 add WikilinkSpan type in internal/reader/markdown.go"
Task: "T004 add proto fields in proto/gorag.proto"
# then sequential: T005 regen → T006 grpc mapping.

# US1 (parallel once T002 lands):
Task: "T010 project Wikilinks on engine.QueryHit (types.go + query.go)"
Task: "T012 render Wikilinks on REST (rest/types.go)"
Task: "T013 reader grammar tests (markdown_test.go)"
```

---

## Implementation Strategy

### MVP First (US1 only)
1. T001 baseline green.
2. Foundational T002–T006 (field + proto + gRPC mapping).
3. US1 T007–T014 (collect + resolve + project to REST + GetChunk + grammar tests).
4. **STOP & VALIDATE**: `quickstart.md` Scenario 1 on an isolated DB. Demo-able: a fetched chunk carries its wikilinks.

### Incremental delivery
- + US2 (T015–T017): chunk-scoped + deterministic + identity-safe — proven by tests.
- + US3 (T018–T021): all four transports byte-identical; non-markdown absent.
- Polish (T022–T025): docs, lint, full quickstart, constitution affirmation.

### Commit cadence
Conventional Commits to `main` after each task or checkpoint (`feat: ...`); `make build && make vet && make test && make lint` green before every push.

---

## FR / Acceptance coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 populate per-chunk | T009 |
| FR-002 alias+anchor strip | T007, T013 |
| FR-003 embeds+transclusions excluded | T007, T013 |
| FR-004 dangling included verbatim | T007, T013 |
| FR-005 chunk-scoped | T009, T016, T017 |
| FR-006 deterministic | T009, T015, T016 |
| FR-007 non-markdown absent | T021 |
| FR-008 omitempty/absent | T002, T012, T013, T021 |
| FR-009 all 4 transports | T004, T006, T010, T011, T012, T018, T019, T020 |
| FR-010 no chunk_id change | T009, T016 |
| FR-011 pure Go | T025 (no new deps — inherent) |
| FR-012 no migration | T009, T025 |
| FR-013 reuse linkTarget, no 2nd parser | T007 |
| FR-014 code-context exclude | T008, T013 |

Every spec acceptance scenario (US1 #1–5, US2 #1–3, US3 #1–2) is covered by T013/T014, T016/T017, T020/T021 respectively.

## Notes
- All tasks carry `[ID]`, a file path, and (`[P]`/`[USx]`) markers per the format rules.
- The feature adds no runtime dependency and no migration — Constitution Principles II/III/V and storage discipline hold throughout.
- If implementation finds the reader's wikilink regex behaves differently than R1/R4 assume, stop and update `research.md` before continuing (self-heal the doc, don't paper over it).
