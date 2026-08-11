# Contract — install.sh

**Spec**: [spec.md](../spec.md) · The user-facing interface of the hosted installer.

This is the interface a user (and the website's install instructions) rely on.
It must stay stable; changes are breaking and require updating every place the
one-liner is printed.

## Invocation

```sh
curl -fsSL https://madeinoz67.github.io/go-rag/install.sh | sh
```

Cautious-user path (FR-013):

```sh
curl -fsSL https://madeinoz67.github.io/go-rag/install.sh -o install.sh
less install.sh        # read it
sh install.sh
```

## Runtime requirements

POSIX `sh`, plus `curl`, `tar`, and one of `sha256sum` or `shasum -a 256`.
Runs `set -e`. No `bash`-isms. No `sudo` (installs to a user-writable dir if
the system dir is not writable).

## Inputs

- **Required**: none.
- **Optional flags** (override defaults; must remain backward-compatible):
  - `--version <tag>` — install a specific release tag instead of latest.
  - `--install-dir <path>` — override the install directory (used by tests to
    install into a temp dir without touching the host PATH).
- **Environment**: none required. (No `GORAG_*` config consumed; the installer
  only fetches the binary.)

## Outputs

- **Filesystem**: exactly one `go-rag` binary, executable, at the install
  directory. Nothing else is created or modified (no profile edits, no state
  files, no background services — FR-012).
- **stdout**: short, plain progress lines — detecting, resolving latest,
  downloading, verifying, extracting, installing — and a final next-step line
  (`go-rag init`). Voice matches the style guide (no hype, no exclamation
  points).
- **stderr**: error diagnostics only.

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Installed; binary on PATH (or PATH-warning printed). |
| `1`  | Unsupported OS/arch; download failure (HTTP non-200); `checksums.txt` unreachable or has no entry for the platform tarball; SHA-256 mismatch; extraction failure; no `sha256sum`/`shasum` tool. |

Every non-zero exit prints a one-line cause and a manual-download URL
(`https://github.com/madeinoz67/go-rag/releases/latest`) before exiting.

## Network calls (exhaustive)

1. `GET https://api.github.com/repos/madeinoz67/go-rag/releases/latest` — resolve tag.
2. `GET https://github.com/madeinoz67/go-rag/releases/download/<tag>/go-rag-<tag>-<goos>-<goarch>.tar.gz` — the binary archive.
3. `GET https://github.com/madeinoz67/go-rag/releases/download/<tag>/checksums.txt` — the checksums.

No other network calls. No telemetry. The downloaded tarball is verified
against `checksums.txt` **before** extraction (see
[release-assets.md](release-assets.md) for the format).

## Invariants (the contract tests probe)

1. **Verify-before-extract**: the tarball's SHA-256 must match `checksums.txt`
   before extraction begins. A mismatch deletes the download and exits 1.
2. **No unverified install**: a missing `checksums.txt`, or a `checksums.txt`
   with no entry for the platform tarball, is fatal (exit 1) — never a warning.
   This matches `internal/upgrade`'s `ErrNoChecksum` posture (Principle II).
3. **Single artifact written**: on success, exactly one new file exists on the
   host — the `go-rag` binary at the install directory.
4. **Latest at runtime**: the version is resolved at run time from the GitHub
   API; the published script body never embeds a version (FR-009/015), so a
   cached script still installs whatever is latest.
