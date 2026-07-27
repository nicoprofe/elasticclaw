package hub

// Hub-side pull request creation.
//
// Measured across every completed run: after the test gate passed, the agent spent
// 96-159s narrating three mechanical steps — push the branch, compose a PR body,
// call the GitHub API — a quarter of total wall clock, at LLM speed, for work with
// exactly one correct outcome. The hub already executes commands in the workspace
// (that is how the gate runs) and already mints repo-scoped App tokens (that is how
// the credential helper works), so it performs those steps itself in seconds.
//
// The agent is deliberately left out of the loop but not out of the picture: it
// receives the PR link as a hub message, and the pipeline advances through the
// pr_opened trigger. If anything here fails, the run degrades to exactly the old
// behaviour — the agent is asked to push and open the PR itself — so a GitHub
// hiccup costs the old 100 seconds rather than the run.

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"

	"github.com/elasticclaw/elasticclaw/pkg/hub/pipeline"
)

// executeOpenPRAction pushes the workspace's current branch and opens the pull
// request. Returns the PR URL on success.
func (s *Server) executeOpenPRAction(clawID string, action *pipeline.OpenPRAction, ctx pipelineContext) (string, error) {
	var name, reposJSON string
	if err := s.db.QueryRow(`SELECT name, COALESCE(github_repos,'') FROM claws WHERE id=?`, clawID).Scan(&name, &reposJSON); err != nil {
		return "", fmt.Errorf("load claw: %w", err)
	}
	repo := firstRepoFromJSON(reposJSON)
	if repo == "" {
		return "", fmt.Errorf("claw has no GitHub repository to open a pull request against")
	}
	repoDir := path.Base(repo)

	// Commit anything the agent left uncommitted, then push. The gate has already
	// verified this exact tree, so an uncommitted remainder is verified work that
	// would otherwise silently not make it into the pull request.
	pushCmd := fmt.Sprintf(`cd %s
if ! git diff --quiet || ! git diff --cached --quiet; then
  git add -A
  git commit -m "chore: include remaining verified work (committed by ElasticClaw)"
fi
branch=$(git rev-parse --abbrev-ref HEAD)
case "$branch" in
  main|master|HEAD)
    echo "refusing to open a pull request from branch $branch" >&2
    exit 1
    ;;
esac
git push -u origin "$branch"
echo "ELASTICCLAW_BRANCH=$branch"`, repoDir)

	result, err := s.executePipelineRunAction(clawID, pipeline.RunAction{Command: pushCmd, Timeout: "3m"})
	if err != nil {
		return "", fmt.Errorf("push branch: %w", err)
	}
	if result == nil || result.ExitCode != 0 {
		detail := ""
		if result != nil {
			detail = truncateString(strings.TrimSpace(result.Stdout+"\n"+result.Stderr), 1200)
		}
		return "", fmt.Errorf("push branch failed: %s", detail)
	}
	branch := ""
	for _, line := range strings.Split(result.Stdout, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ELASTICCLAW_BRANCH="); ok {
			branch = strings.TrimSpace(v)
		}
	}
	if branch == "" {
		return "", fmt.Errorf("could not determine the pushed branch name")
	}

	token := s.resolveGitHubTokenWithRepos([]RepoAccess{{Repo: repo, Permissions: "write"}})
	if token == "" {
		return "", fmt.Errorf("no GitHub App can write to %s", repo)
	}

	base := strings.TrimSpace(action.Base)
	if base == "" {
		repoInfo, err := githubAPI("repos/"+repo, token)
		if err != nil {
			return "", fmt.Errorf("read default branch: %w", err)
		}
		base, _ = repoInfo["default_branch"].(string)
		if base == "" {
			base = "main"
		}
	}

	title := strings.TrimSpace(name)
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	body := fmt.Sprintf(
		"%s\n\n---\nOpened by ElasticClaw after the workflow's test gate passed: the full gate "+
			"(tests, typecheck, build) ran green against this exact tree in the sandbox before "+
			"this pull request was created.\n\nBranch: `%s`",
		strings.TrimSpace(name), branch)

	resp, err := githubAPIPostWithBase("https://api.github.com", "repos/"+repo+"/pulls", token, "POST", map[string]interface{}{
		"title": title,
		"head":  branch,
		"base":  base,
		"body":  body,
	})
	if err != nil {
		return "", fmt.Errorf("create pull request: %w", err)
	}
	prURL, _ := resp["html_url"].(string)
	prNumberFloat, _ := resp["number"].(float64)
	prNumber := int(prNumberFloat)
	if prURL == "" || prNumber == 0 {
		return "", fmt.Errorf("GitHub accepted the request but returned no pull request")
	}

	// Same bookkeeping as an agent-opened PR: tracked by the watcher, recorded in
	// the workspace's task history.
	if err := s.storePRMention(clawID, repo, prNumber, prURL); err != nil {
		log.Printf("[open-pr] tracking %s#%d: %v", repo, prNumber, err)
	}
	log.Printf("[open-pr] opened %s#%d for claw %s (branch %s)", repo, prNumber, clawID[:8], branch)
	s.injectHubMessageByID(clawID, fmt.Sprintf(
		"[hub] Your branch was pushed and the pull request opened for you: %s\nDo not push again or open another pull request. Follow the next instructions.", prURL))

	// Advance the pipeline through the pr_opened trigger, the same goroutine
	// pattern the judge and gate auto-transitions use.
	if pl := parsePipelineForContext(ctx); pl != nil {
		if next := pl.StageForPROpened(); next != nil {
			stage := *next
			s.safeGo("pipeline pr-opened auto-transition", func() {
				s.transitionPipelineStageWithContext(clawID, stage, ctx)
			})
		}
	}
	return prURL, nil
}

// firstRepoFromJSON extracts the first repository from the claws.github_repos
// column, which stores a JSON array of {repo, permissions} objects.
func firstRepoFromJSON(reposJSON string) string {
	var repos []struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal([]byte(reposJSON), &repos); err != nil {
		return ""
	}
	for _, r := range repos {
		if strings.TrimSpace(r.Repo) != "" {
			return r.Repo
		}
	}
	return ""
}
