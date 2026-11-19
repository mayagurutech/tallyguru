package report

import (
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

const rs = 100 // paise per rupee

// books: opening capital 1,00,000 = cash 60,000 + stock 40,000 (books from Apr 2026).
// Apr: sale 1,00,000 on credit; purchase 70,000 on credit. May: rent 5,000 cash; loan 20,000.
// Jun: advance 3,000 from a customer. Closing stock 50,000.
// Profit = 1,00,000 - 70,000 + 50,000 - 40,000 - 5,000 = 35,000.
func books(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMeta("books_from", "2026-04-01")
	db.ReplaceGroups([]tally.Group{
		{Name: "Capital Account"}, {Name: "Current Assets"}, {Name: "Current Liabilities"}, {Name: "Loans (Liability)"},
		{Name: "Sundry Debtors", Parent: "Current Assets"}, {Name: "Cash-in-Hand", Parent: "Current Assets"},
		{Name: "Stock-in-Hand", Parent: "Current Assets"}, {Name: "Sundry Creditors", Parent: "Current Liabilities"},
		{Name: "Unsecured Loans", Parent: "Loans (Liability)"},
		{Name: "Sales Accounts", IsRevenue: true, AffectsGrossProfit: true},
		{Name: "Purchase Accounts", IsRevenue: true, AffectsGrossProfit: true},
		{Name: "Indirect Expenses", IsRevenue: true},
	})
	db.ReplaceLedgers([]tally.Ledger{
		{GUID: "1", Name: "Capital", Parent: "Capital Account", Opening: 100000 * rs},
		{GUID: "2", Name: "Cash", Parent: "Cash-in-Hand", Opening: -60000 * rs},
		{GUID: "3", Name: "Stock", Parent: "Stock-in-Hand", Opening: -40000 * rs, Closing: -50000 * rs},
		{GUID: "4", Name: "Ram", Parent: "Sundry Debtors"},
		{GUID: "5", Name: "Gita", Parent: "Sundry Debtors"},
		{GUID: "6", Name: "Shyam", Parent: "Sundry Creditors"},
		{GUID: "7", Name: "Loan", Parent: "Unsecured Loans"},
		{GUID: "8", Name: "Sales", Parent: "Sales Accounts"},
		{GUID: "9", Name: "Purchases", Parent: "Purchase Accounts"},
		{GUID: "10", Name: "Rent", Parent: "Indirect Expenses"},
		{GUID: "11", Name: "Profit & Loss A/c"},
	})
	tx, _ := db.BeginVouchers()
	v := func(g, date string, lines ...tally.LedgerLine) {
		d, _ := time.Parse(time.DateOnly, date)
		tx.Put(tally.Voucher{GUID: g, Date: d, Lines: lines})
	}
	l := func(n string, a int64) tally.LedgerLine {
		return tally.LedgerLine{Ledger: n, Amount: tally.Paise(a * rs)}
	}
	v("a", "2026-04-05", l("Ram", -100000), l("Sales", 100000))
	v("b", "2026-04-10", l("Purchases", -70000), l("Shyam", 70000))
	v("c", "2026-05-01", l("Rent", -5000), l("Cash", 5000))
	v("d", "2026-05-20", l("Cash", -20000), l("Loan", 20000))
	v("e", "2026-06-02", l("Cash", -3000), l("Gita", 3000))
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.RebuildSummaries(); err != nil {
		t.Fatal(err)
	}
	return db
}

func load(t *testing.T, db *store.DB, from, to string) *Books {
	t.Helper()
	b, err := Load(db.DB, "TEST MCP", Period{from, to})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePeriod(t *testing.T) {
	cases := []struct{ fy, from, to, want string }{
		{"", "", "", "2026-04..2026-06"},        // year to date
		{"2026-27", "", "", "2026-04..2026-06"}, // running year stops at the latest month
		{"2025-26", "", "", "2025-04..2026-03"}, // closed year: full
		{"", "2026-05", "2026-05", "2026-05..2026-05"},
	}
	for _, c := range cases {
		p, err := ParsePeriod(c.fy, c.from, c.to, "2026-04", "2026-06")
		if err != nil || p.From+".."+p.To != c.want {
			t.Errorf("%+v: got %v %v", c, p, err)
		}
	}
	for _, bad := range [][3]string{{"2026-28", "", ""}, {"26-27", "", ""}, {"", "2026-13", ""}, {"", "2026-06", "2026-04"}} {
		if _, err := ParsePeriod(bad[0], bad[1], bad[2], "2026-04", "2026-06"); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if FYOf("2027-03") != "2026-27" || FYOf("2027-04") != "2027-28" {
		t.Error("FYOf")
	}
}

func TestPLByGroup(t *testing.T) {
	b := load(t, books(t), "2026-04", "2026-06")
	r := b.PLByGroup()
	if r.OpenStock != 40000*rs || r.CloseStock != 50000*rs {
		t.Errorf("stock %d %d", r.OpenStock, r.CloseStock)
	}
	if r.GrossProfit != 40000*rs || r.NetProfit != 35000*rs {
		t.Errorf("gross %d net %d", r.GrossProfit, r.NetProfit)
	}
	if len(b.Warnings()) != 0 {
		t.Errorf("unexpected warnings %v", b.Warnings())
	}
}

func TestPLPartPeriodWarnsOnStock(t *testing.T) {
	b := load(t, books(t), "2026-05", "2026-05")
	r := b.PLByGroup()
	if r.OpenStock != 0 || r.CloseStock != 0 || r.NetProfit != -5000*rs {
		t.Errorf("May: %+v", r)
	}
	if len(b.Warnings()) != 2 {
		t.Errorf("want 2 stock warnings, got %v", b.Warnings())
	}
}

func TestBSByGroupBalances(t *testing.T) {
	b := load(t, books(t), "2026-04", "2026-06")
	bs := b.BSByGroup()
	if bs.Diff != 0 || bs.PL != 35000*rs {
		t.Fatalf("diff %d profit %d", bs.Diff, bs.PL)
	}
	if bs.Liab[0].Total != 225000*rs {
		t.Errorf("liabilities %d", bs.Liab[0].Total)
	}
}

func TestS3BS(t *testing.T) {
	b := load(t, books(t), "2026-04", "2026-06")
	bs, notes := b.S3BS()
	if bs.Diff != 0 {
		t.Fatalf("diff %d", bs.Diff)
	}
	got := map[string]int64{}
	for _, side := range [][]Section{bs.Liab, bs.Assets} {
		for _, s := range side {
			for _, l := range s.Lines {
				got[l.Label] = l.Amount
			}
		}
	}
	want := map[string]int64{
		"Share capital / Owners' capital": 100000 * rs, "Reserves and surplus": 35000 * rs,
		"Long-term borrowings": 20000 * rs, "Trade payables - others": 70000 * rs,
		"Other current liabilities": 3000 * rs, // customer advance moved out of receivables
		"Inventories":               50000 * rs, "Trade receivables": 100000 * rs, "Cash and cash equivalents": 78000 * rs,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
	if len(notes) < 2 {
		t.Errorf("notes %v", notes)
	}
}

func TestS3PL(t *testing.T) {
	b := load(t, books(t), "2026-04", "2026-06")
	inc, exp, pbt, tax, _ := b.S3PL()
	if inc.Total != 100000*rs || pbt != 35000*rs || tax != 0 {
		t.Errorf("income %d pbt %d tax %d", inc.Total, pbt, tax)
	}
	for _, l := range exp.Lines {
		if l.Label == "Changes in inventories" && l.Amount != -10000*rs {
			t.Errorf("inventory change %d", l.Amount)
		}
	}
}

func TestGroupLedgers(t *testing.T) {
	b := load(t, books(t), "2026-04", "2026-06")
	ls := b.GroupLedgers("Current Assets")
	if len(ls) != 4 || ls[0].Label != "Ram" {
		t.Errorf("%+v", ls)
	}
}

func TestAgeingFIFOAndAdvances(t *testing.T) {
	db := books(t)
	asOf, _ := time.Parse(time.DateOnly, "2026-07-01")
	r, err := AgeParties(db.DB, "Sundry Debtors", -1, asOf)
	if err != nil {
		t.Fatal(err)
	}
	// Ram: 1,00,000 billed 5-Apr, 87 days -> 61-90. Gita: credit balance -> advance.
	if r.Total != 100000*rs || r.By[2] != 100000*rs || r.NFIFO != 1 {
		t.Errorf("debtors %+v", r)
	}
	if r.Advances != 3000*rs || r.NAdv != 1 {
		t.Errorf("advances %d/%d", r.Advances, r.NAdv)
	}
}

func TestAgeingBillWiseAndMSME(t *testing.T) {
	db := books(t)
	db.Exec(`INSERT INTO ledger_map VALUES('6','flag','creditor msme','auto','','rule','')`)
	// Shyam: Tally bill of 50,000 dated 10-Apr; the other 20,000 is on account (FIFO).
	db.Exec(`INSERT INTO bills VALUES('Shyam','P-1','2026-04-10',5000000,0)`)
	// As on today (after the last entry): Tally's open bills are used. 82 days old.
	asOf, _ := time.Parse(time.DateOnly, "2026-07-01")
	r, err := AgeParties(db.DB, "Sundry Creditors", 1, asOf)
	if err != nil {
		t.Fatal(err)
	}
	p := r.Parties[0]
	if !p.BillWise || p.Total != 70000*rs || p.By[2] != 70000*rs {
		t.Errorf("party %+v", p)
	}
	if r.MSMEOver != 70000*rs || r.NMSMEOver != 1 {
		t.Errorf("MSME over 45: %+v", r)
	}
	// As on a past date (year-end style): open bills are as of now, so not used; FIFO up to
	// that date. 21 days: within 45.
	asOf, _ = time.Parse(time.DateOnly, "2026-05-01")
	r, _ = AgeParties(db.DB, "Sundry Creditors", 1, asOf)
	p = r.Parties[0]
	if p.BillWise || p.Total != 70000*rs || p.By[0] != 70000*rs || r.NMSMEOver != 0 {
		t.Errorf("past date: %+v", r)
	}
	// Before the purchase: nothing owed.
	asOf, _ = time.Parse(time.DateOnly, "2026-04-05")
	if r, _ = AgeParties(db.DB, "Sundry Creditors", 1, asOf); r.Total != 0 {
		t.Errorf("before the bill: %+v", r)
	}
}

func TestAgeingBillsExceedBalance(t *testing.T) {
	db := books(t)
	// Shyam owes 70,000 but Tally shows open bills of 50,000 (10-Apr) and 40,000 (1-Jan):
	// 20,000 paid on account. Bills not set against receipts are not trusted for the age
	// (author's rule, 16 Nov 2026): FIFO on entries, so all 70,000 dates from 10-Apr (82 days).
	db.Exec(`INSERT INTO bills VALUES('Shyam','P-1','2026-04-10',5000000,0),('Shyam','P-0','2026-01-01',4000000,0)`)
	asOf, _ := time.Parse(time.DateOnly, "2026-07-01")
	r, _ := AgeParties(db.DB, "Sundry Creditors", 1, asOf)
	p := r.Parties[0]
	if p.Total != 70000*rs || p.BillWise || p.By[2] != 70000*rs || r.Unadjusted != 20000*rs {
		t.Errorf("%+v / unadjusted %d", p, r.Unadjusted)
	}
	var sum int64
	for _, v := range p.By {
		if v < 0 {
			t.Errorf("negative bucket %v", p.By)
		}
		sum += v
	}
	if sum != p.Total {
		t.Errorf("buckets %d != total %d", sum, p.Total)
	}
}

func TestRatios(t *testing.T) {
	b := load(t, books(t), "2026-04", "2026-06")
	got := map[string]Ratio{}
	for _, r := range b.Ratios() {
		got[r.Name] = r
	}
	// Current assets 2,28,000 / current liabilities 73,000; debt 20,000 / equity 1,35,000.
	if r := got["Current ratio"]; !r.OK || int(r.Value*100) != 312 {
		t.Errorf("current %+v", r)
	}
	if r := got["Debt-equity"]; int(r.Value*1000) != 148 {
		t.Errorf("debt-equity %+v", r)
	}
	if r := got["Net profit margin"]; int(r.Value) != 35 {
		t.Errorf("NP %+v", r)
	}
	if r := got["Interest cover"]; r.OK {
		t.Errorf("no finance costs: interest cover must be n/a, got %+v", r)
	}
}

func TestCashFlowTies(t *testing.T) {
	db := books(t)
	for _, p := range []Period{{"2026-04", "2026-06"}, {"2026-05", "2026-06"}} {
		b := load(t, db, p.From, p.To)
		st, err := LoadStart(db.DB, "TEST MCP", p)
		if err != nil {
			t.Fatal(err)
		}
		cf := b.CashFlowFrom(st)
		if cf.Diff != 0 {
			t.Errorf("%v: does not tie by %d (%+v)", p, cf.Diff, cf)
		}
	}
	// Full period: cash 60,000 -> 78,000.
	b := load(t, db, "2026-04", "2026-06")
	st, _ := LoadStart(db.DB, "TEST MCP", b.P)
	cf := b.CashFlowFrom(st)
	if cf.OpenCash != 60000*rs || cf.CloseCash != 78000*rs || cf.Net != 18000*rs || cf.Financing.Total != 20000*rs {
		t.Errorf("%+v", cf)
	}
}

func TestPastBalanceSheetBalances(t *testing.T) {
	// May: closing stock unknown (latest month is June) -> stock stays at opening, still balances.
	for _, p := range [][2]string{{"2026-04", "2026-04"}, {"2026-04", "2026-05"}, {"2026-05", "2026-03"}} {
		b := load(t, books(t), p[0], p[1])
		if bs := b.BSByGroup(); bs.Diff != 0 {
			t.Errorf("%v: Tally layout differs by %d", p, bs.Diff)
		}
		if bs, _ := b.S3BS(); bs.Diff != 0 {
			t.Errorf("%v: Schedule III differs by %d", p, bs.Diff)
		}
	}
}

func TestMIS(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2026-07-01")
	m, err := BuildMIS(books(t).DB, "TEST MCP", now)
	if err != nil {
		t.Fatal(err)
	}
	if m.Sales != 100000*rs || m.Profit != 35000*rs || m.Expenses != 65000*rs {
		t.Errorf("sales %d profit %d expenses %d", m.Sales, m.Profit, m.Expenses)
	}
	if m.Cash != 78000*rs || m.CashStart != 60000*rs || m.Borrowings != 20000*rs {
		t.Errorf("cash %d start %d loans %d", m.Cash, m.CashStart, m.Borrowings)
	}
	if m.Receivable != 100000*rs || m.Payable != 70000*rs || m.LastMonth != "2026-06" || m.PrevMonth != "2026-05" {
		t.Errorf("%+v", m)
	}
	if m.ProfitPrev != -5000*rs {
		t.Errorf("May profit %d", m.ProfitPrev)
	}
	if len(m.TopDebtors) != 1 || m.TopDebtors[0].Label != "Ram" {
		t.Errorf("top debtors %+v", m.TopDebtors)
	}
	if len(m.Alerts) != 0 {
		t.Errorf("alerts %v", m.Alerts)
	}
}

func TestDayBook(t *testing.T) {
	db := books(t)
	types, largest, n, err := DayBook(db.DB, DayBookFilter{P: Period{"2026-04", "2026-06"}}, 2)
	if err != nil || n != 5 || len(largest) != 2 {
		t.Fatalf("%v n=%d largest=%d", err, n, len(largest))
	}
	if largest[0].Value != 100000*rs || largest[0].Against != "Ram" || largest[1].Value != 70000*rs {
		t.Errorf("largest %+v", largest)
	}
	var tot int64
	for _, x := range types {
		tot += x.Value
	}
	if tot != (100000+70000+5000+20000+3000)*rs {
		t.Errorf("total %d", tot)
	}
	lines := 0
	EachLine(db.DB, DayBookFilter{P: Period{"2026-05", "2026-05"}}, func(VLine) error { lines++; return nil })
	if lines != 4 {
		t.Errorf("May lines %d", lines)
	}
}
