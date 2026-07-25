// Package release centralizes where elasticclaw looks for its own release
// artifacts, how those assets are named per platform, and how downloads are
// verified before they replace a running binary.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// DefaultRepo is the "owner/repo" that serves releases. It is a var, not a
// const, so a distribution can retarget it at link time without patching
// source:
//
//	go build -ldflags "-X github.com/elasticclaw/elasticclaw/pkg/release.DefaultRepo=myorg/myfork"
var DefaultRepo = "elasticclaw/elasticclaw"

// RepoEnv overrides DefaultRepo at runtime, for testing a release candidate or
// pointing a fleet at a fork without redistributing binaries.
const RepoEnv = "ELASTICCLAW_RELEASE_REPO"

// Repo returns the owner and repository serving releases.
func Repo() (owner, repo string) {
	spec := strings.TrimSpace(os.Getenv(RepoEnv))
	if spec == "" {
		spec = DefaultRepo
	}
	owner, repo, ok := splitRepo(spec)
	if !ok {
		// An unparseable override must not silently redirect downloads.
		owner, repo, _ = splitRepo(DefaultRepo)
	}
	return owner, repo
}

func splitRepo(spec string) (owner, repo string, ok bool) {
	parts := strings.Split(strings.Trim(spec, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// AssetName returns the release asset filename for a platform, e.g.
// "elasticclaw-windows-amd64.exe". These names are the contract between the
// release pipeline and the self-updater: changing one requires changing both.
func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "linux", "darwin":
		switch goarch {
		case "amd64", "arm64":
			return fmt.Sprintf("elasticclaw-%s-%s", goos, goarch), nil
		}
	case "windows":
		switch goarch {
		case "amd64", "arm64":
			return fmt.Sprintf("elasticclaw-windows-%s.exe", goarch), nil
		}
	}
	return "", fmt.Errorf("unsupported platform: %s/%s — download manually from %s", goos, goarch, ReleasesPageURL())
}

// DownloadURL returns the URL of the asset for a platform at a given version.
func DownloadURL(version, goos, goarch string) (string, error) {
	asset, err := AssetName(goos, goarch)
	if err != nil {
		return "", err
	}
	owner, repo := Repo()
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", owner, repo, version, asset), nil
}

// BridgeAssetName is the claw-bridge agent binary published with each release.
// Sandboxes are always linux/amd64.
const BridgeAssetName = "claw-bridge-linux-amd64"

// BridgeDownloadURL returns the URL sandboxes use to fetch claw-bridge.
func BridgeDownloadURL(version string) string {
	owner, repo := Repo()
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", owner, repo, version, BridgeAssetName)
}

// ChecksumsName is the manifest the release pipeline publishes alongside binaries.
const ChecksumsName = "checksums.txt"

// ChecksumsURL returns the URL of the checksum manifest for a version.
func ChecksumsURL(version string) string {
	owner, repo := Repo()
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", owner, repo, version, ChecksumsName)
}

// ReleasesPageURL returns the human-facing releases page, for error messages.
func ReleasesPageURL() string {
	owner, repo := Repo()
	return fmt.Sprintf("https://github.com/%s/%s/releases", owner, repo)
}

// ExpectedSHA256 finds the checksum recorded for assetName in a checksums.txt
// body. The format is one "<hex sha256>  <filename>" per line, as emitted by
// sha256sum and GoReleaser.
func ExpectedSHA256(checksums, assetName string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		// Binary-mode manifests prefix the filename with '*'.
		if strings.TrimPrefix(fields[1], "*") == assetName {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s in %s", assetName, ChecksumsName)
}

// FileSHA256 hashes a file on disk.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyFile confirms a downloaded file matches the checksum published for it.
// A mismatch means the artifact was corrupted or tampered with; callers must
// treat the error as fatal and discard the download.
func VerifyFile(path, assetName, checksums string) error {
	want, err := ExpectedSHA256(checksums, assetName)
	if err != nil {
		return err
	}
	got, err := FileSHA256(path)
	if err != nil {
		return fmt.Errorf("cannot hash download: %w", err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s:\n  expected %s\n  got      %s", assetName, want, got)
	}
	return nil
}
