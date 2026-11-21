// Package config holds settings and safety switches.
package config

import (
	"fmt"
	"os"
	"strings"
)

// TestCompany is the dummy company that is always writable.
const TestCompany = "TEST MCP"

// LiveWord must be typed by the user (panel, or a form the AI app shows the user) to allow
// posting to a company other than TEST MCP.
const LiveWord = "ALLOW WRITES"

// AllowWritesFor records that the user allowed posting to company (saved in config.json and
// applied at once in this process).
func AllowWritesFor(root, company string) error {
	c, err := Load(root)
	if err != nil {
		return err
	}
	for _, n := range c.LiveWrites {
		if strings.EqualFold(n, company) {
			SetLiveWrites(c.LiveWrites)
			return nil
		}
	}
	c.LiveWrites = append(c.LiveWrites, company)
	if err := c.Save(root); err != nil {
		return err
	}
	SetLiveWrites(c.LiveWrites)
	return nil
}

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
	return fmt.Errorf("posting to %q refused: posting works only on %q until the user allows it for %q (type %s when asked, or in the control panel)", name, TestCompany, name, LiveWord)
}
