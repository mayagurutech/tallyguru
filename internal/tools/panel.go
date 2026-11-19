package tools

import "fmt"

// Panel runs a write action from the local control panel through the same code, checks and
// confirmations as the AI's tools: drafts (list/post/discard), a change with its typed word,
// and rules. Nothing else is reachable this way.
func (e *Env) Panel(tool string, a map[string]any) (string, error) {
	e.Cfg.NoAutoRefresh = true // the panel shows the books as synced; posting itself re-syncs
	switch tool {
	case "drafts":
		return e.drafts(a)
	case "change":
		if _, ok := a["draft_id"].(float64); !ok || str(a, "confirm") == "" {
			return "", fmt.Errorf("a change needs its draft id and the typed word")
		}
		return e.changeVoucher(a)
	case "rules":
		return e.rules(a)
	}
	return "", fmt.Errorf("unknown action %q", tool)
}
