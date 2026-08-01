//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Windows installation.
//
// Copying an exe into a folder is not an installed application: there is no Start
// menu entry, no shortcut, nothing in Add or Remove Programs and no way to
// uninstall. This performs a real per-user install, which needs no administrator
// rights and no separate installer toolchain — the same binary installs itself.
//
// Per-user (HKCU + LOCALAPPDATA) rather than machine-wide is deliberate: it keeps
// the install unprivileged, which matters for an unsigned binary that already has
// to get past SmartScreen.

const (
	appDisplayName = "ElasticClaw"
	appExeName     = "elasticclaw-desktop.exe"
	// uninstallKey is where Windows looks to populate Add or Remove Programs.
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ElasticClaw`
)

// maybeOfferInstall runs when the app is started with no arguments — which is
// what double-clicking does — from somewhere other than its install directory,
// typically the Downloads folder.
//
// Without this, downloading the exe and double-clicking it only ever launches the
// app: --install is a flag, and a double-click passes no flags, so the install
// path was unreachable for anyone who did not open a terminal. The app would run
// but never actually be installed.
//
// Returns true when the caller should exit because the installed copy was
// launched in its place.
func maybeOfferInstall() bool {
	if installPromptSuppressed() {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	self, _ = filepath.EvalSymlinks(self)

	dir, err := installDir()
	if err != nil {
		return false
	}
	// Already running from the install directory: nothing to do.
	if strings.HasPrefix(strings.ToLower(self), strings.ToLower(dir)) {
		return false
	}

	if !askYesNo("Install ElasticClaw",
		"Install ElasticClaw for your user account?\n\n"+
			"It will be added to your Start menu and Desktop, and listed in Add or Remove "+
			"Programs so you can uninstall it later. No administrator rights are needed.\n\n"+
			"Choose No to just run it this once without installing.") {
		return false
	}

	if err := runInstall(); err != nil {
		// Installing is a convenience; a failure should not stop the app running.
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		messageBox("ElasticClaw", "Could not install:\n\n"+err.Error()+
			"\n\nElasticClaw will run from its current location instead.")
		return false
	}

	// Hand over to the installed copy, so the running process is the one the
	// Start menu shortcut and the uninstaller point at.
	target := filepath.Join(dir, appExeName)
	cmd := exec.Command(target)
	if err := cmd.Start(); err != nil {
		// Installed, but could not hand over: keep running from here.
		return false
	}
	return true
}

func installDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", fmt.Errorf("LOCALAPPDATA is not set")
	}
	return filepath.Join(base, "Programs", "ElasticClaw"), nil
}

func startMenuShortcut() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("APPDATA is not set")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", appDisplayName+".lnk"), nil
}

func desktopShortcut() (string, error) {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		return "", fmt.Errorf("USERPROFILE is not set")
	}
	return filepath.Join(home, "Desktop", appDisplayName+".lnk"), nil
}

// runInstall copies this executable into the per-user programs directory, creates
// shortcuts, and registers the app so it appears in Add or Remove Programs.
func runInstall() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate this executable: %w", err)
	}
	self, _ = filepath.EvalSymlinks(self)

	dir, err := installDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	target := filepath.Join(dir, appExeName)

	// Running the installer from the installed location is a no-op copy, and
	// copying a file onto itself would truncate it.
	if !strings.EqualFold(self, target) {
		if err := copyFile(self, target); err != nil {
			return fmt.Errorf("copy into %s: %w", dir, err)
		}
	}

	if err := writeShortcuts(target, dir); err != nil {
		return err
	}
	if err := registerUninstall(target, dir); err != nil {
		return err
	}
	fmt.Printf("Installed %s to %s\n", appDisplayName, dir)
	fmt.Println("It is now in your Start menu and on your Desktop, and listed in Add or Remove Programs.")
	return nil
}

// writeShortcuts creates the Start menu and Desktop shortcuts.
//
// Shortcuts are .lnk files, which are a COM structure rather than anything a
// program can simply write. Driving WScript.Shell through PowerShell is the
// pragmatic way to create one without pulling COM bindings into a cross-compiled
// binary, and PowerShell is present on every supported Windows version.
func writeShortcuts(target, workDir string) error {
	locations := map[string]func() (string, error){
		"Start menu": startMenuShortcut,
		"Desktop":    desktopShortcut,
	}
	for label, resolve := range locations {
		path, err := resolve()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s folder: %w", label, err)
		}
		script := fmt.Sprintf(
			`$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%s); `+
				`$s.TargetPath=%s; $s.WorkingDirectory=%s; $s.IconLocation=%s; `+
				`$s.Description='ElasticClaw'; $s.Save()`,
			psQuote(path), psQuote(target), psQuote(workDir), psQuote(target))
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create %s shortcut: %w: %s", label, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// psQuote renders a Go string as a PowerShell single-quoted literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// registerUninstall writes the keys Add or Remove Programs reads. Without these
// the app is installed but invisible to Windows, and cannot be removed the way
// users expect.
func registerUninstall(target, dir string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("register in Add or Remove Programs: %w", err)
	}
	defer key.Close()

	size := uint64(0)
	if info, statErr := os.Stat(target); statErr == nil {
		size = uint64(info.Size() / 1024) // Windows expects kilobytes
	}

	values := []struct {
		name string
		val  string
	}{
		{"DisplayName", appDisplayName},
		{"DisplayVersion", Version},
		{"Publisher", "ElasticClaw"},
		{"DisplayIcon", target},
		{"InstallLocation", dir},
		{"UninstallString", fmt.Sprintf(`"%s" --uninstall`, target)},
		{"QuietUninstallString", fmt.Sprintf(`"%s" --uninstall`, target)},
	}
	for _, v := range values {
		if err := key.SetStringValue(v.name, v.val); err != nil {
			return fmt.Errorf("write %s: %w", v.name, err)
		}
	}
	// Signals there is nothing to modify or repair, so Windows offers only Uninstall.
	_ = key.SetDWordValue("NoModify", 1)
	_ = key.SetDWordValue("NoRepair", 1)
	if size > 0 {
		_ = key.SetDWordValue("EstimatedSize", uint32(size))
	}
	return nil
}

// runUninstall removes shortcuts and registration. The executable itself cannot
// delete the copy that is currently running, so it schedules that separately.
func runUninstall() error {
	for _, resolve := range []func() (string, error){startMenuShortcut, desktopShortcut} {
		if path, err := resolve(); err == nil {
			_ = os.Remove(path)
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, uninstallKey); err != nil && !strings.Contains(err.Error(), "cannot find") {
		fmt.Fprintf(os.Stderr, "warning: could not remove the Add or Remove Programs entry: %v\n", err)
	}

	dir, err := installDir()
	if err != nil {
		return err
	}
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)

	// A running image cannot delete itself, so hand the deletion to a short-lived
	// shell that waits for this process to exit first.
	if strings.HasPrefix(strings.ToLower(self), strings.ToLower(dir)) {
		script := fmt.Sprintf(
			`Start-Sleep -Seconds 2; Remove-Item -LiteralPath %s -Recurse -Force -ErrorAction SilentlyContinue`,
			psQuote(dir))
		_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script).Start()
		fmt.Println("Uninstalled ElasticClaw. Removing the remaining files…")
		return nil
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	fmt.Println("Uninstalled ElasticClaw.")
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Replacing a running exe fails, but renaming it aside is allowed; the stale
	// copy is cleaned up on the next install.
	if _, err := os.Stat(dst); err == nil {
		old := dst + ".old"
		_ = os.Remove(old)
		if err := os.Rename(dst, old); err != nil {
			return fmt.Errorf("close ElasticClaw and try again: %w", err)
		}
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	_ = os.Remove(dst + ".old")
	return nil
}
