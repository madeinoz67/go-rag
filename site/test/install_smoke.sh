#!/bin/sh
# install_smoke.sh — exercise site/install.sh against local fixtures.
#
# Design: a stub `curl` (earlier on PATH) serves canned responses for the three
# URLs install.sh hits (the releases/latest API, the platform tarball, and
# checksums.txt). The real install.sh runs unchanged against these fixtures, so
# the verify/extract/install logic is tested deterministically and offline.
#
# Scenarios:
#   happy    — valid tarball + matching checksum  ⇒ installed, version prints
#   tamper   — tarball byte-flipped after checksum gen ⇒ exit 1, no binary  (RED-sanity for the gate)
#   missing  — checksums.txt has no entry for the asset ⇒ exit 1, no binary
#   platform — off-matrix OS/arch ⇒ exit 1, no asset download
#
# Usage: sh site/test/install_smoke.sh [happy|tamper|missing|platform|all]
#        (default: all)
set -u

HERE=$(cd "$(dirname "$0")" && pwd)
INSTALL="$HERE/../install.sh"
PASS=0; FAIL=0
SCENARIO="${1:-all}"

fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }
pass() { echo "  pass"; PASS=$((PASS+1)); }

# ── Build fixtures once ──────────────────────────────────────────────────────
# A fake "binary" + a fake checksums.txt + a fake tarball containing it. The
# tarball's real SHA-256 is recorded; the happy scenario serves both; the
# tamper scenario corrupts the served tarball so its hash no longer matches.
FIX=$(mktemp -d)
BIN="$FIX/go-rag"
printf '#!/bin/sh\necho v0.0.0-smoke\n' > "$BIN"
chmod +x "$BIN"
TAG="v0.0.0-smoke"
OS=$(uname -s | tr A-Z a-z)
case $(uname -m) in x86_64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; *) ARCH=amd64;; esac
ASSET="go-rag-${TAG}-${OS}-${ARCH}.tar.gz"
( cd "$FIX" && tar -czf "$ASSET" go-rag )
REAL_SHA=$( ( cd "$FIX" && (sha256sum "$ASSET" 2>/dev/null || shasum -a 256 "$ASSET") | awk '{print $1}') )
printf '%s  %s\n' "$REAL_SHA" "$ASSET" > "$FIX/checksums.txt"
# A tampered tarball: flip the last byte.
cp "$FIX/$ASSET" "$FIX/$ASSET.tampered"
TamperedSha=$( ( cd "$FIX" && (sha256sum "$ASSET.tampered" 2>/dev/null || shasum -a 256 "$ASSET.tampered") | awk '{print $1}') )
# Byte-flip the last byte of the tampered copy so its hash differs from REAL_SHA.
python3 - "$FIX/$ASSET.tampered" <<'PY' 2>/dev/null || : > /dev/null
import sys
p = sys.argv[1]
with open(p, "rb+") as f:
    f.seek(-1, 2)
    b = f.read(1)
    f.seek(-1, 2)
    f.write(bytes([b[0] ^ 0xFF]))
PY
TamperedSha=$( ( cd "$FIX" && (sha256sum "$ASSET.tampered" 2>/dev/null || shasum -a 256 "$ASSET.tampered") | awk '{print $1}') )

cleanup() { rm -rf "$FIX"; }
trap cleanup EXIT

# ── Stub curl ────────────────────────────────────────────────────────────────
# Serves: the releases/latest JSON, the tarball (real or tampered), checksums
# (full or empty). Controlled by env: SERVE_TAMPERED, SERVE_EMPTY_SUMS.
make_stub_dir() {
  d=$(mktemp -d)
  cat > "$d/curl" <<EOF
#!/bin/sh
# stub curl: parse -o/-w, serve the matching fixture for the URL (last arg).
out=""; wfmt=""
while [ \$# -gt 0 ]; do
  case "\$1" in
    -o) out="\$2"; shift 2;;
    -w) wfmt="\$2"; shift 2;;
    -*) shift;;
    *) url="\$1"; shift;;
  esac
done
serve() { if [ -n "\$out" ]; then cat >"\$out"; else cat; fi; }
case "\$url" in
  *api.github.com*releases/latest) printf '{"tag_name":"$TAG"}\n';;
  */$ASSET)
    if [ -n "\${SERVE_TAMPERED:-}" ]; then serve < "$FIX/$ASSET.tampered"
    else serve < "$FIX/$ASSET"; fi
    ;;
  */checksums.txt)
    [ -z "\${SERVE_EMPTY_SUMS:-}" ] && serve < "$FIX/checksums.txt"
    ;;
  *) echo "stub: unhandled URL \$url" >&2;;
esac
case "\$wfmt" in *http_code*) printf '200';; esac
EOF
  chmod +x "$d/curl"
  echo "$d"
}

run_install() {
  outdir=$(mktemp -d)
  stubdir=$(make_stub_dir)
  env PATH="$stubdir:$PATH" sh "$INSTALL" --install-dir "$outdir" --version "$TAG" >/tmp/go-rag-smoke.log 2>&1
  rc=$?
  rm -rf "$stubdir"
  echo "$rc:$outdir"
}

# ── happy ────────────────────────────────────────────────────────────────────
happy() {
  echo "== happy =="
  res=$(SERVE_TAMPERED="" SERVE_EMPTY_SUMS="" run_install)
  rc=${res%%:*}; outdir=${res#*:}
  if [ "$rc" = "0" ] && [ -x "$outdir/go-rag" ]; then
    if "$outdir/go-rag" 2>/dev/null | grep -q smoke; then
      pass
    else fail "installed binary did not run"; fi
  else fail "expected success + installed binary (rc=$rc)"; cat /tmp/go-rag-smoke.log; fi
  rm -rf "$outdir"
}

# ── tamper (RED-sanity for the checksum gate) ────────────────────────────────
tamper() {
  echo "== tamper =="
  res=$(SERVE_TAMPERED=1 SERVE_EMPTY_SUMS="" run_install)
  rc=${res%%:*}; outdir=${res#*:}
  if [ "$rc" != "0" ] && [ ! -e "$outdir/go-rag" ]; then
    pass
  else fail "tampered tarball must be refused (rc=$rc) and leave no binary"; cat /tmp/go-rag-smoke.log; fi
  rm -rf "$outdir"
}

# ── missing checksum ─────────────────────────────────────────────────────────
missing() {
  echo "== missing =="
  res=$(SERVE_TAMPERED="" SERVE_EMPTY_SUMS=1 run_install)
  rc=${res%%:*}; outdir=${res#*:}
  if [ "$rc" != "0" ] && [ ! -e "$outdir/go-rag" ]; then
    pass
  else fail "missing checksum must be fatal (rc=$rc)"; cat /tmp/go-rag-smoke.log; fi
  rm -rf "$outdir"
}

# ── unsupported platform ─────────────────────────────────────────────────────
platform() {
  echo "== platform =="
  outdir=$(mktemp -d)
  # Force an off-matrix OS by overriding uname via a stub.
  stubdir=$(mktemp -d)
  cat > "$stubdir/uname" <<'EOF'
#!/bin/sh
# pretend to be Windows/arm64 (off-matrix for this installer)
if [ "$1" = "-s" ]; then echo MINGW64_NT; exit 0; fi
if [ "$1" = "-m" ]; then echo x86_64; exit 0; fi
uname "$@"
EOF
  chmod +x "$stubdir/uname"
  # Path with a real uname fallback so other tools keep working is overkill here;
  # install.sh only calls uname -s/-m at the top.
  rc=0
  env PATH="$stubdir:$PATH" sh "$INSTALL" --install-dir "$outdir" >/tmp/go-rag-smoke-platform.log 2>&1 || rc=$?
  if [ "$rc" != "0" ] && [ ! -e "$outdir/go-rag" ]; then
    pass
  else fail "unsupported platform must exit non-zero with no install (rc=$rc)"; cat /tmp/go-rag-smoke-platform.log; fi
  rm -rf "$outdir" "$stubdir"
}

case "$SCENARIO" in
  happy) happy;;
  tamper) tamper;;
  missing) missing;;
  platform) platform;;
  all) happy; tamper; missing; platform;;
  *) echo "unknown scenario: $SCENARIO" >&2; exit 2;;
esac

echo ""
echo "results: $PASS passed, $FAIL failed"
[ "$FAIL" = "0" ]
