//go:build windows

package cmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/inconshreveable/mousetrap"
	"github.com/spf13/cobra"
)

func init() {
	// Cobra's default behaviour for a Windows binary launched from Explorer is to
	// print "This is a command line tool. You need to open cmd.exe and run it from
	// there." and quit. That is a dead end for anyone who downloaded the exe and
	// double-clicked it, so suppress it and handle the case ourselves below.
	cobra.MousetrapHelpText = ""
}

// startedByExplorer reports whether this process was launched by double-clicking
// in Explorer rather than from an existing terminal.
func startedByExplorer() bool {
	return mousetrap.StartedByExplorer()
}

// runDesktopLaunch handles a double-click: start the server and open the
// dashboard, which is the only thing a GUI launch can usefully do for a tool
// whose main artifact is a local web UI.
func runDesktopLaunch() {
	const addr = "127.0.0.1:8080"
	url := "http://" + addr

	fmt.Println()
	fmt.Println("  ElasticClaw " + Version)
	fmt.Println()
	fmt.Println("  Starting the server and opening the dashboard at " + url)
	fmt.Println("  Keep this window open while you use it. Press Ctrl-C to stop.")
	fmt.Println()
	fmt.Println("  This is also a command-line tool. For everything else, open a")
	fmt.Println("  terminal and run: elasticclaw --help")
	fmt.Println()

	// Open the browser once the port actually accepts connections, so the first
	// request does not land before the server is listening.
	go func() {
		if !waitForListener(addr, 30*time.Second) {
			fmt.Println("  Server did not start in time; open " + url + " manually.")
			return
		}
		openBrowser(url)
	}()

	// Hand off to the normal hub command so there is exactly one code path that
	// knows how to run the server.
	rootCmd.SetArgs([]string{"hub", "--addr", addr})
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "\n  Failed to start: "+err.Error())
		fmt.Fprintln(os.Stderr, "  Press Enter to close this window.")
		fmt.Scanln()
		os.Exit(1)
	}
}

// waitForListener polls until something accepts TCP connections on addr.
func waitForListener(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// openBrowser opens url in the user's default browser. rundll32 is used rather
// than "cmd /c start" because it needs no shell quoting and shows no extra window.
func openBrowser(url string) {
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		fmt.Println("  Could not open a browser automatically; go to " + url)
	}
}
