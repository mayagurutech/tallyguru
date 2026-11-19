package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/store"
)

func monthName(m string) string {
	t, _ := time.Parse("2006-01", m)
	return t.Format("Jan 2006")
}

func (e *Env) gst(b *strings.Builder, db *store.DB, p report.Period) error {
	ms, tagged, err := report.GSTSummary(db.DB, p)
	if err != nil {
		return err
	}
	if tagged == 0 {
		b.WriteString("No ledger is tagged as a GST ledger in the ledger mapping; run the mapping first.\n")
		return nil
	}
	fmt.Fprintf(b, "%s, GST from the books, %s\nMonth | Output tax | Input credit | RCM payable | Net payable | Set-off/paid\n", db.Label.Company, p.Label())
	var t report.GSTMonth
	for _, m := range ms {
		fmt.Fprintf(b, "%s | %s | %s | %s | %s | %s\n", monthName(m.Month), bracket(m.Output), bracket(m.Input), bracket(m.RCM), bracket(m.Net()), bracket(m.Settled))
		t.Output, t.Input, t.RCM, t.Settled = t.Output+m.Output, t.Input+m.Input, t.RCM+m.RCM, t.Settled+m.Settled
	}
	fmt.Fprintf(b, "Total | %s | %s | %s | %s | %s\n", bracket(t.Output), bracket(t.Input), bracket(t.RCM), bracket(t.Net()), bracket(t.Settled))
	b.WriteString("Note: books only; output and input are from vouchers that book tax (credit notes reduce output). " +
		"Set-off journals and challans are shown apart. Compare with GSTR-1/3B/2B in the GST reco module.\n")
	return nil
}

func (e *Env) tds(b *strings.Builder, db *store.DB, p report.Period) error {
	ls, ms, err := report.TDSSummary(db.DB, p)
	if err != nil {
		return err
	}
	if len(ls) == 0 {
		b.WriteString("No ledger is tagged 'tds payable' in the ledger mapping.\n")
		return nil
	}
	fmt.Fprintf(b, "%s, TDS from the books, %s\nMonth | Deducted | Deposited\n", db.Label.Company, p.Label())
	for _, m := range ms {
		fmt.Fprintf(b, "%s | %s | %s\n", monthName(m.Month), amt(m.Deducted), amt(m.Deposited))
	}
	b.WriteString("TDS ledger | Deducted | Deposited | Payable at end\n")
	var d, dep, c int64
	for _, l := range ls {
		fmt.Fprintf(b, "%s | %s | %s | %s\n", l.Ledger, amt(l.Deducted), amt(l.Deposited), bracket(l.Closing))
		d, dep, c = d+l.Deducted, dep+l.Deposited, c+l.Closing
	}
	fmt.Fprintf(b, "Total | %s | %s | %s\n", amt(d), amt(dep), bracket(c))
	b.WriteString("Note: books only. Late deposit, missing deduction and 26Q/26AS matching are checked in the TDS reco module.\n")
	return nil
}
