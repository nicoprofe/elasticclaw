package procutil

import (
	"os/exec"
	"runtime"
	"testing"
)

// A GUI process owns no console, so Windows allocates one per console child. The
// docker provider spawns many per agent run, which made windows flash open and
// shut continuously while an agent was working.
func TestHideSuppressesTheConsoleWindow(t *testing.T) {
	cmd := Hide(exec.Command("docker", "ps"))
	if cmd == nil {
		t.Fatal("Hide returned nil")
	}
	if runtime.GOOS != "windows" {
		if cmd.SysProcAttr != nil {
			t.Error("nothing should be set away from Windows")
		}
		return
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr not set — the child would open a console window")
	}
}

func TestHideToleratesNil(t *testing.T) {
	if Hide(nil) != nil {
		t.Error("Hide(nil) should stay nil rather than panic")
	}
}
