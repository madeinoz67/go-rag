# Tasks: Secret / PII Scanning at Ingest

**Input**: Design documents from `/specs/022-secret-pii-scan/` (plan.md, spec.md, research.md, data-model.md, contracts/patterns.md, quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅ (US1–US2), research.md ✅ (D1–D7), data-model.md ✅, contracts/ ✅

**Tests**: Included — quickstart.md requires a per-pattern-type redaction test, an identity-stability test, and a default-off no-regression test.

**Organization**: Tasks grouped by user story (US1 P1 = redact MVP; US2 P2 = visibility/rescan). Go project — `internal/<pkg>/` paths per plan.md.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[Story]**: US1/US2 — maps to spec.md user stories
- All paths project-relative; **stdlib only, no new dependency** (Constitution III)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: the config keys every story uses.

- [X] T001 [P] Add redaction config keys to `internal/config` — `pii_redact_enabled` (default `false`), `pii_patterns` (path to an additional/override patterns file); Get/Set/Validate + Load backward-compat (absent ⇒ defaults)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the `internal/redact` package — regex scanner + redactor (stdlib only). ⚠️ No story work until this lands.

- [X] T002 `internal/redact/patterns.go` — curated built-in regex set (AWS access key `AKIA…`, GitHub token `gh[opsu]_…`, generic secret heuristic, PEM private key block, credit card + LUHN, SSN, email) + a custom-pattern file loader (`<type>\t<regex>` per line, `#` comments)
- [X] T003 [P] `internal/redact/redact.go` — `Scanner{patterns}` + `Apply(text) → (redactedText, []Finding)`; each match → typed placeholder `[REDACTED:<type>]`; credit-card matches LUHN-validated; `Finding{Type, Count}` per-type aggregate, sorted
- [X] T004 redact package test — each built-in pattern type → correct placeholder; LUHN false-positive guard (a non-LUHN digit string NOT redacted); custom patterns honored; deterministic

**Checkpoint**: `internal/redact` ready. Pipeline wiring can begin.

---

## Phase 3: User Story 1 — Redact secrets before indexing (Priority: P1) 🎯 MVP

**Goal**: with `--redact`, ingested text is scanned + redacted before indexing; secrets are not retrievable verbatim; identity stable.

**Independent Test**: ingest a doc with secrets via `--redact` → query for each secret → not found; identity same with/without `--redact`; default-off → verbatim.

### Implementation for User Story 1

- [X] T005 [US1] Wire `redact.Apply` into `pipeline.processFile` (`internal/pipeline/pipeline.go`) — insert **between** identity (`docID = GenerateID(content, …)`, ~line 222) and chunking (`splitter.Split(content)`, ~line 238); when redact enabled, `content, findings = scanner.Apply(content)` then the chunker splits the redacted text; original docID preserved (Constitution II)
- [X] T006 [P] [US1] Add `--redact` flag to CLI `add`/`scan`/`reprocess` commands (`internal/cli`) — forwards `pii_redact_enabled` to the pipeline via a new `Pipeline.SetRedactor(scanner)` method (mirrors `SetDetector`)
- [X] T007 [P] [US1] Add `Redactions []Finding` to `IngestSummary` (`internal/engine/types.go`) + surface in the CLI ingest render (`internal/cli`) as "redacted: N (type=…)"
- [X] T008 [US1] Integration test — ingest a doc containing an AWS key + GitHub token + email via `--redact`; query for each → not found; chunks carry `[REDACTED:<type>]`; same doc without `--redact` → verbatim (secret found); same docID both ways (identity over original — Constitution II/SC-004)

**Checkpoint**: US1 — secrets redacted from the index; identity stable; default-off preserved.

---

## Phase 4: User Story 2 — Finding visibility + rescan + custom patterns (Priority: P2)

**Goal**: findings reported in the audit log; reprocess --redact rescans the corpus; custom patterns detected.

### Implementation for User Story 2

- [X] T009 [P] [US2] Report findings to the H18 audit log — emit a `redaction` `audit.Event` with per-type counts in the pipeline after `redact.Apply` (`internal/pipeline`)
- [X] T010 [US2] Test — findings appear in the ingest summary + the audit log; a reprocess with `--redact` redacts existing corpus (secrets gone from results); a custom pattern (via `pii_patterns` file) is detected + redacted

**Checkpoint**: US2 — operator sees what was redacted + can rescan + custom patterns work.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [X] T011 [P] Docs — redaction pattern set + the threat (indexed secrets retrievable verbatim) + the redaction trade-off (recall on redacted terms) in `docs/redaction.md` (FR-008); reference `contracts/patterns.md`
- [X] T012 Final gates — `go build ./...`, `go vet ./...`, `go test -race -cover ./...` green; `make test-eval` recall@10 unchanged (default-off — SC-006); `go.mod` unchanged (no new dep — Constitution III); run `quickstart.md` scenarios 1–5

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately
- **Foundational (Phase 2)**: depends on Phase 1 — `internal/redact` BLOCKS both stories
- **US1 (Phase 3)**: depends on Phase 2 — the MVP
- **US2 (Phase 4)**: depends on Phase 2 + US1 (findings from US1's redact.Apply)
- **Polish (Phase 5)**: depends on US1+US2

### User Story Dependencies

- **US1 (P1)**: starts after Foundational — no story deps. **MVP.**
- **US2 (P2)**: starts after US1 (uses the findings pipeline produces)

### Parallel Opportunities

- Phase 2: T003 ∥ T002 (once patterns land, the scanner wraps them — or co-developed)
- US1: T006 (CLI flag) ∥ T007 (IngestSummary) — different files
- Polish: T011 (docs) ∥ T012 (gates, partially)

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (config) → Phase 2 (`internal/redact`)
2. Phase 3 (US1): wire into pipeline → CLI `--redact` → IngestSummary → integration test
3. **STOP and VALIDATE**: ingest with `--redact` → secrets not retrievable; identity stable; default-off verbatim
4. This alone closes the privacy gap (no indexed secrets when opted in)

### Incremental Delivery

1. Setup + Foundational → `internal/redact` ready
2. + US1 → redact at ingest (**MVP** — no indexed secrets)
3. + US2 → audit-log findings + rescan + custom patterns
4. Polish → docs + final gates

---

## Notes

- `[P]` = different files, no deps on incomplete tasks
- `[Story]` maps the task to its user story for traceability
- Every story is independently completable and testable; stop at any checkpoint to validate
- Commit (Conventional Commits, straight to `main`) after each task or logical group
- Constitution gates (plan.md): **I** local regex (no egress), **II** identity over original (redaction inserts post-identity, pre-chunk), **III** stdlib `regexp` no dep, **IV** opt-in (default off, bounded regex cost off the default ACK path)
- **Privacy**: per-type counts only in findings — never the matched text or positions
