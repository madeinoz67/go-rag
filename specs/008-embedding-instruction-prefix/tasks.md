# Tasks: Embedding Instruction-Prefix (Asymmetric Query/Document Encoding)

**Input**: Design documents from `/specs/008-embedding-instruction-prefix/` —
[plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/embed-role-prefix.md](contracts/embed-role-prefix.md),
[quickstart.md](quickstart.md).

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅, quickstart.md ✅.

**Tests**: Included per the repo constitution (build gate `go test ./...` MUST stay
green) and the quickstart validation scenarios. Written alongside each story's
implementation (repo convention; specs 005–007 all ship `_test.go`).

**Organization**: Tasks grouped by user story so each story is independently
implementable and testable. Touch-points from
[research.md §Summary](research.md#summary-of-code-touch-points-informs-tasksmd).

**Constitution guardrails (apply to every task)**: pure Go, no CGo (Principle III);
`Embedder.Embed` signature stays unchanged — prefix logic is a pure function at the
boundary (Principle V); no new Pebble prefix — the convention rides the existing
0x04 record; prefix never touches `Chunk.Content` or identity hashes (Principle II);
prefixing is off the write path (Principle IV).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: Which user story (US1/US2/US3) — user-story phases only
- Exact file paths in every description

---

## Phase 1: Setup (Baseline)

**Purpose**: Confirm a green baseline before changing anything.

- [X] T001 Verify baseline gate is green: run `make build && make vet && make test` from repo root; resolve any pre-existing failures before starting (so a later red run is unambiguously this feature's)

**Checkpoint**: Clean baseline established.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared primitives every user story depends on — the pure Prefixer,
the config keys, and the convention-aware Corpus Profile.

**⚠️ CRITICAL**: No user-story work begins until this phase is complete.

- [X] T002 [P] Create the pure Prefixer in `internal/embed/prefix.go`: a `Role` type (`query`/`document`), the default convention map (`nomic-embed-text`→`search_query:`/`search_document:`, E5→`query:`/`passage:`, BGE→query-only), a `Resolve(model, mode, queryOverride, docOverride string) (queryPrefix, docPrefix string)` function with override precedence (explicit > mode-derived > none; unknown model → none), and an idempotent `Prepend(prefix, text string) string` that does not double-prefix (research D1/D2/D4). Pure — no I/O, no config import.
- [X] T003 [P] Add prefix config keys in `internal/config/config.go` (`EmbeddingPrefix` mode `auto`|`on`|`off` default `auto`, `EmbeddingQueryPrefix`, `EmbeddingDocPrefix`) with JSON tags; update `Default()`, `Get`, `Set`; and add the three keys to `knownConfigKeys` in `internal/engine/config.go` (follow the spec-006 rerank-field pattern). `Set` validates the mode enum and rejects malformed prefix strings (e.g. containing a newline).
- [X] T004 Write unit tests in `internal/embed/prefix_test.go` (depends T002): nomic/E5/BGE resolution, unknown-model→none, `off`→none, explicit overrides applied verbatim per role, idempotent prepend (text already starting with prefix not doubled), empty/whitespace text handled without error.
- [X] T005 [P] Make the Corpus Profile convention-aware in `internal/engine/embedding_profile.go`: add `Convention string` to the mirrored `storedEmbed` struct (backward-compat: missing field reads as `""`), add `MajorityConvention string` + `ConventionCounts map[string]int` to `EmbeddingProfile`, populate them in `CorpusProfile`, and widen `Consistent` to also require ≤1 convention (data-model.md).

**Checkpoint**: Shared Prefixer + config + convention-aware profile ready. User-story implementation can begin.

---

## Phase 3: User Story 1 — Encode Queries & Documents in the Right Role (Priority: P1) 🎯 MVP

**Goal**: For prefix models (default `nomic-embed-text`), queries are embedded with the query prefix and documents with the document prefix, each reaching the model in its trained role.

**Independent Test**: The Prefixer unit tests (T004) plus a wiring test proving the pipeline applies the document prefix and `engine.Query` applies the query prefix; the eval `DeterministicEmbedder` is role-aware (quickstart Scenario 1). Cross-transport parity (Scenario 5).

### Implementation for User Story 1

- [X] T006 [P] [US1] Wire the document prefix in `internal/pipeline/workers.go`: resolve the document prefix from config via the Prefixer, prepend it (idempotent) to each chunk text before `p.embed.Embed(...)`, and write the resolved convention into the persisted 0x04 `storedEmbedding` record (add the `Convention` field to that struct) (depends T002, T003).
- [X] T007 [P] [US1] Wire the query prefix in `internal/engine/query.go`: resolve the query prefix from config, wrap `em.Embed` into a query-role `index.EmbedFunc` (prepend the query prefix) before `index.NewRetrieval(...)`, and apply the same query prefix in the `checkEmbeddingMismatch` probe embed (depends T002, T003).
- [X] T008 [P] [US1] Make the eval embedder role-aware in `internal/eval/embedder.go`: the `DeterministicEmbedder` derives its vector incorporating the active prefix so the harness can exercise the query-vs-document mechanism deterministically (research D5; depends T002).
- [X] T009 [US1] Add wiring tests: pipeline document-prefix application + convention written to 0x04 (`internal/pipeline/`), query-prefix applied via the engine path (`internal/engine/`), and eval role-awareness (`internal/eval/`) (depends T006, T007, T008).
- [X] T010 [US1] Pass quickstart Scenario 1 (mechanism) and Scenario 5 (cross-transport parity) from `specs/008-embedding-instruction-prefix/quickstart.md` (depends T009).

**Checkpoint**: US1 fully functional — default nomic encodes queries and documents in the correct role; MVP shippable.

---

## Phase 4: User Story 2 — Never Corrupt a Model That Doesn't Use Prefixes (Priority: P2)

**Goal**: Prefixing is model-gated and overridable — unknown models and `off` apply no prefix; explicit overrides apply verbatim; enabling the feature can never corrupt a non-prefix model.

**Independent Test**: Unknown model → no prefix; `off` → no prefix; explicit overrides → exact strings per role; malformed prefixes rejected (quickstart Scenario 2).

### Implementation for User Story 2

- [X] T011 [US2] Complete + verify the gating surface in `internal/embed/prefix.go` and `internal/config/config.go`: confirm override precedence (explicit > mode > none), `off` and unknown-model short-circuit to no prefix, and `config.Set` rejects malformed prefix strings (extends T002/T003; depends T002, T003).
- [X] T012 [US2] Add safety tests in `internal/embed/prefix_test.go` and `internal/config/`: unknown-model-no-prefix, `off`-no-prefix, explicit-override-verbatim, malformed-prefix-rejected (depends T011).
- [X] T013 [US2] Pass quickstart Scenario 2 from `specs/008-embedding-instruction-prefix/quickstart.md` (depends T012).

**Checkpoint**: US1 and US2 both independently functional — feature is safe to ship generally.

---

## Phase 5: User Story 3 — Don't Silently Half-Prefix an Existing Corpus (Priority: P3)

**Goal**: The prefix convention is recorded as provenance; a query whose convention differs from the corpus majority is detected and refused/warned, never silently scored across a mixed-convention corpus; re-embedding creates no duplicates.

**Independent Test**: Legacy corpus + prefixes enabled → mismatch refuse with re-embed hint; re-embed → consistent + no duplicate docs; mixed convention → skip minority + warn; status surfaces the convention (quickstart Scenario 3).

### Implementation for User Story 3

- [X] T014 [US3] Extend the mismatch guard in `internal/engine/query.go` (`checkEmbeddingMismatch`): compare the query's active convention to `prof.MajorityConvention`; refuse with a clear error naming both conventions + a re-embed hint (extend the existing `ErrEmbeddingMismatch` message so all transports map it consistently); for a mixed-convention corpus queried by the majority, skip the minority with a logged warning mirroring the spec-005 graceful-degradation path (depends T005, T006).
- [X] T015 [P] [US3] Surface the convention in `internal/engine/status.go`: report the active prefix mode + resolved query/document prefixes, the corpus `MajorityConvention`, and a convention-drift flag when >1 convention is present (mirror `EmbeddingDrift`) (depends T005).
- [X] T016 [US3] Add US3 tests in `internal/engine/`: legacy-corpus+prefixes-on→mismatch refuse, re-embed→consistent+no-duplicates, mixed-convention→skip-minority+warn, status surfaces convention + drift flag (depends T014, T015).
- [X] T017 [US3] Pass quickstart Scenario 3 from `specs/008-embedding-instruction-prefix/quickstart.md` (depends T016).

**Checkpoint**: All three stories independently functional; no silent half-prefixed corpus possible.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Docs, full gate, manual quality proof, commit, and backlog closure.

- [X] T018 [P] Document the new config keys (`embedding_prefix` mode + `embedding_query_prefix` / `embedding_doc_prefix` overrides, default `auto`) in `README.md`, alongside the existing embedding-model docs.
- [X] T019 Run the full gate from repo root: `make build && make vet && make test` (`-race -cover`); resolve any regressions until green. No new Pebble prefix; `Embedder` interface unchanged; confirm `internal/embed/ollama.go` and `internal/index/retrieval.go` are untouched.
- [X] T020 Manually run quickstart Scenario 4 against a real `nomic-embed-text`: baseline eval (`embedding_prefix: off`) vs prefixes-on; record recall@5/10 and NDCG@10 to prove SC-001 (prefixes-on no lower — and higher — than unprefixed). Record the numbers in the commit message or spec notes.
- [X] T021 Commit to `main` with Conventional Commits: `feat(embed): asymmetric query/document instruction prefixes (H07)` (single-author repo — direct to `main`, no branch/PR per project CLAUDE.md).
- [X] T022 Mark H07 ✅ COMPLETE in `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 1 line, item H07): replace the unchecked row with a completed entry referencing `spec 008` and the gates summary (build/vet/test green + manual eval numbers), matching the H03/H09/H13 entry format. **This is the explicit "update backlog when completed" instruction.** (depends T019 green.)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately.
- **Foundational (Phase 2)**: depends on T001; **BLOCKS** all user stories.
- **User Stories (Phases 3–5)**: each depends on Foundational; proceed in priority order (US1 → US2 → US3) or in parallel if staffed (US1 and US2 share `prefix.go`, so sequence those two; US3's profile/guard work is largely independent).
- **Polish (Phase 6)**: depends on all stories; T022 (backlog) depends on T019 green.

### User Story Dependencies

- **US1 (P1)**: starts after Foundational. T006/T007/T008 are different files → parallel within the story; T009 wires them; T010 validates. No dependency on other stories.
- **US2 (P2)**: starts after Foundational; refines the same `prefix.go`/`config.go` as US1 → **sequence after US1** to avoid same-file conflicts, but independently testable.
- **US3 (P3)**: starts after Foundational; T014 depends on T006 (convention must be written) and T005 (profile reads it); T015 is parallel with T014 (different file). Independently testable.

### Within Each User Story

- Foundational primitives before wiring.
- Wiring (different files) can parallelize.
- Tests after wiring; quickstart scenario last.
- Story checkpoint before next priority.

### Parallel Opportunities

- Phase 2: T002, T003, T005 are different files → run together; T004 follows T002.
- US1: T006 (`workers.go`), T007 (`query.go`), T008 (`eval/embedder.go`) are different files → run together once Foundational is done.
- US3: T014 (`query.go`) and T015 (`status.go`) are different files → run together.
- Polish: T018 (README) parallels the manual eval/commit work.

---

## Parallel Example: User Story 1

```bash
# Once Phase 2 is complete, launch the three US1 wiring tasks together (different files):
Task: "T006 [US1] document prefix in internal/pipeline/workers.go"
Task: "T007 [US1] query prefix in internal/engine/query.go"
Task: "T008 [US1] role-aware eval embedder in internal/eval/embedder.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. T001 — baseline green.
2. Phase 2 (T002–T005) — Foundational.
3. Phase 3 (T006–T010) — US1.
4. **STOP and VALIDATE**: US1 independently (Prefixer tests + wiring + Scenario 1 + Scenario 5).
5. Ship/demo if ready — the default nomic now encodes in the correct role.

### Incremental Delivery

1. Setup + Foundational → shared primitives ready.
2. + US1 → correct role encoding (MVP).
3. + US2 → safe for non-prefix models.
4. + US3 → no silent half-prefixed corpus.
5. Polish → docs, full gate, manual quality proof, commit, backlog closure.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[USx]` maps a task to its user story for traceability.
- **No proto/RPC change**: prefixes are a global config setting resolved by the engine, not a per-query parameter, so cross-transport parity (FR-009) is free via the shared `engine.Query` + pipeline paths — no REST/gRPC/MCP adapter edits.
- **Two files deliberately untouched** to honor Principle V: `internal/embed/ollama.go`, `internal/index/retrieval.go`.
- Commit after each task or logical group; stop at any checkpoint to validate a story independently.
- T022 (backlog update) is the final closing action — only after the full gate (T019) is green, so the backlog never claims done for code that isn't verified.
