//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Installation on macOS means one thing: the .app bundle lives in an Applications
// folder instead of Downloads.
//
// The release ships ElasticClaw.app inside a zip, so by the time this code runs the
// user has an unzipped bundle wherever their browser put it. It works from there,
// but it does not appear in Launchpad or survive a Downloads clear-out, and the copy
// in Downloads keeps its quarantine flag so every launch is a Gatekeeper prompt.
// Moving it once fixes all three.
//
// There is no separate uninstaller to register: on macOS deleting the bundle is the
// uninstall, and runUninstall exists only so `--uninstall` does something sensible.

const (
	appBundleName      = "ElasticClaw.app"
	systemApplications = "/Applications"
)

// applicationsDirs lists the two places macOS indexes for Launchpad, in preference
// order. A bundle already in either one is installed as far as this code cares.
func applicationsDirs() []string {
	dirs := []string{systemApplications}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// installDir picks where the bundle belongs.
//
// /Applications is the expected home and is writable by admin users, which is most
// single-user Macs. When it is not — a managed or non-admin machine — ~/Applications
// is a real, Launchpad-indexed location that needs no password, and is a better
// outcome than failing with an authorization prompt the user cannot satisfy.
func installDir() (string, error) {
	if isWritableDir(systemApplications) {
		return systemApplications, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

func isWritableDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	// Permission bits alone do not answer this on macOS, where ACLs and SIP both
	// have a say. Actually creating something is the only reliable test.
	probe, err := os.CreateTemp(path, ".elasticclaw-write-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return true
}

// alreadyInstalled reports whether bundle is already in an Applications folder, in
// which case there is nothing to offer.
func alreadyInstalled(bundle string) bool {
	for _, dir := range applicationsDirs() {
		if strings.HasPrefix(bundle, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// maybeOfferInstall offers to move the bundle to Applications on first launch, and
// reports whether the installed copy took over.
//
// Returning true means this process must exit: the relaunched copy is now serving
// the user, and two instances would fight over the same port and the same database.
func maybeOfferInstall() bool {
	if installPromptSuppressed() {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return false
	}
	bundle := appBundleRoot(exe)
	if bundle == "" {
		// A bare executable, not a bundle. Nothing here can turn it into an app, so
		// run in place rather than pretending otherwise.
		return false
	}
	if alreadyInstalled(bundle) {
		return false
	}
	if !askYesNo("Move ElasticClaw to your Applications folder?",
		"ElasticClaw is running from "+filepath.Dir(bundle)+".\n\n"+
			"Moving it there makes it appear in Launchpad and lets it keep working after\n"+
			"you clear out your downloads. Nothing else on your Mac is modified.") {
		return false
	}

	target, err := installBundle(bundle)
	if err != nil {
		fatal("Could not move ElasticClaw to Applications.\n\n" + err.Error())
		return false
	}
	// -n forces a new instance: without it, LaunchServices can decide the already
	// running copy (this process, about to exit) satisfies the request and nothing
	// visible happens at all.
	if err := exec.Command("open", "-n", target).Run(); err != nil {
		fatal("ElasticClaw was installed to " + target + " but could not be started.\n\n" + err.Error())
		return false
	}
	return true
}

// installBundle copies the bundle into Applications, replacing any previous copy,
// and returns where it landed.
func installBundle(bundle string) (string, error) {
	dir, err := installDir()
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, appBundleName)

	if same, _ := sameFile(bundle, target); same {
		return target, nil
	}

	// Copy beside the target and swap, rather than deleting first and copying into
	// the gap. A copy that fails halfway through — no disk, no permission, a yanked
	// USB stick — would otherwise leave the user with neither the old app nor a new
	// one. The rename is within one directory, so it is atomic.
	staged := target + ".new"
	if err := os.RemoveAll(staged); err != nil {
		return "", fmt.Errorf("clear %s: %w", staged, err)
	}

	// ditto rather than a Go file walk: it is the only copy on macOS that preserves
	// the code signature's extended attributes. A bundle copied without them fails
	// its signature check and macOS refuses to launch it.
	if out, err := exec.Command("ditto", bundle, staged).CombinedOutput(); err != nil {
		os.RemoveAll(staged)
		return "", fmt.Errorf("copy to %s: %w: %s", staged, err, strings.TrimSpace(string(out)))
	}

	// Replacing rather than merging: an old bundle left in place would keep files
	// this version no longer ships, and a half-old app is harder to reason about
	// than a clean one.
	if err := os.RemoveAll(target); err != nil {
		os.RemoveAll(staged)
		return "", fmt.Errorf("remove the previous %s: %w", target, err)
	}
	if err := os.Rename(staged, target); err != nil {
		return "", fmt.Errorf("move %s into place: %w", staged, err)
	}

	// The download is quarantined, and the copy inherits it. Clearing it is the same
	// decision the user just made by confirming this move — and by opening the app in
	// the first place, which already required a Gatekeeper confirmation. Leaving it
	// set would re-prompt on every launch of a copy the user explicitly installed.
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", target).Run()

	return target, nil
}

func sameFile(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

func runInstall() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate this executable: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return fmt.Errorf("resolve this executable: %w", err)
	}
	bundle := appBundleRoot(exe)
	if bundle == "" {
		return errors.New("this is a bare executable, not an application bundle.\n\n" +
			"Only ElasticClaw.app can be installed. Download ElasticClaw-macos.zip from\n" +
			"the releases page, or install the command-line hub instead:\n" +
			"  curl -fsSL https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.sh | sh")
	}
	target, err := installBundle(bundle)
	if err != nil {
		return err
	}
	fmt.Printf("Installed: %s\n", target)
	return nil
}

func runUninstall() error {
	removed := false
	for _, dir := range applicationsDirs() {
		target := filepath.Join(dir, appBundleName)
		if _, err := os.Stat(target); err != nil {
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove %s: %w", target, err)
		}
		fmt.Printf("Removed: %s\n", target)
		removed = true
	}
	if !removed {
		fmt.Println("ElasticClaw is not installed in Applications; nothing to remove.")
	}
	fmt.Println("Your data in ~/.elasticclaw was left in place.")
	return nil
}
