//go:build linux && !desktopgui

package main

import "errors"

// runWindow reports that this binary was built without GUI support.
//
// The Linux window backend needs GTK and WebKitGTK headers at compile time, which
// most machines and every CI job here does not have. Rather than make the whole
// module unbuildable on those machines, the real backend sits behind the
// `desktopgui` build tag and this stands in when the tag is absent — so `go build
// ./...` and `go test ./...` work everywhere, and a binary that cannot open a window
// says so instead of failing to link.
func runWindow(string) error {
	return errors.New("This build has no graphical support.\n\n" +
		"The Linux desktop binary is built with -tags desktopgui against\n" +
		"libwebkit2gtk-4.1; this one was not. Run the hub and use the dashboard\n" +
		"in a browser instead:\n  elasticclaw hub")
}
