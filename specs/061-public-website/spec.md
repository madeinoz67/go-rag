# Feature Specification: Public Website + Hosted Installer

**Feature Branch**: `061-public-website`

**Created**: 2026-08-11

**Status**: Draft

**Input**: User description: "I want to start on public facing website, there is website styleguide and mockup in docs/internal. Website will be using the github site. Also want to include a hosted install shell script just like how muninn install works."

## User Scenarios & Testing *(mandatory)*

This feature has two independently shippable halves: (1) a public landing page
built from the existing style guide and mockup, hosted on GitHub Pages; and
(2) a hosted `install.sh` that installs the latest go-rag release in one
command. Each half delivers value on its own.

### User Story 1 - A visitor understands what go-rag is, fast (Priority: P1)

A developer who has never heard of go-rag follows a link to the project's
public website. Within the first screen they understand: this is a RAG database
that lives in one binary, runs locally, and works offline. Scrolling the page,
they see the hybrid-retrieval story (two lists becoming one), the architecture
at a glance, the quickstart, and the full CLI surface. The page's voice,
layout, and visual identity match the existing style guide — quiet, technical,
code-forward, no hype.

**Why this priority**: The project currently has no public face beyond the
GitHub README. A clear, fast landing page is the difference between a visitor
bouncing and a visitor installing. This is the existence-proof of the project
as a product, and everything else (the installer, the docs links) hangs off it.

**Independent Test**: Publish the landing page alone. A first-time visitor can
state, in one sentence, what go-rag is and how it differs from a cloud RAG
service after viewing only the hero and the "why" section.

**Acceptance Scenarios**:

1. **Given** a browser with no prior visit, **When** the visitor opens the
   project's GitHub Pages URL, **Then** the landing page loads with the hero
   headline, the one-line thesis, the terminal quickstart, and the primary
   call-to-action all visible above or within the first scroll.
2. **Given** a visitor scanning the page, **When** they reach the retrieval
   section, **Then** the RRF fusion diagram (lexical + semantic → one ranked
   list) communicates the core differentiator with a text label on every
   colored element (color is never the only signal).
3. **Given** a visitor who wants detail, **When** they scroll through features,
   architecture, CLI reference, MCP, benchmarks, and security, **Then** every
   section is populated with content consistent with the shipping go-rag binary
   at publish time — no stale placeholder commands, tool names, ports, or
   benchmark figures.
4. **Given** a visitor on a phone (360px wide), **When** the page loads, **Then**
   the layout reflows to a single column, navigation collapses, and all content
   remains readable and reachable.

---

### User Story 2 - A would-be user installs go-rag in one command (Priority: P1)

A developer who has decided to try go-rag copies a single line from the
website and runs it. The install script resolves the latest release, picks the
right binary for their OS and architecture, verifies it against the published
SHA-256 checksums, installs it on their PATH, and prints the exact next
command to run. This mirrors the muninn installer's shape: `curl -fsSL <url>/install.sh | sh`.

**Why this priority**: The product thesis is "as frictionless as `git init`."
That promise is broken if the first step — getting the binary — requires
reading release notes, picking an asset, and placing it manually. The one-line
installer is the load-bearing entry point; it shares priority with the page.

**Independent Test**: On a clean macOS or Linux machine with no go-rag present,
run the install command from the site. `go-rag --version` (or the documented
equivalent) succeeds immediately after, with no manual steps.

**Acceptance Scenarios**:

1. **Given** a macOS or Linux machine on amd64 or arm64, **When** the user runs
   the one-line install command from the website, **Then** the latest go-rag
   binary is downloaded, checksum-verified, installed on the PATH, and the next
   command to run is printed.
2. **Given** the same machine, **When** the install completes, **Then** running
   `go-rag` (or the documented smoke command) from a fresh shell works without
   any further setup.
3. **Given** a binary whose checksum does not match the published
   `checksums.txt`, **When** the installer runs, **Then** it deletes the
   downloaded file, refuses to install, prints a clear mismatch message, and
   exits non-zero.
4. **Given** a user who will not pipe to `sh` unseen, **When** they look at the
   install section of the site, **Then** a "download and inspect first" path is
   documented alongside the one-liner, and the script is readable at its URL.

---

### User Story 3 - A returning user always gets the latest, stably (Priority: P2)

The install URL printed on the site, in the README, and anywhere else never
needs editing across releases. A user who saved the install command six months
ago runs it today and still gets the current release. The site itself may be
cached by GitHub Pages or a CDN; the install still resolves "latest" at the
moment it runs.

**Why this priority**: Stability of the install URL is what makes the installer
worth printing. It is lower priority than the page and the first install only
because it is a property of how the installer is built rather than a separate
user-facing surface — but it must hold from day one.

**Independent Test**: Pin the install command in a doc. After a new release is
published, run the same command and confirm it installs the new version with no
edits to the command or the site.

**Acceptance Scenarios**:

1. **Given** the install script as published on the site, **When** it runs on
   two different days spanning a release, **Then** it installs whichever
   release is latest on each day without the script itself changing.
2. **Given** a cached copy of the site's `install.sh`, **When** the script runs,
   **Then** it queries the release source at runtime and is not pinned to a
   version baked into the script body.
3. **Given** a new release published through the existing release pipeline, **When**
   no one edits the website, **Then** the install command on the site still
   installs the new release.

---

### Edge Cases

- **Unsupported OS or architecture** (e.g. Windows, armv7, i386): the install
  script exits non-zero with a short message and a pointer to the manual
  download on GitHub releases. Windows users are expected to download manually;
  `curl | sh` is not a Windows path.
- **Anonymous GitHub API rate limit** when resolving the latest release: the
  script reports the cause, suggests retrying shortly, and points to the manual
  download rather than failing opaquely.
- **Release asset missing for the detected platform** (HTTP non-200 on
  download): clear error naming the platform, the URL attempted, and the
  manual-download link — not a raw curl failure.
- **No `checksums.txt` published** (e.g. a very old or partial release): warn
  plainly that integrity could not be verified and continue, rather than block
  — matching the muninn installer's posture. (For normal go-rag releases,
  `checksums.txt` is produced by `make release`, so this is a safety net, not
  the expected path.)
- **Install directory not writable** (`/usr/local/bin` requires root): fall
  back to a user directory and, if that directory is not on `PATH`, print the
  exact `export PATH=...` line for the user's shell profile.
- **Cautious user who will not `curl | sh`**: a documented "download, read,
  then run" path is provided; the script is plain POSIX `sh` and auditable.
- **Site content drift**: a CLI command, MCP tool name, transport port, or
  benchmark figure on the page diverges from the shipping binary. This is a
  content-quality failure caught at publish time, not at runtime — see FR-003
  and the content-reconciliation assumption.
- **JavaScript unavailable**: the page's informational content (every section's
  text, tables, code blocks) is readable with JS off; the fusion-diagram hover
  highlight and copy buttons are additive enhancements that degrade silently.

## Requirements *(mandatory)*

### Functional Requirements

**Website (landing page)**

- **FR-001**: A public website MUST be reachable at the project's GitHub Pages
  URL and serve a landing page whose structure follows the existing style guide
  and mockup — hero, why, features, retrieval/RRF, architecture, quickstart,
  CLI reference, MCP, benchmarks, security, and footer.
- **FR-002**: The hero MUST communicate go-rag's thesis — single-binary,
  local-first, offline-by-default retrieval — above the fold, written in the
  style guide's voice (the person's side of the terminal; no "supercharge /
  unlock / seamless"; no exclamation points).
- **FR-003**: Every piece of website copy that states product behavior — CLI
  commands, MCP tool names, transport ports, feature claims, benchmark figures,
  version/status badges — MUST match the shipping go-rag binary and PRD at
  publish time. Illustrative placeholder content carried over from the mockup
  MUST be reconciled to the real values before the page goes live.
- **FR-004**: The site MUST follow the style guide's accessibility floor:
  visible keyboard focus, color never the sole signal (lexical and semantic
  elements always carry a text label), `prefers-reduced-motion` honored,
  4.5:1 minimum body-text contrast, and responsive behavior down to 360px.
- **FR-005**: The site's informational content MUST be readable with JavaScript
  disabled. Interactive elements (fusion-diagram motion, copy-to-clipboard,
  mobile nav) are progressive enhancements that degrade to a fully labeled,
  usable static state.
- **FR-006**: The site MUST be served over HTTPS from a stable URL that does not
  change between publishes, so that any install command printed on the page, in
  the README, or elsewhere remains valid across releases.

**Hosted install script**

- **FR-007**: A POSIX-`sh` install script MUST be published at a stable URL on
  the site (`<site-url>/install.sh`) and installable with a one-line
  `curl -fsSL <url> | sh` command, mirroring the muninn installer's shape.
- **FR-008**: The install script MUST detect the host operating system and
  architecture and select the matching prebuilt release artifact. Unsupported
  platforms MUST exit non-zero with a pointer to the manual download on GitHub
  releases.
- **FR-009**: The install script MUST resolve the latest release dynamically at
  install time rather than from a version baked into the script body, so a
  single published URL never goes stale across releases.
- **FR-010**: The install script MUST verify the downloaded binary against the
  release's published SHA-256 checksums (`checksums.txt`) before installing. A
  checksum mismatch MUST be fatal — the downloaded file deleted, the install
  aborted, a clear mismatch message printed, exit non-zero. This reuses the same
  `checksums.txt` contract already produced by `make release` and already
  verified in-process by `go-rag upgrade` (spec 034).
- **FR-011**: The install script MUST place the binary on the user's PATH —
  preferring a system directory when writable, otherwise falling back to a user
  directory — and, when the fallback directory is not on `PATH`, MUST print the
  exact shell line to add it.
- **FR-012**: The install script MUST run under `sh` (not require bash), use
  `set -e`, write plain progress and error messages, and make no changes
  outside the single binary destination — no shell-profile edits, no telemetry,
  no background services, no network calls beyond resolving the release and
  downloading the one binary and its checksums.
- **FR-013**: The site MUST document a "download and inspect first" path
  alongside the pipe-to-`sh` one-liner, so a cautious user can read the script
  before executing it.

**Publishing and source of truth**

- **FR-014**: The site's source and the install script MUST live in the
  repository, and whatever is live at the Pages URL MUST be reproducible from
  `main` — no out-of-band manual edits to the live site.
- **FR-015**: The published `install.sh` MUST always install the latest
  release, even when the site is served from cache. This is satisfied
  structurally because the script resolves "latest" at runtime (FR-009) rather
  than embedding a version.
- **FR-016**: Publishing the site MUST be a CI-driven step, not a manual local
  command. A merge to `main` (and, where relevant, a release event) MUST
  trigger the publish workflow that builds/deploys the site and the hosted
  install script to GitHub Pages, with no human action beyond the merge. The
  publish workflow MUST fail closed — a broken build or a failed gate leaves
  the existing live site untouched rather than publishing a half-site.

### Key Entities *(include if feature involves data)*

- **Landing Page**: the single public HTML page and its sections (per the
  mockup), the unit of content a visitor sees. Sourced from the style guide for
  visual identity and from the shipping CLI/PRD for factual content.
- **Install Script**: the `install.sh` artifact published at `<site-url>/install.sh`.
  Its inputs are the host OS/arch and the release source; its output is one
  installed binary on the PATH and a printed next step.
- **Release Artifact**: the prebuilt binary and its `checksums.txt`, produced by
  the existing `make release` pipeline (spec 034). The installer is a consumer
  of this artifact, not a producer of it; no change to the release pipeline is
  in scope unless plan-phase research finds the asset format blocks a clean
  install (see Assumptions).
- **GitHub Pages URL**: the stable, HTTPS address from which both the page and
  the install script are served, and which appears in the install command
  printed on the page and in the README.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a clean macOS or Linux machine (amd64 or arm64) with no prior
  go-rag, a visitor who copies the one-line install command from the site and
  runs it ends with a working `go-rag` binary on their PATH and the next
  command to run visible in their terminal — no manual asset selection, no
  build, no edit.
- **SC-002**: A first-time visitor, viewing only the hero and the "why"
  section, can state in one sentence what go-rag is and how it differs from a
  cloud RAG service — measurable by whether the hero headline, lede, and
  terminal demo together carry the thesis without the rest of the page.
- **SC-003**: The published installer provably refuses a tampered binary —
  given a download whose SHA-256 does not match the published `checksums.txt`
  entry, the script deletes it and exits non-zero, every time.
- **SC-004**: The landing page's core content (every section's text, tables, and
  code samples) is static and readable in a fresh browser with JavaScript off;
  interactive enhancements are additive, not load-bearing.
- **SC-005**: The install URL printed on the site is byte-identical before and
  after a release, and the same saved command installs the new release with no
  edit to the site or the command.
- **SC-006**: At publish time, zero stale claims — every CLI command, MCP tool
  name, transport port, and benchmark figure shown on the site matches the
  shipping binary, verified against the live `go-rag` CLI and the PRD.
- **SC-007**: Publishing is hands-off — a merge to `main` deploys the site and
  the hosted installer through CI with no manual command, and a failed publish
  leaves the existing live site intact (the deploy is atomic-or-bust, never a
  half-published site).

## Assumptions

- **Default scope is the single landing page the mockup depicts**, not a
  multi-page docs site. The mockup is a one-pager; shipping it faithfully is v1.
  The page's information architecture (section anchors, GitHub/PRD links for
  depth) should not preclude adding standalone pages later, but building a
  multi-page docs site is out of scope unless the principal widens the ask.
  *(This is the one place a reasonable default was chosen over a blocking
  question — flag it back to the principal if a fuller docs site was intended.)*
- **Hosting is GitHub Pages at the project's github.io URL** (no custom domain
  in v1), and **publishing is a GitHub Actions workflow** that deploys on merge
  to `main` — the site goes live through CI, not a manual local command. The
  install command is therefore `curl -fsSL https://<user>.github.io/go-rag/install.sh | sh`.
  A custom domain can be layered on later (it only changes the host in the
  printed command; the script is still served by Pages). The publish workflow
  slots into the existing `.github/workflows/` alongside `ci.yml`.
- **The website is a project/marketing artifact, not a component of the go-rag
  binary.** It does not ship inside the binary and is therefore not governed by
  constitution Principles I–V (which constrain the binary's architecture and
  the on-disk key-space). This is a third, separate category from (a) the
  in-product management console carved back into scope by spec 046 (an embedded
  loopback SPA) and (b) the original PRD §2.2 "web UI" exclusion (which meant a
  product UI, not a public web presence). Noting this explicitly so the
  plan-phase Constitution Check does not flag "web UI" as out of scope.
- **The installer consumes the existing release pipeline** (`make release` from
  spec 034, which already publishes per-OS/arch assets and `checksums.txt`).
  Whether the script downloads a bare binary or downloads-and-extracts an
  archive is a plan-phase implementation decision driven by the existing asset
  naming — not a product decision. The integrity contract (`checksums.txt`) is
  reused unchanged; `go-rag upgrade` already proves in-process SHA-256
  verification against it.
- **Installer platform coverage defaults to the muninn set** — darwin and Linux
  on amd64 and arm64. Windows users get a manual-download pointer; `curl | sh`
  is not offered as a Windows path. go-rag's published releases already span
  more targets; the installer covers the subset that makes sense for shell
  piping.
- **Footer identity details still open in the mockup** (e.g. "License: TBD
  (likely MIT)") are resolved to real values before publish.
- **Content reconciliation is a publish gate, not an afterthought**: benchmark
  figures, the MCP tool list, and CLI command tables in the mockup are
  illustrative placeholders to verify against the shipping CLI, not values to
  ship on trust (see FR-003, SC-006).
