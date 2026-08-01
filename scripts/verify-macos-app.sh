#!/usr/bin/env bash
#
# Verify that a built ElasticClaw.app is something a Mac user can actually install.
#
# Every check here stands for a failure that is invisible in CI and obvious on a
# user's machine: an unsigned bundle will not launch on Apple Silicon at all, a
# bundle missing NSAllowsLocalNetworking opens to a permanently blank window, and a
# zip that lost the executable bit installs an app that does nothing when clicked.
# That class of bug is the reason the macOS download was broken in the first place,
# so it is asserted rather than assumed.
#
# Usage:
#   scripts/verify-macos-app.sh [dist]
#
# Runs on Linux too. The macOS-only checks — lipo, codesign, ditto — are skipped
# there and reported as skipped, so the structural half can be tested from any
# machine while the release runner does the whole thing.

set -uo pipefail

DIR="${1:-dist}"
APP="$DIR/ElasticClaw.app"
EXE="$APP/Contents/MacOS/ElasticClaw"
ZIP="$DIR/ElasticClaw-macos.zip"
PLIST="$APP/Contents/Info.plist"

failed=0

fail() {
	printf '%s\n' "❌ $*" >&2
	# Also emit a GitHub annotation when running in Actions, so the failure lands on
	# the step rather than only in the log.
	[[ -n "${GITHUB_ACTIONS:-}" ]] && printf '::error::%s\n' "$*"
	failed=1
}

pass() { printf '%s\n' "  ✓ $*"; }
skip() { printf '%s\n' "  – $* (skipped: not macOS)"; }

echo "Verifying $APP"

# --- Structure ----------------------------------------------------------------
# macOS only treats a directory as an application when it has exactly this shape.
for path in "$APP" "$EXE" "$PLIST" "$APP/Contents/PkgInfo" "$APP/Contents/Resources/ElasticClaw.icns" "$ZIP"; do
	[[ -e "$path" ]] || fail "missing $path"
done
if [[ $failed -ne 0 ]]; then
	echo "the bundle is not present or not complete; nothing further can be checked" >&2
	exit 1
fi
pass "bundle layout"

# This is the bit a browser download loses and the whole reason a bare binary was
# uninstallable. It must be set inside the bundle.
if [[ -x "$EXE" ]]; then
	pass "executable bit"
else
	fail "$EXE is not executable"
fi

# --- Info.plist ---------------------------------------------------------------
# NSAllowsLocalNetworking is not boilerplate: the window is a WKWebView pointed at
# http://127.0.0.1, and App Transport Security blocks plaintext HTTP for any app that
# has a plist. Without it the app launches to a blank window and looks like a hang.
for key in CFBundleIdentifier CFBundleExecutable CFBundleIconFile CFBundlePackageType NSHighResolutionCapable NSAllowsLocalNetworking; do
	grep -q "<key>${key}</key>" "$PLIST" || fail "Info.plist has no ${key}"
done

if command -v plutil >/dev/null 2>&1; then
	# plutil parses the plist the same way LaunchServices does, so a plist that passes
	# here cannot be malformed at launch.
	plutil -lint "$PLIST" >/dev/null || fail "Info.plist does not parse"
	pass "Info.plist parses and carries every required key"
else
	# python is not guaranteed either, so this is best-effort off macOS. The Go test
	# in scripts/ does a full XML parse.
	if command -v python3 >/dev/null 2>&1; then
		python3 -c "import plistlib,sys; plistlib.load(open(sys.argv[1],'rb'))" "$PLIST" >/dev/null 2>&1 ||
			fail "Info.plist does not parse"
	fi
	pass "Info.plist carries every required key"
	skip "plutil lint"
fi

# --- Architectures ------------------------------------------------------------
if command -v lipo >/dev/null 2>&1; then
	archs="$(lipo -archs "$EXE" 2>/dev/null)"
	for want in x86_64 arm64; do
		grep -q "$want" <<<"$archs" || fail "the executable is missing $want (has: ${archs:-none})"
	done
	pass "universal binary ($archs)"
else
	skip "architecture check"
fi

# --- Signature ----------------------------------------------------------------
# Ad-hoc, not Developer ID — but it must be signed and the signature must still match
# the bundle after packaging touched it. arm64 refuses to execute an unsigned binary,
# and lipo strips the signature the Go linker applied.
if command -v codesign >/dev/null 2>&1; then
	if codesign --verify --strict "$APP" 2>&1; then
		pass "ad-hoc signature is valid"
	else
		fail "the bundle is not validly signed — it will not launch on Apple Silicon"
	fi
else
	skip "signature check"
fi

# --- The archive users actually download ---------------------------------------
unpacked="$(mktemp -d)"
trap 'rm -rf "$unpacked"' EXIT

if command -v ditto >/dev/null 2>&1; then
	ditto -x -k "$ZIP" "$unpacked" || fail "could not unpack $ZIP"
elif command -v unzip >/dev/null 2>&1; then
	unzip -q "$ZIP" -d "$unpacked" || fail "could not unpack $ZIP"
else
	echo "  – zip round trip (skipped: no unarchiver)"
	unpacked=""
fi

if [[ -n "$unpacked" ]]; then
	# --keepParent: it must unzip as ElasticClaw.app, not as loose Contents/
	# directories dumped wherever the user unzipped it.
	unzipped_exe="$unpacked/ElasticClaw.app/Contents/MacOS/ElasticClaw"
	if [[ ! -e "$unzipped_exe" ]]; then
		fail "the zip does not contain ElasticClaw.app/Contents/MacOS/ElasticClaw"
	elif [[ ! -x "$unzipped_exe" ]]; then
		fail "the zipped app lost its executable bit"
	else
		pass "zip round trip keeps the bundle intact"
	fi

	if command -v codesign >/dev/null 2>&1 && [[ -e "$unzipped_exe" ]]; then
		codesign --verify --strict "$unpacked/ElasticClaw.app" 2>&1 ||
			fail "the zipped app lost its signature"
	fi
fi

echo
if [[ $failed -ne 0 ]]; then
	echo "❌ $APP would not install correctly on a Mac" >&2
	exit 1
fi
echo "✓ $APP verified"
