// Package scripts_test exercises the release packaging scripts.
//
// package-macos-app.sh produces the only artifact Mac users install, and every way
// it can go wrong — a missing plist key, a lost executable bit, a bundle that is not
// a bundle — is invisible until someone double-clicks the app on a Mac. Running the
// script here, on any platform, catches the structural half of that in `go test`.
//
// What this cannot cover is the macOS-only half: lipo, codesign and ditto do not
// exist off a Mac. Those are asserted by the release workflow's "Check the app
// bundle is well formed" step, which runs on macos-14.
package scripts_test

import (
	"archive/zip"
	"encoding/binary"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPackageMacOSApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the packaging script needs a POSIX shell")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot, "scripts", "package-macos-app.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("packaging script missing: %v", err)
	}

	out := t.TempDir()

	// A stand-in for the real Mach-O binary. The script does not inspect its
	// contents, and building a genuine darwin desktop binary needs cgo against
	// WebKit — that is the release runner's job, not this test's.
	stub := filepath.Join(out, "elasticclaw-desktop-darwin-arm64")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"VERSION=v1.2.3-beta.4",
		"OUTPUT_DIR="+out,
	)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("packaging failed: %v\n%s", err, combined)
	}

	app := filepath.Join(out, "ElasticClaw.app")

	t.Run("bundle layout", func(t *testing.T) {
		// macOS recognises an application by this exact shape. Anything missing and
		// Finder shows a folder, not an app.
		for _, rel := range []string{
			"Contents/Info.plist",
			"Contents/PkgInfo",
			"Contents/MacOS/ElasticClaw",
			"Contents/Resources/ElasticClaw.icns",
		} {
			if _, err := os.Stat(filepath.Join(app, rel)); err != nil {
				t.Errorf("bundle is missing %s: %v", rel, err)
			}
		}
	})

	t.Run("executable bit", func(t *testing.T) {
		// The input stub is 0644 on purpose: this is the bit a browser download
		// loses, and the script has to set it rather than inherit it.
		info, err := os.Stat(filepath.Join(app, "Contents/MacOS/ElasticClaw"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("bundle executable is not executable: mode %v", info.Mode().Perm())
		}
	})

	t.Run("info plist", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(app, "Contents/Info.plist"))
		if err != nil {
			t.Fatal(err)
		}
		// Well-formed XML first: LaunchServices rejects the whole bundle over a
		// stray character, and a shell heredoc is exactly where one creeps in.
		if err := xml.Unmarshal(raw, new(struct {
			XMLName xml.Name `xml:"plist"`
		})); err != nil {
			t.Fatalf("Info.plist is not valid XML: %v", err)
		}

		plist := string(raw)
		required := map[string]string{
			"CFBundleExecutable":      "<string>ElasticClaw</string>",
			"CFBundleIdentifier":      "<string>ai.elasticclaw.desktop</string>",
			"CFBundleIconFile":        "<string>ElasticClaw</string>",
			"CFBundlePackageType":     "<string>APPL</string>",
			"NSHighResolutionCapable": "<true/>",
			"NSAllowsLocalNetworking": "<true/>",
		}
		for key, want := range required {
			if !strings.Contains(plist, "<key>"+key+"</key>") {
				t.Errorf("Info.plist has no %s", key)
				continue
			}
			if !strings.Contains(plist, want) {
				t.Errorf("Info.plist %s does not carry the expected value %s", key, want)
			}
		}

		// A tag with a prerelease suffix is not a valid CFBundleShortVersionString;
		// shipping one makes codesign refuse the bundle.
		if !strings.Contains(plist, "<key>CFBundleShortVersionString</key>\n\t<string>1.2.3</string>") {
			t.Errorf("CFBundleShortVersionString was not normalised to 1.2.3:\n%s", plist)
		}
		// ...but the full tag must survive somewhere, or a bug report cannot name
		// the build it came from.
		if !strings.Contains(plist, "v1.2.3-beta.4") {
			t.Error("the full version tag is not recorded anywhere in Info.plist")
		}
	})

	t.Run("icns", func(t *testing.T) {
		assertValidICNS(t, filepath.Join(app, "Contents/Resources/ElasticClaw.icns"))
	})

	// The verifier is what stands between a broken bundle and a release, so it is
	// itself worth running here: a verifier that silently passes everything is worse
	// than none at all. Its macOS-only checks skip on other platforms; the release
	// job runs the same script on macos-14 where none of them skip.
	t.Run("verifier accepts a good bundle", func(t *testing.T) {
		verify := exec.Command("bash", filepath.Join(repoRoot, "scripts", "verify-macos-app.sh"), out)
		verify.Dir = repoRoot
		if combined, err := verify.CombinedOutput(); err != nil {
			t.Fatalf("verify-macos-app.sh rejected a freshly built bundle: %v\n%s", err, combined)
		}
	})

	t.Run("verifier rejects a bundle whose executable bit was lost", func(t *testing.T) {
		// The exact regression that made the old macOS download uninstallable. If the
		// verifier does not catch this, it protects nothing.
		exe := filepath.Join(app, "Contents/MacOS/ElasticClaw")
		if err := os.Chmod(exe, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(exe, 0o755) })

		verify := exec.Command("bash", filepath.Join(repoRoot, "scripts", "verify-macos-app.sh"), out)
		verify.Dir = repoRoot
		combined, err := verify.CombinedOutput()
		if err == nil {
			t.Fatalf("verify-macos-app.sh passed a non-executable bundle:\n%s", combined)
		}
		if !strings.Contains(string(combined), "not executable") {
			t.Errorf("the failure does not name the problem:\n%s", combined)
		}
	})

	t.Run("zip contains the bundle", func(t *testing.T) {
		zipPath := filepath.Join(out, "ElasticClaw-macos.zip")
		r, err := zip.OpenReader(zipPath)
		if err != nil {
			t.Fatalf("open %s: %v", zipPath, err)
		}
		defer r.Close()

		// --keepParent: the app must unzip as ElasticClaw.app, not as loose Contents/
		// directories dumped wherever the user unzipped it.
		var foundExe bool
		for _, f := range r.File {
			if f.Name == "ElasticClaw.app/Contents/MacOS/ElasticClaw" {
				foundExe = true
				if f.Mode().Perm()&0o111 == 0 {
					t.Errorf("zipped executable lost its executable bit: mode %v", f.Mode().Perm())
				}
			}
		}
		if !foundExe {
			t.Error("the zip does not contain ElasticClaw.app/Contents/MacOS/ElasticClaw")
		}
	})
}

// assertValidICNS parses the icon container the way macOS does: a magic word, a
// total length that must match the file, then typed length-prefixed PNG entries. A
// truncated or mis-sized icns is silently ignored by the Dock, which is how an app
// ends up shipping with a blank icon and no error anywhere.
func assertValidICNS(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read icns: %v", err)
	}
	if len(data) < 8 {
		t.Fatalf("icns is too short: %d bytes", len(data))
	}
	if string(data[:4]) != "icns" {
		t.Fatalf("icns has the wrong magic: %q", data[:4])
	}
	if total := binary.BigEndian.Uint32(data[4:8]); int(total) != len(data) {
		t.Fatalf("icns header claims %d bytes, file is %d", total, len(data))
	}

	pngMagic := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	entries := 0
	for off := 8; off < len(data); {
		if off+8 > len(data) {
			t.Fatalf("icns entry header runs past the end of the file at offset %d", off)
		}
		size := int(binary.BigEndian.Uint32(data[off+4 : off+8]))
		if size < 8 || off+size > len(data) {
			t.Fatalf("icns entry at offset %d has an impossible size %d", off, size)
		}
		payload := data[off+8 : off+size]
		if len(payload) < len(pngMagic) || string(payload[:len(pngMagic)]) != string(pngMagic) {
			t.Fatalf("icns entry %q at offset %d is not a PNG", data[off:off+4], off)
		}
		entries++
		off += size
	}
	// Retina sizes included, there should be far more than a couple. One or two
	// means the generator silently produced a stub.
	if entries < 5 {
		t.Errorf("icns has only %d entries; the Dock will scale a low-resolution image", entries)
	}
}
