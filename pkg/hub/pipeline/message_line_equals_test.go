package pipeline

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The test stage of a manual-task workflow is guarded by a marker the agent
// prints. The trigger key was parsed by a switch with no default, so
// message_line_equals fell through into an empty Trigger: the stage became
// unreachable, the run sat in "working" forever, and nothing was logged. A real
// run reached [READY_TO_TEST] and then hung for nine minutes because of this.
func TestMessageLineEqualsEntersTheGuardedStage(t *testing.T) {
	p, err := Parse([]byte(`
stages:
    - id: working
      entry: true
    - id: test
      triggers:
        - message_line_equals: '[READY_TO_TEST]'
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := p.Stages[1].Triggers[0].MessageLineEquals; got != "[READY_TO_TEST]" {
		t.Fatalf("MessageLineEquals = %q — the key was dropped during parsing", got)
	}

	stage := p.StageForMessageContains("All checks green.\n[READY_TO_TEST]")
	if stage == nil || stage.ID != "test" {
		t.Fatalf("marker on its own line did not enter the test stage (got %v)", stage)
	}
}

// Why this is not message_contains: the agent states its intent to emit the
// marker while planning, before doing any work. A substring match would run the
// test gate against an untouched branch and fail the run at its first step.
func TestPlanningProseDoesNotFireTheMarker(t *testing.T) {
	p, err := Parse([]byte(`
stages:
    - id: working
      entry: true
    - id: test
      triggers:
        - message_line_equals: '[READY_TO_TEST]'
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	plan := "Rough plan:\n1. Read README.md.\n" +
		"When the implementation is ready for the full test gate, I'll say [READY_TO_TEST].\n" +
		"I'll wait for the hub's proceed before doing tool work."
	if stage := p.StageForMessageContains(plan); stage != nil {
		t.Errorf("planning prose entered stage %q — the run would test an empty branch", stage.ID)
	}
}

// The marker is emitted by a language model, so it does not arrive as clean
// bytes. Every form here is one an agent actually produces.
func TestMarkerToleratesAgentMarkdown(t *testing.T) {
	for _, line := range []string{
		"[READY_TO_TEST]",
		"  [READY_TO_TEST]  ",
		"`[READY_TO_TEST]`",
		"**[READY_TO_TEST]**",
		"[READY_TO_TEST].",
		"[ready_to_test]",
	} {
		if !hasLineEqual("Work done.\n"+line, "[READY_TO_TEST]") {
			t.Errorf("marker written as %q was not recognised", line)
		}
	}
	// A line that merely mentions the marker is still not the marker.
	if hasLineEqual("say [READY_TO_TEST] when done", "[READY_TO_TEST]") {
		t.Error("a sentence containing the marker was treated as the marker")
	}
}

// An unsupported or misspelled trigger key used to parse into a Trigger with no
// condition set, producing a stage nothing could ever enter. That is a config
// error and must be reported when the workflow loads.
func TestUnknownTriggerKeyIsRejected(t *testing.T) {
	_, err := Parse([]byte(`
stages:
    - id: test
      triggers:
        - message_line_eqals: '[TYPO]'
`))
	if err == nil {
		t.Fatal("a misspelled trigger key was accepted, so the stage is silently unreachable")
	}
	if !strings.Contains(err.Error(), "unknown trigger") {
		t.Errorf("error should name the problem, got: %v", err)
	}
	// The message must list what is allowed, or the user cannot fix it.
	if !strings.Contains(err.Error(), "message_line_equals") {
		t.Errorf("error should list supported triggers, got: %v", err)
	}
}

func TestTriggerWithNoConditionIsRejected(t *testing.T) {
	var tr Trigger
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("{}"), &node); err != nil {
		t.Fatal(err)
	}
	// yaml.Unmarshal wraps the document; the mapping is the first content node.
	if err := tr.UnmarshalYAML(node.Content[0]); err == nil {
		t.Error("an empty trigger was accepted; the stage it guards can never be entered")
	}
}

// Triggers that already worked must keep working.
func TestExistingTriggerKeysStillParse(t *testing.T) {
	p, err := Parse([]byte(`
stages:
    - id: done
      triggers:
        - message_contains: '[DONE]'
    - id: merged
      triggers:
        - pr_merged: {}
    - id: closed
      triggers:
        - pr_closed: {}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.StageForMessageContains("finished [DONE] here") == nil {
		t.Error("message_contains regressed")
	}
	if p.StageForPRMerged() == nil {
		t.Error("pr_merged regressed")
	}
}
