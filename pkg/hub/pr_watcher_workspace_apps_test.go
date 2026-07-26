package hub

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elasticclaw/elasticclaw/pkg/types"
)

// A hub set up through the GitHub App flow keeps its Apps per workspace, not in
// hub.yaml. The PR watcher read hub.yaml alone, so on such a hub it resolved no
// token, and an empty token aborts the whole poll: PR #7 was opened and then never
// watched again — no CI relay, no review comments, and no pr_merged transition, so
// the pipeline could not leave review.
func TestGitHubAppsForTokenResolutionIncludesWorkspaceApps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows

	writeWorkspaceWithApp(t, home, "agent-race", 4317485)

	s := &Server{hubCfg: &types.HubConfig{}} // nothing in hub.yaml, as after the setup flow
	s.workspaceGitHubApps = s.loadWorkspaceGitHubAppsFromDisk

	apps := s.githubAppsForTokenResolution()
	if len(apps) == 0 {
		t.Fatal("no GitHub Apps found; the watcher would treat this hub as unconfigured and stop polling every PR")
	}
	if apps[0].AppID != 4317485 {
		t.Errorf("app id = %d, want the workspace's app 4317485", apps[0].AppID)
	}
}

// Hub-level Apps must keep working, and must be tried first.
func TestHubLevelAppsAreStillPreferred(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	writeWorkspaceWithApp(t, home, "agent-race", 222)

	s := &Server{hubCfg: &types.HubConfig{
		GitHubApps: []*types.GitHubAppConfig{{AppID: 111, PrivateKeyPEM: "pem"}},
	}}
	s.workspaceGitHubApps = s.loadWorkspaceGitHubAppsFromDisk

	apps := s.githubAppsForTokenResolution()
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want the hub app and the workspace app", len(apps))
	}
	if apps[0].AppID != 111 {
		t.Errorf("first app = %d, want the hub-level app 111 tried first", apps[0].AppID)
	}
}

// One App attached to several workspaces must not be asked for a token repeatedly:
// each attempt is a network round trip on every poll.
func TestSharedAppIsNotDuplicated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	writeWorkspaceWithApp(t, home, "agent-race", 999)
	writeWorkspaceWithApp(t, home, "sura-web-app", 999)

	s := &Server{hubCfg: &types.HubConfig{}}
	s.workspaceGitHubApps = s.loadWorkspaceGitHubAppsFromDisk

	apps := s.githubAppsForTokenResolution()
	if len(apps) != 1 {
		t.Fatalf("got %d apps, want 1 — the same app_id appeared in two workspaces", len(apps))
	}
}

func writeWorkspaceWithApp(t *testing.T, home, workspace string, appID int64) {
	t.Helper()
	dir := filepath.Join(home, ".elasticclaw", "workspaces", workspace)
	if err := os.MkdirAll(filepath.Join(dir, ".elasticclaw-managed"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "schema_version: v1\nname: " + workspace + "\nrepositories: []\nenv: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "elasticclaw-config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	apps := "github_apps:\n    " + workspace + "-app:\n        app_id: " +
		itoa(appID) + "\n        private_key_pem: |\n            not-a-real-key\n"
	if err := os.WriteFile(filepath.Join(dir, ".elasticclaw-managed", "github_apps.yaml"), []byte(apps), 0o600); err != nil {
		t.Fatal(err)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

// A Server constructed without the loader must read nothing from disk. The tests
// in this package build Server literals directly, and an unconditional $HOME read
// made them pick up the developer's own GitHub App and call the real API with it.
func TestNoLoaderMeansNoDiskAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeWorkspaceWithApp(t, home, "agent-race", 4317485)

	s := &Server{hubCfg: &types.HubConfig{}} // no loader installed

	if apps := s.githubAppsForTokenResolution(); len(apps) != 0 {
		t.Errorf("got %d apps without a loader, want 0 — tests must not reach the filesystem", len(apps))
	}
}
