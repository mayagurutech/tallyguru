package tbcheck

import (
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

func setup(t *testing.T, cashClosing tally.Paise) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// Cash opens 1,000 Dr; one sale of 500 in cash; P&L closing shows the profit (computed).
	err = db.ReplaceLedgers([]tally.Ledger{
		{Name: "Cash", GUID: "1", Opening: -100000, Closing: cashClosing},
		{Name: "Capital", GUID: "2", Opening: 100000, Closing: 100000},
		{Name: "Sales", GUID: "3", Closing: 50000},
		{Name: PLLedger, GUID: "4", Closing: 50000},
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := db.BeginVouchers()
	tx.Put(tally.Voucher{GUID: "v1", Date: time.Now(), Lines: []tally.LedgerLine{{Ledger: "Cash", Amount: -50000}, {Ledger: "Sales", Amount: 50000}}})
	// Optional voucher must be ignored.
	tx.Put(tally.Voucher{GUID: "v2", Date: time.Now(), Optional: true, Lines: []tally.LedgerLine{{Ledger: "Cash", Amount: -999}, {Ledger: "Sales", Amount: 999}}})
	tx.Commit()
	return db
}

func TestMatch(t *testing.T) {
	db := setup(t, -150000)
	r, err := Run(db)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK() {
		t.Fatalf("expected match:\n%s", r)
	}
	if Gate(db) != nil {
		t.Fatal("gate should open")
	}
}

func TestMismatchBlocks(t *testing.T) {
	db := setup(t, -160000) // Tally says 1,600 Dr, we have 1,500 Dr
	r, _ := Run(db)
	if r.OK() || len(r.Mismatches) != 1 || r.Mismatches[0].Ledger != "Cash" {
		t.Fatalf("expected one Cash mismatch, got %+v", r)
	}
	err := Gate(db)
	if err == nil || !strings.Contains(err.Error(), "does NOT match") || !strings.Contains(err.Error(), "1,600.00 Dr") {
		t.Fatalf("gate must block with warning, got %v", err)
	}
}

func TestNonAccountingVouchersExcluded(t *testing.T) {
	db := setup(t, -150000)
	db.ReplaceVoucherTypes([]tally.VoucherType{{Name: "Sales", Parent: "Sales"}, {Name: "Delivery Note", Parent: "Delivery Note"}})
	tx, _ := db.BeginVouchers()
	// A delivery note posts nothing to the books in Tally; it must not move balances.
	tx.Put(tally.Voucher{GUID: "dn1", Type: "Delivery Note", Date: time.Now(), Lines: []tally.LedgerLine{{Ledger: "Cash", Amount: -777}, {Ledger: "Sales", Amount: 777}}})
	// Zero-amount line with no ledger (stock journal) must not count as unknown ledger.
	tx.Put(tally.Voucher{GUID: "sj1", Type: "Sales", Date: time.Now(), Lines: []tally.LedgerLine{{Ledger: "", Amount: 0}}})
	tx.Commit()
	if r, _ := Run(db); !r.OK() {
		t.Fatalf("delivery note / blank line must not break TB:\n%s", r)
	}
}

func TestUnknownLedgerBlocks(t *testing.T) {
	db := setup(t, -150000)
	tx, _ := db.BeginVouchers()
	tx.Put(tally.Voucher{GUID: "v3", Date: time.Now(), Lines: []tally.LedgerLine{{Ledger: "Ghost", Amount: -1}, {Ledger: "Sales", Amount: 1}}})
	tx.Commit()
	if r, _ := Run(db); r.OK() {
		t.Fatal("unknown ledger must block")
	}
}

func TestGateBeforeAnyCheck(t *testing.T) {
	db, _ := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	defer db.Close()
	if Gate(db) == nil {
		t.Fatal("gate must be closed before the first check")
	}
}

func TestRs(t *testing.T) {
	for p, want := range map[int64]string{0: "0.00 Cr", -150000: "1,500.00 Dr", 12345678901: "12,34,56,789.01 Cr", -5: "0.05 Dr"} {
		if got := Rs(p); got != want {
			t.Errorf("Rs(%d)=%q want %q", p, got, want)
		}
	}
}

// A voucher line written differently from the ledger ("Swiss Pac Pvt. Ltd" vs "SWISS PAC PVT LTD")
// is the same ledger to Tally; after ResolveNames it must match, and a Stock-in-Hand ledger whose
// Tally closing is the inventory valuation must not count as a mismatch.
func TestNameVariantsAndStock(t *testing.T) {
	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.ReplaceLedgers([]tally.Ledger{
		{Name: "Swiss Pac Pvt. Ltd", GUID: "1", Parent: "Sundry Creditors", Closing: 50000},
		{Name: "Purchase", GUID: "2", Parent: "Purchase Accounts", Closing: -50000},
		{Name: "Stocks", GUID: "3", Parent: "Stock-in-Hand", Opening: -10000, Closing: -3000000},
		{Name: "Capital", GUID: "4", Parent: "Capital Account", Opening: 10000, Closing: 10000},
	})
	tx, _ := db.BeginVouchers()
	tx.Put(tally.Voucher{GUID: "v1", Date: time.Now(), Party: "SWISS PAC  PVT LTD", Lines: []tally.LedgerLine{{Ledger: "Purchase", Amount: -50000}, {Ledger: "SWISS PAC  PVT LTD", Amount: 50000}}})
	tx.Commit()
	if r, _ := Run(db); r.OK() || len(r.Unknown) != 1 {
		t.Fatalf("before resolving, the variant must be unknown: %+v", r)
	}
	if n, err := db.ResolveNames(); err != nil || n != 1 {
		t.Fatalf("resolve: %d %v", n, err)
	}
	r, _ := Run(db)
	if !r.OK() || len(r.Stock) != 1 {
		t.Fatalf("expected a match with Stocks skipped:\n%s %+v", r, r.Stock)
	}
	var party string
	db.QueryRow(`SELECT party FROM vouchers WHERE guid = 'v1'`).Scan(&party)
	if party != "Swiss Pac Pvt. Ltd" {
		t.Errorf("party not resolved: %q", party)
	}
}

// A post-dated voucher stays out of the books until its date; Tally's closing excludes it.
func TestPostDatedLeftOut(t *testing.T) {
	db := setup(t, -150000)
	tx, _ := db.BeginVouchers()
	tx.Put(tally.Voucher{GUID: "pd", Date: time.Now().AddDate(0, 1, 0), PostDated: true,
		Lines: []tally.LedgerLine{{Ledger: "Cash", Amount: -7000}, {Ledger: "Sales", Amount: 7000}}})
	tx.Put(tally.Voucher{GUID: "pd-due", Date: time.Now().AddDate(0, 0, -1), PostDated: true,
		Lines: []tally.LedgerLine{{Ledger: "Cash", Amount: -1}, {Ledger: "Sales", Amount: 1}}})
	tx.Commit()
	r, _ := Run(db)
	if len(r.Mismatches) != 2 || r.Mismatches[0].Ours != -150001 && r.Mismatches[1].Ours != -150001 {
		t.Fatalf("future post-dated must be left out, a due one counted: %+v", r.Mismatches)
	}
}
