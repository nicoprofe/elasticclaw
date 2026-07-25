package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointExcludeRepositoryEnvironments(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".venv", "node_modules"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		var found os.DirEntry
		for _, entry := range entries {
			if entry.Name() == name {
				found = entry
				break
			}
		}
		if found == nil {
			t.Fatalf("did not find fixture %s", name)
		}
		if !checkpointExclude(path, found) {
			t.Fatalf("checkpointExclude(%q) = false, want true", name)
		}
	}
}
