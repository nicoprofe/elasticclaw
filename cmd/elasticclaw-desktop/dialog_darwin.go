//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// showDialog uses osascript, which is part of macOS, so there is no dependency to
// install and no case where the tool is simply absent.
func showDialog(title, text string) error {
	script := fmt.Sprintf(
		`display dialog "%s" with title "ElasticClaw" buttons {"OK"} default button "OK" with icon caution`,
		escapeAppleScript(dialogText(title, text)),
	)
	return exec.Command("osascript", "-e", script).Run()
}

// promptYesNo asks with osascript. Cancel and the dialog being dismissed both
// report false, which is the safe direction for a question that offers to modify
// the user's system.
func promptYesNo(title, text string) (bool, bool) {
	script := fmt.Sprintf(
		`display dialog "%s" with title "ElasticClaw" buttons {"No","Yes"} default button "Yes"`,
		escapeAppleScript(dialogText(title, text)),
	)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		// A non-zero exit is what Cancel produces, so this is not an error path
		// worth reporting — it is a "no".
		return false, false
	}
	return strings.Contains(string(out), "button returned:Yes"), true
}
