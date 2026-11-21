//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// startDetached starts this program again with args and does not wait for it.
func startDetached(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
