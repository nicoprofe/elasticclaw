package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The manifest must never ask for more than InstallationToken actually requests,
// or the user grants access the product cannot justify.
func TestManifestPermissionsMatchWhatTokensRequest(t *testing.T) {
	m := buildGitHubAppManifest("ElasticClaw", "http://127.0.0.1:8080", "http://127.0.0.1:8080/cb", "http://127.0.0.1:8080/hook", false)

	want := map[string]string{
		"contents":      "write",
		"pull_requests": "write",
		"issues":        "write",
		"checks":        "read",
		"metadata":      "read",
	}
	for name, level := range want {
		if got := m.DefaultPermissions[name]; got != level {
			t.Errorf("permission %q = %q, want %q", name, got, level)
		}
	}
	for name := range m.DefaultPermissions {
		if _, ok := want[name]; !ok {
			t.Errorf("manifest requests unexpected permission %q — every permission must be justified", name)
		}
	}
	if m.Public {
		t.Error("the App must be private: it is created for one user's own repositories")
	}
}

// GitHub rejects a manifest with no hook url ("Hook url cannot be blank"), so the
// url is always declared. A loopback hub cannot receive deliveries, so the hook is
// marked inactive rather than omitted — an earlier version omitted it and GitHub
// refused to create the App at all.
func TestManifestAlwaysDeclaresAHookURL(t *testing.T) {
	loopback := buildGitHubAppManifest("ElasticClaw", "http://127.0.0.1:8080",
		"http://127.0.0.1:8080/cb", "http://127.0.0.1:8080/api/workspaces/w/webhooks/github", false)
	if loopback.HookAttributes["url"] == "" {
		t.Error("hook url is blank — GitHub rejects the manifest outright")
	}
	if active, _ := loopback.HookAttributes["active"].(bool); active {
		t.Error("deliveries should be disabled for a hub GitHub cannot reach")
	}

	reachable := buildGitHubAppManifest("ElasticClaw", "https://hub.example.com",
		"https://hub.example.com/cb", "https://hub.example.com/api/workspaces/w/webhooks/github", true)
	if active, _ := reachable.HookAttributes["active"].(bool); !active {
		t.Error("deliveries should be enabled for a publicly reachable hub")
	}
}

// The url must be present regardless of whether a workspace was selected.
func TestManifestWebhookNeverBlank(t *testing.T) {
	for _, ws := range []string{"agent-race", ""} {
		url, active := manifestWebhook("http://127.0.0.1:8080", ws, "127.0.0.1:8080")
		if url == "" {
			t.Errorf("workspace %q produced a blank hook url", ws)
		}
		if active {
			t.Errorf("workspace %q enabled deliveries to a loopback host", ws)
		}
	}
	if _, active := manifestWebhook("https://hub.example.com", "w", "hub.example.com"); !active {
		t.Error("a reachable host should have deliveries enabled")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1:8080", "localhost:3000", "localhost", "[::1]:8080"} {
		if !isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = false, want true", host)
		}
	}
	for _, host := range []string{"hub.example.com", "hub.example.com:443", "10.0.0.5:8080"} {
		if isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = true, want false", host)
		}
	}
}

// The state is the only thing authorizing the callback, since GitHub redirects a
// browser that cannot carry a bearer token. It must therefore work exactly once.
func TestManifestStateIsSingleUse(t *testing.T) {
	var store manifestStateStore
	store.put("abc", manifestState{Workspace: "demo", CreatedAt: time.Now()})

	got, ok := store.take("abc")
	if !ok || got.Workspace != "demo" {
		t.Fatalf("first take = (%+v, %v), want the stored state", got, ok)
	}
	if _, ok := store.take("abc"); ok {
		t.Error("state was accepted twice — a callback could be replayed")
	}
}

func TestManifestStateRejectsUnknownAndExpired(t *testing.T) {
	var store manifestStateStore
	if _, ok := store.take("never-issued"); ok {
		t.Error("an unissued state was accepted")
	}

	store.put("stale", manifestState{Workspace: "demo", CreatedAt: time.Now().Add(-manifestStateTTL - time.Minute)})
	if _, ok := store.take("stale"); ok {
		t.Error("an expired state was accepted")
	}
}

func TestRandomManifestStateIsUnguessable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		s, err := randomManifestState()
		if err != nil {
			t.Fatalf("randomManifestState: %v", err)
		}
		if len(s) < 24 {
			t.Fatalf("state %q is too short to resist guessing", s)
		}
		if seen[s] {
			t.Fatalf("state %q repeated", s)
		}
		seen[s] = true
	}
}

func TestHubBaseURLFromRequestHonoursForwardedProto(t *testing.T) {
	// Behind a TLS-terminating proxy the redirect must be https, or GitHub
	// returns the user to a URL the browser refuses to post to.
	r := httptest.NewRequest(http.MethodGet, "http://hub.example.com/api/github/app-manifest", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := hubBaseURLFromRequest(r); !strings.HasPrefix(got, "https://") {
		t.Errorf("hubBaseURLFromRequest = %q, want an https URL", got)
	}
}

// GitHub App names are globally unique, so a fixed name collides with any App
// that already exists and creation fails outright.
func TestDefaultAppNameIsQualifiedByWorkspace(t *testing.T) {
	if got := defaultAppName("agent-race"); got != "ElasticClaw agent-race" {
		t.Errorf("defaultAppName(agent-race) = %q, want %q", got, "ElasticClaw agent-race")
	}
	// With no workspace there is nothing to qualify with; the user renames on GitHub.
	if got := defaultAppName(""); got != "ElasticClaw" {
		t.Errorf("defaultAppName(\"\") = %q, want ElasticClaw", got)
	}
	// Avoid the silly "ElasticClaw elasticclaw".
	if got := defaultAppName("ElasticClaw"); got != "ElasticClaw" {
		t.Errorf("defaultAppName(ElasticClaw) = %q, want ElasticClaw", got)
	}
}

// GitHub rejects a loopback homepage url with `"url" wasn't supplied`, so the
// manifest must declare a public one. redirect_url stays on the hub, because that
// is where the callback has to land and GitHub permits loopback there.
func TestManifestHomepageIsPublicButRedirectIsLocal(t *testing.T) {
	m := buildGitHubAppManifest("ElasticClaw", appHomepageURL(),
		"http://127.0.0.1:8080/api/github/app-manifest/callback", "", false)
	if strings.Contains(m.URL, "127.0.0.1") || strings.Contains(m.URL, "localhost") {
		t.Errorf("homepage url %q is loopback — GitHub refuses the manifest", m.URL)
	}
	if !strings.HasPrefix(m.URL, "https://") {
		t.Errorf("homepage url %q should be https", m.URL)
	}
	if !strings.Contains(m.RedirectURL, "127.0.0.1") {
		t.Errorf("redirect_url %q must point back at the local hub", m.RedirectURL)
	}
}
