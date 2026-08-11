#!/bin/sh
# go-rag installer — https://madeinoz67.github.io/go-rag/
# Usage: curl -fsSL https://madeinoz67.github.io/go-rag/install.sh | sh
#
# Resolves the latest release, downloads the platform tarball, verifies its
# SHA-256 against the release's checksums.txt (a mismatch is fatal — go-rag
# never installs an unverified binary), extracts, and installs on PATH.
# Mirrors internal/upgrade (release.go + verify.go): the checksum is over the
# .tar.gz, keyed by filename; the binary is extracted AFTER verification.
set -e

REPO="madeinoz67/go-rag"
BIN_NAME="go-rag"
INSTALL_DIR=""
VERSION=""

usage() {
  cat <<EOF
Usage: sh install.sh [--version <tag>] [--install-dir <path>]

  --version <tag>       install a specific release tag (default: latest)
  --install-dir <path>  override the install directory (default: /usr/local/bin
                        or ~/.local/bin)
  -h, --help            show this help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "go-rag: unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

# ── Detect platform ─────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "${ARCH}" in
  x86_64)        ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "go-rag: unsupported architecture: ${ARCH}" >&2
    echo "  Download manually: https://github.com/${REPO}/releases/latest" >&2
    exit 1
    ;;
esac
case "${OS}" in
  darwin|linux) ;;
  *)
    echo "go-rag: unsupported OS: ${OS}" >&2
    echo "  This installer supports macOS and Linux." >&2
    echo "  Download manually: https://github.com/${REPO}/releases/latest" >&2
    exit 1
    ;;
esac

# ── Resolve latest release tag ───────────────────────────────────────────────
if [ -z "${VERSION}" ]; then
  echo "  Checking latest release..."
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    2>/dev/null | sed -n 's/.*"tag_name" *: *"\([^"]*\)".*/\1/p' | head -1)
  if [ -z "${VERSION}" ]; then
    echo "go-rag: could not determine the latest version (GitHub API rate limit?)" >&2
    echo "  Try again in a minute, or download from: https://github.com/${REPO}/releases/latest" >&2
    exit 1
  fi
fi

ASSET="go-rag-${VERSION}-${OS}-${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
SUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

# ── Download tarball ─────────────────────────────────────────────────────────
TMP=$(mktemp)
echo "  Downloading go-rag ${VERSION} for ${OS}/${ARCH}..."
HTTP_CODE=$(curl -sSL --progress-bar -w "%{http_code}" -o "${TMP}" "${URL}" 2>/dev/null)
if [ "${HTTP_CODE}" != "200" ]; then
  rm -f "${TMP}"
  echo "" >&2
  echo "go-rag: download failed (HTTP ${HTTP_CODE})" >&2
  echo "  URL: ${URL}" >&2
  echo "  The release asset for ${OS}/${ARCH} may not be available yet." >&2
  echo "  Download manually: https://github.com/${REPO}/releases/tag/${VERSION}" >&2
  exit 1
fi

# ── Fetch checksums and verify the tarball BEFORE extraction ─────────────────
# go-rag always publishes checksums.txt; a missing entry means a broken or
# tampered release — refuse (Principle II: never install an unverified binary).
EXPECTED=$(curl -fsSL "${SUMS_URL}" 2>/dev/null | grep " ${ASSET}\$" | awk '{print $1}' | head -1)
if [ -z "${EXPECTED}" ]; then
  rm -f "${TMP}"
  echo "" >&2
  echo "go-rag: no checksum published for ${ASSET} — refusing to install." >&2
  echo "  The release is incomplete or the checksums file is unreachable." >&2
  echo "  Download manually and verify: https://github.com/${REPO}/releases/tag/${VERSION}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "${TMP}" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "${TMP}" | awk '{print $1}')
else
  rm -f "${TMP}"
  echo "go-rag: no sha256sum or shasum tool found — cannot verify the download." >&2
  echo "  Install one of them, or download manually: https://github.com/${REPO}/releases/tag/${VERSION}" >&2
  exit 1
fi

if [ "${ACTUAL}" != "${EXPECTED}" ]; then
  rm -f "${TMP}"
  echo "" >&2
  echo "go-rag: CHECKSUM VERIFICATION FAILED — refusing to install." >&2
  echo "  expected: ${EXPECTED}" >&2
  echo "  actual:   ${ACTUAL}" >&2
  echo "  The downloaded archive does not match the published checksum." >&2
  exit 1
fi
echo "  Checksum verified."

# ── Extract ──────────────────────────────────────────────────────────────────
EXTRACT_DIR=$(mktemp -d)
tar -xzf "${TMP}" -C "${EXTRACT_DIR}"
rm -f "${TMP}"
if [ ! -x "${EXTRACT_DIR}/${BIN_NAME}" ]; then
  rm -rf "${EXTRACT_DIR}"
  echo "go-rag: the archive did not contain an executable '${BIN_NAME}'." >&2
  exit 1
fi

# ── Install on PATH ──────────────────────────────────────────────────────────
if [ -z "${INSTALL_DIR}" ]; then
  if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "${INSTALL_DIR}"
  fi
else
  mkdir -p "${INSTALL_DIR}"
fi
mv "${EXTRACT_DIR}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
rm -rf "${EXTRACT_DIR}"

# Warn if the install directory is not on PATH.
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "  ⚠  ${INSTALL_DIR} is not in your PATH."
    echo "     Add this to your shell profile (~/.zshrc or ~/.bashrc):"
    echo ""
    echo "       export PATH=\"${INSTALL_DIR}:\$PATH\""
    echo ""
    ;;
esac

# ── Done ─────────────────────────────────────────────────────────────────────
echo ""
echo "  go-rag ${VERSION} installed to ${INSTALL_DIR}/${BIN_NAME}"
echo ""
echo "  Get started:"
echo "    go-rag init"
echo ""
