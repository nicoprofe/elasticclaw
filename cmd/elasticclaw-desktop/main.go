// Command elasticclaw-desktop is the native desktop application.
//
// It is a separate binary from the elasticclaw CLI on purpose. A CLI needs the
// console subsystem so its output appears in a terminal; a desktop app must be
// linked with -H=windowsgui on Windows so double-clicking it does not flash up a
// console window. One executable cannot be both.
//
// The window is a native one hosting the platform's own web view — not a browser
// process, so there is no address bar, no tab strip, and no dependency on which
// browser is installed. Each platform supplies its own backend through
// runWindow: WebView2 on Windows, WKWebView on macOS, WebKitGTK on Linux.
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/elasticclaw/elasticclaw/cmd"
)

// Version is stamped at build time and shown in Add or Remove Programs.
var Version = "dev"

func main() {
	// LaunchServices appends a -psn_0_12345 process-serial-number argument when it
	// starts a bundled app on macOS. It is not a flag anyone passed, and leaving it
	// in argv makes a Finder launch look like a launch with arguments — which
	// silently skips the install offer below. Drop it before anything reads args.
	args := stripLaunchServicesArgs(os.Args[1:])

	// Installation runs before the log is redirected, so its output reaches a
	// console when invoked from one.
	if len(args) > 0 {
		switch args[0] {
		case "--install", "/install":
			if err := runInstall(); err != nil {
				fatal("Install failed.\n\n" + err.Error())
				os.Exit(1)
			}
			return
		case "--uninstall", "/uninstall":
			if err := runUninstall(); err != nil {
				fatal("Uninstall failed.\n\n" + err.Error())
				os.Exit(1)
			}
			return
		}
	}

	// Started with no arguments from outside the install directory — a
	// double-click on a freshly downloaded exe. Offer to install first, so the app
	// ends up in the Start menu instead of only ever running from Downloads.
	if len(args) == 0 && maybeOfferInstall() {
		return // the installed copy is now running in our place
	}

	// No console is attached, so anything written to stderr is lost. Send the
	// server's log somewhere the user can actually read after the fact.
	logPath, closeLog := openLog()
	defer closeLog()

	port, err := pickFreePort(8080, 8090, 18080, 18090)
	if err != nil {
		fatal("Could not find a free local port.\n\n" + err.Error())
		return
	}
	// Bind every interface, not just loopback. Agents run in Docker containers that
	// reach the host through host.docker.internal, which arrives from the Docker
	// network rather than over loopback — a hub bound to 127.0.0.1 refuses those
	// connections, so claw-bridge could never call back and every run died at
	// "Connect". The hub still requires its token, so exposure is authenticated.
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	// The window itself always uses loopback.
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Run the server through the CLI's own hub command so startup, config
	// discovery and migrations follow exactly one code path. Its lifetime is the
	// window's: closing the window exits the process and takes the server with it.
	go func() {
		os.Args = []string{"elasticclaw", "hub", "--addr", addr}
		cmd.Execute()
	}()

	if !waitForListener(fmt.Sprintf("127.0.0.1:%d", port), 60*time.Second) {
		fatal("The ElasticClaw server did not start.\n\nSee the log at:\n" + logPath)
		return
	}

	// The window backend is per-platform; everything above this line is not.
	if err := runWindow(url); err != nil {
		fatal(err.Error())
		return
	}
}

// openLog redirects the standard logger to a file next to the hub's own data,
// and returns the path so failures can point at it.
func openLog() (string, func()) {
	dir := "."
	if home, err := os.UserHomeDir(); err == nil {
		dir = filepath.Join(home, ".elasticclaw")
		_ = os.MkdirAll(dir, 0o700)
	}
	path := filepath.Join(dir, "desktop.log")
	// Append rather than truncate: two instances share this path, and truncating
	// let each one destroy the other's log — including the startup errors that are
	// the whole reason the file exists.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return path, func() {}
	}
	log.SetOutput(f)
	os.Stdout = f
	os.Stderr = f
	return path, func() { f.Close() }
}

// fatal shows a message box, because a windowsgui binary has nowhere to print.
func fatal(msg string) {
	log.Printf("fatal: %s", msg)
	messageBox("ElasticClaw", msg)
}

func pickFreePort(candidates ...int) (int, error) {
	for _, p := range candidates {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p, nil
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

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
