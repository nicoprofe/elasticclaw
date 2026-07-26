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
# Names must differ from the CLI by more than case: GitHub release asset names are
# case-insensitive for uniqueness, so "ElasticClaw-..." collided with "elasticclaw-...".
# It must be a separate binary from the CLI because -H=windowsgui drops the
# console subsystem — required so double-clicking shows no console window, and
# fatal for a command-line tool, which needs somewhere to print.

# Embed the app icon and Windows version metadata. Without a .rsrc resource the
# taskbar shows a blank default icon and the file's Properties pane has no
# version or product name — it does not read as an installed application.
# The icon and version metadata are part of what makes this read as an installed
# application, so a release that cannot embed them fails rather than shipping a
# blank-icon exe that nobody notices until it is on a user's taskbar.
build_windows_resource() {
  local syso="cmd/elasticclaw-desktop/resource_windows_amd64.syso"
  rm -f "$syso"
  if ! command -v goversioninfo >/dev/null 2>&1; then
    GOFLAGS=-mod=mod go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest >/dev/null 2>&1 || true
    export PATH="$PATH:$(go env GOPATH)/bin"
  fi
  if ! command -v goversioninfo >/dev/null 2>&1; then
    echo "❌ goversioninfo unavailable — refusing to build a desktop app with no icon" >&2
    exit 1
  fi
  if [[ ! -f build/windows/elasticclaw.ico ]]; then
    echo "❌ build/windows/elasticclaw.ico is missing — refusing to build without the brand icon" >&2
    exit 1
  fi
  # Windows version fields are four integers; derive them from the CalVer tag and
  # fall back to zeros for anything non-numeric.
  local nums
  nums="$(printf '%s' "$VERSION" | tr -cd '0-9.' | tr '.' ' ')"
  set -- $nums 0 0 0 0
  python3 - "$1" "$2" "$3" "$4" "$VERSION" <<'PY'
import json, sys
maj, mnr, pat, bld, version = int(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4]), sys.argv[5]
tmpl = open("build/windows/versioninfo.json.tmpl").read().replace("__VERSION__", version)
d = json.loads(tmpl)
for k in ("FileVersion", "ProductVersion"):
    d["FixedFileInfo"][k] = {"Major": maj, "Minor": mnr, "Patch": pat, "Build": bld}
json.dump(d, open("build/windows/versioninfo.json", "w"), indent=2)
PY
  goversioninfo -o "$syso" -platform-specific=false -64 build/windows/versioninfo.json >/dev/null
  echo "  ✓ icon and version metadata embedded"
}

build_desktop() {
  local goarch="$1" asset="$2"
  echo "→ ${asset}"
  CGO_ENABLED=0 GOOS=windows GOARCH="$goarch" \
    go build -trimpath -tags embedweb -ldflags "$LDFLAGS -H=windowsgui -X main.Version=${VERSION}" \
    -o "${OUTPUT_DIR}/${asset}" ./cmd/elasticclaw-desktop/
}

# The macOS and Linux desktop builds need cgo, so unlike everything else here they
# cannot be cross-compiled from one machine. Each is produced by a job running on its
# own platform and uploaded into the same release; see .github/workflows/release.yml.
# These helpers exist so those jobs and a developer on that platform run the same
# command rather than two similar ones that drift.
build_desktop_darwin() {
  local goarch="$1" asset="$2"
  echo "→ ${asset} (requires macOS; links against WebKit)"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -tags embedweb -ldflags "$LDFLAGS -X main.Version=${VERSION}" \
    -o "${OUTPUT_DIR}/${asset}" ./cmd/elasticclaw-desktop/
}

# desktopgui selects the real GTK/WebKitGTK backend. Without it the package builds
# but reports that it has no GUI support, which is what keeps `go build ./...`
# working on machines with no WebKit headers.
build_desktop_linux() {
  local goarch="$1" asset="$2"
  echo "→ ${asset} (requires libwebkit2gtk-4.1-dev; dynamically linked)"
  CGO_ENABLED=1 GOOS=linux GOARCH="$goarch" \
    go build -trimpath -tags "embedweb desktopgui" -ldflags "$LDFLAGS -X main.Version=${VERSION}" \
    -o "${OUTPUT_DIR}/${asset}" ./cmd/elasticclaw-desktop/
}

build_windows_resource

build_desktop amd64 elasticclaw-desktop-windows-amd64.exe
build_desktop arm64 elasticclaw-desktop-windows-arm64.exe

# DESKTOP_TARGET lets the per-platform CI jobs ask for just their own artifact.
# Unset means "the cross-compilable set", which is what the main release job wants.
case "${DESKTOP_TARGET:-}" in
darwin)
  build_desktop_darwin amd64 elasticclaw-desktop-darwin-amd64
  build_desktop_darwin arm64 elasticclaw-desktop-darwin-arm64
  ;;
linux)
  # Only the host architecture: cgo cross-compilation would need a full sysroot for
  # the other one, and a wrong-but-present binary is worse than an absent one.
  build_desktop_linux "$(go env GOHOSTARCH)" "elasticclaw-desktop-linux-$(go env GOHOSTARCH)"
  ;;
esac

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
