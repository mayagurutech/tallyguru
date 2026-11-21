package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/mcp"
)

var allowForm = json.RawMessage(`{"type":"object","properties":{` +
	`"typed":{"type":"string","title":"Type ` + config.LiveWord + ` to allow posting to this company"}},"required":["typed"]}`)

// allowPost is the only gate before anything is sent to Tally for a company other than TEST
// MCP. The first time, the user types ALLOW WRITES in a form the AI app shows them (the AI
// cannot fill it); the answer is saved for that company. Without forms: the control panel.
func (e *Env) allowPost(company string) error {
	if c, err := config.Load(e.Root); err == nil { // the panel may have switched it on since start
		config.SetLiveWrites(c.LiveWrites)
	}
	if config.CheckWrite(company) == nil {
		return nil
	}
	if e.Confirm != nil {
		yes, err := e.Confirm("LedgerGuru: allow posting to "+company, fmt.Sprintf("Allow posting to %s in TallyPrime?\n\n"+
			"Until now only the test company %q could be posted to. Every entry is still shown to you first and posted only when you confirm. "+
			"Take a Tally backup (Alt+Y) before posting. You can switch this off again in the control panel (Drafts and writes).\n\nClick Yes to allow.", company, config.TestCompany))
		if err != nil {
			return fmt.Errorf("could not show the dialog: %v", err)
		}
		if !yes {
			return fmt.Errorf("not posted: posting to %q was not allowed (No was clicked)", company)
		}
		return config.AllowWritesFor(e.Root, company)
	}
	var ans mcp.Elicitation
	err := mcp.ErrNoElicit
	if e.Ask != nil {
		ans, err = e.Ask(fmt.Sprintf("Allow posting to %s in TallyPrime?\n\nUntil now only the test company %q could be posted to. "+
			"Every entry is still shown to you first and posted only when you confirm. Take a Tally backup (Alt+Y) before posting. "+
			"You can switch this off again in the control panel (Drafts and writes).", company, config.TestCompany), allowForm)
	}
	if errors.Is(err, mcp.ErrNoElicit) {
		if e.OpenPanel != nil && e.OpenPanel() == nil {
			return fmt.Errorf("posting to %q is not allowed yet. The control panel opened: in %s's Drafts and writes, type %s, then ask again", company, company, config.LiveWord)
		}
		return config.CheckWrite(company)
	}
	if err != nil {
		return err
	}
	typed, _ := ans.Content["typed"].(string)
	if ans.Action != "accept" || !strings.EqualFold(strings.TrimSpace(typed), config.LiveWord) {
		return fmt.Errorf("not posted: posting to %q was not allowed (the user must type %s)", company, config.LiveWord)
	}
	return config.AllowWritesFor(e.Root, company)
}
