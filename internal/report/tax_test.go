package report

import (
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

// taxBooks: Apr sale 10,000 + CGST/SGST 900 each; Apr fees 10,000 + CGST input 900, TDS 1,000.
// May credit note 1,000 + 90 + 90; May set-off journal 810; May TDS challan 1,000.
func taxBooks(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.ReplaceGroups([]tally.Group{{Name: "Current Liabilities"}, {Name: "Current Assets"},
		{Name: "Duties & Taxes", Parent: "Current Liabilities"}, {Name: "Sundry Debtors", Parent: "Current Assets"},
		{Name: "Sundry Creditors", Parent: "Current Liabilities"}, {Name: "Bank Accounts", Parent: "Current Assets"},
		{Name: "Sales Accounts", IsRevenue: true, AffectsGrossProfit: true}, {Name: "Indirect Expenses", IsRevenue: true}})
	led := []tally.Ledger{
		{GUID: "1", Name: "CGST Output", Parent: "Duties & Taxes"}, {GUID: "2", Name: "SGST Output", Parent: "Duties & Taxes"},
		{GUID: "3", Name: "CGST Input", Parent: "Duties & Taxes"}, {GUID: "4", Name: "TDS 194J", Parent: "Duties & Taxes"},
		{GUID: "5", Name: "Sales", Parent: "Sales Accounts"}, {GUID: "6", Name: "Fees", Parent: "Indirect Expenses"},
		{GUID: "7", Name: "Bank", Parent: "Bank Accounts"}, {GUID: "8", Name: "Ram", Parent: "Sundry Debtors"},
		{GUID: "9", Name: "Vendor", Parent: "Sundry Creditors"},
	}
	db.ReplaceLedgers(led)
	for g, tag := range map[string]string{"1": "cgst output", "2": "sgst output", "3": "cgst input", "4": "tds payable"} {
		db.Exec(`INSERT INTO ledger_map VALUES(?, 'tax', ?, 'auto', '', 'rule', '')`, g, tag)
	}
	tx, _ := db.BeginVouchers()
	v := func(g, date string, lines ...tally.LedgerLine) {
		d, _ := time.Parse(time.DateOnly, date)
		tx.Put(tally.Voucher{GUID: g, Date: d, Lines: lines})
	}
	l := func(n string, a int64) tally.LedgerLine {
		return tally.LedgerLine{Ledger: n, Amount: tally.Paise(a * rs)}
	}
	v("s", "2026-04-02", l("Ram", -11800), l("Sales", 10000), l("CGST Output", 900), l("SGST Output", 900))
	v("p", "2026-04-09", l("Fees", -10000), l("CGST Input", -900), l("TDS 194J", 1000), l("Vendor", 9900))
	v("cn", "2026-05-03", l("Sales", -1000), l("CGST Output", -90), l("SGST Output", -90), l("Ram", 1180))
	v("so", "2026-05-20", l("CGST Output", -810), l("CGST Input", 810))
	v("ch", "2026-05-07", l("TDS 194J", -1000), l("Bank", 1000))
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.RebuildSummaries()
	return db
}

func TestGSTSummary(t *testing.T) {
	ms, tagged, err := GSTSummary(taxBooks(t).DB, Period{"2026-04", "2026-05"})
	if err != nil || tagged != 3 || len(ms) != 2 {
		t.Fatalf("%v %d %+v", err, tagged, ms)
	}
	if a := ms[0]; a.Output != 1800*rs || a.Input != 900*rs || a.Net() != 900*rs || a.Settled != 0 {
		t.Errorf("Apr %+v", a)
	}
	// Credit note reduces output; the set-off journal is a settlement, not output or input.
	if m := ms[1]; m.Output != -180*rs || m.Input != 0 || m.Settled != 0 {
		t.Errorf("May %+v", m)
	}
}

func TestTDSSummary(t *testing.T) {
	ls, ms, err := TDSSummary(taxBooks(t).DB, Period{"2026-04", "2026-05"})
	if err != nil || len(ls) != 1 || len(ms) != 2 {
		t.Fatalf("%v %+v %+v", err, ls, ms)
	}
	if r := ls[0]; r.Deducted != 1000*rs || r.Deposited != 1000*rs || r.Closing != 0 {
		t.Errorf("%+v", r)
	}
	// Period ending in April: payable 1,000 at the end.
	ls, _, _ = TDSSummary(taxBooks(t).DB, Period{"2026-04", "2026-04"})
	if ls[0].Closing != 1000*rs {
		t.Errorf("April closing %+v", ls[0])
	}
}

func TestGSTMixedLedgerSplitPerVoucher(t *testing.T) {
	db := taxBooks(t)
	// A sale charged on the input-tagged ledger (one ledger used both ways): it is output.
	tx, _ := db.BeginVouchers()
	d, _ := time.Parse(time.DateOnly, "2026-06-01")
	tx.Put(tally.Voucher{GUID: "mx", Date: d, Lines: []tally.LedgerLine{
		{Ledger: "Ram", Amount: -1180 * rs}, {Ledger: "Sales", Amount: 1000 * rs}, {Ledger: "CGST Input", Amount: 180 * rs}}})
	tx.Commit()
	db.RebuildSummaries()
	ms, _, _ := GSTSummary(db.DB, Period{"2026-06", "2026-06"})
	if len(ms) != 1 || ms[0].Output != 180*rs || ms[0].Input != 0 {
		t.Errorf("%+v", ms)
	}
}
