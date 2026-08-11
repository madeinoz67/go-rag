# Contract — Release Assets (consumed by install.sh)

**Spec**: [spec.md](../spec.md) · The dependency surface `install.sh` consumes.

This pins the release-asset contract the installer depends on. It is sourced
from the **live code** — `.github/workflows/release.yml` (the producer) and
`internal/upgrade/release.go` + `verify.go` (the existing in-process consumer).
A change to the release pipeline that breaks any field here is a **breaking
change to the installer** and must be coordinated.

## Producer

`.github/workflows/release.yml`, triggered by a `v*` tag. **Unchanged by this
feature** — the installer is a pure consumer. The pipeline cross-compiles
(`CGO_ENABLED=0`), archives per OS/arch, generates `checksums.txt` via
`sha256sum go-rag-*`, and publishes a GitHub Release.

## Published assets (per release `<tag>`, e.g. `v0.3.3`)

| Asset | Platforms |
|-------|-----------|
| `go-rag-<tag>-darwin-arm64.tar.gz` | macOS Apple Silicon |
| `go-rag-<tag>-darwin-amd64.tar.gz` | macOS Intel |
| `go-rag-<tag>-linux-amd64.tar.gz` | Linux x86-64 |
| `go-rag-<tag>-linux-arm64.tar.gz` | Linux ARM64 |
| `go-rag-<tag>-windows-amd64.zip` | Windows x86-64 |
| `go-rag-model-bge-small-en-v1.5-int8.tar.gz` | bundled embedder (spec 032) |
| `checksums.txt` | SHA-256 over every `go-rag-*` archive |

Note: `<tag>` includes the leading `v`. The Unix archives are gzip tarballs
containing a single `go-rag` binary (executable bit preserved).

## Asset URL pattern

```
https://github.com/madeinoz67/go-rag/releases/download/<tag>/<asset>
```

e.g. `https://github.com/madeinoz67/go-rag/releases/download/v0.3.3/go-rag-v0.3.3-linux-amd64.tar.gz`

This matches `internal/upgrade.ReleaseAssetURL(version, goos, goarch)` exactly.
The installer must construct the same URL with the resolved tag and detected
platform.

## checksums.txt format

`sha256sum` output — one line per archive:

```
<sha256-hex>  go-rag-<tag>-<goos>-<goarch>.tar.gz
<sha256-hex>  go-rag-<tag>-windows-amd64.zip
<sha256-hex>  go-rag-model-bge-small-en-v1.5-int8.tar.gz
…
```

(Two spaces between hash and filename, per `sha256sum`. The installer parses
with whitespace-tokenisation — same as `internal/upgrade.parseChecksumForAsset`
— so one-or-two spaces both work.) The installer greps for the line whose
filename field equals the platform tarball it downloaded.

**Critical**: the checksum is over the **archive**, not the extracted binary.
`internal/upgrade.VerifyChecksum` hashes the file at the download path (the
tarball) and compares to the archive's line in `checksums.txt`. The installer
must do the same — verifying the extracted binary would never match.

## "Latest" resolution

```
GET https://api.github.com/repos/madeinoz67/go-rag/releases/latest
→ JSON { "tag_name": "<tag>", … }
```

Anonymous GitHub API: 60 requests/hour/IP. The installer makes one call.
Matches `internal/upgrade.latestVersionDefault`. On rate-limit or parse
failure: exit 1 with a retry hint + the manual-download URL.

## Platform detection mapping

| `uname -s` | `uname -m` | go-rag asset |
|------------|-----------|--------------|
| Darwin | arm64 / aarch64 | `darwin-arm64` |
| Darwin | x86_64 | `darwin-amd64` |
| Linux | x86_64 | `linux-amd64` |
| Linux | arm64 / aarch64 | `linux-arm64` |
| * | * (other) | unsupported → exit 1 |

(Windows is not reachable via `curl|sh`; the page routes Windows users to the
`.zip` release asset manually.)

## Breaking-change triggers (coordinated with the installer)

Any of these is a breaking change to this contract and requires updating
`install.sh` in the same release:
- Renaming the asset pattern (e.g. dropping `<tag>` from the filename).
- Switching from tarball to bare binary (or vice versa).
- Changing what `checksums.txt` hashes (archive vs binary).
- Changing the checksum algorithm away from SHA-256.
- Adding a required new platform the installer must cover.
- Moving releases off GitHub.
