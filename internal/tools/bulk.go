package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/write"
)

// MaxBulk is the most entries one draft_vouchers call takes; larger files go in parts.
const MaxBulk = 50

// Bulk returns the bulk import tool (module "bulk"). The AI reads the file; the server checks
// each entry and keeps drafts. Posting stays with the drafts tool.
func Bulk(e *Env) []mcp.Tool {
	return []mcp.Tool{{Name: "draft_vouchers",
		Description: "Bulk import: up to 50 entries per call (same fields as draft_voucher), read by you from the user's file. " +
			"group labels the import (post or discard with ids group:<name>). Returns counts, refused rows and totals, not previews.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp +
			`,"group":{"type":"string"},"entries":{"type":"array","items":{"type":"object"}}},"required":["group","entries"]}`),
		Handler: e.draftVouchers}}
}

func (e *Env) draftVouchers(a map[string]any) (string, error) {
	grp := str(a, "group")
	es, _ := a["entries"].([]any)
	if grp == "" || len(es) == 0 {
		return "", fmt.Errorf("give a group name and entries")
	}
	if len(es) > MaxBulk {
		return "", fmt.Errorf("%d entries: send at most %d per call", len(es), MaxBulk)
	}
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		return "", err
	}
	var ids []int64
	var refused, warned []string
	byType := map[string][2]int64{} // count, total
	from, to := "", ""
	for i, x := range es {
		m, _ := x.(map[string]any)
		row := i + 1
		if r, ok := m["row"].(float64); ok {
			row = int(r)
		}
		v, err := write.FromArgs(m)
		if err != nil {
			refused = append(refused, fmt.Sprintf("row %d: %v", row, err))
			continue
		}
		errs, warns := bk.Check(&v)
		if len(errs) > 0 {
			refused = append(refused, fmt.Sprintf("row %d: %s", row, strings.Join(errs, "; ")))
			continue
		}
		if err := write.Save(db.DB, &v); err != nil {
			return "", err
		}
		write.SetGroup(db.DB, v.ID, grp)
		ids = append(ids, v.ID)
		for _, w := range warns {
			if !strings.HasPrefix(w, "company state not set") {
				warned = append(warned, fmt.Sprintf("row %d (draft %d): %s", row, v.ID, w))
			}
		}
		t := byType[v.Type]
		byType[v.Type] = [2]int64{t[0] + 1, t[1] + v.Total()}
		if from == "" || v.Date < from {
			from = v.Date
		}
		if v.Date > to {
			to = v.Date
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Group %q: %d drafted", grp, len(ids))
	if len(ids) > 0 {
		fmt.Fprintf(&b, " (ids %d-%d, %s to %s)", ids[0], ids[len(ids)-1], from, to)
	}
	fmt.Fprintf(&b, ", %d refused.\n", len(refused))
	var types []string
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Fprintf(&b, "%s: %d, total %s\n", t, byType[t][0], amt(byType[t][1]))
	}
	list := func(title string, xs []string) {
		if len(xs) == 0 {
			return
		}
		b.WriteString(title + ":\n")
		for i, x := range xs {
			if i == e.top(a) {
				fmt.Fprintf(&b, "... %d more\n", len(xs)-i)
				break
			}
			b.WriteString("- " + x + "\n")
		}
	}
	list("Refused (fix and send again)", refused)
	list("Warnings", warned)
	b.WriteString("Show this summary to the user; post the group only after they confirm (drafts action post, ids group:" + grp + ").")
	return b.String(), nil
}
