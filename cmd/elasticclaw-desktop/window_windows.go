//go:build windows

package main

import (
	"errors"

	"github.com/jchv/go-webview2"
)

// runWindow opens the app in a Win32 window hosting WebView2.
//
// go-webview2 is pure Go, so the Windows build needs no cgo and stays a single
// self-contained executable. The other platforms have no equivalent and go through
// cgo instead; see window_unix.go.
func runWindow(url string) error {
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
		// WebView2 ships with Windows 11 and current Windows 10, but a stripped or
		// very old install may not have it. Say so instead of exiting silently.
		return errors.New("Could not create the application window.\n\n" +
			"This needs the Microsoft Edge WebView2 runtime, which is normally part of Windows.\n" +
			"Install it from:\nhttps://developer.microsoft.com/microsoft-edge/webview2/\n\n" +
			"You can also run the server and use it in a browser:\n  elasticclaw hub")
	}
	defer w.Destroy()

	// Paint the caption in the app's colours rather than the user's accent colour,
	// and put our own icon in the title bar instead of the generic glyph.
	applyBrandTitleBar(w.Window())
	applyWindowIcon(w.Window())

	w.Navigate(url)
	w.Run() // blocks until the user closes the window
	return nil
}
