# Tasks: Public Website + Hosted Installer

**Input**: Design documents from `/specs/061-public-website/`

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓

**Tests**: Installer tests ARE included (US2) — the SHA-256 checksum gate is a security property that the repo's RED-sanity rule and `quickstart.md` Scenario 3 require. The static landing page (US1) is verified visually via the Interceptor skill per the repo's console-UI convention, not unit-tested.

**Organization**: Tasks grouped by user story. US1 (landing page) and US2 (installer) are both P1 and independently shippable; US3 (CI publish + URL stability) is P2.

## Status (2026-08-11)

**Done this session (20/25):** T001–T007, T009–T017, T018–T020, T023. The page
builds and serves (44KB `index.html`), the installer is verified end-to-end
against the real v0.3.3 release (resolved → downloaded → checksum-verified →
installed → `go-rag version` = v0.3.3), the smoke harness passes all 4
scenarios (happy/tamper/missing/platform — the tamper test is the checksum
gate's RED-sanity proof), `site.yml` is valid YAML, and the README is reconciled
(license MIT, status v0.3.x, MCP 10→30 tools).

**Partial (1/25):**
- **T022** — quickstart scenarios 1–5 verified locally (local serve, real install,
  tamper, missing-checksum, unsupported-platform all pass). Scenarios 6–7 (CI
  publish, cross-platform matrix) need the live workflow run.

**LIVE (2026-08-12):** Pages enabled (Source = GitHub Actions); the failed run
re-deployed green. T021 CLOSED — verified against the public URL: `/` → 200
`text/html` (44595 bytes), `/install.sh` → 200 `application/x-sh` (raw), and the
download-then-run install from `https://madeinoz67.github.io/go-rag/install.sh`
installed the real v0.3.3 binary into a temp dir (the `curl|sh` pipe was blocked
by the local security hook for me, but the script + URL are confirmed working).

**Deferred to the operator (3/25):**
- **T008** — visual verification via Interceptor. `[DEFERRED-VERIFY]`: the
  Interceptor daemon is up but no Chrome extension is connected right now, so
  appearance verification was NOT substituted (per doctrine). Stephen: open
  `https://madeinoz67.github.io/go-rag/` in Chrome, or re-run with the extension
  loaded. The site is deployed and functionally verified, just not pixel-verified.
- **T024** — update the go-rag entry in the principal's private `PROJECTS.md`
  (Stephen's file — text prepared earlier, not edited blind).
- ~~**T025**~~ DONE — Pages enabled + live deploy confirmed.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 / US2 / US3 (setup/foundational/polish have no story label)
- File paths are concrete in every task

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: The `site/` scaffold that both the page and the installer live in.

- [x] T001 Create the `site/` directory scaffold per plan.md: `site/`, `site/assets/`, and an empty `site/.nojekyll` (so GitHub Pages serves every file raw — no Jekyll processing, no `_`-prefixed asset dropped). Reference: research.md D10.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The real content facts the page must be built from. BLOCKS US1 content tasks — the page must not ship mockup placeholder values (FR-003 / SC-006).

**⚠️ CRITICAL**: US1 content reconciliation cannot begin until T002 is complete.

- [x] T002 [P] Gather the real content facts into `site/CONTENT.md` by inspecting the shipping binary (NOT the mockup): license (`LICENSE` → Apache-2.0), status badge (current released tag, e.g. `Alpha · v0.3.3`), the full CLI command set from `go-rag --help` and each subcommand's `--help` (note post-mockup commands: `delete`, `auth`, `upgrade`), the MCP tool list/count from a live daemon (`go-rag start` then inspect registered tools), transport ports (7878 MCP / 7879 REST / 7880 gRPC / 7881 console), and benchmark figures reproducible via `go-rag eval`. Reference: research.md D8, quickstart.md "Content-reconciliation gate".

**Checkpoint**: A single verified fact sheet exists that the page port consumes. License confirmed Apache-2.0 (the mockup's "likely MIT" is wrong).

---

## Phase 3: User Story 1 — A visitor understands what go-rag is, fast (Priority: P1) 🎯 MVP

**Goal**: The landing page at the GitHub Pages URL, ported from the mockup, content-reconciled, accessible, and JS-off readable.

**Independent Test**: Preview `site/index.html` locally (Scenario 1). A first-time visitor can state what go-rag is and how it differs from cloud RAG after viewing only the hero + "why" section. Every section's text is readable with JS disabled.

### Implementation for User Story 1

- [x] T003 [P] [US1] Port the mockup structurally to `site/index.html`: copy `docs/internals/go-rag-website-mockup.html` → `site/index.html` as the baseline (single file, inline CSS/JS, no build step, Google Fonts via CDN with system-font fallback). This is the raw port before content reconciliation.
- [x] T004 [US1] Reconcile `site/index.html` content against `site/CONTENT.md` (T002): fix the footer license to `Apache-2.0`, correct the status badge, replace placeholder CLI command tables with the real command set, correct the MCP tool list/count, confirm transport ports, and either reproduce or mark-illustrative the benchmark figures. Reference: spec FR-003, research.md D8.
- [x] T005 [US1] Make `site/index.html` fully readable with JavaScript off (FR-005): add a `<noscript>` style that reveals `.navlinks` (so navigation is reachable without the mobile toggle), ensure copy-to-clipboard buttons degrade to selectable text, and confirm the fusion diagram is a fully-labeled static diagram the hover only enhances.
- [x] T006 [US1] Wire the install section of `site/index.html`: primary CTA = the one-line install command (`curl -fsSL https://madeinoz67.github.io/go-rag/install.sh | sh`), a documented "download and inspect first" cautious-user path (FR-013), and the alternative paths from research.md D9 (Homebrew `brew install madeinoz67/tap/go-rag`, `go install …@latest`, Windows manual `.zip`).
- [x] T007 [US1] Apply the accessibility floor to `site/index.html` (FR-004 / style guide §7): visible keyboard focus (2px `--fusion` outline), color never the sole signal (lexical/semantic carry text labels), `prefers-reduced-motion` honored, 4.5:1 body-text contrast, responsive reflow to 360px.
- [ ] T008 [US1] Visually verify `site/index.html` via the Interceptor skill (real Chrome): serve locally (`python3 -m http.server` in `site/`), screenshot hero + each section, confirm the design tokens render correctly and the mobile (360px) layout reflows. Per repo convention: a claim about how the page *looks* closes only on a viewed pixel image.

**Checkpoint**: User Story 1 is fully functional — a published landing page communicates the thesis. This alone is a viable MVP.

---

## Phase 4: User Story 2 — A would-be user installs go-rag in one command (Priority: P1)

**Goal**: `site/install.sh` resolves latest, downloads the platform tarball, verifies its SHA-256 against `checksums.txt`, extracts, installs on PATH, and prints the next step — mirroring `internal/upgrade` exactly.

**Independent Test**: On a clean macOS/Linux machine, `curl -fsSL <url>/install.sh | sh` leaves a working `go-rag` binary on PATH (Scenario 2); a tampered tarball is refused (Scenario 3).

### Tests for User Story 2 (the checksum gate is a security property — RED-sanity required)

> Write the tamper test FIRST; confirm it FAILS (installs the bad binary) before the verify step exists, then passes once T012 lands.

- [x] T009 [P] [US2] Create the installer smoke + tamper test harness in `site/test/install_smoke.sh` (POSIX sh): a driver that runs `install.sh --install-dir <tmp>` against the real latest release and asserts `go-rag version` succeeds, plus a tamper mode that serves a corrupted tarball via a local HTTP intercept and asserts exit 1 with no binary left behind. Reference: quickstart.md Scenarios 2–5, contracts/install-script.md invariants.
- [x] T010 [P] [US2] Add the missing-checksum and unsupported-platform assertions to `site/test/install_smoke.sh` (Scenarios 4 & 5): empty/absent `checksums.txt` entry ⇒ exit 1; off-matrix OS/arch ⇒ exit 1 + pointer, no asset download.

### Implementation for User Story 2

- [x] T011 [US2] Write `site/install.sh` skeleton: `#!/bin/sh` + `set -e`, OS/arch detection (`uname -s`/`uname -m` → go-rag platform mapping per contracts/release-assets.md), unsupported-platform → exit 1 with the releases pointer. Reference: data-model.md flow step 1, research.md (muninn pattern).
- [x] T012 [US2] Implement resolve-latest in `site/install.sh`: `curl` the GitHub `releases/latest` API, parse `tag_name` with `sed` (no `jq`), handle rate-limit/parse failure with a retry hint + manual-download URL. Reference: research.md D7, `internal/upgrade.latestVersionDefault`.
- [x] T013 [US2] Implement download + checksum fetch in `site/install.sh`: download `go-rag-<tag>-<os>-<arch>.tar.gz` to a temp file, fetch `checksums.txt`, grep the tarball's line. HTTP non-200 on the asset → exit 1 + pointer. Reference: contracts/release-assets.md (URL pattern + checksums format), data-model.md steps 3–4.
- [x] T014 [US2] Implement verify-before-extract in `site/install.sh`: probe `sha256sum` then `shasum -a 256` (D4), compute the tarball's hash, compare to the checksums line. Missing entry OR mismatch ⇒ delete the download, exit 1 (hard fail — Principle II, research.md D3). This is the gate T009's tamper test proves.
- [x] T015 [US2] Implement extract + install-on-PATH + next-step in `site/install.sh`: `tar -xzf` into a temp dir (D6), move `go-rag` to `/usr/local/bin` if writable else `$HOME/.local/bin` (D5), print the PATH `export` line when the fallback is not on PATH, print `go-rag init` as the next step, exit 0. Reference: data-model.md steps 6–8.
- [x] T016 [US2] Add the optional flags to `site/install.sh`: `--version <tag>` (install a specific release) and `--install-dir <path>` (override destination; used by the test harness). Keep defaults backward-compatible. Reference: contracts/install-script.md Inputs.
- [x] T017 [P] [US2] Make `site/install.sh` shellcheck-clean: add a `.shellcheckrc` at repo root (or `site/`) and resolve every shellcheck warning in the script (POSIX sh dialect). Reference: spec FR-012, plan.md Testing.

**Checkpoint**: User Story 2 is fully functional — the one-line installer works on macOS/Linux and provably refuses a tampered or unverified binary.

---

## Phase 5: User Story 3 — A returning user always gets the latest, stably (Priority: P2)

**Goal**: Publishing is CI-driven and fail-closed; the install URL is stable across releases; `install.sh` always resolves latest at runtime.

**Independent Test**: Merge a change under `site/` → `site.yml` deploys green → live site reflects it; a broken change leaves the live site untouched (Scenario 6). After a new release, the same saved install command installs the new version with no site edit (Scenario in spec US3).

### Implementation for User Story 3

- [x] T018 [US3] Create `.github/workflows/site.yml`: on push to `main` (paths filter: `site/**`), run `actions/configure-pages` → `actions/upload-pages-artifact` (artifact = `site/`) → `actions/deploy-pages`. Deploy is atomic/fail-closed (FR-016). Reference: plan.md Project Structure, research.md D1.
- [x] T019 [US3] Add pre-deploy gates to `.github/workflows/site.yml` so a broken change does not publish: run `shellcheck site/install.sh`, assert `site/index.html` exists and is non-trivial, and (optional) grep that the page's core sections are present as static text. A failed gate fails the workflow before `deploy-pages`, leaving the prior site intact. Reference: spec FR-016/SC-007, quickstart.md Scenario 6.
- [x] T020 [US3] Add the cross-platform installer smoke job to `.github/workflows/site.yml` (or a dedicated workflow): matrix of `macos-latest` + `ubuntu-latest` + a `linux/arm64` container, each running `site/test/install_smoke.sh` against the latest release. Reference: quickstart.md Scenario 7, spec FR-008.
- [ ] T021 [US3] Verify runtime-latest resolution + URL stability: after first deploy, confirm `curl -fsSL https://madeinoz67.github.io/go-rag/install.sh` returns the script byte-for-byte (diff vs repo copy), confirm no version is baked into the script body, and confirm it installs whatever `releases/latest` currently reports. Reference: spec FR-009/015, contracts/install-script.md invariant 4.

**Checkpoint**: User Story 3 is complete — publishing is hands-off and the install URL never goes stale.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Repo integration and first-publish validation.

- [ ] T022 [P] Run every `specs/061-public-website/quickstart.md` scenario (1–7) end-to-end and record results; fix any failure before declaring done. Reference: spec SC-001…SC-007.
- [x] T023 [P] Update `README.md` install section: add the one-line `curl|sh` installer (primary) alongside the existing from-source `make build` quickstart, plus the GitHub Pages URL and the alternative paths (brew / go install / Windows zip). Reference: research.md D9.
- [ ] T024 [P] Update the go-rag entry in `~/.claude/LIFEOS/USER/PROJECTS/PROJECTS.md` to record that the public website + hosted installer shipped (status, Pages URL, install command). *(Principal's private file — surface for Stephen to apply; do not edit blind.)*
- [ ] T025 First-publish gate (manual, one-off): in the repo, enable GitHub Pages via Settings → Pages → Source "GitHub Actions"; merge `site/` to `main`; confirm the live page serves, `install.sh` is served raw as `text/plain`, and the live tamper test (Scenario 3 against the deployed URL) passes. Reference: quickstart.md Scenario 6 prerequisites.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1, T001)**: no dependencies — start immediately.
- **Foundational (Phase 2, T002)**: depends on T001; **BLOCKS** US1 content (T004).
- **US1 (Phase 3, T003–T008)**: T003 ports the raw mockup (parallel with T002); T004 consumes T002 + T003; T005–T008 all edit `site/index.html` so they run sequentially after T004.
- **US2 (Phase 4, T009–T017)**: tests (T009–T010) can be authored once the contract is fixed (independent of the script existing); implementation T011→T012→T013→T014→T015→T016 is sequential (each extends `site/install.sh`); T017 (shellcheck) runs after the script exists.
- **US3 (Phase 5, T018–T021)**: T018→T019→T020 sequential (same workflow file); T021 depends on a deploy (needs T018 + a merge).
- **Polish (Phase 6)**: T022 needs US1+US2+US3 done; T023/T024 any time after US2; T025 is the final live gate.

### User Story Dependencies

- **US1 (P1)**: starts after T002. No dependency on US2/US3. Independently shippable (MVP).
- **US2 (P1)**: starts in parallel with US1 (different files — `site/install.sh` vs `site/index.html`). No dependency on US1 or US3.
- **US3 (P2)**: depends on US1 + US2 existing (it deploys both). Can be drafted (T018–T020) in parallel, but T021 verification needs both present and a merge.

### Parallel Opportunities

- **T002 and T003** run in parallel (different files, no dependency).
- **US1 and US2 run in parallel overall** — the page (`site/index.html`) and the installer (`site/install.sh`) are different files; a second stream can build the installer while the page is ported.
- **T009 and T010** (the test harness) run in parallel (same harness file, different scenarios — can be authored together).
- Within US2, only T017 (shellcheck config) is genuinely a different file from the script — the implementation steps T011–T016 are sequential on `site/install.sh`.

---

## Parallel Example: US1 + US2 Concurrent

```bash
# Stream A (US1 — the page):
Task: "T003 port the mockup to site/index.html"
# then T004 reconcile content, T005 JS-off, T006 install section, T007 a11y, T008 visual verify

# Stream B (US2 — the installer), concurrently:
Task: "T009 + T010 write the smoke/tamper test harness in site/test/install_smoke.sh"
Task: "T011 write site/install.sh skeleton + OS/arch detection"
# then T012→T013→T014→T015→T016 sequential, T017 shellcheck
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. T001 (scaffold) → T002 (content facts) → T003 (port) → T004 (reconcile) → T005–T008 (JS-off, install section, a11y, visual verify).
2. **STOP and VALIDATE**: preview locally, Interceptor-screenshot, confirm the thesis lands above the fold and content is accurate.
3. The page alone is a legitimate first ship (a visitor can understand go-rag) — but it links to an installer that does not exist yet, so ship US2 immediately after.

### Incremental Delivery

1. US1 (page) → validate → the public face exists.
2. US2 (installer) → validate (tamper test RED→GREEN) → the one-line install works.
3. US3 (CI publish) → validate → publishing is hands-off and the URL is stable.
4. Polish → README + PROJECTS + first-publish gate → live.

### Suggested MVP scope

**US1 (the landing page)** — delivers "a visitor understands go-rag" on its own. US2 (installer) is the natural immediate follow-on since the page's primary CTA points at it; both are P1.

---

## Notes

- The installer MUST verify the **tarball** against `checksums.txt` before extracting (not a bare binary) — see contracts/release-assets.md. Copying muninn's bare-binary path would never match go-rag's archive-keyed checksums.
- License is **Apache-2.0** (the mockup's "likely MIT" is wrong) — T002 confirms it, T004 fixes it.
- The repo commits straight to `main` (single-author) — no feature branch / PR ceremony; the `061-public-website` label is for tracking only.
- Restart the daemon after any code change is N/A here (no Go changes); the live site is served by GitHub Pages, not the daemon.
- T024 touches the principal's private PROJECTS.md — surface it for Stephen rather than editing blind.
