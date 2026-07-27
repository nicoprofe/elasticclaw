package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The digest exists so an agent asked for "another button" knows the previous
// buttons happened even though their branches are unmerged and invisible in its
// fresh checkout of main.
func TestTaskHistoryAccumulatesEntries(t *testing.T) {
	dir := t.TempDir()
	for _, e := range []string{
		"- 2026-07-26 — \"add button A\" → nicoprofe/agent-race#7 https://github.com/x/7\n",
		"- 2026-07-26 — \"add button B\" → nicoprofe/agent-race#8 https://github.com/x/8\n",
	} {
		if err := appendTaskHistoryEntry(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(filepath.Join(dir, taskHistoryFile))
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !strings.Contains(s, "#7") || !strings.Contains(s, "#8") {
		t.Errorf("entries missing:\n%s", s)
	}
	// The header carries the one fact agents get wrong without it.
	if !strings.Contains(s, "NOT merged") {
		t.Error("header must warn that prior branches are unmerged")
	}
}

// Context is paid for on every future run, so the file must not grow forever.
func TestTaskHistoryIsCapped(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < taskHistoryMaxEntries+5; i++ {
		e := "- 2026-07-26 — \"task\" → repo#" + string(rune('0'+i%10)) + " url" + strings.Repeat("x", i) + "\n"
		if err := appendTaskHistoryEntry(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	content, _ := os.ReadFile(filepath.Join(dir, taskHistoryFile))
	got := strings.Count(string(content), "\n- ") + boolToInt(strings.HasPrefix(string(content), "- "))
	if got != taskHistoryMaxEntries {
		t.Errorf("got %d entries, want exactly %d — oldest must fall off", got, taskHistoryMaxEntries)
	}
	// Newest survives, oldest is gone.
	if !strings.Contains(string(content), strings.Repeat("x", taskHistoryMaxEntries+4)) {
		t.Error("newest entry missing")
	}
	if strings.Contains(string(content), "url\n") {
		t.Error("oldest entry still present")
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
