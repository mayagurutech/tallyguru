package report

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// MIS is the owner's one-page summary. Every figure is worked out here; the AI only words it.
type MIS struct {
	Company                  string
	P                        Period // financial year to date
	Sales, Expenses, Profit  int64  // year to date (expenses include purchases and stock change)
	LastMonth, PrevMonth     string
	SalesLast, SalesPrev     int64
	ProfitLast, ProfitPrev   int64
	Cash, CashStart          int64 // cash and bank, now and at the start of the year
	Receivable, Payable      int64
	RecvOld                  int64 // receivables over 180 days
	SupplierAdvances         int64
	Borrowings               int64
	GSTNet, TDSPayable       int64
	TopDebtors, TopCreditors []Line
	Alerts                   []string
}

// salesOf is the period's revenue from operations (sales and direct incomes).
func (b *Books) salesOf() int64 {
	inc, _, _, _, _ := b.S3PL()
	for _, l := range inc.Lines {
		if l.Label == "Revenue from operations" {
			return l.Amount
		}
	}
	return 0
}

// BuildMIS works out the owner's summary for the year to date as on now.
func BuildMIS(db *sql.DB, company string, now time.Time) (*MIS, error) {
	var first, last string
	db.QueryRow(`SELECT COALESCE(MIN(month),''), COALESCE(MAX(month),'') FROM month_ledger`).Scan(&first, &last)
	p, err := ParsePeriod("", "", "", first, last)
	if err != nil {
		return nil, err
	}
	m := &MIS{Company: company, P: p, LastMonth: p.To, PrevMonth: prevMonth(p.To)}
	ytd, err := Load(db, company, p)
	if err != nil {
		return nil, err
	}
	pl := ytd.PLByGroup()
	m.Sales, m.Profit = ytd.salesOf(), pl.NetProfit
	inc, _, _, _, _ := ytd.S3PL()
	m.Expenses = inc.Total - m.Profit

	for _, mm := range []struct {
		month       string
		sales, prof *int64
	}{{m.LastMonth, &m.SalesLast, &m.ProfitLast}, {m.PrevMonth, &m.SalesPrev, &m.ProfitPrev}} {
		b, err := Load(db, company, Period{mm.month, mm.month})
		if err != nil {
			return nil, err
		}
		*mm.sales, *mm.prof = b.salesOf(), b.PLByGroup().NetProfit
	}

	h, _, _, suspense := ytd.S3Heads()
	start, err := LoadStart(db, company, p)
	if err != nil {
		return nil, err
	}
	sh, _, _, _ := start.S3Heads()
	m.Cash, m.CashStart = h["Cash and cash equivalents"], sh["Cash and cash equivalents"]
	m.Borrowings = h["Long-term borrowings"] + h["Short-term borrowings"]

	recv, err := AgeParties(db, "Sundry Debtors", -1, now)
	if err != nil {
		return nil, err
	}
	pay, err := AgeParties(db, "Sundry Creditors", 1, now)
	if err != nil {
		return nil, err
	}
	m.Receivable, m.Payable, m.SupplierAdvances = recv.Total, pay.Total, pay.Advances
	m.RecvOld = recv.By[len(recv.By)-1]
	for i, ps := range []*[]Line{&m.TopDebtors, &m.TopCreditors} {
		src := []*Ageing{recv, pay}[i]
		for _, x := range src.Parties[:min(3, len(src.Parties))] {
			*ps = append(*ps, Line{Label: x.Name, Amount: x.Total, Note: x.Parent})
		}
	}

	gst, _, err := GSTSummary(db, p)
	if err != nil {
		return nil, err
	}
	for _, g := range gst {
		m.GSTNet += g.Net()
	}
	tds, _, err := TDSSummary(db, p)
	if err != nil {
		return nil, err
	}
	for _, t := range tds {
		m.TDSPayable += t.Closing
	}

	// Alerts: facts that need the owner's or accountant's attention (never conclusions).
	var negCash []string
	for _, l := range ytd.Ledgers {
		if l.Under("Cash-in-Hand") && l.Close() > 0 {
			negCash = append(negCash, l.Name)
		}
	}
	sort.Strings(negCash)
	if len(negCash) > 0 {
		m.Alerts = append(m.Alerts, fmt.Sprintf("Cash in hand shows a credit (negative) balance in %d ledger(s): %s", len(negCash), joinMax(negCash, 3)))
	}
	if suspense != 0 {
		m.Alerts = append(m.Alerts, "Suspense A/c has a balance of "+money(abs(suspense)))
	}
	if m.Receivable > 0 && m.RecvOld*4 > m.Receivable {
		m.Alerts = append(m.Alerts, fmt.Sprintf("%d%% of receivables are over 180 days old", m.RecvOld*100/m.Receivable))
	}
	if recv.Unadjusted+pay.Unadjusted > 0 {
		m.Alerts = append(m.Alerts, "Some receipts/payments are not set against bills in Tally ("+money(recv.Unadjusted+pay.Unadjusted)+")")
	}
	if m.Cash < 0 {
		m.Alerts = append(m.Alerts, "Cash and bank together are negative (overdrawn)")
	}
	if bs := ytd.BSByGroup(); bs.Diff != 0 {
		m.Alerts = append(m.Alerts, "Balance sheet does not balance by "+money(bs.Diff)+"; check opening balances")
	}
	for _, w := range ytd.Warnings() {
		m.Alerts = append(m.Alerts, w)
	}
	return m, nil
}

func joinMax(s []string, n int) string {
	out := ""
	for i, x := range s {
		if i == n {
			return out + fmt.Sprintf(" and %d more", len(s)-n)
		}
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
