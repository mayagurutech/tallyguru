package tools

import "github.com/mayagurutech/tallyguru/internal/mcp"

func names(ts []mcp.Tool) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

// PostingTools send entries to Tally; AI apps keep asking the user before each use.
var PostingTools = map[string]bool{"drafts": true, "change_voucher": true}

// NoPromptTools lists every tool except the posting ones: the installer lets Claude Code use
// them without asking each time (reading, syncing, matching, drafting stay on this PC or
// only read Tally).
func NoPromptTools() []string {
	e := &Env{}
	var all []string
	seen := map[string]bool{}
	for _, ts := range [][]string{names(Core(e)), names(Mapping(e)), names(Reports(e)), names(Audit(e)),
		names(Accounting(e)), names(Changes(e)), names(Masters(e)), names(Bulk(e)), names(Bank(e)), names(Rules(e))} {
		for _, n := range ts {
			if !seen[n] && !PostingTools[n] {
				seen[n] = true
				all = append(all, n)
			}
		}
	}
	return all
}
