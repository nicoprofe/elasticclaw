package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppBundleRoot(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		want string
	}{
		{
			name: "executable inside a bundle",
			exe:  "/Applications/ElasticClaw.app/Contents/MacOS/ElasticClaw",
			want: "/Applications/ElasticClaw.app",
		},
		{
			name: "bundle in a path with spaces",
			exe:  "/Users/ada/My Downloads/ElasticClaw.app/Contents/MacOS/ElasticClaw",
			want: "/Users/ada/My Downloads/ElasticClaw.app",
		},
		{
			name: "bare binary in Downloads",
			exe:  "/Users/ada/Downloads/elasticclaw-desktop-darwin-arm64",
			want: "",
		},
		{
			// Nothing under Resources may be treated as the bundle's executable:
			// installing from here would move an app based on a helper's location.
			name: "helper buried in Resources",
			exe:  "/Applications/ElasticClaw.app/Contents/Resources/helper/tool",
			want: "",
		},
		{
			name: "right depth but not an app bundle",
			exe:  "/opt/thing/Contents/MacOS/thing",
			want: "",
		},
		{
			name: "MacOS directory but no Contents",
			exe:  "/opt/ElasticClaw.app/MacOS/ElasticClaw",
			want: "",
		},
		{
			name: "empty path",
			exe:  "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := appBundleRoot(tc.exe); got != tc.want {
				t.Errorf("appBundleRoot(%q) = %q, want %q", tc.exe, got, tc.want)
			}
		})
	}
}

func TestStripLaunchServicesArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "a Finder launch looks like no arguments at all",
			args: []string{"-psn_0_774521"},
			want: []string{},
		},
		{
			name: "real arguments survive",
			args: []string{"--install"},
			want: []string{"--install"},
		},
		{
			name: "mixed",
			args: []string{"-psn_0_1", "--uninstall"},
			want: []string{"--uninstall"},
		},
		{
			// -psn_ is a prefix match, not an exact one, but it must not swallow
			// anything that merely starts similarly.
			name: "similar-looking flag is kept",
			args: []string{"-ps", "--psn_0_1"},
			want: []string{"-ps", "--psn_0_1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripLaunchServicesArgs(tc.args)
			if len(got) != len(tc.want) {
				t.Fatalf("stripLaunchServicesArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("stripLaunchServicesArgs(%q) = %q, want %q", tc.args, got, tc.want)
				}
			}
		})
	}
}

func TestInstallPromptSuppressed(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
	}

	for _, tc := range tests {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv("ELASTICCLAW_NO_INSTALL_PROMPT", tc.value)
			if got := installPromptSuppressed(); got != tc.want {
				t.Errorf("ELASTICCLAW_NO_INSTALL_PROMPT=%q: got %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestAppIconIsEmbeddable guards the embed the Linux install depends on: the icon is
// referenced by //go:embed in install_linux.go, so a rename or a deletion breaks the
// Linux build while leaving every other platform green.
func TestAppIconIsEmbeddable(t *testing.T) {
	info, err := os.Stat(filepath.Join(".", "appicon.png"))
	if err != nil {
		t.Fatalf("appicon.png must exist for the Linux desktop entry: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("appicon.png is empty")
	}
}
