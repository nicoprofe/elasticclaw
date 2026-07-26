//go:build windows

package procutil

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Hide stops a child console program from opening a visible console window.
//
// The desktop app is linked with -H=windowsgui and therefore owns no console, so
// Windows allocates a fresh one for every console program it starts. The docker
// provider shells out to the docker CLI many times per agent run — exec, cp,
// inspect, logs — so a running agent made windows flash open and shut continuously.
//
// CREATE_NO_WINDOW is the documented way to run a console program with no console
// at all, rather than creating one and hiding it after the fact, which still
// flickers.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	if cmd == nil {
		return cmd
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	return cmd
}
