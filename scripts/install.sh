#!/bin/sh
# Installs the elasticclaw CLI on macOS and Linux.
#
# Downloads the latest release binary for this machine, verifies it against the
# release checksum manifest, and installs it somewhere on PATH. No root required
# when the target directory is writable; sudo is used only if it is not.
#
# After installation, `elasticclaw upgrade` handles every future update, so this
# script is only needed once per machine.
#
#   curl -fsSL https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.sh | sh
#
# Environment overrides:
#   ELASTICCLAW_VERSION       install a specific tag instead of the latest
#   ELASTICCLAW_RELEASE_REPO  install from a different repository
#   ELASTICCLAW_INSTALL_DIR   install somewhere other than the default
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

# --- Download ----------------------------------------------------------------
printf '%s\n' "Downloading ${asset} (${VERSION})..."
curl -fsSL -o "$tmp/$asset" "$base/$asset" ||
	die "could not download ${asset} for ${VERSION}. Check that this release publishes a build for ${os_name}/${arch_name}."

# --- Verify ------------------------------------------------------------------
# An unverified binary is never made executable or installed.
printf 'Verifying checksum... '
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" ||
	die "could not download checksums.txt for ${VERSION}; refusing to install an unverified binary"

expected=$(sed -n "s/^\([0-9a-fA-F]\{64\}\)[[:space:]][[:space:]]*[*]\{0,1\}${asset}\$/\1/p" \
	"$tmp/checksums.txt" | head -n 1 | tr 'A-F' 'a-f')
[ -n "$expected" ] || die "checksums.txt for ${VERSION} does not list ${asset}"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
else
	die "neither sha256sum nor shasum is available; cannot verify the download"
fi
actual=$(printf '%s' "$actual" | tr 'A-F' 'a-f')

if [ "$actual" != "$expected" ]; then
	die "checksum mismatch for ${asset}.
  expected $expected
  actual   $actual
The download was corrupted or tampered with; nothing was installed."
fi
printf 'OK\n'

# --- Install -----------------------------------------------------------------
chmod +x "$tmp/$asset"
target="$install_dir/elasticclaw"

if mkdir -p "$install_dir" 2>/dev/null && [ -w "$install_dir" ]; then
	# A running elasticclaw holds its image open; replacing the inode rather than
	# writing through it avoids "text file busy" and leaves the running copy alone.
	mv -f "$tmp/$asset" "$target"
else
	printf '%s\n' "Installing to ${install_dir} requires elevated permissions."
	command -v sudo >/dev/null 2>&1 || die "cannot write to ${install_dir} and sudo is not available. Set ELASTICCLAW_INSTALL_DIR to a writable directory."
	sudo mkdir -p "$install_dir"
	sudo mv -f "$tmp/$asset" "$target"
fi

# macOS quarantines anything downloaded, and the binaries are not notarized, so
# Gatekeeper would refuse to run it. Clearing the attribute is the same decision a
# user makes by hand in Security settings, made explicit here.
if [ "$os_name" = darwin ] && command -v xattr >/dev/null 2>&1; then
	xattr -d com.apple.quarantine "$target" 2>/dev/null || true
fi

installed=$("$target" version 2>/dev/null || printf '%s' "$VERSION")
printf '\n%s\n\n' "Installed: $installed"

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
