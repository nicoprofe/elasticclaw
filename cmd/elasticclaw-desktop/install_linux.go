//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Installation on Linux means a desktop entry and an icon, which together are what
// put the app in the activities overview and the application grid with a name and
// artwork rather than a generic gear. There is no registry and no uninstaller to
// register, so this is the whole of it.
//
// The binary itself is not copied. On Windows a downloaded exe sits in Downloads and
// has to be moved somewhere permanent; here it is normally already on PATH because
// scripts/install.sh put it there, so copying it would leave two divergent copies.

// The same 512x512 artwork the Windows .ico and the macOS .icns are generated from.
// It is embedded rather than read from disk because the desktop binary is shipped on
// its own: there is no install tree next to it to find a PNG in.
//
//go:embed appicon.png
var appIconPNG []byte

// iconName is what the desktop entry's Icon= key refers to. Freedesktop resolves it
// by searching the icon theme directories for <name>.png, which is why the file
// below has to be installed under a hicolor size directory and not just anywhere.
const iconName = "elasticclaw"

func dataHome() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

func desktopEntryPath() (string, error) {
	dir, err := dataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "applications", "elasticclaw.desktop"), nil
}

func iconPath() (string, error) {
	dir, err := dataHome()
	if err != nil {
		return "", err
	}
	// 512x512 matches the source artwork exactly. Filing it under the wrong size
	// directory makes the theme engine rescale it and the icon comes out blurry.
	return filepath.Join(dir, "icons", "hicolor", "512x512", "apps", iconName+".png"), nil
}

// maybeOfferInstall offers to add a desktop entry the first time the app is run
// without one. It returns false always: unlike Windows, nothing is re-executed, so
// the caller carries on and opens the window in this same process.
func maybeOfferInstall() bool {
	if installPromptSuppressed() {
		return false
	}
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
		"This adds a desktop entry and icon so ElasticClaw appears alongside your other\n"+
			"apps.\n\nNothing else on your system is modified.") {
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

	if err := installIcon(); err != nil {
		// A missing icon is not worth refusing the whole install over — the entry
		// still launches the app, it just looks generic. Say so and carry on.
		fmt.Fprintf(os.Stderr, "warning: could not install the icon: %v\n", err)
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
	//
	// StartupWMClass must match the window's WM_CLASS exactly or the running window
	// shows up as a second, unnamed icon in the dock instead of lighting up this
	// entry. window_linux.go sets the class to "ElasticClaw" for this reason; the
	// two values are a pair and changing one alone breaks the association.
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=ElasticClaw\n" +
		"Comment=Run and supervise coding agents\n" +
		"Exec=\"" + exe + "\"\n" +
		"Icon=" + iconName + "\n" +
		"Terminal=false\n" +
		"Categories=Development;\n" +
		"StartupWMClass=ElasticClaw\n"

	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	refreshDesktopCaches()
	fmt.Printf("Added desktop entry: %s\n", path)
	return nil
}

func installIcon() error {
	path, err := iconPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, appIconPNG, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// refreshDesktopCaches nudges the desktop into noticing the new files.
//
// GNOME and KDE both pick new entries up on their own eventually, but "eventually"
// can mean the next login — which reads as the install having done nothing. Both
// tools are optional and both are best-effort: a failure here changes nothing that
// matters by the next session.
func refreshDesktopCaches() {
	dir, err := dataHome()
	if err != nil {
		return
	}
	if bin, ok := lookPath("update-desktop-database"); ok {
		_ = exec.Command(bin, filepath.Join(dir, "applications")).Run()
	}
	if bin, ok := lookPath("gtk-update-icon-cache"); ok {
		_ = exec.Command(bin, "-q", "-t", "-f", filepath.Join(dir, "icons", "hicolor")).Run()
	}
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

	if icon, err := iconPath(); err == nil {
		if err := os.Remove(icon); err == nil {
			fmt.Printf("Removed icon: %s\n", icon)
		}
	}
	refreshDesktopCaches()
	fmt.Println("The elasticclaw-desktop binary itself was left in place.")
	return nil
}
