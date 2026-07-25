package hub

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openInDefaultBrowser opens a URL in the browser the user already uses.
//
// The hub runs on the user's own machine, so it can do this for them. The
// alternative — showing a link to copy into a browser — is not automation, and
// asking them to sign in to GitHub inside the app's embedded WebView would mean
// a second GitHub login in a window that looks nothing like a browser. Handing
// the URL to the system browser reuses the GitHub session they already have.
func openInDefaultBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// rundll32 needs no shell quoting and opens no extra console window.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open a browser: %w", err)
	}
	// Reap the child rather than leaving a zombie; the browser itself outlives it.
	go func() { _ = cmd.Wait() }()
	return nil
}
