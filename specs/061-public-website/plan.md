# Implementation Plan: Public Website + Hosted Installer

**Branch**: `061-public-website` | **Date**: 2026-08-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/061-public-website/spec.md`

## Summary

Ship a public, single-page marketing site for go-rag from the existing style
guide and mockup, hosted on GitHub Pages and deployed by CI; and a hosted
POSIX-`sh` `install.sh` that installs the latest release in one line
(`curl -fsSL <site>/install.sh | sh`), mirroring the muninn installer's shape
but consuming go-rag's *existing* release-asset contract — the per-OS/arch
`go-rag-<tag>-<os>-<arch>.tar.gz` archives and `checksums.txt` produced by
`.github/workflows/release.yml`, verified exactly as `internal/upgrade`
already verifies them (SHA-256 over the tarball, then extract). No Go source
in the binary changes; the deliverables are static HTML/CSS/JS, one shell
script, and one GitHub Actions workflow. The site is a *project artifact*, not
a binary component, so constitution Principles I–V (which govern the binary)
do not bind it; Principle II's integrity spirit is reinforced, not weakened,
by the installer's hard-fail-on-checksum-mismatch posture.

## Technical Context

**Language/Version**: Static HTML5 + CSS + vanilla JS (the mockup is already a
self-contained single `index.html` with inline CSS/JS — no build step, no
framework); POSIX `sh` for the installer; YAML for the GitHub Actions workflow.
No Go.

**Primary Dependencies**: Google Fonts CDN (Space Grotesk, IBM Plex Sans,
JetBrains Mono — per the style guide) with system-font fallback; no JS
libraries. The installer depends only on POSIX sh + `curl` + `tar` +
`sha256sum`/`shasum`, all already required to run the one-liner.

**Storage**: N/A — static site, no server-side state. The installer writes
exactly one file (the binary) to one install directory; no profile edits, no
state.

**Testing**:
- `install.sh` — `shellcheck` (CI gate) + a POSIX-sh smoke harness run on the
  GitHub Actions macOS + Linux matrix that installs into a temp dir and
  asserts `go-rag version` succeeds; a **tamper test** (flip a byte in the
  downloaded tarball → assert non-zero exit, no binary left behind) as the
  RED-sanity proof for the checksum gate.
- `index.html` — a grep assertion that every section's content is present
  with JS disabled (server-rendered text, not JS-injected); manual visual
  verification via the Interceptor skill before first publish and after any
  content change (per the repo's console-UI verification convention).
- `site.yml` — the workflow's own build/deploy status is the gate;
  `actions/deploy-pages` is atomic (a failed deploy leaves the prior site).

**Target Platform**: Modern evergreen browsers (page); macOS and Linux on
amd64/arm64 (installer). Windows and other platforms get a manual-download
pointer (no `curl|sh` path).

**Project Type**: static-site + cli-distribution-artifact (the installer is a
binary-distribution surface, not a Go package).

**Performance Goals**: Page LCP under ~1.5s on a cable connection (it is one
HTML file plus web fonts); installer wall-clock a few seconds on broadband
(one small tarball + one checksums file).

**Constraints**: HTTPS + stable URL (GitHub Pages default); informational
content readable with JS off (FR-005); the style guide's accessibility floor
(FR-004); the installer makes no changes outside one binary destination
(FR-012); publishing is CI-driven and fail-closed (FR-016).

**Scale/Scope**: One page, one script, one workflow. The page is hand-curated
from the real CLI (not generated) for v1; full CI-generation of the CLI table
from `go-rag --help` is a deferred enhancement.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Verdict: PASS — no violations, no Complexity-Tracking entries required.**

Framing: the website and installer are **project artifacts** (public presence +
binary distribution), not components of the go-rag binary. They do not ship
inside the binary and are not part of any core operation (ingest/index/query).
Principles I–V govern the binary's architecture and the on-disk key-space;
they therefore do not bind this feature. This is the same separation spec 046
established for the management console (an embedded loopback SPA) — and a
third, distinct category from the PRD §2.2 "web UI" exclusion (which meant an
in-product UI, not a public marketing site). No PRD amendment is needed; the
PRD governs the product, and the website is not part of the product.

- **Principle I (Local-First, Single-Binary)** — *not applicable to the
  artifact; reinforced in spirit for the binary.* The binary stays local-first,
  single-binary, no cloud. The installer's one-time download from GitHub is a
  distribution channel, not a runtime dependency — identical to the already-
  cleared `go-rag upgrade` (spec 034). The page loading web fonts from a CDN is
  a marketing-page concern, not a product-runtime concern.
- **Principle II (Content-Addressed Identity)** — *reinforced.* The installer
  hard-fails on any checksum mismatch or missing checksum, matching
  `internal/upgrade`'s `ErrNoChecksum` = fatal ("go-rag never installs an
  unverified binary"). This extends the existing integrity posture to the
  first-install path.
- **Principle III (Pure Go)** — *not applicable.* The installer is POSIX sh
  and the page is HTML/CSS/JS; neither is Go code in the binary. The principle
  governs the binary's Go dependencies, which are unchanged.
- **Principle IV (Async-After-ACK)** — *not applicable.* No write path touched.
- **Principle V (Extension by Interface, MCP-First)** — *not applicable.* No
  new file formats, embedders, or CLI operations.
- **Storage discipline / schema evolution** — *not applicable.* No Pebble, no
  key-space, no migration. No `migrate.ExpectedVersion` change. The plan adds
  no on-disk layout to the binary.

**PRD §2.2 "web UI" exclusion** — addressed head-on: a public marketing site
on GitHub Pages is not the in-product web UI the PRD excluded; spec 046 already
narrowed that exclusion to admit the loopback management console. No PRD text
change is in scope.

## Project Structure

### Documentation (this feature)

```text
specs/061-public-website/
├── plan.md              # this file
├── research.md          # Phase 0 — D1–D11 design decisions
├── data-model.md        # Phase 1 — entities + install-script flow
├── quickstart.md        # Phase 1 — end-to-end validation guide
├── contracts/
│   ├── install-script.md   # installer I/O + exit-code contract
│   └── release-assets.md   # consumed release-asset contract (the dependency)
└── tasks.md             # Phase 2 (/speckit-tasks — NOT this command)
```

### Source Code (repository root)

```text
site/                       # NEW — public website (GitHub Pages source of truth)
├── index.html              # landing page, ported from docs/internals mockup, content-reconciled
├── install.sh              # hosted one-line installer
├── .nojekyll               # serve all files raw (no Jekyll processing)
└── assets/                 # local images/svg only if not inlined (likely empty in v1)

.github/workflows/
├── ci.yml                  # existing — unchanged
├── release.yml             # existing — UNCHANGED; produces the assets install.sh consumes
└── site.yml                # NEW — deploy site/ to GitHub Pages on push to main (FR-016)

docs/internals/
├── go-rag-website-style-guide.md   # existing — the design source of truth the page follows
└── go-rag-website-mockup.html      # existing — the structural source of truth the page ports
```

**Structure Decision**: a single new top-level `site/` directory holds the
public site and the installer, deployed to GitHub Pages by a new
`.github/workflows/site.yml`. This deliberately does **not** reuse `docs/`
(which holds dev-facing internals + the PRD), does **not** reuse
`internal/ui/web/` (the in-product console SPA embedded in the binary, spec
046), and does **not** add a `gh-pages` branch. The existing `release.yml` is
untouched — the installer is a pure consumer of its artifacts. No Go package
changes; no new `main` package (constitution: single entrypoint
`cmd/go-rag` — unaffected).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

*Empty — Constitution Check passes with no violations. No principles are
weakened; Principle II is reinforced by the installer's hard-fail checksum
posture. The "web UI" PRD-position question is resolved by category separation
(project artifact vs. product component), not by a principle override.*
