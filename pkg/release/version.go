package release

import (
	"strconv"
	"strings"
)

// Release channels. A client only ever upgrades within its own channel, so a
// stable install is never moved onto a prerelease build.
const (
	ChannelStable = "stable"
)

// Channel reports which release channel a version belongs to: ChannelStable for
// a plain version such as "2026.7.24" or "v1.2.0", or the prerelease label for a
// tag such as "2026.7.24-beta.1" (-> "beta") and "v1.2.0-rc.2" (-> "rc").
//
// Channels are deliberately independent of the version numbers themselves. An
// earlier implementation derived the "track" from the leading version
// components, which meant a client could only ever move within its own version
// line — under date-based tags that stranded every install on the day it was
// built.
func Channel(version string) string {
	_, pre := splitVersion(version)
	if pre == "" {
		return ChannelStable
	}
	// "beta.1" -> "beta"; a bare "beta" is its own channel.
	if idx := strings.Index(pre, "."); idx != -1 {
		return pre[:idx]
	}
	return pre
}

// splitVersion separates the numeric portion of a version from its prerelease
// suffix, tolerating an optional leading "v" and build metadata after "+".
func splitVersion(version string) (base, pre string) {
	v := strings.TrimSpace(version)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.Index(v, "+"); idx != -1 {
		v = v[:idx] // build metadata does not affect precedence
	}
	if idx := strings.Index(v, "-"); idx != -1 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

// Compare orders two version strings, returning -1 if a sorts before b, +1 if
// after, and 0 if they are equivalent. Numeric components are compared as
// numbers so "2026.7.9" sorts before "2026.7.10", and a shorter version sorts
// before its own patch releases ("2026.7.24" < "2026.7.24.1"). A prerelease
// sorts before the release it precedes ("1.2.0-beta.1" < "1.2.0").
func Compare(a, b string) int {
	aBase, aPre := splitVersion(a)
	bBase, bPre := splitVersion(b)

	if c := compareNumericParts(aBase, bBase); c != 0 {
		return c
	}

	// Equal numeric bases: a prerelease precedes the final release.
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	}

	aLabel, aNum := splitPrerelease(aPre)
	bLabel, bNum := splitPrerelease(bPre)
	if aLabel != bLabel {
		return strings.Compare(aLabel, bLabel)
	}
	switch {
	case aNum < bNum:
		return -1
	case aNum > bNum:
		return 1
	}
	return 0
}

func compareNumericParts(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		// A missing component counts as zero, so "1.2" and "1.2.0" are equal
		// while "1.2.1" is greater than both.
		av, bv := partAt(aParts, i), partAt(bParts, i)
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func partAt(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0 // non-numeric components do not participate in ordering
	}
	return n
}

func splitPrerelease(pre string) (label string, num int) {
	if idx := strings.Index(pre, "."); idx != -1 {
		n, err := strconv.Atoi(pre[idx+1:])
		if err != nil {
			return pre, 0
		}
		return pre[:idx], n
	}
	return pre, 0
}

// Newest returns the highest-sorting tag that shares current's channel, or ""
// if the list contains none. The result may equal current, which callers read as
// "already up to date".
func Newest(current string, tags []string) string {
	channel := Channel(current)
	best := ""
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || Channel(tag) != channel {
			continue
		}
		if best == "" || Compare(tag, best) > 0 {
			best = tag
		}
	}
	return best
}
