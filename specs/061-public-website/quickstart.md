# Quickstart — Public Website + Hosted Installer

**Spec**: [spec.md](spec.md) · A runnable validation guide, not an
implementation. Use this to prove the feature works end-to-end before and
after publish. Implementation bodies live in `tasks.md`.

Each scenario is independent. Run them in order the first time.

---

## Prerequisites

- A clone of `madeinoz67/go-rag` on `main`.
- For installer scenarios: a macOS or Linux host (amd64 or arm64) you can write
  to a temp dir on. The scenarios install into a **temp dir**, never the host
  PATH.
- The latest go-rag release published (so `releases/latest` resolves and
  `checksums.txt` exists). If no release exists yet, point `install.sh` at a
  draft release via `--version <tag>` once that override lands.

---

## Scenario 1 — Preview the page locally (no JS build)

**Proves**: the page is a single static `index.html`, readable without a build
step or JavaScript (FR-001, FR-005).

```
cd site
python3 -m http.server 8000
# open http://localhost:8000/  → the landing page renders
```

**Expected**: the hero, terminal demo, and all sections render. Disabling JS
in the browser (devtools → render → JavaScript) leaves every section's text,
tables, and code blocks readable; only the fusion-diagram hover, copy buttons,
and mobile-nav toggle go inert. The nav links remain reachable.

**Pass**: every section's text content is present with JS off.

---

## Scenario 2 — Install into a temp dir (happy path)

**Proves**: the one-line installer resolves latest, downloads, verifies, and
installs a runnable binary (FR-007/008/009/010/011, SC-001).

```
tmpbin=$(mktemp -d)
curl -fsSL https://madeinoz67.github.io/go-rag/install.sh | sh -s -- --install-dir "$tmpbin"
"$tmpbin/go-rag" version
```

**Expected**: progress lines on stdout (detect → resolve → download → verify →
extract → install); the final line prints the next step. `go-rag version`
prints the tag that `releases/latest` reported. Exactly one file exists in
`$tmpbin` (`go-rag`).

**Pass**: `go-rag version` succeeds and matches the latest release tag; no
extra files written.

> Until the site is live, substitute the local script:
> `sh site/install.sh --install-dir "$tmpbin"` (the script is identical to the
> deployed copy).

---

## Scenario 3 — Tamper test (RED-sanity for the checksum gate)

**Proves**: the installer refuses a tampered binary — integrity is enforced,
not optional (FR-010, SC-003). This is the test that must **fail without the
fix** and pass with it.

```
# Drive install.sh at a tampered tarball: flip a byte after download, before verify.
# (The harness in tasks.md does this by intercepting the download URL with a
#  local HTTP server that serves a corrupted copy of the real tarball.)
tmpbin=$(mktemp -d)
sh site/install.sh --install-dir "$tmpbin" --version <latest-tag>   # against the tampered source
echo "exit=$?"
ls -1 "$tmpbin"
```

**Expected**: the installer detects the SHA-256 mismatch, deletes the
download, prints a mismatch message, and exits `1`. **No binary** is left in
`$tmpbin`.

**Pass**: exit `1`, no `go-rag` in `$tmpbin`. (To confirm the test is
load-bearing: temporarily skip the verify step → the install must succeed,
proving the test catches the regression.)

---

## Scenario 4 — Missing-checksum hard fail

**Proves**: a release with no `checksums.txt` entry for the platform is fatal,
not a warning (D3, FR-010 — diverges from muninn deliberately).

```
# Point the checksums URL at an empty file via the test harness, then run:
sh site/install.sh --install-dir "$(mktemp -d)"
echo "exit=$?"
```

**Expected**: installer reports no checksum published, exits `1`, installs
nothing.

**Pass**: exit `1`, no binary written.

---

## Scenario 5 — Unsupported platform

**Proves**: an unsupported OS/arch exits cleanly with a manual-download
pointer (FR-008 edge case).

```
# Fake the platform detection (harness overrides uname) to e.g. "windows"/"arm64":
sh site/install.sh --install-dir "$(mktemp -d)"   # with detection forced off-matrix
echo "exit=$?"
```

**Expected**: a one-line "unsupported" message naming the platform and the
GitHub releases URL; exit `1`; nothing downloaded.

**Pass**: exit `1`, no network call to the asset URL.

---

## Scenario 6 — CI publish is hands-off and fail-closed

**Proves**: a merge to `main` deploys the site through CI; a broken build
leaves the live site untouched (FR-016, SC-007).

1. Merge a change under `site/` to `main`.
2. Watch the `site.yml` workflow run to green.
3. Confirm `https://madeinoz67.github.io/go-rag/` reflects the change.
4. **Negative check**: push a change that breaks the page (e.g. malformed
   `index.html` that fails the workflow's check step); confirm the workflow
   fails and the live site still serves the previous version.

**Expected**: green runs deploy; red runs do not mutate the live site.

**Pass**: the live site only changes on green workflow runs.

> First-time setup (one-off, not per-publish): enable GitHub Pages in repo
> Settings → Pages → Source "GitHub Actions". No `gh-pages` branch, no `main
> /docs` setting.

---

## Scenario 7 — Cross-platform smoke (CI matrix)

**Proves**: the installer works on every supported target (FR-008).

The `site.yml` (or a dedicated `installer-smoke` workflow) runs Scenario 2 on
a matrix of GitHub Actions runners — `macos-latest` (arm64) and
`ubuntu-latest` (amd64), plus a linux/arm64 container — asserting `go-rag
version` succeeds in each.

**Expected**: all matrix jobs green on every release tag.

---

## Content-reconciliation gate (before first publish)

Before the page goes live, verify against the shipping binary (FR-003,
SC-006). Checklist:

- [ ] Footer **License** = `Apache-2.0` (the mockup's "likely MIT" is wrong).
- [ ] Status badge reflects the released alpha line (e.g. `Alpha · v0.3.3`).
- [ ] Every CLI command in the reference tables matches `go-rag --help` and
      subcommand help (note post-mockup commands: `delete`, `auth`, `upgrade`).
- [ ] MCP tool list/count matches the live daemon (`go-rag start`, then
      inspect the registered tools).
- [ ] Transport ports match the daemon: 7878 MCP / 7879 REST / 7880 gRPC /
      7881 console.
- [ ] Benchmark figures are either reproducible via `go-rag eval` or marked
      illustrative.

Record the verification in the publish checklist; the page does not ship until
every box is ticked.
