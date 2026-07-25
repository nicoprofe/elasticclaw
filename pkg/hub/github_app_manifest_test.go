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
	m := buildGitHubAppManifest("ElasticClaw", "http://127.0.0.1:8080", "http://127.0.0.1:8080/cb", "")

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

// A loopback hub cannot receive webhooks, so requesting them would only produce
// delivery failures in the user's GitHub settings.
func TestManifestOmitsWebhookForUnreachableHub(t *testing.T) {
	m := buildGitHubAppManifest("ElasticClaw", "http://127.0.0.1:8080", "http://127.0.0.1:8080/cb", "")
	if len(m.HookAttributes) != 0 {
		t.Errorf("expected no webhook for a loopback hub, got %v", m.HookAttributes)
	}

	reachable := buildGitHubAppManifest("ElasticClaw", "https://hub.example.com",
		"https://hub.example.com/cb", "https://hub.example.com/api/workspaces/w/webhooks/github")
	if reachable.HookAttributes["url"] == "" {
		t.Error("expected a webhook URL for a publicly reachable hub")
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
