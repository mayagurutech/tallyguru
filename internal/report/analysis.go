package report

import (
	"database/sql"
	"time"
)

// Ratio is one ratio with how it was worked out; OK is false when the base is zero.
type Ratio struct {
	Name, Formula string
	Value         float64
	Unit          string // "x", "%", "days"
	OK            bool
}

func ratio(name, formula, unit string, num, den int64, scale float64) Ratio {
	if den == 0 {
		return Ratio{Name: name, Formula: formula, Unit: unit}
	}
	return Ratio{name, formula, float64(num) / float64(den) * scale, unit, true}
}

// days in the period, for debtor and creditor days.
func (p Period) days() float64 {
	f, _ := time.Parse("2006-01", p.From)
	t, _ := time.Parse("2006-01", p.To)
	return t.AddDate(0, 1, 0).Sub(f).Hours() / 24
}

// Ratios works out the usual ratios from the Schedule III heads at the period end and the
// period's profit and loss. Days ratios use the period's own length (not annualised).
func (b *Books) Ratios() []Ratio {
	h, _, _, _ := b.S3Heads()
	_, _, pbt, tax, _ := b.S3PL()
	var rev, purch, fin int64
	for _, l := range b.s3PLLines() {
		switch l.Label {
		case "Revenue from operations":
			rev = l.Amount
		case "Purchases / cost of materials":
			purch = l.Amount
		case "Finance costs":
			fin = l.Amount
		}
	}
	gp := b.PLByGroup().GrossProfit
	ca := h["Inventories"] + h["Trade receivables"] + h["Cash and cash equivalents"] + h["Short-term loans and advances"] + h["Other current assets"]
	cl := h["Short-term borrowings"] + h["Trade payables - MSME"] + h["Trade payables - others"] + h["Other current liabilities"] + h["Short-term provisions"]
	debt := h["Long-term borrowings"] + h["Short-term borrowings"]
	equity := h["Share capital / Owners' capital"] + h["Reserves and surplus"]
	tp := h["Trade payables - MSME"] + h["Trade payables - others"]
	d := b.P.days()
	return []Ratio{
		ratio("Current ratio", "current assets / current liabilities", "x", ca, cl, 1),
		ratio("Quick ratio", "(current assets - inventories) / current liabilities", "x", ca-h["Inventories"], cl, 1),
		ratio("Debt-equity", "borrowings / (capital + reserves)", "x", debt, equity, 1),
		ratio("Gross profit margin", "gross profit / revenue from operations", "%", gp, rev, 100),
		ratio("Net profit margin", "profit after tax / revenue from operations", "%", pbt-tax, rev, 100),
		ratio("Return on equity", "profit after tax / (capital + reserves), for the period", "%", pbt-tax, equity, 100),
		ratio("Interest cover", "(profit before tax + finance costs) / finance costs", "x", pbt+fin, fin, 1),
		ratio("Debtor days", "trade receivables / revenue x days in period", "days", h["Trade receivables"], rev, d),
		ratio("Creditor days", "trade payables / purchases x days in period", "days", tp, purch, d),
	}
}

// s3PLLines is the Schedule III expense and income lines in one list.
func (b *Books) s3PLLines() []Line {
	inc, exp, _, _, _ := b.S3PL()
	return append(append([]Line{}, inc.Lines...), exp.Lines...)
}

// CashFlow is an indirect-method cash flow statement.
type CashFlow struct {
	Operating, Investing, Financing Section
	Net                             int64 // sum of the three
	OpenCash, CloseCash             int64
	Diff                            int64 // Net minus actual change in cash; 0 when it ties
}

// prevMonth is the month before m ("2026-04" -> "2026-03").
func prevMonth(m string) string {
	t, _ := time.Parse("2006-01", m)
	return t.AddDate(0, -1, 0).Format("2006-01")
}

// LoadStart loads the books as they stood just before the period began.
func LoadStart(db *sql.DB, company string, p Period) (*Books, error) {
	return Load(db, company, Period{p.From, prevMonth(p.From)})
}

// CashFlowFrom builds the cash flow for b's period, using start (the books just before the
// period) for the opening heads. Every head's movement is placed in one section, so the
// net always ties to the change in cash unless the books do not balance.
func (b *Books) CashFlowFrom(start *Books) CashFlow {
	e, _, _, _ := b.S3Heads()
	s, _, _, _ := start.S3Heads()
	ch := func(h string) int64 { return e[h] - s[h] } // increase in the head
	var fin, dep int64
	for _, l := range b.s3PLLines() {
		switch l.Label {
		case "Finance costs":
			fin = l.Amount
		case "Depreciation and amortisation":
			dep = l.Amount
		}
	}
	// Profit as the movement in profit to date, so it matches the balance sheets used
	// (equals the P&L when stock is known at both ends).
	profit := b.ProfitToDate() - start.ProfitToDate()
	var cf CashFlow
	cf.Operating.Title, cf.Investing.Title, cf.Financing.Title = "Operating activities", "Investing activities", "Financing activities"
	o := &cf.Operating
	o.add("Net profit for the period", profit, "")
	o.add("Add: depreciation", dep, "")
	o.add("Add: finance costs (shown under financing)", fin, "")
	o.add("(Increase)/decrease in inventories", -ch("Inventories"), "")
	o.add("(Increase)/decrease in trade receivables", -ch("Trade receivables"), "")
	o.add("(Increase)/decrease in short-term loans and advances", -ch("Short-term loans and advances"), "")
	o.add("(Increase)/decrease in other current assets", -ch("Other current assets"), "")
	o.add("Increase/(decrease) in trade payables", ch("Trade payables - MSME")+ch("Trade payables - others"), "")
	o.add("Increase/(decrease) in other current liabilities", ch("Other current liabilities"), "")
	o.add("Increase/(decrease) in provisions", ch("Short-term provisions")+ch("Long-term provisions"), "")
	i := &cf.Investing
	i.add("Purchase of property, plant and equipment (net)", -(ch("Property, plant and equipment") + dep), "")
	i.add("(Increase)/decrease in investments", -ch("Non-current investments"), "")
	i.add("(Increase)/decrease in long-term loans and advances", -ch("Long-term loans and advances"), "")
	f := &cf.Financing
	// Capital and reserves move by the profit plus capital brought in or taken out.
	f.add("Capital introduced / (withdrawn), net", ch("Share capital / Owners' capital")+ch("Reserves and surplus")-profit, "")
	f.add("Borrowings taken / (repaid), long-term", ch("Long-term borrowings"), "")
	f.add("Borrowings taken / (repaid), short-term", ch("Short-term borrowings"), "")
	f.add("Finance costs paid", -fin, "")
	cf.Net = o.Total + i.Total + f.Total
	cf.OpenCash, cf.CloseCash = s["Cash and cash equivalents"], e["Cash and cash equivalents"]
	cf.Diff = cf.Net - (cf.CloseCash - cf.OpenCash)
	return cf
}
