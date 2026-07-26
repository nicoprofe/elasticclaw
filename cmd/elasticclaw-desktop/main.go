//go:build windows

// Command elasticclaw-desktop is the native Windows desktop application.
//
// It is a separate binary from elasticclaw.exe on purpose. A CLI needs the
// console subsystem so its output appears in a terminal; a desktop app must be
// linked with -H=windowsgui so double-clicking it does not flash up a console
// window. One executable cannot be both.
//
// The window is a real Win32 window hosting WebView2 (the Edge engine that ships
// with Windows 10 and 11) — not a browser process, so there is no address bar,
// no tab strip, and no dependency on which browser is installed.
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/elasticclaw/elasticclaw/cmd"
	"github.com/jchv/go-webview2"
)

// Version is stamped at build time and shown in Add or Remove Programs.
var Version = "dev"

func main() {
	// Installation runs before the log is redirected, so its output reaches a
	// console when invoked from one.
	if len(os.Args) > 1 {
		switch os.Args[1] {
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
	if len(os.Args) == 1 && maybeOfferInstall() {
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
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := "http://" + addr

	// Run the server through the CLI's own hub command so startup, config
	// discovery and migrations follow exactly one code path. Its lifetime is the
	// window's: closing the window exits the process and takes the server with it.
	go func() {
		os.Args = []string{"elasticclaw", "hub", "--addr", addr}
		cmd.Execute()
	}()

	if !waitForListener(addr, 60*time.Second) {
		fatal("The ElasticClaw server did not start.\n\nSee the log at:\n" + logPath)
		return
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title:  "ElasticClaw",
			Width:  1440,
			Height: 900,
			Center: true,
		},
	})
	if w == nil {
		// WebView2 ships with Windows 11 and current Windows 10, but a stripped
		// or very old install may not have it. Say so instead of exiting silently.
		fatal("Could not create the application window.\n\n" +
			"This needs the Microsoft Edge WebView2 runtime, which is normally part of Windows.\n" +
			"Install it from:\nhttps://developer.microsoft.com/microsoft-edge/webview2/\n\n" +
			"You can also run the server and use it in a browser:\n  elasticclaw hub")
		return
	}
	defer w.Destroy()

	// Paint the caption in the app's colours rather than the user's accent colour.
	applyBrandTitleBar(w.Window())
	applyWindowIcon(w.Window())

	w.Navigate(url)
	w.Run() // blocks until the user closes the window
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
