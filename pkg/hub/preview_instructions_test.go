package hub

import (
	"strings"
	"testing"

	"github.com/elasticclaw/elasticclaw/pkg/types"
)

// The preview contract used to be keyed to [DONE], which only the shipped
// single-stage example workflow ever sends. A workflow with a test gate announces
// its own marker ([PR_OPENED]) and then parks in a review stage, so its agent
// never reached the condition: the port was published and the URL allocated, but
// nothing marked the preview ready and no link ever appeared.
func TestPreviewInstructionsDoNotDependOnASpecificMarker(t *testing.T) {
	ctx := appendWorkflowPreviewContext("", &types.WorkflowPreview{Port: 3000, Label: "Open QA preview"})

	if strings.Contains(ctx, "before sending `[DONE]`") {
		t.Error("preview still gated on [DONE]; workflows using their own marker never trigger it")
	}
	if !strings.Contains(ctx, "the pull request being open is the trigger") {
		t.Error("instructions must name a condition every workflow reaches")
	}
}

// Marking the preview ready detaches and stops the agent. An agent that does it
// before announcing the workflow's marker is killed mid-flight and the pipeline
// stays in the stage it was in, so the ordering has to be stated.
func TestPreviewInstructionsStateTheOrderingHazard(t *testing.T) {
	ctx := appendWorkflowPreviewContext("", &types.WorkflowPreview{Port: 3000})

	if !strings.Contains(ctx, "Announce this workflow's completion marker first") {
		t.Error("ordering not stated: marking ready first strands the workflow")
	}
	if !strings.Contains(ctx, "ends your involvement") {
		t.Error("the agent is not warned that marking ready stops it")
	}
	// The port must still be concrete.
	if !strings.Contains(ctx, "3000") {
		t.Error("configured port missing from the instructions")
	}
}

func TestNoPreviewMeansNoPreviewSection(t *testing.T) {
	if got := appendWorkflowPreviewContext("existing context", nil); got != "existing context" {
		t.Errorf("context altered for a workflow with no preview: %q", got)
	}
}
