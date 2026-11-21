package main

import (
	"syscall"
	"unsafe"
)

// confirmDialog shows a Windows Yes/No box on top of every window, straight to the person at
// this PC (the AI app and the AI never see or answer it). No is the default button. Used for
// the yearly authorisation and for allowing posting to a company (author's choice, 21 Nov 2026:
// Claude Code's desktop app closed MCP forms without showing them).
var confirmDialog = winConfirm

func winConfirm(title, text string) (bool, error) {
	const (
		mbYesNo         = 0x00000004
		mbIconQuestion  = 0x00000020
		mbDefButton2    = 0x00000100
		mbSystemModal   = 0x00001000
		mbSetForeground = 0x00010000
		mbTopmost       = 0x00040000
		idYes           = 6
	)
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false, err
	}
	m, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return false, err
	}
	r, _, callErr := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)),
		mbYesNo|mbIconQuestion|mbDefButton2|mbSystemModal|mbSetForeground|mbTopmost)
	if r == 0 {
		return false, callErr
	}
	return r == idYes, nil
}
