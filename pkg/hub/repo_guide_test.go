package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func agentRaceLikeInput() repoGuideInput {
	return repoGuideInput{
		Repo:          "nicoprofe/agent-race",
		DefaultBranch: "main",
		Tree: []string{
			"README.md", "pyproject.toml",
			"api/main.py", "api/routers/core.py", "api/routers/github.py",
			"runner/cli.py", "runner/grader.py",
			"tests/test_scoring.py", "tests/test_grader.py",
			"web/app/page.tsx", "web/components/events.test.ts", "web/package.json",
			".venv/lib/site.py", // must not appear anywhere
		},
		Manifests: map[string]string{
			"pyproject.toml":   "[project]\ndependencies=[]\n[tool.ruff]\nline-length=100\n[tool.pytest.ini_options]\n",
			"web/package.json": `{"scripts":{"typecheck":"tsc --noEmit","test":"vitest run"}}`,
		},
	}
}

// The guide must answer the questions an agent otherwise spends its first minutes
// on: where things are, what verifies a change, and which focused test covers the
// module being touched.
func TestGeneratedGuideAnswersTheExpensiveQuestions(t *testing.T) {
	g := generateRepoGuide(agentRaceLikeInput())

	for _, want := range []string{
		repoGuideMarker,                       // regeneration must be able to recognise its own output
		"`api/` — 3 files",                    // layout
		"npm --prefix web run typecheck",      // command with the prefix that makes it correct
		".venv/bin/python -m pytest",          // python verification, without reinstalling
		"`tests/test_scoring.py` covers `scoring`", // test map
		"api/routers/core.py",                 // API surface via router files
		"linted with ruff",                    // conventions from config, not guesswork
		"checked out at `agent-race/`",        // the cwd trap that stalled a real run
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide is missing %q\n---\n%s", want, g)
		}
	}
	if strings.Contains(g, ".venv/lib") {
		t.Error("dot-directories leaked into the layout")
	}
}

// A committed OpenAPI document beats pointing at router files.
func TestGuidePrefersCommittedOpenAPISpec(t *testing.T) {
	in := agentRaceLikeInput()
	in.Manifests["openapi.json"] = `{"paths":{"/races":{},"/races/{id}":{}}}`
	g := generateRepoGuide(in)
	if !strings.Contains(g, "`/races/{id}`") {
		t.Errorf("OpenAPI paths not extracted:\n%s", g)
	}
	if strings.Contains(g, "Routes are defined in") {
		t.Error("router-file fallback rendered even though a spec exists")
	}
}

// A malformed spec must degrade to the fallback, not sink the guide.
func TestGuideSurvivesMalformedOpenAPISpec(t *testing.T) {
	in := agentRaceLikeInput()
	in.Manifests["openapi.json"] = `{"paths": not-json`
	g := generateRepoGuide(in)
	if !strings.Contains(g, "api/routers/core.py") {
		t.Error("router fallback missing after a bad spec")
	}
}

// The refusal that protects hand-written guides: a file without the marker is the
// user's, and no button may replace it.
func TestRegenerationRefusesToOverwriteHandWrittenGuide(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".elasticclaw", "workspaces", "agent-race")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := "schema_version: v1\nname: agent-race\nrepositories:\n    - repo: nicoprofe/agent-race\n      permissions: write\nenv: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "elasticclaw-config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# my own notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{}
	_, err := s.generateWorkspaceRepoGuide("agent-race")
	if err == nil || !strings.Contains(err.Error(), "written by hand") {
		t.Fatalf("expected a hand-written refusal, got: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if string(content) != "# my own notes\n" {
		t.Error("the hand-written file was modified")
	}
}

func TestGuideManifestCandidatesAreBoundedAndRelevant(t *testing.T) {
	tree := []string{"pyproject.toml", "web/package.json", "node_modules/x/package.json", "docs/openapi.yaml"}
	got := guideManifestCandidates(tree)
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "node_modules") {
		t.Error("node_modules manifests must never be fetched")
	}
	if !strings.Contains(joined, "docs/openapi.yaml") {
		t.Error("committed OpenAPI documents should be fetched wherever they live")
	}
}
