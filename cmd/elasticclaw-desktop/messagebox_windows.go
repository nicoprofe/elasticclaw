//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// messageBox shows a native modal dialog. A -H=windowsgui binary has no console,
// so this is the only way a startup failure can reach the user.
func messageBox(title, text string) {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	textPtr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}

	const (
		mbOK            = 0x00000000
		mbIconError     = 0x00000010
		mbSetForeground = 0x00010000
	)

	proc := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	proc.Call(
		0, // no owner window
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbOK|mbIconError|mbSetForeground,
	)
}

// askYesNo shows a Yes/No dialog and reports whether the user chose Yes.
// Installing creates shortcuts and registry entries, so it is asked rather than
// done silently — but it is one click, because the alternative is a downloaded
// exe that never becomes an installed app.
func askYesNo(title, text string) bool {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	textPtr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return false
	}

	const (
		mbYesNo         = 0x00000004
		mbIconQuestion  = 0x00000020
		mbSetForeground = 0x00010000
		idYes           = 6
	)

	proc := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	ret, _, _ := proc.Call(
		0,
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbYesNo|mbIconQuestion|mbSetForeground,
	)
	return int(ret) == idYes
}
