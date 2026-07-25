package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/elasticclaw/elasticclaw/pkg/release"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade elasticclaw to the latest release on the current track",
	Long: `Upgrades the elasticclaw binary to the latest release on the same track.

Stable clients upgrade to the latest stable release; prerelease clients
(e.g. beta, rc) upgrade to the latest release on the same prerelease track.
Cross-track jumps (beta → stable) are prevented.

Downloads are verified against the release's checksums.txt before the running
binary is replaced. Set ELASTICCLAW_RELEASE_REPO=owner/repo to upgrade from a
fork or a release candidate repository.`,
	RunE: runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

// upgradeHTTPClient bounds every network call the updater makes so a hung
// connection cannot leave the command waiting forever.
var upgradeHTTPClient = &http.Client{Timeout: 5 * time.Minute}

func runUpgrade(cmd *cobra.Command, args []string) error {
	fmt.Println("Checking for updates...")

	current := Version
	if current == "dev" {
		return fmt.Errorf("cannot upgrade a dev build — download a release from %s", release.ReleasesPageURL())
	}

	owner, repo := release.Repo()

	// Find the latest release on the same track (stable→stable, beta→beta, etc.)
	latest, err := latestReleaseOnTrack(owner, repo, current)
	if err != nil {
		return fmt.Errorf("no releases found on track %s: %w", extractTrack(current), err)
	}

	// Only ever move forward: a client built from a tag newer than anything
	// published must not be rolled backward.
	if release.Compare(latest, current) <= 0 {
		fmt.Printf("Already up to date (%s)\n", current)
		return nil
	}

	fmt.Printf("Upgrading %s → %s\n", current, latest)

	assetName, err := release.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	downloadURL, err := release.DownloadURL(latest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	// Determine current binary path
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine current binary path: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return fmt.Errorf("cannot resolve symlink: %w", err)
	}

	// Clear any backup left by a previous upgrade on this platform.
	cleanupPreviousBinary(self)

	// Fetch the checksum manifest before spending time on the binary, so a
	// release that was published without one fails fast.
	checksums, err := fetchText(release.ChecksumsURL(latest))
	if err != nil {
		return fmt.Errorf("cannot fetch %s for %s: %w", release.ChecksumsName, latest, err)
	}

	fmt.Printf("Downloading %s...\n", downloadURL)

	// Download to a temp file next to the current binary
	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, ".elasticclaw-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { os.Remove(tmpPath) }() // clean up on failure

	resp, err := upgradeHTTPClient.Get(downloadURL)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write failed: %w", err)
	}
	tmp.Close()

	// Never execute an unverified binary.
	if err := release.VerifyFile(tmpPath, assetName, checksums); err != nil {
		return fmt.Errorf("refusing to install %s: %w", latest, err)
	}
	fmt.Println("✓ Checksum verified")

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	if err := replaceRunningBinary(tmpPath, self); err != nil {
		return err
	}

	fmt.Printf("✓ Upgraded to %s\n", latest)

	// Restart hub service if it's running
	restartHub()

	return nil
}

// fetchText retrieves a small text resource such as the checksum manifest.
func fetchText(url string) (string, error) {
	resp, err := upgradeHTTPClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// The manifest is a few hundred bytes; cap the read so a wrong URL cannot
	// stream something unbounded into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// backupSuffix names the displaced binary during a Windows upgrade.
const backupSuffix = ".old"

// replaceRunningBinary swaps the downloaded binary into the path of the one
// currently executing. On Unix a rename over a running binary is legal. Windows
// holds an image lock that forbids overwriting it, but renaming it is allowed —
// so move it aside first and restore it if the swap fails.
func replaceRunningBinary(tmpPath, self string) error {
	if runtime.GOOS != "windows" {
		if err := os.Rename(tmpPath, self); err != nil {
			return fmt.Errorf("failed to replace binary (try sudo): %w", err)
		}
		return nil
	}

	backup := self + backupSuffix
	_ = os.Remove(backup)
	if err := os.Rename(self, backup); err != nil {
		return fmt.Errorf("failed to move current binary aside (close running elasticclaw processes and retry): %w", err)
	}
	if err := os.Rename(tmpPath, self); err != nil {
		// Put the working binary back rather than leaving nothing at self.
		if restoreErr := os.Rename(backup, self); restoreErr != nil {
			return fmt.Errorf("failed to install new binary (%w) and could not restore the previous one from %s (%v) — restore it manually", err, backup, restoreErr)
		}
		return fmt.Errorf("failed to install new binary, previous version restored: %w", err)
	}
	// The displaced image may still be locked by this very process; it is
	// removed on the next run.
	if err := os.Remove(backup); err != nil {
		fmt.Printf("  Note: previous version left at %s (removed on next upgrade)\n", backup)
	}
	return nil
}

// cleanupPreviousBinary removes a backup a prior Windows upgrade could not
// delete while it was still mapped into memory.
func cleanupPreviousBinary(self string) {
	if runtime.GOOS != "windows" {
		return
	}
	_ = os.Remove(self + backupSuffix)
}

// restartHub attempts to restart the hub systemd service if it's running.
// Non-fatal — we just print a message either way.
func restartHub() {
	if runtime.GOOS != "linux" {
		return // systemd-managed hub service is Linux-only
	}
	out, err := exec.Command("systemctl", "is-active", "elasticclaw").Output()
	if err != nil {
		return // systemctl not available or service not found
	}
	if strings.TrimSpace(string(out)) != "active" {
		return
	}
	fmt.Println("Restarting hub service...")
	if err := exec.Command("systemctl", "restart", "elasticclaw").Run(); err != nil {
		fmt.Printf("  Warning: could not restart service: %v\n", err)
		fmt.Println("  Run: sudo systemctl restart elasticclaw")
		return
	}
	fmt.Println("✓ Hub service restarted")
}
