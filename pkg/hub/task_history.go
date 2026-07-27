package hub

// Workspace task history.
//
// Runs are amnesiac by design — each gets a fresh sandbox and a clone of main —
// which has a consequence nobody warns the agent about: work from previous runs
// sits on unmerged branches, invisible in the checkout. An agent asked to "add
// another button" cannot see the five buttons already opened as pull requests, so
// it neither reuses their pattern nor knows the requests happened at all.
//
// The remedy is the same one that worked for repository structure: hand the next
// run a digest instead of the history. When a run's pull request is detected, one
// line is appended to memory/TASK_HISTORY.md in the workspace, which template
// delivery already ships into every sandbox. A digest, not transcripts: the agent
// pays context for every byte of this file, so it is capped to the most recent
// entries and each entry is one line.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	taskHistoryFile   = "TASK_HISTORY.md"
	taskHistoryHeader = "# Recent tasks in this workspace\n\n" +
		"One line per prior run, newest last. Their branches are usually NOT merged\n" +
		"into main, so their changes are not in your checkout — do not be surprised\n" +
		"by their absence, and follow their pattern when your task is similar.\n\n"
	// taskHistoryMaxEntries caps what every future agent has to read. Beyond
	// recent work the value drops fast, while the context cost never does.
	taskHistoryMaxEntries = 12
)

// appendWorkspaceTaskHistory records one completed hand-off in the workspace's
// memory. Failures are logged and swallowed: history is an accelerant, and it must
// never break the run that would have written it.
func (s *Server) appendWorkspaceTaskHistory(clawID, repo string, prNumber int, prURL string) {
	var workspace, name string
	err := s.db.QueryRow(`SELECT template, name FROM claws WHERE id=?`, clawID).Scan(&workspace, &name)
	if err != nil || strings.TrimSpace(workspace) == "" {
		return
	}

	task := strings.TrimSpace(name)
	if len(task) > 90 {
		task = task[:87] + "..."
	}
	entry := fmt.Sprintf("- %s — %q → %s#%d %s\n",
		time.Now().UTC().Format("2006-01-02"), task, repo, prNumber, prURL)

	if err := appendTaskHistoryEntry(filepath.Join(workspacesDir(), workspace, "memory"), entry); err != nil {
		log.Printf("[task-history] %s: %v", workspace, err)
		return
	}
	log.Printf("[task-history] recorded %s#%d for workspace %s", repo, prNumber, workspace)
}

// appendTaskHistoryEntry adds an entry, keeping the header and only the newest
// taskHistoryMaxEntries lines. Pure file mechanics, split out for testing.
func appendTaskHistoryEntry(memoryDir, entry string) error {
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(memoryDir, taskHistoryFile)

	var entries []string
	if existing, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			if strings.HasPrefix(line, "- ") {
				entries = append(entries, line+"\n")
			}
		}
	}
	entries = append(entries, entry)
	if len(entries) > taskHistoryMaxEntries {
		entries = entries[len(entries)-taskHistoryMaxEntries:]
	}
	return os.WriteFile(path, []byte(taskHistoryHeader+strings.Join(entries, "")), 0o644)
}
