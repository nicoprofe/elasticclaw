package hub

// Automatic repository guides.
//
// Every run starts in a fresh sandbox with a fresh clone, so without help the agent
// re-reads the same unchanged repository on every task — measured at 43% of a run's
// wall clock. The remedy is to hand it the knowledge instead of the reading: an
// AGENTS.md in the workspace, which Codex-family agents load at session start.
//
// The guide is generated deterministically from the repository tree and its
// manifests — no model call. A model could write a richer guide, but a
// deterministic one is instant, free, cannot hallucinate structure, and can run
// unattended the moment a GitHub App is connected. The sections below are exactly
// the things an agent otherwise spends its first minutes discovering: where code
// lives, where tests live, what commands verify a change, and what API surface
// exists.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/elasticclaw/elasticclaw/pkg/types"
)

// repoGuideMarker identifies a generated guide. Regeneration refuses to touch an
// AGENTS.md without it, so a hand-written guide is never overwritten by a button.
const repoGuideMarker = "<!-- generated:elasticclaw-repo-guide -->"

// repoGuideInput is everything the generator reads. It is assembled from the
// GitHub API by the caller; keeping the generator pure makes it testable without
// network.
type repoGuideInput struct {
	Repo          string   // owner/name
	DefaultBranch string
	Tree          []string          // repository file paths, from the recursive tree
	Manifests     map[string]string // file path -> content, for the files fetched
}

// generateRepoGuide renders the AGENTS.md content for a repository.
func generateRepoGuide(in repoGuideInput) string {
	var b strings.Builder
	b.WriteString(repoGuideMarker + "\n")
	b.WriteString("# " + in.Repo + " — repository guide\n\n")
	b.WriteString("This file is the repository map, generated from the tree of `" + in.DefaultBranch + "`.\n")
	b.WriteString("Trust it instead of re-exploring: open only the files your change touches.\n")
	b.WriteString("If it contradicts the actual tree, the repository moved after generation — say so in your PR.\n\n")

	// The checkout-directory trap, learned from a real stall: the repository lives
	// in a subdirectory of the agent workspace named after the repo, and an agent
	// that runs the guide's commands from the workspace root gets "tool is not
	// installed" errors from npm even though setup installed everything.
	checkout := in.Repo
	if _, name, ok := strings.Cut(in.Repo, "/"); ok {
		checkout = name
	}
	b.WriteString("The repository is checked out at `" + checkout + "/` inside your workspace — ")
	b.WriteString("`cd " + checkout + "` before running any of the commands below, or they will fail as if dependencies were missing.\n\n")
	b.WriteString("If `memory/TASK_HISTORY.md` exists in your workspace, read it: it lists recent tasks and their pull requests. ")
	b.WriteString("Their branches are usually not merged, so their changes are absent from your checkout on purpose.\n\n")

	writeLayoutSection(&b, in.Tree)
	writeCommandsSection(&b, in)
	writeTestMapSection(&b, in.Tree)
	writeAPISection(&b, in)

	b.WriteString("## Conventions\n\n")
	b.WriteString("- Minimal diffs; match the style of the surrounding code.\n")
	b.WriteString("- Branch names: `task/<short-slug>`.\n")
	b.WriteString("- Never commit dependency trees, build output, or generated files.\n")
	writeLintersLine(&b, in)
	return b.String()
}

// writeLayoutSection summarises the top-level directories with file counts, so the
// agent knows where things live without listing anything.
func writeLayoutSection(b *strings.Builder, tree []string) {
	counts := map[string]int{}
	for _, p := range tree {
		top, _, found := strings.Cut(p, "/")
		if !found {
			continue // root files are covered by the other sections
		}
		if strings.HasPrefix(top, ".") && top != ".github" {
			continue // tool caches and editor config say nothing about the code
		}
		counts[top]++
	}
	if len(counts) == 0 {
		return
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)

	b.WriteString("## Layout\n\n")
	for _, n := range names {
		b.WriteString(fmt.Sprintf("- `%s/` — %d files\n", n, counts[n]))
	}
	b.WriteString("\n")
}

// writeCommandsSection extracts the verification commands the repository itself
// declares, which is what the agent needs most and guesses worst.
func writeCommandsSection(b *strings.Builder, in repoGuideInput) {
	var lines []string

	// npm scripts, from every package.json that was fetched. The path prefix
	// matters: `npm --prefix web run test` is a different command from `npm test`.
	for manifestPath, content := range in.Manifests {
		if path.Base(manifestPath) != "package.json" {
			continue
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal([]byte(content), &pkg); err != nil || len(pkg.Scripts) == 0 {
			continue
		}
		prefix := path.Dir(manifestPath)
		names := make([]string, 0, len(pkg.Scripts))
		for name := range pkg.Scripts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			cmd := "npm run " + name
			if prefix != "." {
				cmd = "npm --prefix " + prefix + " run " + name
			}
			lines = append(lines, fmt.Sprintf("- `%s` — %s", cmd, pkg.Scripts[name]))
		}
	}

	// pytest, if the project is a Python package.
	if content, ok := in.Manifests["pyproject.toml"]; ok {
		if strings.Contains(content, "pytest") {
			lines = append(lines, "- `.venv/bin/python -m pytest -q <test file>` — focused Python tests (the venv is prepared by workflow setup; do not reinstall)")
		}
	}

	if len(lines) == 0 {
		return
	}
	b.WriteString("## Commands the repository declares\n\n")
	sort.Strings(lines)
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")
}

// writeTestMapSection pairs test files with the modules they cover, following the
// tests/test_<module>.py and <name>.test.<ext> conventions. This is the "which
// focused test do I run" answer, precomputed.
func writeTestMapSection(b *strings.Builder, tree []string) {
	var lines []string
	for _, p := range tree {
		base := path.Base(p)
		switch {
		case strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
			module := strings.TrimSuffix(strings.TrimPrefix(base, "test_"), ".py")
			lines = append(lines, fmt.Sprintf("- `%s` covers `%s`", p, module))
		case strings.Contains(base, ".test.") || strings.Contains(base, ".spec."):
			lines = append(lines, fmt.Sprintf("- `%s` covers `%s`", p, strings.Split(base, ".")[0]))
		}
	}
	if len(lines) == 0 {
		return
	}
	sort.Strings(lines)
	b.WriteString("## Test map\n\n")
	b.WriteString("Change a module → run its focused test before announcing readiness.\n\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")
}

// writeAPISection lists the HTTP surface. If the repository commits an OpenAPI
// document its paths are extracted; otherwise router files are pointed at, which
// is where a FastAPI/Express surface is defined.
func writeAPISection(b *strings.Builder, in repoGuideInput) {
	for manifestPath, content := range in.Manifests {
		base := strings.ToLower(path.Base(manifestPath))
		if base != "openapi.json" && base != "openapi.yaml" && base != "openapi.yml" {
			continue
		}
		// JSON only: committed specs overwhelmingly are, and a YAML parse failure
		// here must not sink guide generation.
		var spec struct {
			Paths map[string]json.RawMessage `json:"paths"`
		}
		if err := json.Unmarshal([]byte(content), &spec); err != nil || len(spec.Paths) == 0 {
			continue
		}
		routes := make([]string, 0, len(spec.Paths))
		for r := range spec.Paths {
			routes = append(routes, r)
		}
		sort.Strings(routes)
		b.WriteString("## API surface (from " + manifestPath + ")\n\n")
		for _, r := range routes {
			b.WriteString("- `" + r + "`\n")
		}
		b.WriteString("\n")
		return
	}

	var routers []string
	for _, p := range tree_filter(in.Tree, func(p string) bool {
		return strings.Contains(p, "/routers/") || strings.HasSuffix(p, "/routes.py") || strings.HasSuffix(p, "/urls.py")
	}) {
		routers = append(routers, p)
	}
	if len(routers) == 0 {
		return
	}
	sort.Strings(routers)
	b.WriteString("## API surface\n\n")
	b.WriteString("No committed OpenAPI document. Routes are defined in:\n\n")
	for _, r := range routers {
		b.WriteString("- `" + r + "`\n")
	}
	b.WriteString("\n")
}

func writeLintersLine(b *strings.Builder, in repoGuideInput) {
	if content, ok := in.Manifests["pyproject.toml"]; ok && strings.Contains(content, "[tool.ruff]") {
		b.WriteString("- Python is linted with ruff; keep to its configuration in pyproject.toml.\n")
	}
	for _, p := range in.Tree {
		base := path.Base(p)
		if strings.HasPrefix(base, ".eslintrc") || base == "eslint.config.js" || base == "eslint.config.mjs" {
			b.WriteString("- JavaScript/TypeScript is linted with eslint.\n")
			return
		}
	}
}

func tree_filter(tree []string, keep func(string) bool) []string {
	var out []string
	for _, p := range tree {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}

// ── Server plumbing ───────────────────────────────────────────────────────────

// guideManifestCandidates are the files fetched to inform the guide, when they
// exist in the tree. Bounded and named, rather than fetching by pattern, so a
// pathological repository cannot make generation slow or huge.
func guideManifestCandidates(tree []string) []string {
	var out []string
	for _, p := range tree {
		base := strings.ToLower(path.Base(p))
		switch {
		case base == "pyproject.toml" && p == "pyproject.toml":
			out = append(out, p)
		case base == "package.json" && !strings.Contains(p, "node_modules/"):
			out = append(out, p)
		case base == "openapi.json" || base == "openapi.yaml" || base == "openapi.yml":
			out = append(out, p)
		}
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// findWorkspaceByName loads the named workspace, or nil if it does not exist.
func (s *Server) findWorkspaceByName(name string) (*types.WorkspaceConfig, error) {
	workspaces, err := s.loadAllWorkspaces()
	if err != nil {
		return nil, err
	}
	for _, ws := range workspaces {
		if ws != nil && ws.Name == name {
			return ws, nil
		}
	}
	return nil, nil
}

// decodeGitHubContent unwraps the contents API's base64 payload.
func decodeGitHubContent(resp map[string]interface{}) (string, bool) {
	encoded, ok := resp["content"].(string)
	if !ok || encoded == "" {
		return "", false
	}
	// The API wraps the base64 in newlines.
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, "\n", ""))
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

// generateWorkspaceRepoGuide builds AGENTS.md for the workspace's first repository
// and writes it into the workspace directory, where template delivery picks it up
// on the next run. It never overwrites a hand-written AGENTS.md.
func (s *Server) generateWorkspaceRepoGuide(workspace string) (string, error) {
	ws, err := s.findWorkspaceByName(workspace)
	if err != nil {
		return "", err
	}
	if ws == nil || len(ws.Repositories) == 0 {
		return "", fmt.Errorf("workspace %q has no repositories to describe", workspace)
	}
	repo := ws.Repositories[0].Repo

	guidePath := filepath.Join(workspacesDir(), workspace, "AGENTS.md")
	if existing, err := os.ReadFile(guidePath); err == nil {
		if !strings.Contains(string(existing), repoGuideMarker) {
			return "", fmt.Errorf("AGENTS.md for %q was written by hand; refusing to overwrite it", workspace)
		}
	}

	token := s.resolveGitHubTokenForRepo(repo)
	if token == "" {
		return "", fmt.Errorf("no GitHub App can read %s", repo)
	}

	repoInfo, err := githubAPI("repos/"+repo, token)
	if err != nil {
		return "", fmt.Errorf("read repository: %w", err)
	}
	branch, _ := repoInfo["default_branch"].(string)
	if branch == "" {
		branch = "main"
	}

	treeResp, err := githubAPI("repos/"+repo+"/git/trees/"+branch+"?recursive=1", token)
	if err != nil {
		return "", fmt.Errorf("read tree: %w", err)
	}
	var tree []string
	if entries, ok := treeResp["tree"].([]interface{}); ok {
		for _, e := range entries {
			m, ok := e.(map[string]interface{})
			if !ok || m["type"] != "blob" {
				continue
			}
			if p, ok := m["path"].(string); ok {
				tree = append(tree, p)
			}
		}
	}
	if len(tree) == 0 {
		return "", fmt.Errorf("repository tree for %s came back empty", repo)
	}

	manifests := map[string]string{}
	for _, p := range guideManifestCandidates(tree) {
		fileResp, err := githubAPI("repos/"+repo+"/contents/"+p+"?ref="+branch, token)
		if err != nil {
			continue // a manifest is an enrichment, not a requirement
		}
		if content, ok := decodeGitHubContent(fileResp); ok {
			manifests[p] = content
		}
	}

	guide := generateRepoGuide(repoGuideInput{
		Repo:          repo,
		DefaultBranch: branch,
		Tree:          tree,
		Manifests:     manifests,
	})
	if err := os.WriteFile(guidePath, []byte(guide), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", guidePath, err)
	}
	log.Printf("[repo-guide] generated %s for %s (%d bytes, %d tree entries)", guidePath, repo, len(guide), len(tree))
	return guide, nil
}

// handleWorkspaceRepoGuide serves the guide state and regenerates on demand.
//
// GET reports whether a guide exists and whether it is generated or hand-written,
// which is what the settings button needs to label itself. POST regenerates.
func (s *Server) handleWorkspaceRepoGuide(w http.ResponseWriter, r *http.Request) {
	workspace := strings.TrimSpace(r.PathValue("workspace"))
	if workspace == "" {
		http.Error(w, "workspace required", http.StatusBadRequest)
		return
	}
	guidePath := filepath.Join(workspacesDir(), workspace, "AGENTS.md")

	switch r.Method {
	case http.MethodGet:
		content, err := os.ReadFile(guidePath)
		if err != nil {
			jsonOK(w, map[string]interface{}{"exists": false})
			return
		}
		jsonOK(w, map[string]interface{}{
			"exists":    true,
			"generated": strings.Contains(string(content), repoGuideMarker),
			"bytes":     len(content),
		})
	case http.MethodPost:
		guide, err := s.generateWorkspaceRepoGuide(workspace)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonOK(w, map[string]interface{}{
			"generated": true,
			"bytes":     len(guide),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// maybeGenerateRepoGuide is the App-connect hook: fired after a GitHub App is
// attached to a workspace, so a new workspace has its guide before its first run.
// Best-effort by design — a guide failure must never break App creation.
func (s *Server) maybeGenerateRepoGuide(workspace string) {
	go func() {
		if _, err := s.generateWorkspaceRepoGuide(workspace); err != nil {
			log.Printf("[repo-guide] %s: %v", workspace, err)
		}
	}()
}
