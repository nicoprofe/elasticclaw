//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Title bar appearance.
//
// A plain Win32 window gets its caption painted by Windows using the user's
// accent colour, so the app appeared with whatever the person had chosen — a
// bright red bar above a near-black UI in the case that prompted this. The window
// is ours, so it should look like the app rather than like a system default.
//
// Both attributes are best-effort: they exist from Windows 11 22H2, and an older
// build simply returns an error and keeps the default caption.
const (
	// DWMWA_USE_IMMERSIVE_DARK_MODE makes the caption's text and buttons light,
	// which is required for them to stay legible on a dark caption.
	dwmwaUseImmersiveDarkMode = 20
	// DWMWA_CAPTION_COLOR sets the caption background.
	dwmwaCaptionColor = 35
	// DWMWA_BORDER_COLOR sets the thin window border.
	dwmwaBorderColor = 34
)

// colorRef converts an RGB triple to the COLORREF layout DWM expects, which is
// 0x00BBGGRR — blue and red swapped relative to the usual hex notation.
func colorRef(r, g, b uint32) uint32 {
	return r | g<<8 | b<<16
}

// applyBrandTitleBar paints the caption in the app's own near-black instead of
// the user's accent colour.
func applyBrandTitleBar(hwnd unsafe.Pointer) {
	if hwnd == nil {
		return
	}
	dwmapi := windows.NewLazySystemDLL("dwmapi.dll")
	setAttr := dwmapi.NewProc("DwmSetWindowAttribute")

	set := func(attr uint32, value uint32) {
		v := value
		// Errors are ignored on purpose: on Windows 10 these attributes are not
		// supported and the window keeps the system caption, which is fine.
		_, _, _ = setAttr.Call(
			uintptr(hwnd),
			uintptr(attr),
			uintptr(unsafe.Pointer(&v)),
			unsafe.Sizeof(v),
		)
	}

	set(dwmwaUseImmersiveDarkMode, 1)
	// #09090b — the same near-black the dashboard and the app icon use.
	set(dwmwaCaptionColor, colorRef(0x09, 0x09, 0x0b))
	set(dwmwaBorderColor, colorRef(0x27, 0x27, 0x2a))
}
