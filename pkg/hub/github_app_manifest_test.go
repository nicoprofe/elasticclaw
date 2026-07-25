package hub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strconv"
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

// The handoff must be a GET navigation, not a form POST. Browsers withhold
// SameSite=Lax cookies on cross-site POSTs, so a POST from the local hub reached
// GitHub with no session: it ignored the manifest and reported `"url" wasn't
// supplied` while the user was signed in in another tab.
func TestManifestHandoffIsAGetWithTheManifestInTheQuery(t *testing.T) {
	srv := &Server{}
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/github/app-manifest/open", nil)

	target, err := srv.manifestCreateURL(r, "agent-race", "state-123")
	if err != nil {
		t.Fatalf("manifestCreateURL: %v", err)
	}
	parsed, err := neturl.Parse(target)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Host != "github.com" || parsed.Path != "/settings/apps/new" {
		t.Errorf("target = %s, want github.com/settings/apps/new", target)
	}
	if parsed.Query().Get("state") != "state-123" {
		t.Error("state must survive into the redirect, or the callback cannot be matched")
	}

	raw := parsed.Query().Get("manifest")
	if raw == "" {
		t.Fatal("manifest is missing from the query string")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("manifest is not valid JSON in the query: %v", err)
	}
	if m["name"] != "ElasticClaw agent-race" {
		t.Errorf("manifest name = %v", m["name"])
	}
	if u, _ := m["url"].(string); !strings.HasPrefix(u, "https://") {
		t.Errorf("homepage url %q must be public", u)
	}
}

// The setup state used to live in memory, so restarting the hub — which every
// upgrade does — silently invalidated links already handed out. A user who created
// the App during that window had it created for real while the callback was
// rejected, and GitHub returns the private key only once, so it was unrecoverable.
// Signing the state means a restart no longer breaks a flow in progress.
func TestSignedManifestStateSurvivesRestartAndCarriesWorkspace(t *testing.T) {
	const secret = "hub-token-that-persists-in-hub-yaml"

	state, err := signManifestState(secret, "agent-race")
	if err != nil {
		t.Fatalf("signManifestState: %v", err)
	}
	// A fresh Server value stands in for a restarted hub: nothing is remembered,
	// only the secret from hub.yaml is available.
	ws, ok := verifyManifestState(secret, state)
	if !ok || ws != "agent-race" {
		t.Errorf("verify after restart = (%q, %v), want (agent-race, true)", ws, ok)
	}
}

func TestSignedManifestStateRejectsTamperingAndOtherHubs(t *testing.T) {
	const secret = "hub-token"
	state, err := signManifestState(secret, "agent-race")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := verifyManifestState("a-different-hubs-token", state); ok {
		t.Error("state signed by one hub was accepted by another")
	}
	// Swapping the workspace must not be possible: it decides which workspace the
	// callback writes an App and private key into.
	forged, _ := signManifestState(secret, "agent-race")
	encoded, sig, _ := strings.Cut(forged, ".")
	_ = encoded
	if _, ok := verifyManifestState(secret, "ZGVmYXVsdHwyNTI0NjA4MDAw."+sig); ok {
		t.Error("a swapped payload kept a valid signature")
	}
	if _, ok := verifyManifestState(secret, "not-a-state"); ok {
		t.Error("malformed state accepted")
	}
	if _, ok := verifyManifestState(secret, ""); ok {
		t.Error("empty state accepted")
	}
}

func TestSignedManifestStateExpires(t *testing.T) {
	const secret = "hub-token"
	// Sign a payload that already expired, the way a link left overnight would be.
	expired := base64.RawURLEncoding.EncodeToString([]byte("agent-race|" + strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("elasticclaw:app-manifest:" + expired))
	state := expired + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if _, ok := verifyManifestState(secret, state); ok {
		t.Error("an expired state was accepted")
	}
}
