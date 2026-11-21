package crypt

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The old product name stays here on purpose: changing it would make existing keys unreadable.
var entropy = []byte("Tally MCP by Guru key v1")

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func take(out windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
}

const uiForbidden = 0x1 // CRYPTPROTECT_UI_FORBIDDEN

func protect(key []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob(key), nil, blob(entropy), 0, nil, uiForbidden, &out); err != nil {
		return nil, err
	}
	return take(out), nil
}

func unprotect(p []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(p), nil, blob(entropy), 0, nil, uiForbidden, &out); err != nil {
		return nil, err
	}
	return take(out), nil
}
