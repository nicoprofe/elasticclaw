package release_test

import (
	"testing"

	"github.com/elasticclaw/elasticclaw/pkg/release"
)

func TestChannel(t *testing.T) {
	cases := map[string]string{
		"2026.7.24":        release.ChannelStable,
		"2026.7.24.1":      release.ChannelStable,
		"v1.2.0":           release.ChannelStable,
		"2026.7.24-beta.1": "beta",
		"2026.7.22-beta.3": "beta",
		"v1.2.0-rc.2":      "rc",
		"v1.2.0-alpha":     "alpha",
		"v1.2.0+build.5":   release.ChannelStable,
	}
	for version, want := range cases {
		if got := release.Channel(version); got != want {
			t.Errorf("Channel(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestCompareOrdersVersions(t *testing.T) {
	// Each pair is {lower, higher}.
	pairs := [][2]string{
		{"2026.7.22", "2026.7.23"},
		{"2026.7.23", "2026.7.23.1"},
		{"2026.7.23.1", "2026.7.24"},
		{"2026.7.9", "2026.7.10"},  // numeric, not lexicographic
		{"2026.7.24", "2026.10.1"}, // month compared as a number
		{"v1.2.0", "v1.10.0"},      // minor compared as a number
		{"1.2.0-beta.1", "1.2.0"},  // prerelease precedes its release
		{"1.2.0-beta.1", "1.2.0-beta.2"},
		{"1.2.0-beta.9", "1.2.0-beta.10"},
		{"2026.7.24-beta.1", "2026.7.25-beta.1"},
	}
	for _, p := range pairs {
		lower, higher := p[0], p[1]
		if got := release.Compare(lower, higher); got != -1 {
			t.Errorf("Compare(%q, %q) = %d, want -1", lower, higher, got)
		}
		if got := release.Compare(higher, lower); got != 1 {
			t.Errorf("Compare(%q, %q) = %d, want 1", higher, lower, got)
		}
	}
}

func TestCompareEquivalentVersions(t *testing.T) {
	pairs := [][2]string{
		{"2026.7.24", "2026.7.24"},
		{"1.2", "1.2.0"},           // a missing component counts as zero
		{"v1.2.0", "1.2.0"},        // the "v" prefix is not significant
		{"1.2.0+build.1", "1.2.0"}, // build metadata does not affect precedence
	}
	for _, p := range pairs {
		if got := release.Compare(p[0], p[1]); got != 0 {
			t.Errorf("Compare(%q, %q) = %d, want 0", p[0], p[1], got)
		}
	}
}

// The bug this logic replaced: the upgrade "track" was derived from the leading
// version components, so a client on 2026.7.22 could not see 2026.7.23.1 and was
// stranded on its build date forever. Channels must be version-independent.
func TestNewestCrossesVersionLines(t *testing.T) {
	tags := []string{"2026.7.24-beta.1", "2026.7.23.1", "2026.7.23", "2026.7.22-beta.3", "2026.7.22"}

	got := release.Newest("2026.7.22", tags)
	if got != "2026.7.23.1" {
		t.Errorf("stable client on 2026.7.22 resolved to %q, want 2026.7.23.1", got)
	}
}

func TestNewestKeepsChannelsSeparate(t *testing.T) {
	tags := []string{"2026.7.24-beta.1", "2026.7.23.1", "2026.7.23", "2026.7.22-beta.3"}

	// A stable client must never be moved onto a prerelease.
	if got := release.Newest("2026.7.23", tags); got != "2026.7.23.1" {
		t.Errorf("stable client resolved to %q, want 2026.7.23.1", got)
	}
	// A beta client tracks betas, and may cross version lines to do it.
	if got := release.Newest("2026.7.22-beta.3", tags); got != "2026.7.24-beta.1" {
		t.Errorf("beta client resolved to %q, want 2026.7.24-beta.1", got)
	}
}

func TestNewestIgnoresPublicationOrder(t *testing.T) {
	// GitHub lists releases newest-published first, which can disagree with
	// version order when a patch is backported after a later release.
	tags := []string{"2026.7.23", "2026.7.24", "2026.7.23.1"}
	if got := release.Newest("2026.7.23", tags); got != "2026.7.24" {
		t.Errorf("resolved to %q, want 2026.7.24", got)
	}
}

func TestNewestReturnsEmptyWhenChannelAbsent(t *testing.T) {
	tags := []string{"2026.7.24-beta.1", "2026.7.22-beta.3"}
	if got := release.Newest("2026.7.23", tags); got != "" {
		t.Errorf("stable client with only betas available resolved to %q, want \"\"", got)
	}
}

func TestNewestIgnoresBlankTags(t *testing.T) {
	if got := release.Newest("1.0.0", []string{"", "  ", "1.0.1"}); got != "1.0.1" {
		t.Errorf("resolved to %q, want 1.0.1", got)
	}
}

// A client built from a tag newer than anything published must not be rolled
// backward; runUpgrade relies on Compare for this.
func TestCompareGuardsAgainstDowngrade(t *testing.T) {
	latest := release.Newest("2026.7.25", []string{"2026.7.24", "2026.7.23"})
	if latest != "2026.7.24" {
		t.Fatalf("Newest returned %q", latest)
	}
	if release.Compare(latest, "2026.7.25") > 0 {
		t.Error("Compare reports an older release as newer, which would downgrade the client")
	}
}
