//go:build darwin

package main

import (
	webview "github.com/webview/webview_go"
)

// runWindow opens the app in a WKWebView, the same engine as Safari, which is part
// of macOS and needs nothing installed.
//
// This goes through cgo, so the macOS build has to be produced on macOS: linking
// against WebKit needs Apple's toolchain and cannot be cross-compiled from Linux.
// The CLI is unaffected and still cross-compiles to a static binary.
//
// Linux does not share this backend. webview_go pins webkit2gtk-4.0 through
// pkg-config, which Ubuntu 24.04 and other current distributions no longer package,
// so Linux has its own 4.1 backend in window_linux.go.
func runWindow(url string) error {
	w := webview.New(false)
	defer w.Destroy()

	w.SetTitle("ElasticClaw")
	w.SetSize(1440, 900, webview.HintNone)
	w.Navigate(url)
	w.Run() // blocks until the user closes the window
	return nil
}
