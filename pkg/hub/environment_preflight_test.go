package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elasticclaw/elasticclaw/pkg/types"
)

func TestWorkflowEnvironmentPreflightMode(t *testing.T) {
	for input, want := range map[string]string{
		"": "warn", "WARN": "warn", " required ": "required", "off": "off",
	} {
		if got := workflowEnvironmentPreflightMode(input); got != want {
			t.Fatalf("workflowEnvironmentPreflightMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWorkflowEnvironmentSetupTimeout(t *testing.T) {
	got, err := workflowEnvironmentSetupTimeout("")
	if err != nil {
		t.Fatal(err)
	}
	if got != 10*time.Minute {
		t.Fatalf("default setup timeout = %s, want 10m", got)
	}
	got, err = workflowEnvironmentSetupTimeout(" 15m ")
	if err != nil {
		t.Fatal(err)
	}
	if got != 15*time.Minute {
		t.Fatalf("configured setup timeout = %s, want 15m", got)
	}
	if _, err := workflowEnvironmentSetupTimeout("0s"); err == nil {
		t.Fatal("zero setup timeout should fail")
	}
}

func TestCommandExecutables(t *testing.T) {
	got := commandExecutables(`cd agent-race && python -m pytest -q; npm --prefix web test`)
	want := []string{"python", "npm"}
	if len(got) != len(want) {
		t.Fatalf("commandExecutables() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commandExecutables()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCommandExecutablesIgnoresControlFlowAndRedirections(t *testing.T) {
	command := `cd agent-race
if git diff --quiet &&
   git diff --cached --quiet &&
   git diff --quiet origin/main...HEAD; then
  echo "branch has no changes" >&2
  exit 1
fi
.venv/bin/python -m pytest -q`
	got := commandExecutables(command)
	want := []string{"git", ".venv/bin/python"}
	if len(got) != len(want) {
		t.Fatalf("commandExecutables() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commandExecutables()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWorkflowEnvironmentValidation(t *testing.T) {
	workflow := &types.WorkflowConfig{
		Name:        "test",
		Environment: types.WorkflowEnv{Preflight: "sometimes"},
	}
	if err := workflow.Validate(); err == nil || !strings.Contains(err.Error(), "environment.preflight") {
		t.Fatalf("Validate() error = %v, want environment.preflight error", err)
	}

	workflow.Environment = types.WorkflowEnv{Setup: &types.WorkflowEnvSetup{Timeout: "5m"}}
	if err := workflow.Validate(); err == nil || !strings.Contains(err.Error(), "environment.setup.command") {
		t.Fatalf("Validate() error = %v, want environment.setup.command error", err)
	}

	workflow.Environment.Setup = &types.WorkflowEnvSetup{Command: "true", Timeout: "never"}
	if err := workflow.Validate(); err == nil || !strings.Contains(err.Error(), "environment.setup.timeout") {
		t.Fatalf("Validate() error = %v, want environment.setup.timeout error", err)
	}

	workflow.Environment.Setup = &types.WorkflowEnvSetup{Command: "true", Timeout: "5m"}
	if err := workflow.Validate(); err != nil {
		t.Fatalf("valid environment setup rejected: %v", err)
	}
}

func TestEnvironmentPreflightDetectsMissingWorkflowExecutable(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "agent-race")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "pyproject.toml"), []byte(
		"[project]\nrequires-python = \">=3.12\"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	command, err := buildEnvironmentPreflightCommand([]environmentCommandRequirement{{
		Command: "python-not-installed -m pytest -q", Executable: "python-not-installed",
	}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("preflight command failed: %v", err)
	}
	var report environmentPreflightReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("parse report: %v\n%s", err, output)
	}
	if report.Status != "incompatible" {
		t.Fatalf("status = %q, want incompatible: %#v", report.Status, report)
	}
	if !strings.Contains(strings.Join(report.Problems, "\n"), "python-not-installed") {
		t.Fatalf("problems do not mention missing executable: %#v", report.Problems)
	}
	if _, err := os.Stat(filepath.Join(root, "REPO_ENVIRONMENT.md")); err != nil {
		t.Fatalf("REPO_ENVIRONMENT.md not written: %v", err)
	}
}
