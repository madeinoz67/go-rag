# Phase 0 Research — Public Website + Hosted Installer

**Spec**: [spec.md](spec.md) · **Date**: 2026-08-11

Resolved by direct inspection of the existing release pipeline
(`.github/workflows/release.yml`), the existing in-process upgrade package
(`internal/upgrade/release.go`, `internal/upgrade/verify.go`), the style guide
and mockup (`docs/internals/`), the Makefile, and the muninn installer
(`~/Documents/src/muninndb/install.sh`) named as the shape reference. No
`[NEEDS CLARIFICATION]` survives.

---

## D1 — Where the site source lives, and how Pages deploys

**Decision**: A new top-level `site/` directory holds `index.html`, `install.sh`,
`.nojekyll`, and any local assets. A new `.github/workflows/site.yml` deploys
`site/` to GitHub Pages on push to `main` using
`actions/configure-pages` → `actions/upload-pages-artifact` →
`actions/deploy-pages`.

**Rationale**: Keeps the public site strictly separate from `docs/internals/`
(dev-facing — the PRD, keyspace registry, design notes) and from
`internal/ui/web/` (the in-product console SPA embedded in the binary, spec
046). `actions/deploy-pages` is GitHub's current recommended Pages deployment
path; it is atomic (a deploy either fully replaces the served artifact or
leaves the previous version intact — satisfies FR-016's fail-closed
requirement). CI-owned deploy means no one ever edits the live site by hand
(FR-014).

**Alternatives rejected**:
- *`gh-pages` branch* — legacy; requires branch hygiene and duplicates content
  across branches. Replaced by the artifact-deploy model.
- *Pages source = `main` / `docs`* — would mix the public site into `docs/`,
  which already holds internal docs, and would path-expose the internals tree.
  Rejected on cleanliness.
- *Jekyll / a static-site generator* — the mockup is plain self-contained HTML
  with no build step; adding a generator would import a toolchain the project
  does not use. Rejected.

## D2 — Bare binary vs. archive: what install.sh downloads

**Decision**: The installer downloads the **tarball**
(`go-rag-<tag>-<os>-<arch>.tar.gz`), verifies the **tarball's** SHA-256
against `checksums.txt`, then extracts the `go-rag` binary. It mirrors
`internal/upgrade` exactly — `ReleaseAssetURL` returns the `.tar.gz` URL,
`ExpectedSHA256` keys on the tarball filename, and `VerifyChecksum` hashes the
file at the download path (the tarball, before extraction).

**Rationale**: This is the *existing, shipped* asset contract. `release.yml`
produces `go-rag-<tag>-<os>-<arch>.tar.gz` for Unix and `.zip` for Windows,
and `checksums.txt` via `sha256sum go-rag-*` (hashing the archives). A bare
binary is not published. Verifying the tarball then extracting is the only
sequence that matches the published checksums. Reinventing a bare-binary asset
would require changing `release.yml` and running a second checksum scheme —
out of scope and needless risk.

**Alternatives rejected**:
- *Publish bare binaries alongside tarballs and checksum them* — requires
  `release.yml` changes + a parallel checksum scheme; the tarball contract
  already works and is verified end-to-end by spec 034.
- *Checksum the extracted binary instead of the tarball* — never matches
  `checksums.txt` (which hashes the archive); every install would fail.

## D3 — Missing-checksum posture (where to diverge from muninn)

**Decision**: If `checksums.txt` cannot be fetched, OR it has no entry for the
host platform's tarball, the installer **refuses to install** (hard fail,
non-zero exit, pointer to the manual download). This matches
`internal/upgrade`'s `ErrNoChecksum` = fatal and is *stricter* than muninn's
warn-and-continue.

**Rationale**: go-rag's release pipeline always publishes `checksums.txt`
(`release.yml` generates it unconditionally). A missing checksum therefore
means a broken or tampered release — precisely the case where refusing is
correct. muninn's warn-and-continue exists for releases that predate its
checksums; go-rag has no such legacy. Matching the in-process posture keeps the
first-install and `go-rag upgrade` paths identical in integrity, reinforcing
Principle II.

**Alternatives rejected**:
- *Warn-and-continue like muninn* — weaker than go-rag's own in-process
  posture; rejected.

## D4 — Checksum tooling across macOS and Linux

**Decision**: Probe with `command -v sha256sum` then `command -v shasum`;
compute via whichever exists (`sha256sum <file>` or `shasum -a 256 <file>`).
If neither exists, refuse with a clear message.

**Rationale**: macOS does not ship `sha256sum` by default (it ships `shasum`);
Linux typically ships `sha256sum`. Both target platforms in the release matrix
must work. muninn already solved this with the same probe; copy the proven
pattern.

## D5 — Install directory and PATH

**Decision**: Try `/usr/local/bin` when writable; otherwise fall back to
`$HOME/.local/bin` (creating it). If the fallback dir is not on `PATH`, print
the exact `export PATH="$HOME/.local/bin:$PATH"` line for the user's shell
profile. Mirror muninn.

**Rationale**: Field-proven; no `sudo` prompt unless the user already has write
access; the PATH-warning copy is the one bit of help a first-time installer
most needs.

## D6 — Extraction

**Decision**: Require `tar` (POSIX, present on all targets); extract with
`tar -xzf <tarball> -C <tmpdir>` and move the `go-rag` binary to the install
directory. Verify the binary is executable (the archive stores the mode bit).

**Rationale**: `tar` is universal on darwin/linux; macOS `bsdtar` and GNU `tar`
both accept `-xzf`. No second decompression tool needed.

## D7 — Resolving "latest" without dependencies

**Decision**: `curl -fsSL https://api.github.com/repos/madeinoz67/go-rag/releases/latest`,
parse `tag_name` with `sed`/`grep` (no `jq`). On rate-limit or parse failure,
exit non-zero with a retry hint and the manual-download URL.

**Rationale**: `jq` is not guaranteed on a minimal system. The sed parse of
`tag_name` is field-proven in muninn and matches the endpoint
`internal/upgrade`'s `latestVersionDefault` already calls. Anonymous GitHub
API is 60 req/hr/IP — the installer hits it once; a rate-limited user gets a
clear message, not an opaque curl error.

## D8 — Keeping page content honest (FR-003 / SC-006)

**Decision**: v1 hand-curates the page from the real CLI with a documented
publish checklist in the repo; full CI generation of the CLI table from
`go-rag --help` is deferred to a v1.1 enhancement. **Concrete reconciliations
resolved now by inspecting the repo**:
- **License**: Apache-2.0 (`LICENSE` + `release.yml` image label both confirm).
  The mockup footer's "License: TBD (likely MIT)" is wrong → fix to
  `Apache-2.0`.
- **Status badge**: v0.3.3 is the released alpha line → "Alpha · v0.3.3"
  (mockup said "v1 working end-to-end").
- **MCP tool list + count**: verify against the live daemon (`go-rag start`
  then inspect the MCP tool registration); do not ship the mockup's "10 tools"
  list on trust.
- **CLI command tables**: verify each command against `go-rag --help` and
  subcommand `--help`; note commands shipped since the mockup (`delete`,
  `auth`, `upgrade`, `vault`, the console) and decide which to surface.
- **Transport ports**: 7878 (MCP) / 7879 (REST) / 7880 (gRPC) / 7881 (console)
  — verify against the daemon; the mockup shows the first three.
- **Benchmark figures**: keep only if reproducible via `go-rag eval`; otherwise
  mark clearly as illustrative or drop the section for v1.

**Rationale**: A marketing page with wrong commands/license/numbers is worse
than none. Generating from `--help` in CI is the robust long-term fix but adds
build complexity for a one-page site; for v1 a hand-curated page plus a
checklist bounds the drift risk. The license catch alone justifies the gate.

**Alternatives rejected**:
- *Ship the mockup's placeholder content as-is* — violates FR-003/SC-006; rejected.
- *Full `--help` → table generation in CI* — over-engineering for v1; deferred.

## D9 — Alternative install paths to surface on the page

**Decision**: The install section offers, in order: (1) `curl|sh` for macOS/Linux
(primary); (2) `brew install madeinoz67/tap/go-rag` for Homebrew users (note
"requires the published tap"); (3) `go install github.com/madeinoz67/go-rag/cmd/go-rag@latest`
(from source, Go required); (4) Windows → manual download of the release
`.zip`. The installer script itself handles only macOS/Linux and points
everyone else to the releases page.

**Rationale**: `release.yml` *already* publishes a Homebrew tap (the `tap` job
with `.github/homebrew/go-rag.rb.tmpl`), a GHCR image, and a Windows `.zip`.
Surfacing them costs nothing and covers the full matrix. Keeping `install.sh`
focused on the Unix case preserves FR-008's clarity.

## D10 — Serving `install.sh` raw; `.nojekyll`

**Decision**: Include `.nojekyll` in the deployed root so GitHub Pages serves
every file as-is (no Jekyll/Liquid processing, no `_`-prefixed asset dropped).
GitHub Pages serves `.sh` as `text/plain`, so `curl` receives the raw script
bytes verbatim. The quickstart smoke confirms this by curling the deployed URL
and diffing against the repo copy.

**Rationale**: Guarantees the one-liner fetches the exact script under version
control. `.nojekyll` is the standard guard for a raw-asset Pages site and
costs one empty file.

## D11 — JavaScript-off readability (FR-005)

**Decision**: Port the mockup as a single `index.html` with inline CSS/JS and
no build step. The mockup's only JS is (a) the mobile-nav toggle, (b)
copy-to-clipboard buttons, (c) the fusion-diagram hover highlight — all of
which degrade gracefully. Concretely: ensure `.navlinks` is reachable without
the toggle (a `<noscript>` style that reveals it, or default-visible then
JS-collapsed on mobile); copy buttons become inert (text still selectable); the
fusion diagram is a fully-labeled static diagram that the hover merely
enhances.

**Rationale**: The mockup already proves a no-build, no-framework page is
sufficient. JS-off readability is both an FR and a public-web norm; the
changes to reach it are small and contained to the port.
