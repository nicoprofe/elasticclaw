//go:build linux

package main

import (
	"errors"
	"os/exec"
	"strings"
)

// showDialog tries the desktop's own dialog tool. Unlike macOS there is no tool
// guaranteed to exist, so a failure here is normal on a minimal system and the
// caller has already written the message to stderr and the log.
func showDialog(title, text string) error {
	body := dialogText(title, text)
	if path, ok := lookPath("zenity"); ok {
		return exec.Command(path, "--error", "--title=ElasticClaw", "--text="+body).Run()
	}
	if path, ok := lookPath("kdialog"); ok {
		return exec.Command(path, "--title", "ElasticClaw", "--error", body).Run()
	}
	return errors.New("no dialog tool available")
}

// promptYesNo asks using whichever dialog tool is installed. The second return
// value reports whether the question could be asked at all; when it could not, the
// caller must not treat the answer as consent.
func promptYesNo(title, text string) (bool, bool) {
	body := dialogText(title, text)
	if path, ok := lookPath("zenity"); ok {
		err := exec.Command(path, "--question", "--title=ElasticClaw", "--text="+body).Run()
		if err == nil {
			return true, true
		}
		// zenity exits 1 for No and 255 when it could not display anything at all.
		// Only the former is an answer.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, true
		}
		return false, false
	}
	if path, ok := lookPath("kdialog"); ok {
		err := exec.Command(path, "--title", "ElasticClaw", "--yesno", body).Run()
		if err == nil {
			return true, true
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, true
		}
		return false, false
	}
	return false, false
}

// silence the unused-import warning when strings is only needed by other builds.
var _ = strings.TrimSpace
