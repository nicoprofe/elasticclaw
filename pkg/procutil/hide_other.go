//go:build !windows

package procutil

import "os/exec"

// Hide is a no-op away from Windows, where consoles are not allocated per process.
func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }
