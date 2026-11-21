// Command tallyguru-agent keeps the AI apps connected and the tool up to date without anyone
// typing a command. It starts at Windows sign-in (set up by the installer) and has no window.
//
//   - Every two minutes it runs `tallyguru connect -quiet`: if Claude Code or Claude Desktop lost
//     the tool (installed after this tool, or Claude wrote its old settings back), it is added
//     again; when nothing is missing, nothing is written.
//   - 10 minutes after sign-in and then every 6 hours it runs `tallyguru update -quiet`: a newer
//     GitHub release is installed quietly, but only while Claude and the control panel are not
//     using the tool (else it tries again 15 minutes later). Only public release files are
//     downloaded; nothing about the user or their data is sent anywhere.
//
// Build for Windows with -ldflags -H=windowsgui (no console window).
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Every is how often the connection is checked.
const Every = 2 * time.Minute

// Update timing: first try after sign-in, normal interval, retry while the tool is in use.
const (
	FirstUpdate = 10 * time.Minute
	UpdateEvery = 6 * time.Hour
	RetryInUse  = 15 * time.Minute
)

// exitInUse is what `tallyguru update` returns while Claude or the panel uses the tool.
const exitInUse = 3

func main() {
	if !firstInstance() {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	exe := filepath.Join(filepath.Dir(self), "tallyguru.exe")
	base := os.Args[1:] // e.g. -data "C:\Users\x\TallyGuru"
	run := func(extra ...string) int {
		cmd := exec.Command(exe, append(append([]string{}, base...), extra...)...)
		hide(cmd)
		if err := cmd.Run(); err != nil {
			if x, ok := err.(*exec.ExitError); ok {
				return x.ExitCode()
			}
			return -1
		}
		return 0
	}
	nextUpdate := time.Now().Add(FirstUpdate)
	for {
		if _, err := os.Stat(exe); err != nil {
			return // the tool was uninstalled
		}
		run("connect", "-quiet")
		if time.Now().After(nextUpdate) {
			if run("update", "-quiet") == exitInUse {
				nextUpdate = time.Now().Add(RetryInUse)
			} else {
				nextUpdate = time.Now().Add(UpdateEvery)
			}
		}
		time.Sleep(Every)
	}
}
