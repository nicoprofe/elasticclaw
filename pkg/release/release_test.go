package release_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elasticclaw/elasticclaw/pkg/release"
)

func TestAssetNamePerPlatform(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:   "elasticclaw-linux-amd64",
		{"linux", "arm64"}:   "elasticclaw-linux-arm64",
		{"darwin", "amd64"}:  "elasticclaw-darwin-amd64",
		{"darwin", "arm64"}:  "elasticclaw-darwin-arm64",
		{"windows", "amd64"}: "elasticclaw-windows-amd64.exe",
		{"windows", "arm64"}: "elasticclaw-windows-arm64.exe",
	}
	for platform, want := range cases {
		got, err := release.AssetName(platform[0], platform[1])
		if err != nil {
			t.Fatalf("AssetName(%s, %s): unexpected error: %v", platform[0], platform[1], err)
		}
		if got != want {
			t.Errorf("AssetName(%s, %s) = %q, want %q", platform[0], platform[1], got, want)
		}
	}
}

func TestAssetNameRejectsUnsupportedPlatform(t *testing.T) {
	if _, err := release.AssetName("plan9", "386"); err == nil {
		t.Fatal("expected an error for an unsupported platform")
	}
}

func TestDownloadURLUsesDefaultRepo(t *testing.T) {
	t.Setenv(release.RepoEnv, "")
	url, err := release.DownloadURL("v1.2.3", "windows", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://github.com/elasticclaw/elasticclaw/releases/download/v1.2.3/elasticclaw-windows-amd64.exe"
	if url != want {
		t.Errorf("DownloadURL = %q, want %q", url, want)
	}
}

func TestRepoEnvOverride(t *testing.T) {
	t.Setenv(release.RepoEnv, "nicoprofe/elasticclaw")
	owner, repo := release.Repo()
	if owner != "nicoprofe" || repo != "elasticclaw" {
		t.Fatalf("Repo() = %s/%s, want nicoprofe/elasticclaw", owner, repo)
	}
	url, err := release.DownloadURL("v0.1.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(url, "nicoprofe/elasticclaw") {
		t.Errorf("override not reflected in URL: %s", url)
	}
}

// A malformed override must fall back to the default rather than producing a
// URL that points somewhere unintended.
func TestMalformedRepoEnvFallsBackToDefault(t *testing.T) {
	for _, bad := range []string{"notarepo", "a/b/c", "/", "owner/"} {
		t.Setenv(release.RepoEnv, bad)
		owner, repo := release.Repo()
		if owner+"/"+repo != release.DefaultRepo {
			t.Errorf("Repo() with %q = %s/%s, want fallback to %s", bad, owner, repo, release.DefaultRepo)
		}
	}
}

const testChecksums = `9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08  elasticclaw-linux-amd64
b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c  elasticclaw-windows-amd64.exe
`

func TestExpectedSHA256(t *testing.T) {
	got, err := release.ExpectedSHA256(testChecksums, "elasticclaw-windows-amd64.exe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c" {
		t.Errorf("wrong checksum returned: %s", got)
	}
}

// A checksum manifest must not be allowed to match an asset by prefix or
// substring — only an exact filename match counts.
func TestExpectedSHA256MissingAsset(t *testing.T) {
	for _, name := range []string{"elasticclaw-darwin-arm64", "elasticclaw-linux", "linux-amd64"} {
		if _, err := release.ExpectedSHA256(testChecksums, name); err == nil {
			t.Errorf("expected an error for asset %q not in the manifest", name)
		}
	}
}

func TestVerifyFileAcceptsMatchingContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	// sha256("test\n") — computed independently of the code under test.
	if err := os.WriteFile(path, []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, err := release.FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := sum + "  elasticclaw-linux-amd64\n"
	if err := release.VerifyFile(path, "elasticclaw-linux-amd64", manifest); err != nil {
		t.Fatalf("verification of a matching file failed: %v", err)
	}
}

func TestVerifyFileRejectsTamperedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	if err := os.WriteFile(path, []byte("malicious payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Manifest advertises the checksum of different content.
	manifest := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08  elasticclaw-linux-amd64\n"
	err := release.VerifyFile(path, "elasticclaw-linux-amd64", manifest)
	if err == nil {
		t.Fatal("tampered file passed verification")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

// GoReleaser and sha256sum binary mode prefix filenames with '*'.
func TestExpectedSHA256HandlesBinaryModePrefix(t *testing.T) {
	manifest := "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c *elasticclaw-windows-amd64.exe\n"
	if _, err := release.ExpectedSHA256(manifest, "elasticclaw-windows-amd64.exe"); err != nil {
		t.Fatalf("binary-mode manifest not parsed: %v", err)
	}
}
