package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// firstInstance holds a named mutex for this sign-in session; a second copy exits.
func firstInstance() bool {
	name, _ := syscall.UTF16PtrFromString(`Local\TallyGuruAgent`)
	h, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	const errorAlreadyExists = 183
	return h != 0 && err != syscall.Errno(errorAlreadyExists)
}
