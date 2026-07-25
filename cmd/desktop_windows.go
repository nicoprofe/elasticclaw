//go:build windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/inconshreveable/mousetrap"
	"github.com/spf13/cobra"
)

func init() {
	// Cobra's default for a Windows binary launched from Explorer is to print
	// "This is a command line tool. You need to open cmd.exe and run it from
	// there." and quit — a dead end for anyone who downloaded the exe and
	// double-clicked it. Suppress it; Execute() routes GUI launches to the
	// desktop command instead.
	cobra.MousetrapHelpText = ""
}

// startedByExplorer reports whether this process was launched by double-clicking
// in Explorer rather than from an existing terminal.
func startedByExplorer() bool {
	return mousetrap.StartedByExplorer()
}

// openAppWindow shows url in a chromeless browser window so the dashboard reads
// as an application rather than a tab: no address bar, no tab strip, its own
// taskbar entry. Edge ships with Windows 10 and 11, so the first candidate is
// almost always present; the default browser is the last resort.
func openAppWindow(url string) error {
	for _, exe := range appModeBrowsers() {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		// --app= is supported by every Chromium-based browser and is what makes
		// the window chromeless.
		cmd := exec.Command(exe, "--app="+url, "--window-size=1440,900")
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	// No Chromium browser found — fall back to the default handler, which opens
	// an ordinary tab. Better a tab than nothing.
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		return fmt.Errorf("could not open a browser: %w", err)
	}
	return nil
}

// appModeBrowsers lists Chromium-based browsers in preference order, expanded
// against this machine's actual program directories.
func appModeBrowsers() []string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}

	rel := []string{
		filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
	}

	var out []string
	for _, r := range rel {
		for _, root := range roots {
			out = append(out, filepath.Join(root, r))
		}
	}
	return out
}
