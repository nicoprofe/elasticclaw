package main

import (
	"os"
	"path/filepath"
	"strings"
)

// installPromptSuppressed reports whether the first-run "install me?" question must
// be skipped.
//
// A dialog nobody can answer is a hang, and the app is started unattended in more
// places than it looks: CI smoke tests, container images, and anyone scripting a
// launch. Those cases need a way to say "run, do not ask" that does not depend on
// guessing from the environment.
func installPromptSuppressed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ELASTICCLAW_NO_INSTALL_PROMPT"))) {
	case "", "0", "false", "no":
		return false
	}
	return true
}

// appBundleRoot returns the path of the macOS .app bundle containing exe, or an
// empty string when exe is not inside one.
//
// This lives in a file with no build tag, and works on plain strings, so the layout
// rule it encodes can be tested from any machine rather than only on a Mac.
//
// A bundle's executable always sits at exactly <Name>.app/Contents/MacOS/<exe>. The
// three-level walk is deliberate: matching any ancestor ending in .app would also
// match a binary that merely happens to live somewhere under a bundle's Resources,
// and moving that to /Applications would be wrong.
func appBundleRoot(exe string) string {
	macOS := filepath.Dir(exe)       // .../Contents/MacOS
	contents := filepath.Dir(macOS)  // .../Contents
	bundle := filepath.Dir(contents) // .../Name.app
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" {
		return ""
	}
	if !strings.HasSuffix(bundle, ".app") {
		return ""
	}
	return bundle
}

// stripLaunchServicesArgs removes arguments macOS adds on its own.
//
// -psn_0_<n> is passed by LaunchServices to bundled apps. Nothing else in this
// program should ever see it, and treating it as a user-supplied argument would
// change how the app starts depending on whether it was opened from Finder or from
// a terminal. Kept alongside appBundleRoot, and free of build tags, for the same
// reason: it is testable anywhere.
func stripLaunchServicesArgs(args []string) []string {
	kept := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-psn_") {
			continue
		}
		kept = append(kept, a)
	}
	return kept
}
