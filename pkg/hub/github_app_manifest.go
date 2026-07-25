package hub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
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

func urlPathEscape(s string) string { return url.PathEscape(s) }

func randomManifestState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// defaultAppName proposes a name for the App to be created.
//
// GitHub App names are globally unique across all of GitHub, so a fixed
// "ElasticClaw" is already taken and creation fails with "Name has already been
// taken" — a dead end for anyone who is not going to think to rename it.
// Qualifying with the workspace makes a collision unlikely, and GitHub still lets
// the name be edited on the creation page if it happens.
func defaultAppName(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || strings.EqualFold(workspace, "elasticclaw") {
		return "ElasticClaw"
	}
	return "ElasticClaw " + workspace
}

// githubAppManifest is the App description GitHub creates from.
type githubAppManifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	RedirectURL        string            `json:"redirect_url"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
	HookAttributes     map[string]any    `json:"hook_attributes,omitempty"`
}

// buildGitHubAppManifest describes the App the hub needs.
//
// The permissions mirror what InstallationToken actually requests, so the App is
// never granted more than the tokens it mints can use. contents and
// pull_requests are write because agents push branches and open PRs; issues is
// write so issue-triggered workflows can comment and label; checks and metadata
// are read-only status inputs.
// buildGitHubAppManifest takes webhookURL unconditionally and disables delivery
// when the hub is unreachable, rather than omitting the field.
func buildGitHubAppManifest(appName, hubBaseURL, redirectURL, webhookURL string, webhookActive bool) githubAppManifest {
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
	// GitHub imposes two constraints that look contradictory until you disable
	// webhooks explicitly:
	//
	//   omit hook_attributes            -> "Hook url cannot be blank"
	//   send a 127.0.0.1 url            -> "Hook url is not supported because it
	//                                      isn't reachable over the public Internet"
	//
	// So a hub GitHub cannot reach declares the webhook inactive and sends no url
	// at all, which is how a manifest asks for an App without webhooks. Issue
	// triggers still work by polling, which is what a local hub relies on anyway.
	if webhookActive && webhookURL != "" {
		m.HookAttributes = map[string]any{"url": webhookURL, "active": true}
	} else {
		m.HookAttributes = map[string]any{"active": false}
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

// manifestWebhook returns the webhook URL to declare and whether deliveries
// should be enabled. The URL is never blank, because GitHub rejects a manifest
// without one.
func manifestWebhook(base, workspace, host string) (string, bool) {
	target := workspace
	if target == "" {
		target = "default"
	}
	url := fmt.Sprintf("%s/api/workspaces/%s/webhooks/github", base, urlPathEscape(target))
	return url, !isLoopbackHost(host)
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
		appName = defaultAppName(workspace)
	}

	state, err := randomManifestState()
	if err != nil {
		http.Error(w, "could not start GitHub setup", http.StatusInternalServerError)
		return
	}
	s.manifestStates.put(state, manifestState{Workspace: workspace, CreatedAt: time.Now()})

	base := hubBaseURLFromRequest(r)
	webhookURL, webhookActive := manifestWebhook(base, workspace, r.Host)
	manifest := buildGitHubAppManifest(appName, base, base+"/api/github/app-manifest/callback", webhookURL, webhookActive)

	// Apps created for an organization must be posted to the org's own path.
	target := "https://github.com/settings/apps/new"
	if org != "" {
		target = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new", url.PathEscape(org))
	}

	// GitHub needs the manifest as a POST body, which a link cannot do, and the
	// desktop app is a WebView window where signing in to GitHub is a bad idea.
	// So hand back a URL the user opens in their own browser: the hub serves a
	// page there that posts the manifest for them.
	//
	// That page cannot require a bearer token, so it is gated by a single-use
	// ticket. Without one, anyone who could reach the hub could start a flow and
	// have the hub store an App created under *their* GitHub account, which would
	// hand them control of the tokens the hub mints.
	ticket, err := randomManifestState()
	if err != nil {
		http.Error(w, "could not start GitHub setup", http.StatusInternalServerError)
		return
	}
	s.manifestTickets.put(ticket, manifestState{Workspace: workspace, CreatedAt: time.Now()})

	jsonOK(w, map[string]interface{}{
		"openUrl":  base + "/api/github/app-manifest/open?ticket=" + url.QueryEscape(ticket),
		"url":      target + "?state=" + url.QueryEscape(state),
		"manifest": manifest,
		"state":    state,
	})
}

// handleGitHubAppManifestLaunch does what the Connect button should do: it opens
// the user's browser on the setup page itself, instead of handing back a link to
// copy. One click in the app, then GitHub.
func (s *Server) handleGitHubAppManifestLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspace := strings.TrimSpace(r.URL.Query().Get("workspace"))

	ticket, err := randomManifestState()
	if err != nil {
		http.Error(w, "could not start GitHub setup", http.StatusInternalServerError)
		return
	}
	s.manifestTickets.put(ticket, manifestState{Workspace: workspace, CreatedAt: time.Now()})

	openURL := hubBaseURLFromRequest(r) + "/api/github/app-manifest/open?ticket=" + url.QueryEscape(ticket)

	// Report whether the browser actually opened, so the UI can fall back to
	// showing the link rather than claiming success it cannot verify.
	opened := true
	message := ""
	if err := openInDefaultBrowser(openURL); err != nil {
		opened = false
		message = err.Error()
	}
	jsonOK(w, map[string]interface{}{
		"opened":  opened,
		"openUrl": openURL,
		"appName": defaultAppName(workspace),
		"error":   message,
	})
}

// manifestOpenPage posts the manifest to GitHub on the user's behalf. It renders
// a real button as well as auto-submitting, because a browser that blocks the
// scripted submit would otherwise show a blank page.
const manifestOpenPage = `<!doctype html>
<html><head><meta charset="utf-8"><title>Connect GitHub</title>
<style>
 body{background:#09090b;color:#fafafa;font:15px/1.6 system-ui,sans-serif;display:flex;
      min-height:100vh;align-items:center;justify-content:center;margin:0}
 .card{max-width:34rem;padding:2rem;text-align:center}
 h1{font-size:1.25rem;margin:0 0 .75rem}
 p{color:#a1a1aa;margin:0 0 1.5rem}
 button{background:#67e8f9;color:#09090b;border:0;border-radius:.5rem;
        padding:.75rem 1.5rem;font-size:15px;font-weight:600;cursor:pointer}
</style></head>
<body><div class="card">
 <h1>Creating your GitHub App</h1>
 <p>GitHub will ask you to approve an app called <strong>{{.AppName}}</strong>. It is created
    under your own account, and you choose which repositories it can touch on the next screen.</p>
 <form id="f" method="post" action="{{.Action}}">
   <input type="hidden" name="manifest" value="{{.Manifest}}">
   <button type="submit">Continue to GitHub</button>
 </form>
 <script>document.getElementById('f').submit()</script>
</div></body></html>`

// handleGitHubAppManifestOpen serves the auto-submitting page for a valid ticket.
func (s *Server) handleGitHubAppManifestOpen(w http.ResponseWriter, r *http.Request) {
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket == "" {
		http.Error(w, "missing ticket", http.StatusBadRequest)
		return
	}
	pending, ok := s.manifestTickets.take(ticket)
	if !ok {
		http.Error(w, "this GitHub setup link has already been used or expired — start again from the app", http.StatusBadRequest)
		return
	}

	state, err := randomManifestState()
	if err != nil {
		http.Error(w, "could not start GitHub setup", http.StatusInternalServerError)
		return
	}
	s.manifestStates.put(state, manifestState{Workspace: pending.Workspace, CreatedAt: time.Now()})

	base := hubBaseURLFromRequest(r)
	webhookURL, webhookActive := manifestWebhook(base, pending.Workspace, r.Host)
	appName := defaultAppName(pending.Workspace)
	manifest := buildGitHubAppManifest(appName, base, base+"/api/github/app-manifest/callback", webhookURL, webhookActive)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, "could not build the manifest", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("manifest").Parse(manifestOpenPage)
	if err != nil {
		http.Error(w, "could not render the page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The manifest is escaped as an HTML attribute by html/template, so the JSON
	// cannot break out of the value and inject markup.
	_ = tmpl.Execute(w, map[string]interface{}{
		"AppName":  appName,
		"Action":   "https://github.com/settings/apps/new?state=" + url.QueryEscape(state),
		"Manifest": string(manifestJSON),
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
