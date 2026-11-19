package audit

import (
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

const rs = 100

// Each check gets a case at its limit and one just past it.
func books(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMeta("books_from", "2026-04-01")
	db.ReplaceGroups([]tally.Group{
		{Name: "Current Assets"}, {Name: "Current Liabilities"}, {Name: "Loans (Liability)"}, {Name: "Capital Account"}, {Name: "Suspense A/c"},
		{Name: "Cash-in-Hand", Parent: "Current Assets"}, {Name: "Bank Accounts", Parent: "Current Assets"},
		{Name: "Sundry Debtors", Parent: "Current Assets"}, {Name: "Sundry Creditors", Parent: "Current Liabilities"},
		{Name: "Duties & Taxes", Parent: "Current Liabilities"}, {Name: "Unsecured Loans", Parent: "Loans (Liability)"},
		{Name: "Sales Accounts", IsRevenue: true, AffectsGrossProfit: true}, {Name: "Indirect Expenses", IsRevenue: true},
	})
	db.ReplaceVoucherTypes([]tally.VoucherType{{Name: "Payment", Parent: "Payment"}, {Name: "Receipt", Parent: "Receipt"},
		{Name: "Journal", Parent: "Journal"}, {Name: "Sales", Parent: "Sales"}, {Name: "Contra", Parent: "Contra"}})
	db.ReplaceLedgers([]tally.Ledger{
		{GUID: "1", Name: "Cash", Parent: "Cash-in-Hand", Opening: -200000 * rs},
		{GUID: "2", Name: "Bank", Parent: "Bank Accounts", Opening: -1000000 * rs},
		{GUID: "3", Name: "Rent", Parent: "Indirect Expenses"},
		{GUID: "4", Name: "Fees", Parent: "Indirect Expenses"},
		{GUID: "5", Name: "Ram", Parent: "Sundry Debtors"},
		{GUID: "6", Name: "Shyam", Parent: "Sundry Creditors"},
		{GUID: "7", Name: "Gopal", Parent: "Sundry Creditors"},
		{GUID: "8", Name: "Mohan Loan", Parent: "Unsecured Loans"},
		{GUID: "9", Name: "Sales", Parent: "Sales Accounts"},
		{GUID: "10", Name: "Suspense", Parent: "Suspense A/c"},
		{GUID: "11", Name: "Capital", Parent: "Capital Account", Opening: 1200000 * rs},
		{GUID: "12", Name: "TDS Payable", Parent: "Duties & Taxes"},
	})
	for _, m := range [][3]string{{"4", "tds", "194J(b) professional"}, {"3", "tds", "194I(b) building rent"},
		{"6", "flag", "creditor msme"}, {"12", "tax", "tds payable"}, {"7", "flag", "related party"}} {
		db.Exec(`INSERT INTO ledger_map VALUES(?,?,?,'auto','','rule','')`, m[0], m[1], m[2])
	}
	tx, _ := db.BeginVouchers()
	n := 0
	v := func(date, typ, party string, lines ...tally.LedgerLine) {
		d, _ := time.Parse(time.DateOnly, date)
		n++
		tx.Put(tally.Voucher{GUID: "v" + string(rune('a'+n/26)) + string(rune('a'+n%26)), Date: d, Type: typ, Party: party,
			Number: string(rune('0'+n%10)) + typ[:1] + string(rune('a'+n/10)), Lines: lines})
	}
	l := func(name string, a int64) tally.LedgerLine {
		return tally.LedgerLine{Ledger: name, Amount: tally.Paise(a * rs)}
	}
	v("2026-04-06", "Payment", "", l("Rent", -10000), l("Cash", 10000))                               // exactly 10,000: not over
	v("2026-04-07", "Payment", "Shyam", l("Shyam", -6000), l("Cash", 6000))                           // 6,000 + 5,000 same day
	v("2026-04-07", "Payment", "Shyam", l("Shyam", -5000), l("Cash", 5000))                           // = 11,000: over
	v("2026-04-08", "Receipt", "Ram", l("Cash", -199999), l("Ram", 199999))                           // below 2 lakh
	v("2026-04-09", "Receipt", "Ram", l("Cash", -200000), l("Ram", 200000))                           // 2 lakh: caught
	v("2026-04-10", "Receipt", "Mohan Loan", l("Cash", -20000), l("Mohan Loan", 20000))               // loan in cash 20,000
	v("2026-04-11", "Receipt", "Mohan Loan", l("Cash", -19999), l("Mohan Loan", 19999))               // below 20,000
	v("2026-05-02", "Payment", "Mohan Loan", l("Mohan Loan", -25000), l("Cash", 25000))               // repaid in cash
	v("2026-04-15", "Journal", "Shyam", l("Fees", -30000), l("Shyam", 30000))                         // fees 30,000 +
	v("2026-05-15", "Journal", "Shyam", l("Fees", -25000), l("Shyam", 25000))                         // 25,000 = 55,000, no TDS
	v("2026-04-16", "Journal", "Gopal", l("Fees", -60000), l("TDS Payable", 6000), l("Gopal", 54000)) // TDS deducted: fine
	v("2026-04-20", "Journal", "", l("Suspense", -50000), l("Capital", 50000))                        // round 50,000
	v("2026-04-21", "Journal", "", l("Suspense", -40000), l("Capital", 40000))                        // round but < 50,000
	v("2026-04-22", "Payment", "", l("Rent", -55000), l("Bank", 55000))                               // not a multiple
	v("2026-04-05", "Sales", "Ram", l("Ram", -1000), l("Sales", 1000))                                // Sunday; duplicate base
	v("2026-04-12", "Sales", "Ram", l("Ram", -1000), l("Sales", 1000))                                // Sunday, 7 days: duplicate
	v("2026-04-20", "Sales", "Ram", l("Ram", -1000), l("Sales", 1000))                                // 8 days: not
	v("2026-06-01", "Contra", "", l("Bank", -1000000), l("Cash", 1000000))                            // cash goes negative
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// The two small Ram sales 7 days apart carry the same bill reference: counted as a duplicate
	// (below 10,000 a pair needs the same bill reference). The third, 8 days later, not.
	db.Exec(`UPDATE vouchers SET reference = 'Bill 7' WHERE type = 'Sales' AND date IN ('2026-04-05','2026-04-12')`)
	db.RebuildSummaries()
	return db
}

func run(t *testing.T) map[string]Result {
	rsl, err := Run(books(t).DB, report.Period{From: "2026-04", To: "2026-06"})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Result{}
	for _, r := range rsl {
		out[r.ID] = r
	}
	if len(out) != len(Checks) {
		t.Fatalf("%d results for %d checks", len(out), len(Checks))
	}
	return out
}

func TestCashChecksAtTheLimits(t *testing.T) {
	r := run(t)
	cp := r["cash_payments"]
	// Only Shyam: 6,000 + 5,000 on one day. Rent at exactly 10,000 is not over; Mohan's
	// repayment is a loan (269T check), not here.
	if cp.Count != 1 || cp.Issues[0].Party != "Shyam" || cp.Issues[0].Amount != 11000*rs || len(cp.Issues[0].Vouchers) != 2 {
		t.Fatalf("cash payments %+v", cp.Issues)
	}
	for _, i := range cp.Issues {
		if i.Party == "Rent" || i.Party == "Mohan Loan" {
			t.Errorf("not expected: %+v", i)
		}
	}
	if !strings.Contains(cp.Section, "36(4)") || !strings.Contains(cp.Section, "40A(3) of 1961") {
		t.Errorf("section %q", cp.Section)
	}
	if c := r["cash_receipts"]; c.Count != 1 || c.Issues[0].Amount != 200000*rs || c.Issues[0].Party != "Ram" {
		t.Errorf("cash receipts %+v", c.Issues)
	}
	if c := r["cash_loans_taken"]; c.Count != 1 || c.Issues[0].Amount != 20000*rs {
		t.Errorf("loans taken %+v", c.Issues)
	}
	if c := r["cash_loans_repaid"]; c.Count != 1 || c.Issues[0].Amount != 25000*rs {
		t.Errorf("loans repaid %+v", c.Issues)
	}
}

func TestOtherChecks(t *testing.T) {
	r := run(t)
	// Rent: 10,000 + 55,000 in April = 65,000 in the month (> 50,000 a month for rent);
	// Shyam: fees 55,000 in the year (> 50,000). Gopal had TDS deducted: not listed.
	if c := r["tds_not_deducted"]; c.Count != 2 || c.Issues[0].Party != "Rent" || c.Issues[0].Amount != 65000*rs ||
		c.Issues[1].Party != "Shyam" || c.Issues[1].Amount != 55000*rs {
		t.Errorf("tds %+v", c.Issues)
	}
	// Gopal's 60,000 journal and the 50,000 journal; 40,000 is below the minimum, 55,000 not a multiple.
	if c := r["round_sums"]; c.Count != 2 || c.Issues[0].Amount != 60000*rs || c.Issues[1].Amount != 50000*rs {
		t.Errorf("round %+v", c.Issues)
	}
	if c := r["sunday_entries"]; c.Count != 2 {
		t.Errorf("sunday %+v", c.Issues)
	}
	if c := r["duplicates"]; c.Count != 1 || c.Issues[0].Party != "Ram" || !strings.Contains(c.Issues[0].Detail, "bill reference") {
		t.Errorf("duplicates %+v", c.Issues)
	}
	if c := r["negative_cash"]; c.Count != 1 || c.Issues[0].Party != "Cash" {
		t.Errorf("negative cash %+v", c.Issues)
	}
	if c := r["suspense"]; c.Count != 1 || c.Issues[0].Amount != 90000*rs {
		t.Errorf("suspense %+v", c.Issues)
	}
	if c := r["related_parties"]; c.Count != 1 || c.Issues[0].Party != "Gopal" {
		t.Errorf("related %+v", c.Issues)
	}
	// Shyam (MSME): owes 55,000 - 11,000 = 44,000 at 30-06; all of it older than 45 days.
	if c := r["msme_45_days"]; c.Count != 1 || c.Issues[0].Amount != 44000*rs {
		t.Errorf("msme %+v", c.Issues)
	}
	if !r["edit_log"].NotAvailable {
		t.Error("edit log must say not available")
	}
}

// Thresholds are rupees, amounts paise: a 9,000 contractor bill and rent of 34,000 a month
// must not be listed (the pilot listed both before this was fixed).
func TestTDSBelowThresholds(t *testing.T) {
	db := books(t)
	db.Exec(`INSERT INTO ledger_map VALUES('13','tds','194C contractor','auto','','rule','')`)
	db.ReplaceLedgers(append([]tally.Ledger{}, // keep the fixture's ledgers and add two
		tally.Ledger{GUID: "1", Name: "Cash", Parent: "Cash-in-Hand", Opening: -200000 * rs},
		tally.Ledger{GUID: "2", Name: "Bank", Parent: "Bank Accounts", Opening: -1000000 * rs},
		tally.Ledger{GUID: "3", Name: "Rent", Parent: "Indirect Expenses"},
		tally.Ledger{GUID: "4", Name: "Fees", Parent: "Indirect Expenses"},
		tally.Ledger{GUID: "5", Name: "Ram", Parent: "Sundry Debtors"},
		tally.Ledger{GUID: "6", Name: "Shyam", Parent: "Sundry Creditors"},
		tally.Ledger{GUID: "7", Name: "Gopal", Parent: "Sundry Creditors"},
		tally.Ledger{GUID: "8", Name: "Mohan Loan", Parent: "Unsecured Loans"},
		tally.Ledger{GUID: "9", Name: "Sales", Parent: "Sales Accounts"},
		tally.Ledger{GUID: "10", Name: "Suspense", Parent: "Suspense A/c"},
		tally.Ledger{GUID: "11", Name: "Capital", Parent: "Capital Account", Opening: 1200000 * rs},
		tally.Ledger{GUID: "12", Name: "TDS Payable", Parent: "Duties & Taxes"},
		tally.Ledger{GUID: "13", Name: "Repairs", Parent: "Indirect Expenses"},
		tally.Ledger{GUID: "14", Name: "Fixit", Parent: "Sundry Creditors"},
		tally.Ledger{GUID: "15", Name: "Landlord", Parent: "Sundry Creditors"}))
	tx, _ := db.BeginVouchers()
	d := func(s string) time.Time { x, _ := time.Parse(time.DateOnly, s); return x }
	tx.Put(tally.Voucher{GUID: "x1", Date: d("2026-06-03"), Type: "Journal", Lines: []tally.LedgerLine{
		{Ledger: "Repairs", Amount: -9000 * rs}, {Ledger: "Fixit", Amount: 9000 * rs}}})
	for i, m := range []string{"2026-04-25", "2026-05-25", "2026-06-25"} {
		tx.Put(tally.Voucher{GUID: "r" + string(rune('0'+i)), Date: d(m), Type: "Journal", Lines: []tally.LedgerLine{
			{Ledger: "Rent", Amount: -34000 * rs}, {Ledger: "Landlord", Amount: 34000 * rs}}})
	}
	tx.Commit()
	db.RebuildSummaries()
	rsl, err := Run(db.DB, report.Period{From: "2026-04", To: "2026-06"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rsl {
		if r.ID != "tds_not_deducted" {
			continue
		}
		for _, i := range r.Issues {
			if i.Party == "Fixit" || i.Party == "Landlord" {
				t.Errorf("below the threshold but listed: %+v", i)
			}
		}
	}
}

// Part 4b: party details from Tally (GSTIN, state, country, transporter).
func TestPartyChecks(t *testing.T) {
	db := books(t)
	db.SetMeta("company_state", "Gujarat")
	db.Exec(`INSERT INTO ledgers VALUES('20','IGST Out','Duties & Taxes',0,0,0,0),('21','CGST Out','Duties & Taxes',0,0,0,0),
		('22','Mumbai Buyer','Sundry Debtors',0,0,0,0),('23','Local Buyer','Sundry Debtors',0,0,0,0),
		('24','Overseas Co','Sundry Creditors',0,0,0,0),('25','Truck Co','Sundry Creditors',0,0,0,0),('26','Typo GST','Sundry Debtors',0,0,0,0)`)
	db.Exec(`INSERT INTO ledger_map VALUES('20','tax','igst output','auto','','rule',''),('21','tax','cgst output','auto','','rule','')`)
	db.Exec(`INSERT INTO ledger_party VALUES
		('Mumbai Buyer','27AAPFU0939F1ZV','Maharashtra','Regular','India','',0),
		('Local Buyer','','Gujarat','Regular','India','',0),
		('Overseas Co','','','','United States','USD',0),
		('Truck Co','','Gujarat','','India','',1),
		('Typo GST','27AAPFU0939F1ZW','Maharashtra','Regular','India','',0)`)
	tx, _ := db.BeginVouchers()
	d := func(s string) time.Time { x, _ := time.Parse(time.DateOnly, s); return x }
	l := func(n string, a int64) tally.LedgerLine {
		return tally.LedgerLine{Ledger: n, Amount: tally.Paise(a * rs)}
	}
	put := func(g, date, typ string, ls ...tally.LedgerLine) {
		tx.Put(tally.Voucher{GUID: g, Date: d(date), Type: typ, Lines: ls})
	}
	put("g1", "2026-05-04", "Sales", l("Mumbai Buyer", -11800), l("Sales", 10000), l("CGST Out", 1800)) // other state, CGST: wrong
	put("g2", "2026-05-05", "Sales", l("Local Buyer", -11800), l("Sales", 10000), l("IGST Out", 1800))  // same state, IGST: wrong
	put("g3", "2026-05-06", "Sales", l("Local Buyer", -11800), l("Sales", 10000), l("CGST Out", 1800))  // fine
	put("g4", "2026-05-07", "Journal", l("Fees", -80000), l("Overseas Co", 80000))                      // foreign: no 194J case
	put("g5", "2026-05-08", "Payment", l("Truck Co", -30000), l("Cash", 30000))                         // transporter: under 35,000
	tx.Commit()
	db.RebuildSummaries()
	rsl, err := Run(db.DB, report.Period{From: "2026-04", To: "2026-06"})
	if err != nil {
		t.Fatal(err)
	}
	r := map[string]Result{}
	for _, x := range rsl {
		r[x.ID] = x
	}
	if c := r["invalid_gstin"]; c.Count != 1 || c.Issues[0].Party != "Typo GST" {
		t.Errorf("invalid gstin %+v", c.Issues)
	}
	g := r["igst_vs_state"]
	if g.Count != 2 {
		t.Fatalf("igst vs state %+v", g.Issues)
	}
	for _, i := range g.Issues {
		if i.Party == "Local Buyer" && !strings.Contains(i.Detail, "IGST charged") || i.Party == "Mumbai Buyer" && !strings.Contains(i.Detail, "CGST+SGST charged") {
			t.Errorf("%+v", i)
		}
	}
	if c := r["foreign_parties"]; c.Count != 1 || c.Issues[0].Party != "Overseas Co" || !strings.Contains(c.Section, "195 of 1961") {
		t.Errorf("foreign %+v %s", c.Issues, c.Section)
	}
	for _, i := range r["tds_not_deducted"].Issues {
		if i.Party == "Overseas Co" {
			t.Errorf("foreign party must not be in the 194 checks: %+v", i)
		}
	}
	for _, i := range r["cash_payments"].Issues {
		if i.Party == "Truck Co" {
			t.Errorf("transporter under 35,000 listed: %+v", i)
		}
	}
}
