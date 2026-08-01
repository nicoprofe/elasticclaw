//go:build linux && desktopgui

package main

// A GTK3 window hosting a WebKitGTK web view.
//
// This is hand-written rather than delegated to webview_go, which is what the macOS
// build uses. That library requests `webkit2gtk-4.0` through pkg-config with no way
// to select otherwise, and 4.0 has been dropped from Ubuntu 24.04, Debian 13 and
// current Fedora in favour of 4.1 — the two differ mainly in linking libsoup3
// instead of libsoup2, not in the handful of calls used here. Building against 4.0
// would produce a binary that cannot run on a current distribution, so this backend
// targets 4.1 directly.
//
// Consequence worth stating plainly: unlike the CLI and unlike the Windows desktop
// build, this binary is dynamically linked and needs libwebkit2gtk-4.1 present at
// runtime. The CLI remains a static single file on every platform.
//
// This file is behind the `desktopgui` build tag so that the ordinary `go build ./...`
// and `go test ./...` keep working on a Linux machine with no GTK or WebKit headers
// installed — which is every CI job in this repository except the one that builds the
// Linux desktop binary, and most contributors. Without the tag the package still
// compiles, via window_linux_nogui.go, and reports that it has no GUI support.

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1

#include <stdlib.h>
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

// ec_open_window creates the window and blocks in the GTK main loop until it is
// closed. It returns 0 on success, or 1 when there is no usable display.
static int ec_open_window(const char *url, const char *title, int width, int height) {
	// The window's WM_CLASS is what a dock or task switcher matches against the
	// StartupWMClass in the desktop entry. Left alone, GTK derives it from argv[0]
	// — "elasticclaw-desktop" — which matches nothing, so the running app appears as
	// a separate nameless icon next to its own launcher. These two calls set the
	// instance and class names, and must happen before gtk_init reads them.
	g_set_prgname("elasticclaw");
	gdk_set_program_class("ElasticClaw");

	// gtk_init_check rather than gtk_init: the latter calls exit() when it cannot
	// open a display, which would kill the process with no message the user can act
	// on. Returning lets Go report something useful instead.
	if (!gtk_init_check(NULL, NULL)) {
		return 1;
	}

	GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(window), title);
	// Resolved from the icon theme, where runInstall put a 512x512 PNG under
	// hicolor. Without it the window and the alt-tab switcher show a placeholder.
	gtk_window_set_icon_name(GTK_WINDOW(window), "elasticclaw");
	gtk_window_set_default_size(GTK_WINDOW(window), width, height);
	gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);

	GtkWidget *view = webkit_web_view_new();
	gtk_container_add(GTK_CONTAINER(window), view);

	webkit_web_view_load_uri(WEBKIT_WEB_VIEW(view), url);

	// Closing the window ends the main loop, which returns from this function and
	// lets main() exit — taking the in-process hub with it.
	g_signal_connect(window, "destroy", G_CALLBACK(gtk_main_quit), NULL);

	gtk_widget_show_all(window);
	gtk_main();
	return 0;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func runWindow(url string) error {
	curl := C.CString(url)
	defer C.free(unsafe.Pointer(curl))
	ctitle := C.CString("ElasticClaw")
	defer C.free(unsafe.Pointer(ctitle))

	if C.ec_open_window(curl, ctitle, 1440, 900) != 0 {
		return errors.New("Could not open a window: no display is available.\n\n" +
			"This build needs a graphical session. Over SSH, or on a server, run the\n" +
			"hub instead and open the dashboard in a browser:\n  elasticclaw hub")
	}
	return nil
}
