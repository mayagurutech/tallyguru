package main

import (
	"errors"
	"fmt"

	"github.com/mayagurutech/tallyguru/internal/statuspage"
	"github.com/mayagurutech/tallyguru/internal/tools"
)

// runConnect adds this server to the AI apps (the installer runs it after installing).
// Neither flag: every app found on this PC. An app not found is skipped, not an error.
func runConnect(root string, desktop, code bool) error {
	all := !desktop && !code
	var errs []error
	if desktop || all && statuspage.ClaudeDesktopInstalled() {
		if p, err := statuspage.ConnectClaude(root); err != nil {
			errs = append(errs, fmt.Errorf("Claude Desktop: %w", err))
		} else if p.Already {
			fmt.Println("Claude Desktop: already connected")
		} else {
			fmt.Println("Claude Desktop: connected (" + p.Path + ")")
		}
	} else if all {
		fmt.Println("Claude Desktop: not found on this PC, skipped")
	}
	if code || all && statuspage.ClaudeCodeInstalled() {
		if changed, err := statuspage.ConnectClaudeCode(root); err != nil {
			errs = append(errs, fmt.Errorf("Claude Code: %w", err))
		} else if changed {
			fmt.Println("Claude Code: connected (" + statuspage.ClaudeCodeConfig() + ")")
		} else {
			fmt.Println("Claude Code: already connected")
		}
		// Reading tools run without a prompt each time; posting tools still ask.
		if n, err := statuspage.AllowInClaudeCode(tools.NoPromptTools()); err != nil {
			errs = append(errs, fmt.Errorf("Claude Code permissions: %w", err))
		} else if n > 0 {
			fmt.Printf("Claude Code: %d reading tools allowed without asking (posting still asks)\n", n)
		}
	} else if all {
		fmt.Println("Claude Code: not found on this PC, skipped")
	}
	if len(errs) == 0 {
		fmt.Println("Quit the AI app fully and open it again, then ask: \"Connect my Tally company\".")
	}
	return errors.Join(errs...)
}
