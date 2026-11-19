// Package config holds settings and safety switches.
package config

import (
	"fmt"
	"os"
	"strings"
)

// TestCompany is the dummy company that is always writable.
const TestCompany = "TEST MCP"

// AllowLiveWrites reports whether ALLOW_LIVE_WRITES=1 is set. Reading never needs a switch.
func AllowLiveWrites() bool { return os.Getenv("ALLOW_LIVE_WRITES") == "1" }

// liveWrites are companies the user switched live writes on for in the control panel.
var liveWrites = map[string]bool{}

// SetLiveWrites loads the per-company switches from the config.
func SetLiveWrites(names []string) {
	liveWrites = map[string]bool{}
	for _, n := range names {
		liveWrites[strings.ToLower(strings.TrimSpace(n))] = true
	}
}

// CheckWrite refuses writes to any company other than TEST MCP unless the user switched live
// writes on for it (control panel) or ALLOW_LIVE_WRITES=1.
func CheckWrite(name string) error {
	if strings.EqualFold(strings.TrimSpace(name), TestCompany) || AllowLiveWrites() || liveWrites[strings.ToLower(strings.TrimSpace(name))] {
		return nil
	}
	return fmt.Errorf("writing to %q refused: writes work only on %q (switch on live writes for it in the control panel)", name, TestCompany)
}
