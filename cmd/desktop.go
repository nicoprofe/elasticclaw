package cmd

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Start the server and open the dashboard in an app window",
	Long: `Starts the ElasticClaw Server on a free local port and opens the dashboard
in its own chromeless window, so it behaves like a desktop application.

This is what double-clicking elasticclaw.exe on Windows runs. Use it directly
when you want the app window from a terminal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDesktop()
	},
}

func init() {
	rootCmd.AddCommand(desktopCmd)
}

// desktopPortCandidates are tried in order before falling back to whatever port
// the OS hands out. 8080 is the hub's own default, but it collides constantly on
// developer machines — Docker Desktop and the WSL relay both claim it — so a
// failure to bind it must not be fatal.
var desktopPortCandidates = []int{8080, 8090, 18080, 18090}

// runDesktop starts the server and opens the dashboard as an app window.
func runDesktop() error {
	port, err := pickFreePort(desktopPortCandidates...)
	if err != nil {
		return fmt.Errorf("no free local port available: %w", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := "http://" + addr

	fmt.Println()
	fmt.Println("  ElasticClaw " + Version)
	fmt.Println()
	fmt.Println("  Dashboard: " + url)
	fmt.Println("  Keep this window open while you use the app. Press Ctrl-C to stop.")
	fmt.Println()
	fmt.Println("  This is also a command-line tool — run: elasticclaw --help")
	fmt.Println()

	// Open the window only once the port actually accepts connections, so the
	// first request cannot land before the server is listening.
	go func() {
		if !waitForListener(addr, 60*time.Second) {
			fmt.Println("  Server did not start in time. Open " + url + " manually.")
			return
		}
		if err := openAppWindow(url); err != nil {
			fmt.Println("  Could not open a window automatically. Open " + url + " manually.")
		}
	}()

	// Delegate to the hub command so server startup lives in exactly one place.
	rootCmd.SetArgs([]string{"hub", "--addr", addr})
	return rootCmd.Execute()
}

// pickFreePort returns the first candidate port that can be bound, or any free
// port if every candidate is taken.
func pickFreePort(candidates ...int) (int, error) {
	for _, p := range candidates {
		if isPortFree(p) {
			return p, nil
		}
	}
	// Let the OS choose. There is a small window between closing this listener
	// and the server binding it; if that loses a race the server reports the
	// bind error, which is preferable to guessing again.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func isPortFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	l.Close()
	return true
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

// runDesktopLaunch is the entry point for a GUI launch (a Windows double-click).
// It keeps the console open on failure, because that console closes with the
// process and would otherwise take the error message with it.
func runDesktopLaunch() {
	if err := runDesktop(); err != nil {
		fmt.Fprintln(os.Stderr, "\n  Failed to start: "+err.Error())
		fmt.Fprintln(os.Stderr, "  Press Enter to close this window.")
		fmt.Scanln()
		os.Exit(1)
	}
}
