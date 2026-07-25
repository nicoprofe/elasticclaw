package hub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	environmentPreflightTimeout    = 30 * time.Second
	defaultEnvironmentSetupTimeout = 10 * time.Minute
	environmentStateTimeout        = 10 * time.Second
	environmentReadyMarker         = "$HOME/.elasticclaw/workflow-environment-ready"
)

type environmentCommandRequirement struct {
	Command    string `json:"command"`
	Executable string `json:"executable"`
}

type environmentPreflightReport struct {
	Status              string   `json:"status"`
	Problems            []string `json:"problems"`
	Detected            []string `json:"detected"`
	RequiredExecutables []string `json:"requiredExecutables"`
}

var shellCommandSeparator = regexp.MustCompile(`[;&|\n]+`)
var shellAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
var safeExecutable = regexp.MustCompile(`^[A-Za-z0-9._/+:-]+$`)
var shellFileDescriptor = regexp.MustCompile(`^[0-9]+$`)

var ignoredShellCommands = map[string]bool{
	"!": true, "[": true, "[[": true, "break": true, "cd": true, "continue": true,
	"echo": true, "else": true, "exit": true, "export": true, "fi": true,
	"if": true, "printf": true, "return": true, "set": true, "shift": true,
	"test": true, "then": true, "true": true,
}

func workflowEnvironmentPreflightMode(value string) string {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		return "warn"
	}
	return mode
}

func workflowEnvironmentSetupTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultEnvironmentSetupTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("timeout must be positive")
	}
	return timeout, nil
}

func (s *Server) workflowEnvironmentApplies(clawID string) bool {
	ctx, ok := s.findPipelineContextForClaw(clawID)
	return ok && ctx.Workflow != nil
}

func (s *Server) workflowEnvironmentPrepared(clawID string) (bool, error) {
	command := fmt.Sprintf("if [ -f %s ]; then printf ready; else printf pending; fi", environmentReadyMarker)
	result, err := s.executePipelineCommand(clawID, command, environmentStateTimeout)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.Stdout) == "ready", nil
}

func (s *Server) markWorkflowEnvironmentPrepared(clawID string) error {
	command := fmt.Sprintf("mkdir -p \"$HOME/.elasticclaw\" && printf 'ready\\n' > %s", environmentReadyMarker)
	_, err := s.executePipelineCommand(clawID, command, environmentStateTimeout)
	return err
}

func (s *Server) runWorkflowEnvironmentSetup(clawID string) error {
	ctx, ok := s.findPipelineContextForClaw(clawID)
	if !ok || ctx.Workflow == nil || ctx.Workflow.Environment.Setup == nil {
		return nil
	}
	setup := ctx.Workflow.Environment.Setup
	command := strings.TrimSpace(setup.Command)
	if command == "" {
		return nil
	}
	timeout, err := workflowEnvironmentSetupTimeout(setup.Timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout %q: %w", setup.Timeout, err)
	}
	log.Printf("[environment] setup started for %s", clawID[:8])
	if _, err := s.executePipelineCommand(clawID, command, timeout); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	log.Printf("[environment] setup completed for %s", clawID[:8])
	return nil
}

func workflowEnvironmentCommandRequirements(ctx pipelineContext) []environmentCommandRequirement {
	pl := parsePipelineForContext(ctx)
	if pl == nil {
		return nil
	}
	seen := map[string]bool{}
	var requirements []environmentCommandRequirement
	for _, stage := range pl.Stages {
		command := strings.TrimSpace(stage.OnEnter.Run.Command)
		if command == "" {
			continue
		}
		for _, executable := range commandExecutables(command) {
			key := command + "\x00" + executable
			if seen[key] {
				continue
			}
			seen[key] = true
			requirements = append(requirements, environmentCommandRequirement{
				Command: command, Executable: executable,
			})
		}
	}
	return requirements
}

func commandExecutables(command string) []string {
	var executables []string
	seen := map[string]bool{}
	for _, segment := range shellCommandSeparator.Split(command, -1) {
		fields := strings.Fields(strings.TrimSpace(segment))
		for len(fields) > 0 {
			token := strings.Trim(fields[0], `"'()`)
			fields = fields[1:]
			if token == "" || shellAssignment.MatchString(token) {
				continue
			}
			if shellFileDescriptor.MatchString(token) {
				continue
			}
			if ignoredShellCommands[token] {
				break
			}
			switch token {
			case "command", "exec", "sudo":
				continue
			case "env":
				for len(fields) > 0 && shellAssignment.MatchString(fields[0]) {
					fields = fields[1:]
				}
				continue
			}
			if strings.HasPrefix(token, "-") || !safeExecutable.MatchString(token) {
				continue
			}
			if !seen[token] {
				seen[token] = true
				executables = append(executables, token)
			}
			break
		}
	}
	return executables
}

func (s *Server) runWorkflowEnvironmentPreflight(clawID string) error {
	ctx, ok := s.findPipelineContextForClaw(clawID)
	if !ok || ctx.Workflow == nil {
		return nil
	}
	mode := workflowEnvironmentPreflightMode(ctx.Workflow.Environment.Preflight)
	if mode == "off" {
		return nil
	}

	requirements := workflowEnvironmentCommandRequirements(ctx)
	command, err := buildEnvironmentPreflightCommand(requirements)
	if err != nil {
		return fmt.Errorf("build scanner: %w", err)
	}
	result, runErr := s.executePipelineCommand(clawID, command, environmentPreflightTimeout)
	if runErr != nil {
		if mode == "required" {
			return fmt.Errorf("scanner command failed: %w", runErr)
		}
		log.Printf("[environment] warning: scanner command failed for %s: %v", clawID[:8], runErr)
		return nil
	}
	var report environmentPreflightReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &report); err != nil {
		if mode == "required" {
			return fmt.Errorf("parse scanner report: %w", err)
		}
		log.Printf("[environment] warning: invalid scanner report for %s: %v", clawID[:8], err)
		return nil
	}
	if len(report.Problems) == 0 {
		log.Printf("[environment] preflight passed for %s (%d requirements detected)", clawID[:8], len(report.Detected))
		return nil
	}
	sort.Strings(report.Problems)
	summary := strings.Join(report.Problems, "; ")
	if mode == "required" {
		return fmt.Errorf("%s", summary)
	}
	log.Printf("[environment] preflight warning for %s: %s", clawID[:8], summary)
	return nil
}

func buildEnvironmentPreflightCommand(requirements []environmentCommandRequirement) (string, error) {
	requirementsJSON, err := json.Marshal(requirements)
	if err != nil {
		return "", err
	}
	script := base64.StdEncoding.EncodeToString([]byte(environmentPreflightJavaScript))
	input := base64.StdEncoding.EncodeToString(requirementsJSON)
	return fmt.Sprintf("node -e %s %s %s", shellQuote(
		"eval(Buffer.from(process.argv[1], 'base64').toString('utf8'))",
	), shellQuote(script), shellQuote(input)), nil
}

const environmentPreflightJavaScript = `
const fs = require('fs');
const path = require('path');
const cp = require('child_process');

const requirements = JSON.parse(Buffer.from(process.argv[2], 'base64').toString('utf8'));
const root = process.cwd();
const problems = [];
const detected = [];

function run(file, args, cwd = root, extraEnv = {}) {
  const result = cp.spawnSync(file, args, {
    cwd,
    encoding: 'utf8',
    env: { ...process.env, ...extraEnv },
  });
  return {
    ok: result.status === 0,
    output: ((result.stdout || '') + ' ' + (result.stderr || '')).trim(),
  };
}

function commandPath(executable) {
  if (executable.includes('/')) {
    const candidates = [path.resolve(root, executable)];
    for (const repo of repositories) candidates.push(path.resolve(repo, executable));
    return candidates.find((candidate) => fs.existsSync(candidate)) || '';
  }
  const result = cp.spawnSync('/bin/sh', ['-c', 'command -v "$1"', 'sh', executable], {
    cwd: root,
    encoding: 'utf8',
  });
  return result.status === 0 ? (result.stdout || '').trim() : '';
}

function parseVersion(value) {
  const match = String(value).match(/(\d+)\.(\d+)(?:\.(\d+))?/);
  return match ? [Number(match[1]), Number(match[2]), Number(match[3] || 0)] : null;
}

function below(installed, required) {
  for (let i = 0; i < 3; i++) {
    if (installed[i] < required[i]) return true;
    if (installed[i] > required[i]) return false;
  }
  return false;
}

function minimumVersion(spec) {
  const match = String(spec || '').match(/>=\s*(\d+\.\d+(?:\.\d+)?)/);
  return match ? parseVersion(match[1]) : null;
}

const entries = fs.readdirSync(root, { withFileTypes: true });
let repositories = entries
  .filter((entry) => entry.isDirectory() && fs.existsSync(path.join(root, entry.name, '.git')))
  .map((entry) => path.join(root, entry.name));
if (repositories.length === 0) repositories = [root];

const requiredExecutables = [...new Set(requirements.map((item) => item.executable))];
for (const item of requirements) {
  if (!commandPath(item.executable)) {
    problems.push('workflow command requires missing executable "' + item.executable + '": ' + item.command);
  }
}

for (const repo of repositories) {
  const name = path.basename(repo);
  const pyprojectPath = path.join(repo, 'pyproject.toml');
  if (fs.existsSync(pyprojectPath)) {
    const content = fs.readFileSync(pyprojectPath, 'utf8');
    const requiresMatch = content.match(/requires-python\s*=\s*["']([^"']+)["']/);
    const pythonRequirement = requiresMatch ? requiresMatch[1] : '';
    detected.push(name + ': Python project' + (pythonRequirement ? ' requires ' + pythonRequirement : ''));

    const venvPython = path.join(repo, '.venv', 'bin', 'python');
    const python = fs.existsSync(venvPython)
      ? venvPython
      : (commandPath('python') || commandPath('python3'));
    if (!python) {
      problems.push(name + ': Python project has no available interpreter');
    } else {
      const versionResult = run(python, ['--version'], repo);
      const installed = parseVersion(versionResult.output);
      const minimum = minimumVersion(pythonRequirement);
      if (minimum && installed && below(installed, minimum)) {
        problems.push(name + ': Python ' + versionResult.output + ' does not satisfy ' + pythonRequirement);
      }
      const needsPytest = requirements.some((item) => /\bpytest\b/.test(item.command));
      if (needsPytest) {
        const pytestResult = run(
          python,
          ['-m', 'pytest', '--version'],
          repo,
          { PYTEST_DISABLE_PLUGIN_AUTOLOAD: '1' },
        );
        if (!pytestResult.ok) problems.push(name + ': pytest is not installed for ' + python);
      }
    }
  }

  const packagePath = path.join(repo, 'package.json');
  if (fs.existsSync(packagePath)) {
    try {
      const pkg = JSON.parse(fs.readFileSync(packagePath, 'utf8'));
      const nodeRequirement = pkg.engines && pkg.engines.node ? String(pkg.engines.node) : '';
      detected.push(name + ': Node.js project' + (nodeRequirement ? ' requires ' + nodeRequirement : ''));
      const node = commandPath('node');
      if (!node) {
        problems.push(name + ': Node.js project has no node executable');
      } else {
        const nodeResult = run(node, ['--version'], repo);
        const installed = parseVersion(nodeResult.output);
        const minimum = minimumVersion(nodeRequirement);
        if (minimum && installed && below(installed, minimum)) {
          problems.push(name + ': Node.js ' + nodeResult.output + ' does not satisfy ' + nodeRequirement);
        }
      }
      const manager = fs.existsSync(path.join(repo, 'pnpm-lock.yaml')) ? 'pnpm'
        : fs.existsSync(path.join(repo, 'yarn.lock')) ? 'yarn'
        : fs.existsSync(path.join(repo, 'package-lock.json')) ? 'npm'
        : '';
      if (manager && !commandPath(manager)) problems.push(name + ': lockfile requires missing package manager "' + manager + '"');
    } catch (error) {
      problems.push(name + ': package.json could not be parsed: ' + error.message);
    }
  }

  const goModPath = path.join(repo, 'go.mod');
  if (fs.existsSync(goModPath)) {
    const content = fs.readFileSync(goModPath, 'utf8');
    const match = content.match(/^go\s+(\d+\.\d+(?:\.\d+)?)/m);
    const requirement = match ? match[1] : '';
    detected.push(name + ': Go project' + (requirement ? ' requires ' + requirement : ''));
    const go = commandPath('go');
    if (!go) {
      problems.push(name + ': Go project has no go executable');
    } else if (requirement) {
      const result = run(go, ['version'], repo);
      const installed = parseVersion(result.output);
      const minimum = parseVersion(requirement);
      if (installed && minimum && below(installed, minimum)) {
        problems.push(name + ': ' + result.output + ' does not satisfy Go ' + requirement);
      }
    }
  }

  const rustToolchain = ['rust-toolchain.toml', 'rust-toolchain']
    .find((file) => fs.existsSync(path.join(repo, file)));
  if (rustToolchain) {
    detected.push(name + ': Rust project declares ' + rustToolchain);
    if (!commandPath('rustc')) problems.push(name + ': Rust project has no rustc executable');
  }
}

const uniqueProblems = [...new Set(problems)].sort();
const uniqueDetected = [...new Set(detected)].sort();
const status = uniqueProblems.length === 0 ? 'compatible' : 'incompatible';
const markdown = [
  '# Repository Environment Preflight',
  '',
  '**Status:** ' + status,
  '',
  '## Detected',
  '',
  ...(uniqueDetected.length ? uniqueDetected.map((item) => '- ' + item) : ['- No supported manifests detected.']),
  '',
  '## Compatibility problems',
  '',
  ...(uniqueProblems.length ? uniqueProblems.map((item) => '- ' + item) : ['- None.']),
  '',
].join('\n');
fs.writeFileSync(path.join(root, 'REPO_ENVIRONMENT.md'), markdown, { mode: 0o600 });

const agentsPath = path.join(root, 'AGENTS.md');
const agentsText = fs.existsSync(agentsPath) ? fs.readFileSync(agentsPath, 'utf8') : '';
if (!agentsText.includes('REPO_ENVIRONMENT.md')) {
  fs.appendFileSync(
    agentsPath,
    (agentsText.endsWith('\n') || agentsText.length === 0 ? '' : '\n') +
    '\n## Repository Environments\n\nRead REPO_ENVIRONMENT.md before running repository commands.\n',
    { mode: 0o600 },
  );
}

process.stdout.write(JSON.stringify({
  status,
  problems: uniqueProblems,
  detected: uniqueDetected,
  requiredExecutables,
}));
`
