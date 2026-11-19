package tools

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/mapping"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/store"
)

// reNum hides amounts and shares (1.2 lakh, 3 cr, 45000 a year, 80%) but keeps
// section references such as 17(5), 194J(b) and 40A(3), which the user needs.
var reNum = regexp.MustCompile(`\d[\d.,]*( lakh| cr)( a year)?|\d[\d.,]* a year|\d[\d.,]*%`)

// MapPage is how many ledgers one review page shows.
const MapPage = 15 // keeps a page under the 500-token answer target

// Mapping returns the Phase 2 ledger-mapping tools. Only ledger names and groups are
// shown; never vouchers or amounts.
func Mapping(e *Env) []mcp.Tool {
	fam := `"family":{"type":"string","enum":["flag","tds","gst","tax"]}`
	ts := []mcp.Tool{
		{Name: "mapping_status", Description: "Ledger tag mapping progress (TDS, GST, tax, audit flag): decided from the books vs needing the user.",
			InputSchema: schema(), Handler: e.mappingStatus},
		{Name: "mapping_review", Description: "Ledgers the books leave in doubt, with suggestion and reason (15 per page).",
			InputSchema: schema(fam, `"page":{"type":"integer"}`), Handler: e.mappingReview},
		{Name: "mapping_set", Description: "Set the user's answer for listed ledgers. Ask the user first.",
			InputSchema: schema(fam, `"ledgers":{"type":"array","items":{"type":"string"}}`, `"tag":{"type":"string"}`),
			Handler:     e.mappingSet},
	}
	for i := range ts {
		ts[i].Handler = e.withNote(ts[i].Handler)
	}
	return ts
}

func (e *Env) openMap(a map[string]any) (*store.DB, error) {
	db, err := e.open(a, false)
	if err != nil {
		return nil, err
	}
	if err := db.RefreshMapping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (e *Env) mappingStatus(a map[string]any) (string, error) {
	db, err := e.openMap(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	rs, err := db.MapRows("", false)
	if err != nil {
		return "", err
	}
	type cnt struct{ user, auto, ask int }
	c := map[string]*cnt{}
	for _, f := range mapping.Families {
		c[f] = &cnt{}
	}
	for _, r := range rs {
		x := c[r.Family]
		switch {
		case r.Confirmed != "":
			x.user++
		case r.Conf == store.Ask:
			x.ask++
		default:
			x.auto++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Ledger mapping: %s\nFamily | Set by user | Decided from books | Needs the user\n", db.Label.Company)
	pending := 0
	for _, f := range mapping.Families {
		x := c[f]
		pending += x.ask
		fmt.Fprintf(&b, "%s | %d | %d | %d\n", f, x.user, x.auto, x.ask)
	}
	if pending == 0 {
		b.WriteString("Nothing needs the user.\n")
	} else {
		fmt.Fprintf(&b, "%d need the user: show them with mapping_review.\n", pending)
	}
	b.WriteString("Allowed tags:\n")
	for _, f := range mapping.Families {
		fmt.Fprintf(&b, "%s: %s\n", f, strings.Join(mapping.Tags[f], ", "))
	}
	return b.String(), nil
}

func (e *Env) mappingReview(a map[string]any) (string, error) {
	db, err := e.openMap(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	f := str(a, "family")
	all, err := db.MapRows(f, true)
	if err != nil {
		return "", err
	}
	var rs []store.MapRow
	for _, r := range all {
		if r.Conf == store.Ask {
			rs = append(rs, r)
		}
	}
	if len(rs) == 0 {
		return "Nothing pending" + map[bool]string{true: " in " + f, false: ""}[f != ""] + ".", nil
	}
	page := 1
	if p, ok := a["page"].(float64); ok && p >= 1 {
		page = int(p)
	}
	lo := (page - 1) * MapPage
	if lo >= len(rs) {
		return fmt.Sprintf("Only %d pages.", (len(rs)+MapPage-1)/MapPage), nil
	}
	hi := min(lo+MapPage, len(rs))
	var b strings.Builder
	fmt.Fprintf(&b, "Needs the user %d-%d of %d\nLedger | Group | Family | Suggested | Why\n", lo+1, hi, len(rs))
	for _, r := range rs[lo:hi] {
		// Amounts stay on this PC: the AI gets names, groups and the reason without figures.
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s\n", e.show(db, r.Name, r.Parent), r.Parent, r.Family, r.Tag, reNum.ReplaceAllString(r.Reason, "#"))
	}
	return b.String(), nil
}

func (e *Env) mappingSet(a map[string]any) (string, error) {
	db, err := e.openMap(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	f := str(a, "family")
	if f == "" {
		return "", fmt.Errorf("give family, tag and ledgers")
	}
	tag, ok := mapping.Valid(f, str(a, "tag"))
	if !ok {
		return "", fmt.Errorf("tag for %s must be one of: %s", f, strings.Join(mapping.Tags[f], ", "))
	}
	var names []string
	if raw, ok := a["ledgers"].([]any); ok {
		for _, x := range raw {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				names = append(names, strings.TrimSpace(s))
			}
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("list the ledgers")
	}
	var done int
	var miss []string
	for _, n := range names {
		real := n
		if e.Cfg.PrivacyMode {
			if p := db.PartyName(n); p != "" {
				real = p
			}
		}
		ok, err := db.SetTag(real, f, tag, "ai", e.Now())
		if err != nil {
			return "", err
		}
		if ok {
			done++
		} else {
			miss = append(miss, n)
		}
	}
	out := fmt.Sprintf("Set %s = %s for %d ledger(s).", f, tag, done)
	if len(miss) > 0 {
		out += " Not found or family not applicable: " + strings.Join(miss, ", ") + "."
	}
	return out, nil
}
