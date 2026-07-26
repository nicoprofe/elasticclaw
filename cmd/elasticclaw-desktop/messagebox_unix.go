//go:build darwin || linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// messageBox reports a failure the user needs to see.
//
// Unlike the Windows build this binary is not linked as a GUI subsystem
// executable, so stderr is real whenever it was started from a terminal — and that
// is the common case on these platforms. stderr is therefore the guaranteed path,
// and a native dialog is attempted on top of it for the case where the app was
// launched from a file manager or dock with nowhere to print.
func messageBox(title, text string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, text)
	if err := showDialog(title, text); err != nil {
		// Nothing to do about it: the message already reached stderr and the log.
		return
	}
}

// askYesNo asks a question, defaulting to no when there is no way to ask.
//
// A missing dialog tool must not be read as consent. The Windows build uses this to
// offer installation; here it declines, which leaves the binary exactly where the
// user put it rather than silently modifying their system.
func askYesNo(title, text string) bool {
	answer, ok := promptYesNo(title, text)
	if !ok {
		return false
	}
	return answer
}

func dialogText(title, text string) string {
	if strings.TrimSpace(title) == "" {
		return text
	}
	return title + "\n\n" + text
}

// escapeAppleScript quotes a Go string for embedding in an AppleScript literal.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func lookPath(name string) (string, bool) {
	path, err := exec.LookPath(name)
	return path, err == nil
}
