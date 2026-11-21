package main

import (
	"os"
	"os/exec"
	"syscall"
)

// startDetached starts this program again with args, with no console window, and does not
// wait for it (the control panel runs this way: only its browser page is seen).
func startDetached(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	const createNoWindow, detached = 0x08000000, 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | detached}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
