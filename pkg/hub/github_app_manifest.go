package hub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/elasticclaw/elasticclaw/pkg/types"
)

// GitHub App manifest flow.
//
// Registering a GitHub App by hand means visiting developer settings, setting
// six permissions, generating a private key, downloading the PEM and pasting an
// App ID — a developer-grade errand that a non-developer will not finish. GitHub's
// manifest flow collapses it: the browser posts a manifest describing the App we
// need, the user approves once, and GitHub creates the App under *their* account
// and hands back the App ID and private key.
//
// The App still belongs to the user, so no private key ships inside the
// distributed binary. A key baked into a downloadable executable could mint
// write-scoped installation tokens for every install, which is why this flow is
// preferable to registering one central App.
//
// https://docs.github.com/apps/sharing-github-apps/registering-a-github-app-from-a-manifest

// manifestState links the redirect GitHub sends back to the workspace that
// started the flow. It doubles as the CSRF token: the callback arrives as a
// plain browser redirect and cannot carry a bearer token, so the state value is
// the only thing proving the request came from a flow we started.
type manifestState struct {
	Workspace string
	CreatedAt time.Time
}

type manifestStateStore struct {
	mu     sync.Mutex
	states map[string]manifestState
}

// manifestStateTTL bounds how long a started flow stays valid. Long enough to
// read GitHub's permission list and decide, short enough that an abandoned state
// is not left usable.
const manifestStateTTL = 15 * time.Minute

func (s *manifestStateStore) put(state string, v manifestState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states == nil {
		s.states = map[string]manifestState{}
	}
	// Opportunistically drop expired entries; this map only grows when a user
	// starts flows they never finish.
	for k, existing := range s.states {
		if time.Since(existing.CreatedAt) > manifestStateTTL {
			delete(s.states, k)
		}
	}
	s.states[state] = v
}

// take returns the state exactly once, so a redirect cannot be replayed.
func (s *manifestStateStore) take(state string) (manifestState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.states[state]
	if !ok {
		return manifestState{}, false
	}
	delete(s.states, state)
	if time.Since(v.CreatedAt) > manifestStateTTL {
		return manifestState{}, false
	}
	return v, true
}

func randomManifestState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// githubAppManifest is the App description GitHub creates from.
type githubAppManifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	RedirectURL        string            `json:"redirect_url"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
	HookAttributes     map[string]string `json:"hook_attributes,omitempty"`
}

// buildGitHubAppManifest describes the App the hub needs.
//
// The permissions mirror what InstallationToken actually requests, so the App is
// never granted more than the tokens it mints can use. contents and
// pull_requests are write because agents push branches and open PRs; issues is
// write so issue-triggered workflows can comment and label; checks and metadata
// are read-only status inputs.
func buildGitHubAppManifest(appName, hubBaseURL, redirectURL, webhookURL string) githubAppManifest {
	m := githubAppManifest{
		Name:        appName,
		URL:         hubBaseURL,
		RedirectURL: redirectURL,
		Public:      false,
		DefaultPermissions: map[string]string{
			"contents":      "write",
			"pull_requests": "write",
			"issues":        "write",
			"checks":        "read",
			"metadata":      "read",
		},
		DefaultEvents: []string{"issues", "pull_request", "pull_request_review", "check_suite"},
	}
	// A loopback hub is not reachable from GitHub, so requesting a webhook there
	// would only produce delivery failures. Issue triggers still work by polling.
	if webhookURL != "" {
		m.HookAttributes = map[string]string{"url": webhookURL}
	}
	return m
}

// hubBaseURLFromRequest reconstructs the URL the browser used to reach the hub,
// so the manifest's redirect points back at this hub rather than a configured
// public URL that may not be what the user is actually browsing.
func hubBaseURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}

// isLoopbackHost reports whether GitHub would be unable to reach this host.
func isLoopbackHost(host string) bool {
	h := host
	if idx := strings.LastIndex(h, ":"); idx != -1 {
		h = h[:idx]
	}
	h = strings.Trim(h, "[]")
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// handleGitHubAppManifestStart returns everything the browser needs to hand a
// manifest to GitHub. The UI renders it as a self-submitting form, because
// GitHub requires the manifest as a POST body rather than a query parameter.
func (s *Server) handleGitHubAppManifestStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspace := strings.TrimSpace(r.URL.Query().Get("workspace"))
	org := strings.TrimSpace(r.URL.Query().Get("org"))
	appName := strings.TrimSpace(r.URL.Query().Get("name"))
	if appName == "" {
		appName = "ElasticClaw"
	}

	state, err := randomManifestState()
	if err != nil {
		http.Error(w, "could not start GitHub setup", http.StatusInternalServerError)
		return
	}
	s.manifestStates.put(state, manifestState{Workspace: workspace, CreatedAt: time.Now()})

	base := hubBaseURLFromRequest(r)
	webhookURL := ""
	if workspace != "" && !isLoopbackHost(r.Host) {
		webhookURL = fmt.Sprintf("%s/api/workspaces/%s/webhooks/github", base, url.PathEscape(workspace))
	}
	manifest := buildGitHubAppManifest(appName, base, base+"/api/github/app-manifest/callback", webhookURL)

	// Apps created for an organization must be posted to the org's own path.
	target := "https://github.com/settings/apps/new"
	if org != "" {
		target = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new", url.PathEscape(org))
	}

	jsonOK(w, map[string]interface{}{
		"url":      target + "?state=" + url.QueryEscape(state),
		"manifest": manifest,
		"state":    state,
	})
}

// handleGitHubAppManifestCallback completes the flow. GitHub redirects the
// browser here with a short-lived code that converts, exactly once, into the
// App's id and private key.
func (s *Server) handleGitHubAppManifestCallback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}
	pending, ok := s.manifestStates.take(state)
	if !ok {
		// Unknown, replayed, or expired state.
		http.Error(w, "this GitHub setup link is no longer valid — start again from the app", http.StatusBadRequest)
		return
	}

	app, err := convertGitHubAppManifest(r.Context(), code)
	if err != nil {
		http.Error(w, "GitHub setup failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	if err := s.saveManifestGitHubApp(pending.Workspace, app); err != nil {
		http.Error(w, "could not save the GitHub App: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Send the browser somewhere useful: GitHub's install page, so the user picks
	// which repositories the App may touch. That choice is theirs to make on
	// GitHub, and it is what scopes every token the hub later mints.
	installURL := app.HTMLURL
	if installURL != "" {
		installURL += "/installations/new"
	} else {
		installURL = "/settings"
	}
	http.Redirect(w, r, installURL, http.StatusFound)
}

// handleGitHubInstallationRepositories lists every repository the workspace's
// GitHub Apps can reach, so the UI can offer a pick-from-a-list instead of
// asking someone to type "owner/repo" correctly.
//
// The hub could already do this internally to expand repository globs; this only
// exposes it, so the picker and glob expansion agree on what access exists.
func (s *Server) handleGitHubInstallationRepositories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspace := strings.TrimSpace(r.URL.Query().Get("workspace"))
	if workspace == "" {
		http.Error(w, "workspace required", http.StatusBadRequest)
		return
	}

	workspaceApps, err := loadWorkspaceGitHubAppConfigs(workspace)
	if err != nil {
		http.Error(w, "load workspace GitHub Apps: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.RLock()
	hubApps := append([]*types.GitHubAppConfig(nil), s.hubCfg.GitHubApps...)
	s.mu.RUnlock()
	apps := append(workspaceApps, hubApps...)

	// No App yet is a normal state on a fresh install, not an error: the UI shows
	// the connect button instead of a picker.
	if len(apps) == 0 {
		jsonOK(w, map[string]interface{}{"connected": false, "repositories": []string{}})
		return
	}

	seen := map[string]bool{}
	repos := []string{}
	var failures []string
	for _, appConfig := range apps {
		provider, err := NewGitHubTokenProvider(appConfig)
		if err != nil {
			failures = append(failures, fmt.Sprintf("app %d: %v", appConfig.AppID, err))
			continue
		}
		if s.githubBaseURL != "" {
			provider.apiBaseURL = s.githubBaseURL
		}
		installations, err := provider.ListInstallations(r.Context())
		if err != nil {
			failures = append(failures, fmt.Sprintf("app %d: %v", appConfig.AppID, err))
			continue
		}
		for _, installation := range installations {
			available, err := provider.ListInstallationRepositories(r.Context(), installation.ID)
			if err != nil {
				failures = append(failures, fmt.Sprintf("app %d installation %d: %v", appConfig.AppID, installation.ID, err))
				continue
			}
			for _, repo := range available {
				if repo.FullName != "" && !seen[repo.FullName] {
					seen[repo.FullName] = true
					repos = append(repos, repo.FullName)
				}
			}
		}
	}

	sort.Strings(repos)
	out := map[string]interface{}{"connected": true, "repositories": repos}
	// An App with no installation is the common half-finished case: created but
	// never installed on any repository. Say so rather than showing an empty list.
	if len(repos) == 0 && len(failures) > 0 {
		out["warning"] = strings.Join(failures, "; ")
	}
	jsonOK(w, out)
}

// saveManifestGitHubApp stores the created App against the workspace that
// started the flow, reusing the same store the CLI and settings UI write to so
// there is one place a GitHub App can live.
func (s *Server) saveManifestGitHubApp(workspace string, app *convertedGitHubApp) error {
	if workspace == "" {
		return fmt.Errorf("no workspace was selected before connecting GitHub")
	}
	name := app.Slug
	if name == "" {
		name = "github-app"
	}
	err := saveWorkspaceGitHubApp(workspace, name, workspaceGitHubApp{
		AppID:         app.ID,
		URL:           app.HTMLURL,
		PrivateKeyPEM: app.PEM,
	})
	if err != nil {
		return err
	}
	// Deliberately logs the app id and never the key.
	log.Printf("[github] registered App %d (%s) for workspace %s via manifest", app.ID, name, workspace)
	return nil
}

type convertedGitHubApp struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	HTMLURL       string `json:"html_url"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
	Slug          string `json:"slug"`
}

// convertGitHubAppManifest exchanges the one-time code for the created App.
// The endpoint needs no authentication: possession of the code is the proof,
// which is why it must be used immediately and never logged.
func convertGitHubAppManifest(ctx context.Context, code string) (*convertedGitHubApp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", url.PathEscape(code)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact GitHub: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}

	var app convertedGitHubApp
	if err := json.Unmarshal(body, &app); err != nil {
		return nil, fmt.Errorf("could not read GitHub's response: %w", err)
	}
	if app.ID == 0 || strings.TrimSpace(app.PEM) == "" {
		return nil, fmt.Errorf("GitHub did not return an app id and private key")
	}
	return &app, nil
}
