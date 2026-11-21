// Command tallyguru-agent keeps the AI apps connected without anyone typing a command. It
// starts at Windows sign-in (set up by the installer), has no window, and every two minutes
// runs `tallyguru connect -quiet`: if Claude Code or Claude Desktop lost the tool (installed
// after this tool, or Claude wrote its old settings back), it is added again; when nothing is
// missing, nothing is written. It reads nothing else and sends nothing anywhere.
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

func main() {
	if !firstInstance() {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	exe := filepath.Join(filepath.Dir(self), "tallyguru.exe")
	args := append(os.Args[1:], "connect", "-quiet") // e.g. -data "C:\Users\x\TallyGuru"
	for {
		if _, err := os.Stat(exe); err != nil {
			return // the tool was uninstalled
		}
		cmd := exec.Command(exe, args...)
		hide(cmd)
		cmd.Run()
		time.Sleep(Every)
	}
}
