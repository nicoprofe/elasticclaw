#!/usr/bin/env bash
#
# Build production release artifacts for every supported platform.
#
# Asset names here are a contract with pkg/release.AssetName() — the self-updater
# constructs download URLs from those names, so renaming an artifact breaks
# `elasticclaw upgrade` for every installed client. Keep them in sync.
#
# Usage:
#   VERSION=v0.1.0 RELEASE_REPO=owner/repo scripts/build-release.sh
#
# Environment:
#   VERSION       release tag to stamp into the binary (default: git describe)
#   RELEASE_REPO  owner/repo that will serve these artifacts, baked in as the
#                 self-update source (default: elasticclaw/elasticclaw)
#   OUTPUT_DIR    where artifacts land (default: dist/)
#   SKIP_WEB      set to 1 to reuse an existing internal/webui/out build
#
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
RELEASE_REPO="${RELEASE_REPO:-elasticclaw/elasticclaw}"
OUTPUT_DIR="${OUTPUT_DIR:-dist}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
# Reproducible: prefer the commit's own timestamp over "now".
BUILD_DATE="${BUILD_DATE:-$(git show -s --format=%cI HEAD 2>/dev/null || date -u +"%Y-%m-%dT%H:%M:%SZ")}"

if [[ "$RELEASE_REPO" != */* ]]; then
  echo "❌ RELEASE_REPO must be owner/repo, got: $RELEASE_REPO" >&2
  exit 1
fi

PKG="github.com/elasticclaw/elasticclaw"
LDFLAGS="-s -w \
  -X ${PKG}/cmd.Version=${VERSION} \
  -X ${PKG}/cmd.Commit=${COMMIT} \
  -X ${PKG}/cmd.BuildDate=${BUILD_DATE} \
  -X ${PKG}/pkg/release.DefaultRepo=${RELEASE_REPO}"

echo "Building elasticclaw ${VERSION} (${COMMIT})"
echo "  self-update source: ${RELEASE_REPO}"
echo "  output:             ${OUTPUT_DIR}/"
echo

# ── Web UI ────────────────────────────────────────────────────────────────────
# The production binary serves the dashboard from embedded files; without this
# the -tags embedweb build fails its "web UI not built" check at runtime.
if [[ "${SKIP_WEB:-0}" == "1" && -f internal/webui/out/index.html ]]; then
  echo "→ Reusing existing web UI build (SKIP_WEB=1)"
else
  echo "→ Building web UI"
  make build-web
fi
echo

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

# ── Binaries ──────────────────────────────────────────────────────────────────
# Asset name  ==  pkg/release.AssetName(goos, goarch)
build() {
  local goos="$1" goarch="$2" asset="$3"
  echo "→ ${asset}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -tags embedweb -ldflags "$LDFLAGS" \
    -o "${OUTPUT_DIR}/${asset}" .
}

build linux   amd64 elasticclaw-linux-amd64
build linux   arm64 elasticclaw-linux-arm64
build darwin  amd64 elasticclaw-darwin-amd64
build darwin  arm64 elasticclaw-darwin-arm64
build windows amd64 elasticclaw-windows-amd64.exe
build windows arm64 elasticclaw-windows-arm64.exe

# The native Windows desktop app: a Win32 window hosting WebView2, no browser.
# It must be a separate binary from the CLI because -H=windowsgui drops the
# console subsystem — required so double-clicking shows no console window, and
# fatal for a command-line tool, which needs somewhere to print.
build_desktop() {
  local goarch="$1" asset="$2"
  echo "→ ${asset}"
  CGO_ENABLED=0 GOOS=windows GOARCH="$goarch" \
    go build -trimpath -tags embedweb -ldflags "$LDFLAGS -H=windowsgui" \
    -o "${OUTPUT_DIR}/${asset}" ./cmd/elasticclaw-desktop/
}

build_desktop amd64 ElasticClaw-windows-amd64.exe
build_desktop arm64 ElasticClaw-windows-arm64.exe

# claw-bridge runs inside sandboxes (always linux/amd64) and is downloaded by
# the hub at run time — it must ship in the same release as the hub binary.
echo "→ claw-bridge-linux-amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "$LDFLAGS" \
  -o "${OUTPUT_DIR}/claw-bridge-linux-amd64" ./cmd/claw-bridge/

# ── Checksums ─────────────────────────────────────────────────────────────────
# `elasticclaw upgrade` refuses to install an artifact absent from this file.
echo
echo "→ checksums.txt"
(cd "$OUTPUT_DIR" && sha256sum ./* > checksums.txt && sed -i 's| \./| |' checksums.txt)

echo
echo "✓ Release artifacts in ${OUTPUT_DIR}/"
ls -lh "$OUTPUT_DIR"
