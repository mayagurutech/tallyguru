package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/store"
)

// Reports returns the Phase 3 report tool: one tool with a short catalog, so new reports
// do not add tool definitions (token target).
func Reports(e *Env) []mcp.Tool {
	ts := []mcp.Tool{{Name: "run_report",
		Description: "Financial reports. name: mis (owner's one-page summary), pl (profit & loss), bs (balance sheet), ratios, cashflow, gst, tds (books only), ageing (kind receivable/payable; as on today), daybook (type: voucher type; default latest month). " +
			"Period: fy like 2026-27, or from/to months like 2026-04 (default: current year to date). " +
			"layout: tally (default) or schedule3. group: list ledgers of one group. excel: full detail to an Excel file on this PC (daybook, ageing).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp +
			`,"name":{"type":"string","enum":["mis","pl","bs","ratios","cashflow","gst","tds","ageing","daybook"]},"kind":{"type":"string","enum":["receivable","payable"]},"fy":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},` +
			`"layout":{"type":"string","enum":["tally","schedule3"]},"group":{"type":"string"},"type":{"type":"string"},"excel":{"type":"boolean"},"top":{"type":"integer"}},"required":["name"]}`),
		Handler: e.runReport}}
	for i := range ts {
		ts[i].Handler = e.withNote(ts[i].Handler)
	}
	return ts
}

func (e *Env) runReport(a map[string]any) (string, error) {
	db, err := e.open(a, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	switch str(a, "name") {
	case "ageing":
		return e.ageing(db, a)
	case "mis":
		return e.mis(db)
	}
	var first, last string
	db.QueryRow(`SELECT COALESCE(MIN(month),''), COALESCE(MAX(month),'') FROM month_ledger`).Scan(&first, &last)
	from := str(a, "from")
	if str(a, "name") == "daybook" && from == "" && str(a, "fy") == "" {
		from = last // Day Book default: the latest month
	}
	p, err := report.ParsePeriod(str(a, "fy"), from, str(a, "to"), first, last)
	if err != nil {
		return "", err
	}
	if str(a, "name") == "daybook" {
		return e.dayBook(db, a, p)
	}
	bk, err := report.Load(db.DB, db.Label.Company, p)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	name, layout := str(a, "name"), str(a, "layout")
	if g := str(a, "group"); g != "" {
		e.groupDetail(&b, db, bk, g, e.top(a))
	} else {
		switch {
		case name == "pl" && layout == "schedule3":
			e.s3PL(&b, bk)
		case name == "pl":
			e.tallyPL(&b, bk)
		case name == "bs" && layout == "schedule3":
			e.s3BS(&b, bk)
		case name == "bs":
			e.tallyBS(&b, bk)
		case name == "gst":
			if err := e.gst(&b, db, p); err != nil {
				return "", err
			}
		case name == "tds":
			if err := e.tds(&b, db, p); err != nil {
				return "", err
			}
		case name == "ratios":
			e.ratios(&b, bk)
		case name == "cashflow":
			st, err := report.LoadStart(db.DB, db.Label.Company, p)
			if err != nil {
				return "", err
			}
			e.cashFlow(&b, bk, st)
		default:
			return "", fmt.Errorf("unknown report %q: use pl, bs, ratios, cashflow, gst, tds or ageing", name)
		}
	}
	for _, w := range bk.Warnings() {
		b.WriteString("Warning: " + w + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// bracket shows a natural-side amount; a negative one (wrong side) in brackets.
func bracket(v int64) string {
	if v < 0 {
		return "(" + amt(-v) + ")"
	}
	return amt(v)
}

func groupRows(b *strings.Builder, gs []report.Group) (t int64) {
	for _, g := range gs {
		fmt.Fprintf(b, "%s | %s\n", g.Name, amt(g.Amount))
		t += g.Amount
	}
	return t
}

func (e *Env) tallyPL(b *strings.Builder, bk *report.Books) {
	r := bk.PLByGroup()
	fmt.Fprintf(b, "%s, profit & loss, %s (Tally groups)\n", bk.Company, bk.P.Label())
	b.WriteString("Trading: expenses | Amount\n")
	if r.OpenStock != 0 {
		fmt.Fprintf(b, "Opening stock | %s\n", amt(r.OpenStock))
	}
	groupRows(b, r.TradingExp)
	b.WriteString("Trading: income | Amount\n")
	groupRows(b, r.TradingInc)
	if r.CloseStock != 0 {
		fmt.Fprintf(b, "Closing stock | %s\n", amt(r.CloseStock))
	}
	fmt.Fprintf(b, "Gross %s | %s\n", profitWord(r.GrossProfit), amt(abs64(r.GrossProfit)))
	if len(r.IndirectExp) > 0 {
		b.WriteString("Indirect expenses | Amount\n")
		groupRows(b, r.IndirectExp)
	}
	if len(r.IndirectInc) > 0 {
		b.WriteString("Indirect income | Amount\n")
		groupRows(b, r.IndirectInc)
	}
	fmt.Fprintf(b, "Net %s | %s\n", profitWord(r.NetProfit), amt(abs64(r.NetProfit)))
}

func (e *Env) s3PL(b *strings.Builder, bk *report.Books) {
	inc, exp, pbt, tax, notes := bk.S3PL()
	fmt.Fprintf(b, "%s, statement of profit and loss, %s (Schedule III)\n", bk.Company, bk.P.Label())
	for _, s := range []report.Section{inc, exp} {
		fmt.Fprintf(b, "%s | Amount\n", s.Title)
		for _, l := range s.Lines {
			fmt.Fprintf(b, "%s | %s\n", l.Label, bracket(l.Amount))
		}
		fmt.Fprintf(b, "Total %s | %s\n", strings.ToLower(s.Title), bracket(s.Total))
	}
	fmt.Fprintf(b, "Profit before tax | %s\n", bracket(pbt))
	if tax != 0 {
		fmt.Fprintf(b, "Tax expense | %s\nProfit after tax | %s\n", bracket(tax), bracket(pbt-tax))
	}
	for _, n := range notes {
		b.WriteString("Note: " + n + "\n")
	}
}

func bsSides(b *strings.Builder, bs report.BS, sideTitles [2]string) {
	for i, side := range [][]report.Section{bs.Liab, bs.Assets} {
		var t int64
		fmt.Fprintf(b, "%s | Amount\n", sideTitles[i])
		for _, s := range side {
			if len(side) > 1 {
				if len(s.Lines) == 0 {
					continue
				}
				fmt.Fprintf(b, "%s: | %s\n", s.Title, bracket(s.Total))
			}
			for _, l := range s.Lines {
				fmt.Fprintf(b, "%s | %s", l.Label, bracket(l.Amount))
				if l.Note != "" {
					b.WriteString(" (" + l.Note + ")")
				}
				b.WriteString("\n")
			}
			t += s.Total
		}
		fmt.Fprintf(b, "Total | %s\n", bracket(t))
	}
	if bs.Diff == 0 {
		b.WriteString("Balanced: both sides agree.\n")
	} else {
		fmt.Fprintf(b, "Warning: sides differ by %s (difference in opening balances or stock not known).\n", bracket(bs.Diff))
	}
}

func (e *Env) tallyBS(b *strings.Builder, bk *report.Books) {
	fmt.Fprintf(b, "%s, balance sheet as at %s (Tally groups)\n", bk.Company, bk.P.End())
	bsSides(b, bk.BSByGroup(), [2]string{"Liabilities", "Assets"})
}

func (e *Env) s3BS(b *strings.Builder, bk *report.Books) {
	bs, notes := bk.S3BS()
	fmt.Fprintf(b, "%s, balance sheet as at %s (Schedule III)\n", bk.Company, bk.P.End())
	bsSides(b, bs, [2]string{"Equity and liabilities", "Assets"})
	for _, n := range notes {
		b.WriteString("Note: " + n + "\n")
	}
}

func (e *Env) groupDetail(b *strings.Builder, db *store.DB, bk *report.Books, g string, top int) {
	ls := bk.GroupLedgers(g)
	if len(ls) == 0 {
		fmt.Fprintf(b, "No ledgers with an amount under group %q for %s.\n", g, bk.P.Label())
		return
	}
	fmt.Fprintf(b, "%s, group %q, %s (Dr/Cr; P&L ledgers: period amount, others: closing)\nLedger | Amount\n", bk.Company, g, bk.P.Label())
	var t int64
	for i, l := range ls {
		t += l.Amount
		if i < top {
			fmt.Fprintf(b, "%s | %s\n", e.show(db, l.Label, l.Note), side(l.Amount))
		}
	}
	if len(ls) > top {
		fmt.Fprintf(b, "(%d more ledgers)\n", len(ls)-top)
	}
	fmt.Fprintf(b, "Group total | %s\n", side(t))
}

func profitWord(v int64) string {
	if v < 0 {
		return "loss"
	}
	return "profit"
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
