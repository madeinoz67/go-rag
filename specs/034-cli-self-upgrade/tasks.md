# Tasks: CLI Self-Upgrade + Schema Migration

**Input**: Design documents from `/specs/034-cli-self-upgrade/` (plan.md, spec.md, research.md, data-model.md, contracts/cli-commands.md, quickstart.md)

**Prerequisites**: plan.md ✅ · spec.md ✅ · research.md ✅ · data-model.md ✅ · contracts/ ✅ · quickstart.md ✅

**Tests**: INCLUDED — go-rag's constitution mandates `go test -race -cover ./...` passes on every change, so each user story carries test tasks.

**Organization**: Tasks grouped by user story (spec.md): US1 (P1, binary self-upgrade = MVP), US2 (P2, schema migration on open), US3 (P2, check-only), US4 (P3, rollback). Dependency order: Setup → Foundational → US1 → {US2 ∥ US3} → US4 → Polish.

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[USx]**: user-story phase only (omitted on Setup / Foundational / Polish)
- File paths are go-rag project-relative (per CLAUDE.md architecture map)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: version-injection plumbing + new package scaffolding. go-rag already builds, so this is light.

- [X] T001 [P] Add compile-time version injection via `-ldflags` in `Makefile` (build target reads the git tag into `internal/cli`'s version var) and wire it at `cmd/go-rag/main.go` (existing `version` arg at main.go:14)
- [X] T002 [P] Scaffold `internal/upgrade` package (package doc + `doc.go` declaring the self-update responsibility)
- [X] T003 [P] Scaffold `internal/storage/migrate` package (package doc + `doc.go` declaring the numbered-migration registry)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the shared release-resolution + semver module that US1 and US3 both consume. MUST complete before US1/US3.

- [X] T004 [P] Implement semver parser + comparator in `internal/upgrade/semver.go` — port MuninnDB's `parseSemver` (strip `v`, drop `-pre`/`+build`, parse major.minor.patch) and `newerVersionAvailable` (false on any parse error to avoid false positives)
- [X] T005 Implement GitHub release resolver in `internal/upgrade/release.go` — `latestVersion()` hitting `api.github.com/repos/madeinoz67/go-rag/releases/latest` (3s timeout, `tag_name`), with an injectable `latestVersionFn` seam for tests; `dev` build ⇒ skip check; network failure ⇒ non-fatal error (depends T004)

**Checkpoint**: discovery layer ready — US1 (full upgrade) and US3 (check-only) can proceed.

---

## Phase 3: User Story 1 — Self-upgrade the go-rag binary in place (Priority: P1) 🎯 MVP

**Goal**: `go-rag upgrade` atomically replaces the running binary with the latest release for the host OS/arch.

**Independent Test**: [quickstart.md](./quickstart.md) Scenario 2 — `go-rag upgrade --yes` against a mock feed; `go-rag version` reports the new tag; `go-rag.prev` retained.

### Implementation for User Story 1

- [X] T006 [P] [US1] Implement asset + checksum URL resolution in `internal/upgrade/release.go` — `releaseAssetURL(version, goos, goarch)` (`go-rag-{ver}-{goos}-{goarch}.tar.gz`) and `checksums.txt` fetch/parse (line per asset)
- [X] T007 [P] [US1] Implement download + extract in `internal/upgrade/download.go` — HTTP GET (5min timeout, progress), gzip→tar extract the `go-rag` binary into a temp file **in `filepath.Dir(exe)`** (same dir is mandatory for atomic rename); cleanup on any error (port MuninnDB `downloadAndExtractBinary`)
- [X] T008 [US1] Implement verification in `internal/upgrade/verify.go` — SHA-256 of the temp file vs `checksums.txt` (fatal mismatch ⇒ abort, remove temp; missing checksum ⇒ fatal, do not install), then functional smoke check running `<tmp> version` (port + strengthen MuninnDB `verifyBinary`)
- [X] T009 [US1] Implement atomic self-replace in `internal/upgrade/selfupdate.go` — `os.Executable()` → `EvalSymlinks`; backup `exe`→`exe.prev`; `os.Rename(tmp, exe)`; on any failure remove temp and leave current binary byte-identical (depends T006, T007, T008)
- [X] T010 [US1] Implement daemon stop/restart coordination in `internal/upgrade/daemon.go` — detect running daemon via existing `internal/daemon` PID/lock; graceful stop (≤15s for Pebble flush+WAL), force-kill if needed, 200ms LOCK-release wait; restart after swap (FR-010)
- [X] T011 [US1] Implement `go-rag upgrade` cobra command in `internal/cli/upgrade.go` — wires resolve→download→verify→backup→swap→restart; `--yes`/`-y` skips confirm; prints step lines; daemon-running warning (FR-010) (depends T009, T010)
- [X] T012 [P] [US1] Tests in `internal/upgrade/*_test.go` — mock feed via `latestVersionFn` seam: happy-path atomic replace, checksum-mismatch abort (binary untouched), verify-failure abort, temp cleanup; isolated DB + non-default ports per CLAUDE.md smoke-test rule

**Checkpoint**: MVP delivered — `go-rag upgrade` works end-to-end.

---

## Phase 4: User Story 3 — Check for a newer version without applying it (Priority: P2)

**Goal**: `go-rag upgrade --check` reports current vs latest, exits non-zero if newer, modifies nothing.

**Independent Test**: [quickstart.md](./quickstart.md) Scenario 1 — binary byte-identical before/after; exit `1` when newer, `0` when current.

### Implementation for User Story 3

- [X] T013 [US3] Add `--check` flag + exit-code semantics to `internal/cli/upgrade.go` — print current/latest + verdict; exit `1` if update available, `0` if up-to-date; no download, no binary change, no identifying data sent (reuses T004/T005 discovery)
- [X] T014 [P] [US3] Tests in `internal/cli/upgrade_test.go` — `--check` leaves binary byte-identical (sha256 before/after); correct exit codes; offline ⇒ clear error, no false "up-to-date"

**Checkpoint**: non-destructive visibility works for scripts/CI.

---

## Phase 5: User Story 2 — Schema auto-migrates safely on first open (Priority: P2)

**Goal**: a newer binary opening an older store auto-migrates it before serving; failed/interrupted migration never bricks the store.

**Independent Test**: [quickstart.md](./quickstart.md) Scenario 6 — first open migrates v1→v2 and returns correct results; `kill -9` mid-migration ⇒ re-open replays and completes; Scenario 7 — older binary refuses a newer-schema store.

> **Note**: US2 is independent of US1/US3 (different packages) — can be developed in parallel with Phase 4.

### Implementation for User Story 2

- [X] T015 [P] [US2] Implement schema-version key helpers in `internal/storage/schema_version.go` — global meta prefix `0xFF|"schema_ver"`, 8-byte big-endian uint64; `readSchemaVersion` returns `0` on `ErrNotFound` (the v0 bootstrap); `writeSchemaVersion` with `pebble.Sync`
- [X] T016 [US2] Implement migration Runner in `internal/storage/migrate/migrate.go` — `Migration{Version, Description, Up func(*pebble.DB) error}`, `Register`, `Run` (sort ascending, apply only `Version > current`, fsync version after each `Up`, return `(applied, firstErr)`) (depends T015; port MuninnDB `migrate.go`)
- [X] T017 [US2] Implement v1 bootstrap migration in `internal/storage/migrate/v1_bootstrap.go` — writes the schema-version key = 1; idempotent (no-op if already ≥1) (depends T016)
- [X] T018 [US2] Wire the Runner into the store-open path in `internal/engine` — run migrations before serving any operation, under the existing single-writer lock; print a one-line `migrating store schema…` notice so the one-time cost is visible (depends T016, T017)
- [X] T019 [US2] Implement refuse-newer-schema guard in `internal/engine` — if stored version > binary's max known, refuse to serve with a clear error (no silent misread, no auto-downgrade) (FR-015/R9)
- [X] T020 [P] [US2] Tests in `internal/storage/migrate/*_test.go` + `internal/engine` — v0→v1 bootstrap (absent key), multi-step ascending, idempotent replay after simulated crash (version un-advanced ⇒ re-run), refuse-newer-schema, no-op when current, correct query results post-migration

**Checkpoint**: the 1:1 binary↔schema coupling is handled — upgraded binaries safely open older stores.

---

## Phase 6: User Story 4 — Safe rollback to the previous binary (Priority: P3)

**Goal**: `go-rag upgrade --rollback` restores the prior binary offline (relies on the US1 backup step).

**Independent Test**: [quickstart.md](./quickstart.md) Scenario 5 — after upgrade, `--rollback` restores the prior tag; no network needed.

### Implementation for User Story 4

- [X] T021 [US4] Add `--rollback` flag to `internal/cli/upgrade.go` — reverse the US1 backup: rename `exe`→`exe.broken`, `exe.prev`→`exe`; clean error if no `.prev` exists; offline (depends T009's backup mechanism)
- [X] T022 [P] [US4] Tests in `internal/upgrade/*_test.go` — rollback restores prior version (reported by `go-rag version`); no-`.prev` ⇒ clean non-destructive exit

**Checkpoint**: upgrades are reversible — trustworthy to actually use.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: docs, release tooling, and the spec correction surfaced by research.

- [X] T023 [P] Document the new `0xFF` global meta prefix in `PRD_RAG_Database.md` §6.7 (key-space schema summary) and the `internal/storage/migrate` row in `CLAUDE.md`'s architecture map
- [X] T024 Release pipeline — `.github/workflows/release.yml` (pre-existing) builds per-OS/arch `go-rag-{ver}-{goos}-{goarch}.tar.gz` + Windows zip + model bundle + `checksums.txt` (SHA-256) on tag push; `-ldflags` injects the version. The upgrade code resolves the hyphen-named assets (verified against the real v0.1.4 release). A redundant `make release` target added during planning was removed — release.yml is canonical.
- [X] T025 Reword spec FR-014 in `specs/034-cli-self-upgrade/spec.md` to name the **idempotent-replay** crash-safety mechanism (per research.md R8), retaining the Pebble Checkpoint only as an escape hatch for non-idempotent steps
- [X] T026 Run all [quickstart.md](./quickstart.md) validation scenarios end-to-end on an isolated DB (non-default ports); capture pass/fail per scenario
- [X] T027 [P] Handle Windows in `internal/cli/upgrade.go` — print the asset URL and exit without self-replace (OS locks running executables); document as a known v1 limitation in `CLAUDE.md`/quickstart

---

## Dependencies & Execution Order

```
Phase 1 (Setup)          T001, T002, T003            [all parallel]
      │
Phase 2 (Foundational)   T004  →  T005              [T004 ‖ T001-T003]
      │
Phase 3 (US1 — MVP)      T006 ‖ T007 → T008 → T009 → T010 → T011 ; T012 [P]
      │
      ├──▶ Phase 4 (US3)   T013 → T014 [P]           [independent of US2]
      ├──▶ Phase 5 (US2)   T015 → T016 → T017 → T018 → T019 ; T020 [P]   [∥ US3]
      │
Phase 6 (US4)            T021 → T022 [P]             [depends on US1 T009 backup]
      │
Phase 7 (Polish)         T023 ‖ T024 ‖ T025 ‖ T026 ‖ T027   [T026 last]
```

**MVP scope**: Phase 3 (US1) alone — `go-rag upgrade` working end-to-end. Everything else is incremental.

**Parallel opportunities**:
- Setup (T001–T003) all parallel.
- Within US1: T006, T007, T008 hit different files (parallel); T012 tests parallel.
- **US2 (Phase 5) and US3 (Phase 4) are fully independent** — develop concurrently after US1.
- Within US2: T015 parallel with US1 work; T020 tests parallel.
- Polish (T023/T024/T025/T027) parallel; T026 (quickstart run) gates release.
