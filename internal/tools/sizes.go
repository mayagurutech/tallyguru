package tools

import (
	"encoding/json"

	"github.com/mayagurutech/tallyguru/internal/mcp"
)

func mustJSONTools(t []mcp.Tool) []byte { b, _ := json.Marshal(t); return b }

// ModuleTokens estimates each module's tool-definition size in tokens (about 4 characters a
// token), for the control panel's feature switches.
func ModuleTokens() map[string]int {
	e := &Env{}
	size := func(ts ...[]mcp.Tool) int {
		n := 0
		for _, t := range ts {
			n += len(mustJSONTools(t))
		}
		return n / 4
	}
	return map[string]int{
		"core": size(Core(e)), "mapping": size(Mapping(e)), "reports": size(Reports(e)), "audit": size(Audit(e)),
		"accounting": size(Accounting(e)), "masters": size(Masters(e)), "changes": size(Changes(e)),
		"bulk": size(Bulk(e), Rules(e)), "bank": size(Bank(e), Rules(e)),
	}
}
