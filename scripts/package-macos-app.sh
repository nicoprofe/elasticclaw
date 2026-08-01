#!/usr/bin/env bash
#
# Assemble ElasticClaw.app from built darwin binaries and zip it for release.
#
# Why this exists: a bare Mach-O executable is not something a Mac user can install.
# Downloaded through a browser it arrives without the execute bit, carries
# com.apple.quarantine, has no icon and no name, and Finder will not launch it — the
# double-click does nothing at all. macOS only treats something as an application
# when it is a bundle: a directory with Contents/MacOS, an Info.plist and an .icns.
# This script produces that bundle, ad-hoc signs it (required for it to run at all on
# Apple Silicon), and packs it with ditto so the bundle structure and the signature
# survive the round trip through a zip.
#
# Usage:
#   VERSION=v0.1.0 scripts/package-macos-app.sh [binary ...]
#
# With no arguments it picks up whichever of
# $OUTPUT_DIR/elasticclaw-desktop-darwin-{amd64,arm64} exist and lipos them into one
# universal executable. Passing a single binary produces a single-architecture
# bundle, which is what a developer building only for their own Mac wants.
#
# Environment:
#   VERSION     tag to stamp into the bundle (default: git describe)
#   OUTPUT_DIR  where the binaries are and the bundle goes (default: dist)
#
# Runs on Linux too, minus the macOS-only steps (lipo, codesign, ditto), so the
# bundle layout can be built and tested without a Mac. A bundle produced that way is
# for tests only — it is unsigned and will not launch on Apple Silicon.

set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUTPUT_DIR="${OUTPUT_DIR:-dist}"

APP_NAME="ElasticClaw"
BUNDLE_ID="ai.elasticclaw.desktop"
ICNS="build/macos/ElasticClaw.icns"

APP="${OUTPUT_DIR}/${APP_NAME}.app"
ZIP="${OUTPUT_DIR}/${APP_NAME}-macos.zip"

die() {
	printf '%s\n' "❌ $*" >&2
	exit 1
}

# --- Inputs -------------------------------------------------------------------
binaries=("$@")
if [[ ${#binaries[@]} -eq 0 ]]; then
	for arch in amd64 arm64; do
		candidate="${OUTPUT_DIR}/elasticclaw-desktop-darwin-${arch}"
		[[ -f "$candidate" ]] && binaries+=("$candidate")
	done
fi
[[ ${#binaries[@]} -gt 0 ]] || die "no darwin desktop binaries found in ${OUTPUT_DIR}/ and none given on the command line"
for b in "${binaries[@]}"; do
	[[ -f "$b" ]] || die "not a file: $b"
done
[[ -f "$ICNS" ]] || die "$ICNS is missing — refusing to build an app with no icon. Regenerate it with scripts/make-macos-icns.py"

# --- Version ------------------------------------------------------------------
# CFBundleShortVersionString is shown in Finder's Get Info and must be a plain
# dotted number; a tag like v0.4.0-beta.1 is rejected by codesign and displays as
# garbage. The full tag is kept alongside it in CFBundleGetInfoString so nothing is
# lost.
numeric_version="${VERSION#v}"
numeric_version="${numeric_version%%-*}"
[[ "$numeric_version" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]] || numeric_version="0.0.0"

echo "Packaging ${APP_NAME}.app ${VERSION}"
echo "  binaries: ${binaries[*]}"

# --- Bundle layout ------------------------------------------------------------
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

exe="$APP/Contents/MacOS/$APP_NAME"
if [[ ${#binaries[@]} -gt 1 ]]; then
	command -v lipo >/dev/null 2>&1 ||
		die "lipo is needed to merge ${#binaries[@]} architectures but is not available. Pass a single binary to build a single-architecture bundle."
	lipo -create -output "$exe" "${binaries[@]}"
	echo "  ✓ universal executable ($(lipo -archs "$exe" 2>/dev/null || echo '?'))"
else
	cp "${binaries[0]}" "$exe"
	echo "  ✓ single-architecture executable"
fi
chmod 755 "$exe"

cp "$ICNS" "$APP/Contents/Resources/${APP_NAME}.icns"

# CFBundlePackageType/CFBundleSignature in a file of their own: Finder still reads
# PkgInfo, and its absence makes an otherwise valid bundle look malformed.
printf 'APPL????' >"$APP/Contents/PkgInfo"

# NSAllowsLocalNetworking is load-bearing, not boilerplate. The window is a WKWebView
# pointed at http://127.0.0.1, and App Transport Security blocks plaintext HTTP for
# any app that has an Info.plist. Without this key the app launches to a permanently
# blank window — which looks exactly like a hang and is impossible to diagnose from
# the outside.
cat >"$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>${APP_NAME}</string>
	<key>CFBundleDisplayName</key>
	<string>${APP_NAME}</string>
	<key>CFBundleExecutable</key>
	<string>${APP_NAME}</string>
	<key>CFBundleIdentifier</key>
	<string>${BUNDLE_ID}</string>
	<key>CFBundleIconFile</key>
	<string>${APP_NAME}</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleSignature</key>
	<string>????</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleShortVersionString</key>
	<string>${numeric_version}</string>
	<key>CFBundleVersion</key>
	<string>${numeric_version}</string>
	<key>CFBundleGetInfoString</key>
	<string>${APP_NAME} ${VERSION}</string>
	<key>LSMinimumSystemVersion</key>
	<string>11.0</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.developer-tools</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSHumanReadableCopyright</key>
	<string>Apache 2.0 open source.</string>
	<key>NSAppTransportSecurity</key>
	<dict>
		<key>NSAllowsLocalNetworking</key>
		<true/>
	</dict>
</dict>
</plist>
PLIST
echo "  ✓ Info.plist and icon"

# --- Signature ----------------------------------------------------------------
# Ad-hoc, not Developer ID: there is no Apple certificate to sign with. This is not
# cosmetic — arm64 refuses to execute an unsigned binary at all, and lipo strips the
# signature the Go linker applied, so the merged executable must be re-signed here or
# the app dies instantly on every Apple Silicon Mac.
#
# It is still not notarized, so a copy downloaded through a browser is quarantined
# and Gatekeeper asks the user to confirm the first launch. scripts/install.sh avoids
# that entirely by downloading with curl, which sets no quarantine attribute.
if command -v codesign >/dev/null 2>&1; then
	codesign --force --sign - --identifier "$BUNDLE_ID" "$APP"
	codesign --verify --strict "$APP" || die "the bundle failed its own signature check"
	echo "  ✓ ad-hoc signed"
else
	echo "  ⚠ codesign unavailable — bundle is unsigned and will not launch on Apple Silicon (fine for tests, not for a release)"
fi

# --- Archive ------------------------------------------------------------------
# ditto, not zip: a plain zip loses the signature's extended attributes and can drop
# the executable bit, and an app that arrives unsigned or non-executable fails on the
# user's machine rather than here.
rm -f "$ZIP"
if command -v ditto >/dev/null 2>&1; then
	ditto -c -k --sequesterRsrc --keepParent "$APP" "$ZIP"
else
	(cd "$OUTPUT_DIR" && zip -qry "$(basename "$ZIP")" "${APP_NAME}.app")
	echo "  ⚠ ditto unavailable — used zip, which does not preserve signatures"
fi

echo "  ✓ ${ZIP}"
echo
echo "✓ ${APP} and ${ZIP}"
