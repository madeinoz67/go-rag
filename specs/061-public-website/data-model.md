# Data Model — Public Website + Hosted Installer

**Spec**: [spec.md](spec.md) · **Date**: 2026-08-11

This feature is **data-light**: no database, no schemas, no migrations. The
"data" it depends on is (a) the four entities from the spec and (b) the
install-script's linear runtime flow. Both are documented here so `/speckit-tasks`
has a single concrete reference for the installer's states and the asset
naming it consumes.

---

## Entities

### Landing Page
The single public HTML page and its sections (per the mockup): hero, why,
features, retrieval/RRF, architecture, quickstart, CLI reference, MCP,
benchmarks, security, footer.

- **Identity**: the file `site/index.html`.
- **Source of visual identity**: `docs/internals/go-rag-website-style-guide.md`
  (color tokens, typography, spacing, components, voice, accessibility floor).
- **Source of structure**: `docs/internals/go-rag-website-mockup.html`.
- **Source of factual content**: the shipping go-rag binary (`go-rag --help`,
  the live daemon's MCP tool registration, `internal/upgrade` for the version
  model) and the PRD — **not** the mockup's placeholder values.
- **Validation rule**: every command, tool name, port, and figure matches the
  binary at publish time (FR-003 / SC-006). License field = Apache-2.0.

### Install Script
The `install.sh` artifact published at `<site-url>/install.sh`.

- **Identity**: the file `site/install.sh` (POSIX `sh`, `set -e`).
- **Inputs**: none required. Optional overrides (flags) for version and install
  directory — see [contracts/install-script.md](contracts/install-script.md).
- **Outputs**: one installed `go-rag` binary on `PATH`; plain stdout progress;
  a printed next-step line. No state files, no profile edits.
- **External calls (exhaustive)**: the GitHub releases "latest" API, the
  platform tarball download, the `checksums.txt` download. Nothing else.

### Release Artifact (consumed, not produced)
The prebuilt binary archives and `checksums.txt` produced by the existing
`.github/workflows/release.yml` (spec 034).

- **Asset naming**: `go-rag-<tag>-<goos>-<goarch>.tar.gz` (Unix) /
  `.zip` (Windows), where `<tag>` includes the leading `v`. Plus
  `go-rag-model-bge-small-en-v1.5-int8.tar.gz` (bundled embedder, spec 032) and
  `checksums.txt`.
- **Checksums format**: `sha256sum` output — `<sha256>  <filename>` per line,
  one entry per `go-rag-*` archive. The installer keys on the platform tarball
  filename.
- **Published platforms**: darwin/arm64, darwin/amd64, linux/amd64,
  linux/arm64, windows/amd64. The installer covers the four Unix targets.
- Full contract: [contracts/release-assets.md](contracts/release-assets.md).

### GitHub Pages URL
The stable HTTPS address serving both the page and the install script.

- **Form**: `https://madeinoz67.github.io/go-rag/` (default; a custom domain
  only changes the host in the printed install command).
- **Properties**: HTTPS, stable across publishes, served raw (`.nojekyll`).

---

## Install-Script Runtime Flow

The installer is a strict linear state machine. Each state either advances or
exits non-zero with a plain message and a manual-download pointer. The
**verify-before-extract** ordering is load-bearing (D2): the checksum is over
the tarball, so it must be checked before extraction.

```
 ┌──────────────────────┐
 │ 1. Detect OS / arch  │   uname -s / uname -m → (goos, arch)
 └──────────┬───────────┘   unsupported → exit 1 + releases pointer
            ▼
 ┌──────────────────────┐
 │ 2. Resolve latest    │   GET releases/latest → tag  (D7)
 └──────────┬───────────┘   rate-limit / parse fail → exit 1 + pointer
            ▼
 ┌──────────────────────┐
 │ 3. Download tarball  │   go-rag-<tag>-<goos>-<arch>.tar.gz → tmp  (D2)
 └──────────┬───────────┘   HTTP non-200 → exit 1 + pointer
            ▼
 ┌──────────────────────┐
 │ 4. Fetch checksums   │   checksums.txt; grep the tarball line  (D3, D4)
 └──────────┬───────────┘   missing file OR missing entry → exit 1 (hard fail)
            ▼
 ┌──────────────────────┐
 │ 5. Verify SHA-256    │   sha256sum / shasum -a 256 over the tarball  (D4)
 └──────────┬───────────┘   mismatch → rm tmp, exit 1  (Principle II)
            ▼
 ┌──────────────────────┐
 │ 6. Extract           │   tar -xzf tmp -C tmpdir → go-rag binary  (D6)
 └──────────┬───────────┘
            ▼
 ┌──────────────────────┐
 │ 7. Install on PATH   │   /usr/local/bin if writable, else ~/.local/bin  (D5)
 └──────────┬───────────┘   fallback not on PATH → print export line
            ▼
 ┌──────────────────────┐
 │ 8. Print next step   │   "go-rag init" → stdout, exit 0
 └──────────────────────┘
```

**Exit codes** (the contract `tasks.md` and the tests encode):
- `0` — installed; binary on PATH.
- `1` — unsupported platform; download failure; missing/mismatched checksum;
  extraction failure; no checksum tool.

The installer never returns success without having verified the tarball's
checksum against `checksums.txt`. This is the single invariant the tamper test
probes (flip a byte in the tarball → step 5 fails → exit 1, no binary left).
