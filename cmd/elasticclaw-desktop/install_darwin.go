//go:build darwin

package main

import "errors"

// Self-installation is not implemented on macOS.
//
// Appearing in Launchpad and the Dock with a name and icon requires a real .app
// bundle — a directory with Contents/MacOS, an Info.plist and an .icns — and for
// Gatekeeper not to object it wants signing and notarization. A bare executable
// dropped somewhere cannot fake that. Rather than write something that half works
// and leaves an app that will not launch from Finder, this build says so plainly and
// runs from wherever it is.
//
// scripts/install.sh already puts the CLI on PATH, which is how the hub is normally
// started on macOS.

func maybeOfferInstall() bool { return false }

func runInstall() error {
	return errors.New("--install is not supported on macOS.\n\n" +
		"This build runs from wherever you put it. Appearing in Launchpad needs a\n" +
		"signed .app bundle, which is not built yet.\n\n" +
		"To install the command-line hub instead:\n" +
		"  curl -fsSL https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.sh | sh")
}

func runUninstall() error {
	return errors.New("--uninstall is not supported on macOS: nothing was installed.\n\n" +
		"Delete the executable to remove it.")
}
