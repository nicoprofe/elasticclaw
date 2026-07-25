//go:build !windows

package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
)

// On platforms without Explorer there is no GUI double-click to handle: the
// binary is always started from a shell.
func startedByExplorer() bool { return false }

// openAppWindow shows url in a chromeless window where a Chromium-based browser
// is available, so `elasticclaw desktop` behaves the same way it does on Windows.
func openAppWindow(url string) error {
	for _, candidate := range appModeBrowsers() {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "--app="+url, "--window-size=1440,900").Start(); err == nil {
			return nil
		}
	}

	// Fall back to the platform's default handler, which opens an ordinary tab.
	var fallback *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		fallback = exec.Command("open", url)
	default:
		fallback = exec.Command("xdg-open", url)
	}
	if err := fallback.Start(); err != nil {
		return fmt.Errorf("could not open a browser: %w", err)
	}
	return nil
}

func appModeBrowsers() []string {
	if runtime.GOOS == "darwin" {
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	}
	return []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"}
}
