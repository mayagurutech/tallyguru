package tools

import (
	"fmt"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/report"
)

func (e *Env) ratios(b *strings.Builder, bk *report.Books) {
	fmt.Fprintf(b, "%s, ratios, %s (balance sheet as at %s, Schedule III heads)\nRatio | Value | How\n", bk.Company, bk.P.Label(), bk.P.End())
	for _, r := range bk.Ratios() {
		v := "n/a (base is zero)"
		if r.OK {
			switch r.Unit {
			case "%":
				v = fmt.Sprintf("%.1f%%", r.Value)
			case "days":
				v = fmt.Sprintf("%.0f days", r.Value)
			default:
				v = fmt.Sprintf("%.2f", r.Value)
			}
		}
		fmt.Fprintf(b, "%s | %s | %s\n", r.Name, v, r.Formula)
	}
	b.WriteString("Note: margins, return and days are for this period only (not annualised).\n")
}

func (e *Env) cashFlow(b *strings.Builder, bk, start *report.Books) {
	cf := bk.CashFlowFrom(start)
	fmt.Fprintf(b, "%s, cash flow (indirect method), %s\n", bk.Company, bk.P.Label())
	for _, s := range []report.Section{cf.Operating, cf.Investing, cf.Financing} {
		fmt.Fprintf(b, "%s | Amount\n", s.Title)
		for _, l := range s.Lines {
			fmt.Fprintf(b, "%s | %s\n", l.Label, bracket(l.Amount))
		}
		fmt.Fprintf(b, "Net cash from %s | %s\n", strings.ToLower(s.Title), bracket(s.Total))
	}
	fmt.Fprintf(b, "Net change in cash | %s\nCash and cash equivalents: opening %s, closing %s\n",
		bracket(cf.Net), bracket(cf.OpenCash), bracket(cf.CloseCash))
	if cf.Diff == 0 {
		b.WriteString("Ties: net change equals the movement in cash and bank balances.\n")
	} else {
		fmt.Fprintf(b, "Warning: does not tie to the cash movement by %s; do not rely on it.\n", bracket(cf.Diff))
	}
	for _, w := range start.Warnings() {
		b.WriteString("Warning (opening position): " + w + "\n")
	}
}
