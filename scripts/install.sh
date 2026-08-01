#!/bin/sh
# Installs ElasticClaw on macOS and Linux.
#
# Downloads the latest release for this machine, verifies everything against the
# release checksum manifest, and installs:
#
#   - the elasticclaw CLI, onto PATH
#   - the desktop app: ElasticClaw.app in Applications on macOS, or the
#     elasticclaw-desktop binary plus a desktop entry on Linux
#
# No root required when the target directories are writable; sudo is used only if
# they are not.
#
# Installing the desktop app this way is deliberately the recommended route on
# macOS. Gatekeeper quarantines anything a browser downloads, and these builds are
# not notarized, so an app unzipped from Downloads makes the user click through a
# security warning. curl sets no quarantine attribute, so an app installed here just
# opens.
#
# After installation, `elasticclaw upgrade` handles every future CLI update, so this
# script is only needed once per machine.
#
#   curl -fsSL https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.sh | sh
#
# Environment overrides:
#   ELASTICCLAW_VERSION       install a specific tag instead of the latest
#   ELASTICCLAW_RELEASE_REPO  install from a different repository
#   ELASTICCLAW_INSTALL_DIR   install the CLI somewhere other than the default
#   ELASTICCLAW_SKIP_DESKTOP  set to 1 to install only the command-line tool
#
# POSIX sh on purpose: macOS ships bash 3.2, and some Linux images have no bash
# at all. Nothing here needs more than sh provides.

set -eu

REPO="${ELASTICCLAW_RELEASE_REPO:-nicoprofe/elasticclaw}"
VERSION="${ELASTICCLAW_VERSION:-}"

die() {
	printf '%s\n' "error: $*" >&2
	exit 1
}

warn() {
	printf '%s\n' "warning: $*" >&2
}

# --- Platform ----------------------------------------------------------------
os=$(uname -s)
case "$os" in
Darwin) os_name=darwin ;;
Linux) os_name=linux ;;
*) die "unsupported operating system: $os. Windows users want scripts/install.ps1." ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch_name=amd64 ;;
arm64 | aarch64) arch_name=arm64 ;;
*) die "unsupported architecture: $arch" ;;
esac

asset="elasticclaw-${os_name}-${arch_name}"

# --- Install directory -------------------------------------------------------
# Prefer a directory the user already owns, so the common case needs no sudo.
if [ -n "${ELASTICCLAW_INSTALL_DIR:-}" ]; then
	install_dir="$ELASTICCLAW_INSTALL_DIR"
elif [ -d "$HOME/.local/bin" ]; then
	install_dir="$HOME/.local/bin"
elif [ -w /usr/local/bin ] 2>/dev/null; then
	install_dir=/usr/local/bin
else
	install_dir="$HOME/.local/bin"
fi

for tool in curl uname; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool is required but not installed"
done

# --- Resolve version ---------------------------------------------------------
if [ -z "$VERSION" ]; then
	printf 'Finding latest release... '
	VERSION=$(curl -fsSL -H 'User-Agent: elasticclaw-installer' \
		"https://api.github.com/repos/${REPO}/releases/latest" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1)
	[ -n "$VERSION" ] || die "could not determine the latest release of ${REPO}. Set ELASTICCLAW_VERSION to install a specific version."
	printf '%s\n' "$VERSION"
fi

base="https://github.com/${REPO}/releases/download/${VERSION}"
tmp=$(mktemp -d 2>/dev/null || mktemp -d -t elasticclaw)
# shellcheck disable=SC2064
trap "rm -rf '$tmp'" EXIT INT TERM

# --- Checksums ---------------------------------------------------------------
# Fetched once and reused: it is both the integrity check and the way to ask what
# this release actually publishes, which is how an older release without the
# desktop artifacts is detected rather than guessed at.
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" ||
	die "could not download checksums.txt for ${VERSION}; refusing to install unverified binaries"

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "neither sha256sum nor shasum is available; cannot verify the download"
	fi
}

# expected_sum <asset> — prints the recorded hash, or nothing if unlisted.
expected_sum() {
	sed -n "s/^\([0-9a-fA-F]\{64\}\)[[:space:]][[:space:]]*[*]\{0,1\}${1}\$/\1/p" \
		"$tmp/checksums.txt" | head -n 1 | tr 'A-F' 'a-f'
}

# fetch_verified <asset> — downloads into $tmp and verifies. Nothing that fails this
# is ever made executable or installed.
fetch_verified() {
	_asset="$1"
	_expected=$(expected_sum "$_asset")
	[ -n "$_expected" ] || die "checksums.txt for ${VERSION} does not list ${_asset}"

	printf '%s\n' "Downloading ${_asset} (${VERSION})..."
	curl -fsSL -o "$tmp/$_asset" "$base/$_asset" ||
		die "could not download ${_asset} for ${VERSION}"

	printf 'Verifying checksum... '
	_actual=$(sha256_of "$tmp/$_asset" | tr 'A-F' 'a-f')
	if [ "$_actual" != "$_expected" ]; then
		die "checksum mismatch for ${_asset}.
  expected $_expected
  actual   $_actual
The download was corrupted or tampered with; nothing was installed."
	fi
	printf 'OK\n'
}

# install_file <src> <dest> — moves into place, elevating only if it has to.
install_file() {
	_src="$1"
	_dest="$2"
	_dir=$(dirname "$_dest")
	if mkdir -p "$_dir" 2>/dev/null && [ -w "$_dir" ]; then
		# A running elasticclaw holds its image open; replacing the inode rather than
		# writing through it avoids "text file busy" and leaves the running copy alone.
		mv -f "$_src" "$_dest"
	else
		printf '%s\n' "Installing to ${_dir} requires elevated permissions."
		command -v sudo >/dev/null 2>&1 || die "cannot write to ${_dir} and sudo is not available. Set ELASTICCLAW_INSTALL_DIR to a writable directory."
		sudo mkdir -p "$_dir"
		sudo mv -f "$_src" "$_dest"
	fi
}

# --- CLI ---------------------------------------------------------------------
fetch_verified "$asset"
chmod +x "$tmp/$asset"
target="$install_dir/elasticclaw"
install_file "$tmp/$asset" "$target"

# macOS quarantines anything downloaded, and the binaries are not notarized, so
# Gatekeeper would refuse to run it. Clearing the attribute is the same decision a
# user makes by hand in Security settings, made explicit here.
if [ "$os_name" = darwin ] && command -v xattr >/dev/null 2>&1; then
	xattr -d com.apple.quarantine "$target" 2>/dev/null || true
fi

installed=$("$target" version 2>/dev/null || printf '%s' "$VERSION")
printf '\n%s\n' "Installed: $installed"
printf '%s\n\n' "  $target"

# --- Desktop app -------------------------------------------------------------
install_desktop_darwin() {
	_zip="ElasticClaw-macos.zip"
	if [ -z "$(expected_sum "$_zip")" ]; then
		warn "${VERSION} does not publish ${_zip}; installed the command-line tool only."
		return 0
	fi
	fetch_verified "$_zip"

	# ditto is the macOS-native unarchiver and preserves the bundle's code signature;
	# unzip is the fallback for the unusual machine without it.
	_stage="$tmp/app"
	mkdir -p "$_stage"
	if command -v ditto >/dev/null 2>&1; then
		ditto -x -k "$tmp/$_zip" "$_stage" || die "could not unpack ${_zip}"
	elif command -v unzip >/dev/null 2>&1; then
		unzip -q "$tmp/$_zip" -d "$_stage" || die "could not unpack ${_zip}"
	else
		warn "neither ditto nor unzip is available; skipped the desktop app."
		return 0
	fi
	[ -d "$_stage/ElasticClaw.app" ] || die "${_zip} did not contain ElasticClaw.app"

	# /Applications is writable by admin users, which is the common case. Falling
	# back to ~/Applications keeps a non-admin or managed Mac working instead of
	# stopping at a password prompt the user may not be able to answer.
	if [ -w /Applications ]; then
		_appdir=/Applications
	else
		_appdir="$HOME/Applications"
		mkdir -p "$_appdir"
	fi
	_app="$_appdir/ElasticClaw.app"

	# Replace rather than merge: files the old version shipped and this one does not
	# would otherwise survive inside the bundle.
	rm -rf "$_app" 2>/dev/null || die "could not replace ${_app}. Quit ElasticClaw and run this again."
	if command -v ditto >/dev/null 2>&1; then
		ditto "$_stage/ElasticClaw.app" "$_app" || die "could not install ${_app}"
	else
		cp -R "$_stage/ElasticClaw.app" "$_app" || die "could not install ${_app}"
	fi

	# Belt and braces: curl does not set com.apple.quarantine, so this normally finds
	# nothing. It matters when the zip was fetched some other way.
	command -v xattr >/dev/null 2>&1 && xattr -dr com.apple.quarantine "$_app" 2>/dev/null || true

	printf '%s\n' "Installed: $_app"
	printf '%s\n\n' "  open it from Launchpad, or run: open -a ElasticClaw"
}

install_desktop_linux() {
	_bin="elasticclaw-desktop-linux-${arch_name}"
	if [ -z "$(expected_sum "$_bin")" ]; then
		# Only the release runner's own architecture is built, so this is expected on
		# arm64 rather than a fault.
		warn "${VERSION} does not publish ${_bin}; installed the command-line tool only."
		printf '%s\n\n' "         Run \`elasticclaw hub\` and open the dashboard in a browser."
		return 0
	fi
	fetch_verified "$_bin"
	chmod +x "$tmp/$_bin"
	_target="$install_dir/elasticclaw-desktop"
	install_file "$tmp/$_bin" "$_target"
	printf '%s\n' "Installed: $_target"

	# Unlike everything else here this binary is dynamically linked, against
	# WebKitGTK. Without the library it does not fail in a way the app can report —
	# the dynamic loader kills it before main runs, and launched from a desktop icon
	# that looks like nothing happening at all. Say so now, while there is a terminal
	# to say it in.
	if ! webkit_present; then
		warn "libwebkit2gtk-4.1 is not installed; the desktop window cannot open without it."
		printf '%s\n' "         Debian/Ubuntu:  sudo apt install libwebkit2gtk-4.1-0"
		printf '%s\n' "         Fedora:         sudo dnf install webkit2gtk4.1"
		printf '%s\n' "         Arch:           sudo pacman -S webkit2gtk-4.1"
		printf '%s\n\n' "         Until then, run \`elasticclaw hub\` and use the dashboard in a browser."
		return 0
	fi

	# Registering the app on a headless box would leave a desktop entry nothing can
	# launch, so this only runs where there is a desktop to register with.
	if [ -n "${DISPLAY:-}" ] || [ -n "${WAYLAND_DISPLAY:-}" ]; then
		"$_target" --install || warn "could not add the desktop entry; run '$_target --install' to retry."
	else
		printf '%s\n' "  No graphical session detected — run '$_target --install' from your desktop"
		printf '%s\n' "  to add it to your applications."
	fi
	printf '\n'
}

webkit_present() {
	if command -v ldconfig >/dev/null 2>&1; then
		ldconfig -p 2>/dev/null | grep -q 'libwebkit2gtk-4\.1' && return 0
	fi
	# ldconfig is absent or its cache is not authoritative (NixOS, musl images), so
	# fall back to looking for the library itself before claiming it is missing.
	for dir in /usr/lib /usr/lib64 /usr/lib/x86_64-linux-gnu /usr/lib/aarch64-linux-gnu /lib64; do
		[ -n "$(find "$dir" -maxdepth 1 -name 'libwebkit2gtk-4.1*' -print -quit 2>/dev/null)" ] && return 0
	done
	return 1
}

if [ "${ELASTICCLAW_SKIP_DESKTOP:-0}" != "1" ]; then
	case "$os_name" in
	darwin) install_desktop_darwin ;;
	linux) install_desktop_linux ;;
	esac
fi

# --- Next steps ---------------------------------------------------------------
case ":${PATH}:" in
*":${install_dir}:"*) ;;
*)
	printf '%s\n' "Note: ${install_dir} is not on your PATH. Add this to your shell profile:"
	printf '%s\n\n' "  export PATH=\"${install_dir}:\$PATH\""
	;;
esac

printf '%s\n' 'Next steps:'
printf '%s\n' '  elasticclaw hub        # start the hub, then open the dashboard it prints'
printf '%s\n' '  elasticclaw --help     # see available commands'
printf '%s\n' '  elasticclaw upgrade    # update in place, any time'
