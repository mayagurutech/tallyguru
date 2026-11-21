package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/statuspage"
)

// exitInUse tells the background helper to try again soon: the tool is in use.
const exitInUse = 3

// runUpdate installs the newest release quietly, but only while nothing else runs this program
// (Claude's server, the panel): the installer would close them and cut Claude off mid-chat.
func runUpdate() error {
	if strings.HasSuffix(version, "-dev") {
		fmt.Println("Development build: update it from the source code.")
		return nil
	}
	if othersRunning() {
		fmt.Println("The tool is in use (Claude or the control panel); trying again later.")
		os.Exit(exitInUse)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tag, path, err := statuspage.FetchUpdate(ctx, version)
	if err != nil {
		return err
	}
	if path == "" {
		fmt.Printf("Up to date (%s).\n", version)
		return nil
	}
	if othersRunning() { // Claude may have started during the download
		fmt.Println("The tool is in use (Claude or the control panel); trying again later.")
		os.Exit(exitInUse)
	}
	// /auto=1: the installer does not open the control panel afterwards.
	cmd := exec.Command(path, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS", "/FORCECLOSEAPPLICATIONS", "/auto=1")
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("Installing %s quietly (you have %s).\n", tag, version)
	return nil
}

// othersRunning reports whether another copy of tallyguru.exe runs (Claude's server, the panel).
func othersRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq tallyguru.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return true // cannot tell: do not risk closing Claude's server
	}
	n := strings.Count(strings.ToLower(string(out)), "\"tallyguru.exe\"")
	return n > 1 // this process is one of them
}
