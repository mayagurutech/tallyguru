package tools

import (
	"fmt"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/store"
)

// ageing answers run_report name=ageing: bucket totals, top parties by bucket, advances,
// MSME creditors over 45 days, and how each party was aged.
func (e *Env) ageing(db *store.DB, a map[string]any) (string, error) {
	group, title, sign := "Sundry Debtors", "Receivables", int64(-1)
	if str(a, "kind") == "payable" {
		group, title, sign = "Sundry Creditors", "Payables", 1
	}
	r, err := report.AgeParties(db.DB, group, sign, e.Now())
	if err != nil {
		return "", err
	}
	if x, _ := a["excel"].(bool); x {
		return e.ageingExcel(db, r, title)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, %s ageing as on %s: %s from %s", db.Label.Company, title, r.AsOf.Format("02-01-2006"), amt(r.Total), parties(len(r.Parties)))
	if len(r.Parties) == 0 {
		return b.String(), nil
	}
	b.WriteString("\nDays")
	for _, bk := range report.Buckets {
		b.WriteString(" | " + bk.Label)
	}
	b.WriteString("\nAmount")
	for _, v := range r.By {
		b.WriteString(" | " + amt(v))
	}
	b.WriteString("\n%")
	for _, v := range r.By {
		fmt.Fprintf(&b, " | %d", v*100/max(r.Total, 1))
	}
	top := min(e.top(a), len(r.Parties))
	fmt.Fprintf(&b, "\nTop %d parties:\nParty | Total", top)
	for _, bk := range report.Buckets {
		b.WriteString(" | " + bk.Label)
	}
	for _, p := range r.Parties[:top] {
		fmt.Fprintf(&b, "\n%s | %s", e.show(db, p.Name, group), amt(p.Total))
		for _, v := range p.By {
			b.WriteString(" | " + amt(v))
		}
	}
	if r.NAdv > 0 {
		fmt.Fprintf(&b, "\nAdvances (opposite balances, not aged): %s from %s", amt(r.Advances), parties(r.NAdv))
	}
	if r.NUnadjusted > 0 {
		fmt.Fprintf(&b, "\nNot set against bills in Tally: %s from %s (open bills exceed the balance; aged against the newest bills)", amt(r.Unadjusted), parties(r.NUnadjusted))
	}
	if sign > 0 {
		if r.NMSMEOver > 0 {
			fmt.Fprintf(&b, "\nMSME creditors over %d days: %s from %s (check 43B(h) in audit)", report.MSMEDays, amt(r.MSMEOver), parties(r.NMSMEOver))
		} else {
			b.WriteString("\nMSME over 45 days: none found (only creditors tagged MSME in the ledger mapping are checked)")
		}
	}
	fmt.Fprintf(&b, "\nMethod: %d bill-wise from Tally's open bills, %d by FIFO on ledger entries", r.NBill, r.NFIFO)
	return b.String(), nil
}
