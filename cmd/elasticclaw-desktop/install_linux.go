//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Installation on Linux means a desktop entry, which is what puts the app in the
// activities overview and the application grid. There is no registry and no
// uninstaller to register, so this is the whole of it.
//
// The binary itself is not copied. On Windows a downloaded exe sits in Downloads and
// has to be moved somewhere permanent; here it is normally already on PATH because
// scripts/install.sh put it there, so copying it would leave two divergent copies.

func desktopEntryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "applications", "elasticclaw.desktop"), nil
}

// maybeOfferInstall offers to add a desktop entry the first time the app is run
// without one. It returns false always: unlike Windows, nothing is re-executed, so
// the caller carries on and opens the window in this same process.
func maybeOfferInstall() bool {
	path, err := desktopEntryPath()
	if err != nil {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return false // already installed
	}
	// Only ask when there is a graphical session to ask in; a headless or
	// SSH invocation should not block on a dialog nobody can see.
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	if !askYesNo("Add ElasticClaw to your applications?",
		"This adds a desktop entry so ElasticClaw appears alongside your other apps.\n\n"+
			"Nothing else on your system is modified.") {
		return false
	}
	if err := runInstall(); err != nil {
		fatal("Could not add the desktop entry.\n\n" + err.Error())
	}
	return false
}

func runInstall() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate this executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	path, err := desktopEntryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	// Exec is quoted because the path may contain spaces, and a desktop entry with
	// an unquoted space silently fails to launch.
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=ElasticClaw\n" +
		"Comment=Run and supervise coding agents\n" +
		"Exec=\"" + exe + "\"\n" +
		"Icon=elasticclaw\n" +
		"Terminal=false\n" +
		"Categories=Development;\n" +
		"StartupWMClass=ElasticClaw\n"

	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("Added desktop entry: %s\n", path)
	return nil
}

func runUninstall() error {
	path, err := desktopEntryPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No desktop entry to remove.")
			return nil
		}
		return fmt.Errorf("remove %s: %w", path, err)
	}
	fmt.Printf("Removed desktop entry: %s\n", path)
	fmt.Println("The elasticclaw-desktop binary itself was left in place.")
	return nil
}
