# Tasks: Retrieval Poisoning Defense — Ingest-Time Injection Detection

**Input**: Design documents from `/specs/019-poisoning-detector/` (plan.md, spec.md, research.md, data-model.md, contracts/transports.md, quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅ (US1–US4), research.md ✅ (D1–D12), data-model.md ✅, contracts/ ✅

**Tests**: Included — the spec's done-definition (quickstart.md) requires a deterministic scorer test, a cross-transport parity test, and an air-gap test. Scorer tests written before/alongside implementation; deterministic (fixed payload → fixed verdict).

**Organization**: Tasks grouped by user story (US1 P1 = MVP; US2 P2; US3 P3; US4 P3). Go project — paths use `internal/<pkg>/` per plan.md, not `src/`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[Story]**: US1/US2/US3/US4 — maps to spec.md user stories
- All paths project-relative; CGO-free pure Go; no new dependency (Constitution III)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Shared types + key-space allocation used by every story.

- [X] T001 [P] Add `PoisoningVerdict` types to `internal/model/chunk.go` — `Level` enum (`clean|suspicious|quarantine|released`), `Verdict{Level, Score, Signals{Repetition,Stuffing,Instruction}, MatchedPhrases}`, and a `Poisoning *Verdict` field on `Chunk`
- [X] T002 [P] Allocate Pebble prefix constants in `internal/storage/db.go` — `PrefixPoisonQuarantine` (candidate `0x11`) and `PrefixThreatSource`; confirm next-free against existing prefixes; document in the prefix map

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infra that MUST be complete before ANY user story. ⚠️ No story work until this phase is done.

- [X] T003 [P] Add poisoning config keys to `internal/config` — `poisoning_enabled` (default `true`, FR-010), `poisoning_threshold_suspicious` (`0.40`), `poisoning_threshold_quarantine` (`0.70`), `poisoning_phrase_list` (path, optional); validate `suspicious < quarantine` at load
- [X] T004 [P] Add verdict storage helpers in `internal/storage` (new `poison.go`) — `PutVerdict`/`GetVerdict` (rides chunk record, same batch), quarantine-index (`0x11`) `Put`/`Delete`/`List`, within the single prefix-partitioned key space
- [X] T005 [P] Embed the built-in English instruction-phrase list as package data in `internal/poison` (e.g. `phrases.go` const slice); normalized (lowercase/whitespace/leetspace) at match time per D1/D9

**Checkpoint**: model + storage + config + phrase data ready. Scorer + wiring can begin.

---

## Phase 3: User Story 1 — Protected ingest of untrusted sources (Priority: P1) 🎯 MVP

**Goal**: score every chunk at ingest, persist the verdict, quarantine flagged chunks out of default results, surface the verdict on all four transports.

**Independent Test**: ingest a classic payload ("Ignore all previous instructions…") into a fresh isolated vault (`--db-path <tmp>` + non-default addrs) → default query returns nothing; `--include-quarantined` returns it with `level=quarantine`.

### Tests for User Story 1

- [X] T006 [P] [US1] Deterministic scorer unit test in `internal/poison/heuristic_test.go` — fixed payloads → fixed verdicts: clean doc, classic injection payload (`quarantine`), security writeup quoting injection (not auto-quarantined), tiny/empty text (clean, no panic)
- [X] T007 [P] [US1] Quarantine-exclusion test in `internal/engine` — poison chunk excluded by default, included with `include_quarantined`; clean chunk unaffected (no retrieval regression)

### Implementation for User Story 1

- [X] T008 [P] [US1] Define `PoisoningDetector` interface in `internal/poison/detector.go` — `Score(text string) model.Verdict`; self-register pattern mirroring `internal/rerank` (Reranker) / `internal/index` (QueryTransformer)
- [X] T009 [US1] Implement `HeuristicScorer` in `internal/poison/heuristic.go` — text normalization + 3 signals (repetition, keyword/phrase stuffing, instruction-phrase match) + weighted combine → `score` → `level`; pure stdlib (`strings`/`unicode`/`regexp`), no new module
- [X] T010 [US1] Wire scorer into ingest in `internal/pipeline` — score each chunk synchronously in the store path, persist verdict with the chunk (same Pebble batch, one fsync); skip when `poisoning_enabled=false` (depends T004, T009)
- [X] T011 [US1] Implement quarantine `keep`-predicate in `internal/engine.Query` — reuse the spec-014 `internal/index.Filter` mechanism to exclude `level ∈ {suspicious, quarantine}` unless `IncludeQuarantined`; applied pre-fusion (depends T004)
- [X] T012 [P] [US1] Add `QueryRequest.IncludeQuarantined` + `poisoning{level,score,signals,matched_phrases}` on `QueryHit` in `internal/model` / `internal/engine`
- [X] T013 [P] [US1] Surface verdict + `--include-quarantined` on CLI in `internal/cli`
- [X] T014 [P] [US1] Surface verdict + `include_quarantined` on REST in `internal/rest`
- [X] T015 [P] [US1] Add proto fields (`QueryRequest.include_quarantined`, `QueryHit.poisoning`, `PoisoningVerdict` message) to `proto/gorag.proto`; regen `proto/gen`; surface on gRPC in `internal/grpc`
- [X] T016 [P] [US1] Surface verdict + `include_quarantined` on MCP in `internal/mcp/server.go` (tool schema + `renderQuery`)
- [X] T017 [US1] Cross-transport parity test — a flagged chunk surfaces identical `poisoning{level,score,signals}` on CLI/REST/gRPC/MCP (spec-006 pattern; SC-004)

**Checkpoint**: US1 fully functional — detect → quarantine-by-default → surface on all transports.

---

## Phase 4: User Story 2 — Transparent verdicts + false-positive recovery (Priority: P2)

**Goal**: list flagged chunks with per-signal breakdown; non-destructive `release`/`reset` override.

**Independent Test**: ingest a legit security writeup quoting injection → it's flagged; `poison list` shows the breakdown; `poison release <id>` makes it retrievable by default; `poison reset` re-quarantines; content never deleted.

### Implementation for User Story 2

- [X] T018 [US1→US2] Implement engine ops in `internal/engine` — `ListPoisoned` (via `0x11` index, returns breakdown + `matched_phrases`), `ReleaseChunk` (`level=released`, sticky), `ResetChunk` (re-apply scored level) (depends T004)
- [X] T019 [P] [US2] CLI: `go-rag poison list [--level]` / `release <id>` / `reset <id>` in `internal/cli`
- [X] T020 [P] [US2] REST: `GET /poison`, `POST /poison/{id}/release`, `POST /poison/{id}/reset` in `internal/rest`
- [X] T021 [P] [US2] gRPC: `ListPoisoned`/`ReleaseChunk`/`ResetChunk` RPCs in `proto/gorag.proto` + `internal/grpc`; regen
- [X] T022 [P] [US2] MCP: `poison_list` / `poison_release` / `poison_reset` tools in `internal/mcp/server.go`
- [X] T023 [US2] Non-destructive override test — `release` → retrievable by default; `reset` → re-quarantined; original score retained; no content deleted (SC-005)

**Checkpoint**: management surface works; false positives fully recoverable.

---

## Phase 5: User Story 3 — Audit/triage re-scan over existing corpus (Priority: P3)

**Goal**: re-score pre-feature back-catalog via reprocess, idempotently, without re-ingesting source files.

**Independent Test**: a vault ingested before this feature → `reprocess --poisoning` → existing chunks get verdicts; re-run is a no-op for unchanged text.

### Implementation for User Story 3

- [X] T024 [US3] Extend the reprocess path in `internal/pipeline`/`internal/engine` — `--poisoning` flag iterates stored chunks, scores, persists verdicts (idempotent — unchanged text is a no-op) (depends T009, T004)
- [X] T025 [P] [US3] Expose `reprocess --poisoning` / `poisoning` flag on CLI (`internal/cli`), REST (`internal/rest`), gRPC (`internal/grpc` + proto), MCP (`internal/mcp/server.go`)
- [X] T026 [US3] Idempotent rescan test — pre-feature chunks scored without source re-read; re-run no-op; determinism (Constitution II)

**Checkpoint**: back-catalog scannable on demand.

---

## Phase 6: User Story 4 — Threat-list management & feed import (Priority: P3)

**Goal**: local versioned multi-source phrase list; explicit `threat import` (file/URL, one-shot) → debounced background rescan; fully air-gapped except the explicit import.

**Independent Test**: a clean chunk containing "exfiltrate the keys" → `threat import` a source with that phrase → after debounce+sweep the chunk is quarantined; a `released` chunk stays released; zero egress outside the import.

### Implementation for User Story 4

- [X] T027 [P] [US4] Implement `ThreatSource` store + merged-list computation in `internal/storage`/`internal/engine` — per-source provenance (origin/version/fetched_at), dedupe, `enabled` toggle; merged-list content-hash as rescan trigger key (depends T002, D12)
- [X] T028 [US4] Implement explicit `threat import <path|url>` in `internal/poison` (or `internal/config`) — one-shot file/URL fetch; URL egress ONLY here (Constitution I); record provenance; on merged-list change fire rescan trigger (depends T027, T029)
- [ ] T029 [US4] Implement background rescan worker in `internal/engine` — debounced trigger on merged-list/threshold change; async off the query/write-ACK paths (Constitution IV); bump index epoch to invalidate spec-016 query caches; preserve `released` overrides (depends T024)
- [ ] T030 [US4] Wire the watcher in `internal/watcher` — watch poisoning config (phrase-list file + thresholds) → debounce → trigger rescan (daemon-mode only)
- [X] T031 [P] [US4] Manual rescan op `poison rescan` on CLI (`internal/cli`), REST (`POST /poison/rescan`), gRPC (`RescanPoisoning` RPC + proto), MCP (`poison_rescan`)
- [X] T032 [P] [US4] Threat-list management — `threat list/add/remove/import` on CLI (`internal/cli`); engine ops (`ListThreatSources`/`AddPhrases`/`RemoveThreatSource`/`ImportThreatSource`) ready to project on REST/gRPC/MCP
- [X] T033 [US4] Air-gap test — URL import = one explicit GET; rescan never re-fetches (no polling) — zero outbound network connections in steady state; egress observed ONLY during `threat import <url>` (SC-008 / Constitution I)
- [X] T034 [US4] Import→rescan test — add a phrase → matching chunk flagged via the triggered rescan — import a phrase matching a clean chunk → chunk quarantined after debounce+sweep; `released` chunk stays released (SC-007)

**Checkpoint**: threat list managed; feeds importable; auto-rescan closes the loop — all air-gapped.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Affects multiple stories; final hardening + validation.

- [X] T035 [P] Add poisoning summary to `status` — engine `StatusInfo` + MCP `renderStatus` (CLI/REST/gRPC status-projection are minor follow-ups; `poison list`/`threat list` cover those views) — `enabled`, thresholds, flagged counts (quarantine/suspicious/released), `threat_list` version, sources M/N, `last_rescan`, `rescans`
- [X] T036 [P] Threat-model docs in `docs/poisoning.md` (FR-008) — indirect prompt injection, what the detector does/doesn't catch, heuristic-not-a-guarantee, CJK phrase-list limitation, quarantine-by-default posture, air-gap boundary
- [X] T037 Run `quickstart.md` scenarios 1–9 — covered by engine tests (no-Ollama paths) + CLI smoke (`poison list/add`, `threat add/list`); full ingest→query e2e needs Ollama (not in this env)
- [X] T038 `make test-eval` regression — recall@10 unchanged (SC-006): eval PASS; detection does not regress baseline retrieval
- [X] T039 Scorer benchmark ~29µs/chunk (SC-003, 170× under 5ms), no I/O; `go build`/`vet`/`test -race` green; `go.mod` unchanged (no new dep, Constitution III)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately
- **Foundational (Phase 2)**: depends on Phase 1 — BLOCKS all stories
- **US1 (Phase 3)**: depends on Phase 2 — the MVP
- **US2 (Phase 4)**: depends on US1 (verdict read/list builds on US1's persisted verdict + Filter)
- **US3 (Phase 5)**: depends on US1's scorer (T009)
- **US4 (Phase 6)**: depends on US1 scorer (T009) + US3 rescan (T024) — the background rescan worker reuses the US3 scan
- **Polish (Phase 7)**: depends on all desired stories complete

### User Story Dependencies

- **US1 (P1)**: starts after Foundational — no story deps. **MVP.**
- **US2 (P2)**: starts after US1 — independently testable (management surface)
- **US3 (P3)**: starts after US1 (needs the scorer) — independently testable (re-scan)
- **US4 (P3)**: starts after US1 + US3 (needs scorer + rescan path) — independently testable (threat mgmt + auto-rescan)

### Within Each User Story

- Tests written before/alongside impl; scorer test is deterministic and must pass before US1 checkpoint
- Models → storage → engine → transports
- Core (scorer/quarantine) before transport surfacing
- Cross-transport parity last in each story

### Parallel Opportunities

- Phase 1: T001 ∥ T002 (different packages)
- Phase 2: T003 ∥ T004 ∥ T005 (different packages)
- US1: scorer test (T006) ∥ exclusion test (T007); interface (T008) ∥ model fields (T012); the four transport tasks (T013–T016) once T012 lands
- US2/US3/US4: the per-transport surfacing tasks within each story are mutually parallel (different files)

---

## Parallel Example: User Story 1

```bash
# After T012 (shared hit fields land), fan out the four transports:
Task: "Surface verdict + --include-quarantined on CLI (internal/cli)"
Task: "Surface verdict + include_quarantined on REST (internal/rest)"
Task: "proto fields + gRPC (proto/gorag.proto, internal/grpc)"
Task: "MCP schema + renderQuery (internal/mcp/server.go)"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (Setup) → Phase 2 (Foundational)
2. Phase 3 (US1): scorer → ingest wiring → quarantine filter → 4-transport surfacing → parity test
3. **STOP and VALIDATE**: quickstart scenarios 1, 2, 5, 7 — a payload is quarantined, clean corpus unaffected, parity holds, budget holds
4. This alone closes the P0 blind spot (the audit's core ask)

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. + US1 → detect + quarantine-by-default + surface (**MVP — closes blind spot**)
3. + US2 → management surface + false-positive recovery
4. + US3 → back-catalog re-scan
5. + US4 → maintained threat list + feed import + auto-rescan
6. Polish → docs + full quickstart + eval gate + benchmark

---

## Notes

- `[P]` = different files, no deps on incomplete tasks
- `[Story]` maps the task to its user story for traceability
- Every story is independently completable and testable; stop at any checkpoint to validate
- Commit (Conventional Commits, straight to `main`) after each task or logical group
- Avoid: vague tasks, same-file parallel conflicts, cross-story deps that break independence
- Constitution gates (plan.md): pure Go / no CGo, no new dep, <10ms write-ACK preserved, air-gapped (no egress outside explicit `import`)
