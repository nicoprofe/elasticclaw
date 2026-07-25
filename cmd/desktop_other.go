//go:build !windows

package cmd

// On platforms without Explorer there is no GUI double-click to handle: the
// binary is always started from a shell.
func startedByExplorer() bool { return false }

func runDesktopLaunch() {}
