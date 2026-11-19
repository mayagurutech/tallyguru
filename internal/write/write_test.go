package write

import (
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

var testNow = time.Now()

func testBook(t *testing.T) (*store.DB, *Book) {
	t.Helper()
	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.Exec(Schema)
	if err := db.ReplaceGroups([]tally.Group{
		{Name: "Bank Accounts"}, {Name: "Cash-in-Hand"}, {Name: "Sundry Debtors"}, {Name: "Sundry Creditors"},
		{Name: "Sales Accounts", IsRevenue: true}, {Name: "Indirect Expenses", IsRevenue: true}, {Name: "Duties & Taxes"},
	}); err != nil {
		t.Fatal(err)
	}
	db.ReplaceVoucherTypes([]tally.VoucherType{{Name: "Payment", Parent: "Payment"}, {Name: "Sales", Parent: "Sales"}, {Name: "Journal", Parent: "Journal"}})
	db.ReplaceStockItems([]tally.StockItem{{Name: "Steel Rod", Unit: "Kg"}})
	l := func(n, p string, pi tally.PartyInfo) tally.Ledger {
		return tally.Ledger{Name: n, GUID: "g-" + n, Parent: p, Party: pi}
	}
	if err := db.ReplaceLedgers([]tally.Ledger{
		l("HDFC Bank", "Bank Accounts", tally.PartyInfo{}),
		l("Rent", "Indirect Expenses", tally.PartyInfo{}),
		l("Ram Traders", "Sundry Debtors", tally.PartyInfo{GSTIN: "23AAPFU0939F1Z3", State: "Madhya Pradesh"}),
		l("Shyam Enterprises", "Sundry Debtors", tally.PartyInfo{State: "Gujarat"}),
		l("Sales 18%", "Sales Accounts", tally.PartyInfo{}),
		l("Output CGST", "Duties & Taxes", tally.PartyInfo{}),
		l("Output SGST", "Duties & Taxes", tally.PartyInfo{}),
		l("Output IGST", "Duties & Taxes", tally.PartyInfo{}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.RefreshMapping(); err != nil {
		t.Fatal(err)
	}
	for n, tag := range map[string]string{"Output CGST": "cgst output", "Output SGST": "sgst output", "Output IGST": "igst output"} {
		db.SetTag(n, "tax", tag, "user", testNow)
	}
	db.SetTag("Rent", "tds", "194I", "user", testNow)
	db.SetMeta("company_state", "Madhya Pradesh")
	db.SetMeta("books_from", "2026-04-01")
	bk, err := LoadBook(db.DB)
	if err != nil {
		t.Fatal(err)
	}
	return db, bk
}

func args(typ, date, party, ref string, lines ...[3]any) map[string]any {
	var ls []any
	for _, l := range lines {
		m := map[string]any{"ledger": l[0]}
		if l[1] != nil {
			m["dr"] = l[1]
		}
		if l[2] != nil {
			m["cr"] = l[2]
		}
		ls = append(ls, m)
	}
	return map[string]any{"type": typ, "date": date, "party": party, "reference": ref, "lines": ls}
}

func TestPaymentDraftAndXML(t *testing.T) {
	db, bk := testBook(t)
	v, err := FromArgs(args("payment", "2026-05-01", "", "", [3]any{"rent", 25000.0, nil}, [3]any{"HDFC Bank", nil, "25,000"}))
	if err != nil {
		t.Fatal(err)
	}
	errs, warns := bk.Check(&v)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if v.Type != "Payment" || v.Lines[0].Ledger != "Rent" {
		t.Errorf("names not corrected to Tally's spelling: %q %q", v.Type, v.Lines[0].Ledger)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "no TDS line") {
		t.Errorf("want TDS warning, got %v", warns)
	}
	if err := Save(db.DB, &v); err != nil || v.ID == 0 {
		t.Fatal("save", err)
	}
	x := VouchersXML("TEST MCP", []Voucher{v})
	for _, want := range []string{
		`<SVCURRENTCOMPANY>TEST MCP</SVCURRENTCOMPANY>`,
		`<VOUCHER VCHTYPE="Payment" ACTION="Create" OBJVIEW="Accounting Voucher View">`,
		`<DATE>20260501</DATE>`,
		`<LEDGERNAME>Rent</LEDGERNAME><ISDEEMEDPOSITIVE>Yes</ISDEEMEDPOSITIVE><ISPARTYLEDGER>No</ISPARTYLEDGER><AMOUNT>-25000.00</AMOUNT>`,
		`<LEDGERNAME>HDFC Bank</LEDGERNAME><ISDEEMEDPOSITIVE>No</ISDEEMEDPOSITIVE><ISPARTYLEDGER>No</ISPARTYLEDGER><AMOUNT>25000.00</AMOUNT>`,
		Remark + " " + Marker(v.ID),
	} {
		if !strings.Contains(x, want) {
			t.Errorf("XML missing %s\n%s", want, x)
		}
	}
}

func TestChecksStopBadEntries(t *testing.T) {
	_, bk := testBook(t)
	cases := map[string]map[string]any{
		"differ by 1.00":       args("Journal", "2026-05-01", "", "", [3]any{"Rent", 100.0, nil}, [3]any{"HDFC Bank", nil, 99.0}),
		"not found":            args("Journal", "2026-05-01", "", "", [3]any{"Rnt Exp", 100.0, nil}, [3]any{"HDFC Bank", nil, 100.0}),
		"before the books":     args("Journal", "2026-03-31", "", "", [3]any{"Rent", 100.0, nil}, [3]any{"HDFC Bank", nil, 100.0}),
		"needs a cash or bank": args("Payment", "2026-05-01", "", "", [3]any{"Rent", 100.0, nil}, [3]any{"Ram Traders", nil, 100.0}),
		"voucher type":         args("Stock Journal", "2026-05-01", "", "", [3]any{"Rent", 100.0, nil}, [3]any{"HDFC Bank", nil, 100.0}),
	}
	for want, a := range cases {
		v, err := FromArgs(a)
		if err != nil {
			t.Fatal(err)
		}
		errs, _ := bk.Check(&v)
		if len(errs) == 0 || !strings.Contains(strings.Join(errs, ";"), want) {
			t.Errorf("want error %q, got %v", want, errs)
		}
	}
	if _, err := FromArgs(args("Journal", "2026-05-01", "", "", [3]any{"Rent", 100.0, 100.0})); err == nil {
		t.Error("line with both dr and cr accepted")
	}
}

func TestGSTWarnings(t *testing.T) {
	_, bk := testBook(t)
	// Same-state party charged IGST.
	v, _ := FromArgs(args("Sales", "2026-05-03", "Ram Traders", "INV-1",
		[3]any{"Ram Traders", 11800.0, nil}, [3]any{"Sales 18%", nil, 10000.0}, [3]any{"Output IGST", nil, 1800.0}))
	errs, warns := bk.Check(&v)
	if len(errs) > 0 || !strings.Contains(strings.Join(warns, ";"), "IGST charged") {
		t.Errorf("errs %v warns %v", errs, warns)
	}
	// Other-state party, CGST+SGST at an odd rate.
	v, _ = FromArgs(args("Sales", "2026-05-03", "", "INV-2",
		[3]any{"Shyam Enterprises", 11500.0, nil}, [3]any{"Sales 18%", nil, 10000.0},
		[3]any{"Output CGST", nil, 750.0}, [3]any{"Output SGST", nil, 750.0}))
	_, warns = bk.Check(&v)
	w := strings.Join(warns, ";")
	if v.Party != "Shyam Enterprises" || !strings.Contains(w, "CGST+SGST charged") || !strings.Contains(w, "not a usual rate") {
		t.Errorf("party %q warns %v", v.Party, warns)
	}
	// Correct local sale: no warnings.
	v, _ = FromArgs(args("Sales", "2026-05-03", "Ram Traders", "INV-3",
		[3]any{"Ram Traders", 11800.0, nil}, [3]any{"Sales 18%", nil, 10000.0},
		[3]any{"Output CGST", nil, 900.0}, [3]any{"Output SGST", nil, 900.0}))
	if errs, warns := bk.Check(&v); len(errs)+len(warns) > 0 {
		t.Errorf("clean sale flagged: %v %v", errs, warns)
	}
}

func TestDuplicateDraft(t *testing.T) {
	db, bk := testBook(t)
	a := args("Sales", "2026-05-03", "Ram Traders", "INV-9",
		[3]any{"Ram Traders", 11800.0, nil}, [3]any{"Sales 18%", nil, 10000.0},
		[3]any{"Output CGST", nil, 900.0}, [3]any{"Output SGST", nil, 900.0})
	v, _ := FromArgs(a)
	bk.Check(&v)
	Save(db.DB, &v)
	v2, _ := FromArgs(a)
	if errs, _ := bk.Check(&v2); len(errs) == 0 || !strings.Contains(errs[0], "duplicate") {
		t.Errorf("duplicate not caught: %v", errs)
	}
	a["override_duplicate"] = true
	v3, _ := FromArgs(a)
	if errs, _ := bk.Check(&v3); len(errs) > 0 {
		t.Errorf("override not honoured: %v", errs)
	}
}

func TestItemInvoiceXML(t *testing.T) {
	_, bk := testBook(t)
	a := args("Sales", "2026-05-03", "Ram Traders", "INV-5",
		[3]any{"Ram Traders", 1180.0, nil}, [3]any{"Output CGST", nil, 90.0}, [3]any{"Output SGST", nil, 90.0})
	a["items"] = []any{map[string]any{"item": "Steel Rod", "qty": 10.0, "unit": "Kg", "rate": 100.0, "ledger": "Sales 18%"}}
	v, err := FromArgs(a)
	if err != nil {
		t.Fatal(err)
	}
	if errs, warns := bk.Check(&v); len(errs)+len(warns) > 0 {
		t.Fatalf("%v %v", errs, warns)
	}
	x := VouchersXML("TEST MCP", []Voucher{v})
	for _, want := range []string{`OBJVIEW="Invoice Voucher View"`, `<ISINVOICE>Yes</ISINVOICE>`,
		`<STOCKITEMNAME>Steel Rod</STOCKITEMNAME><ISDEEMEDPOSITIVE>No</ISDEEMEDPOSITIVE><RATE>100.00/Kg</RATE><AMOUNT>1000.00</AMOUNT><ACTUALQTY>10 Kg</ACTUALQTY>`,
		`<LEDGERENTRIES.LIST><LEDGERNAME>Ram Traders</LEDGERNAME><ISDEEMEDPOSITIVE>Yes</ISDEEMEDPOSITIVE><ISPARTYLEDGER>Yes</ISPARTYLEDGER><AMOUNT>-1180.00</AMOUNT>`} {
		if !strings.Contains(x, want) {
			t.Errorf("XML missing %s", want)
		}
	}
}

func TestParseResultAndIDs(t *testing.T) {
	r, _ := ParseResult(strings.NewReader(`<RESPONSE><CREATED>2</CREATED><ALTERED>0</ALTERED><ERRORS>1</ERRORS><LINEERROR>Ledger 'X' does not exist!</LINEERROR></RESPONSE>`))
	if r.Created != 2 || r.Errors != 1 || len(r.LineErrors) != 1 {
		t.Errorf("%+v", r)
	}
	ids, err := IDs("3, 5-7")
	if err != nil || len(ids) != 4 || ids[3] != 7 {
		t.Errorf("%v %v", ids, err)
	}
	if p, _ := Rupees("1,18,000.50"); p != 11800050 {
		t.Errorf("rupees %d", p)
	}
}

func TestStockJournal(t *testing.T) {
	db, bk := testBook(t)
	db.ReplaceVoucherTypes([]tally.VoucherType{{Name: "Stock Journal", Parent: "Stock Journal"}})
	db.ReplaceStockItems([]tally.StockItem{{Name: "Steel Rod", Unit: "Kg"}, {Name: "Steel Scrap", Unit: "Kg"}})
	bk, _ = LoadBook(db.DB)
	a := map[string]any{"type": "Stock Journal", "date": "2026-07-25", "items": []any{
		map[string]any{"item": "Steel Rod", "qty": -2.0, "rate": 100.0},
		map[string]any{"item": "Steel Scrap", "qty": 2.0, "rate": 40.0}}}
	v, err := FromArgs(a)
	if err != nil {
		t.Fatal(err)
	}
	if errs, _ := bk.Check(&v); len(errs) > 0 {
		t.Fatal(errs)
	}
	x := VouchersXML("TEST MCP", []Voucher{v})
	for _, want := range []string{`OBJVIEW="Consumption Voucher View"`,
		`<INVENTORYENTRIESOUT.LIST><STOCKITEMNAME>Steel Rod</STOCKITEMNAME><ISDEEMEDPOSITIVE>No</ISDEEMEDPOSITIVE><RATE>100.00/Kg</RATE><AMOUNT>200.00</AMOUNT><ACTUALQTY>2 Kg</ACTUALQTY>`,
		`<INVENTORYENTRIESIN.LIST><STOCKITEMNAME>Steel Scrap</STOCKITEMNAME><ISDEEMEDPOSITIVE>Yes</ISDEEMEDPOSITIVE><RATE>40.00/Kg</RATE><AMOUNT>-80.00</AMOUNT>`} {
		if !strings.Contains(x, want) {
			t.Errorf("missing %s\n%s", want, x)
		}
	}
	if strings.Contains(x, "ISINVOICE") {
		t.Error("stock journal marked as invoice")
	}
	// Only items coming in: refused.
	a["items"] = []any{map[string]any{"item": "Steel Rod", "qty": 2.0, "rate": 100.0}, map[string]any{"item": "Steel Scrap", "qty": 2.0, "rate": 40.0}}
	v, _ = FromArgs(a)
	if errs, _ := bk.Check(&v); len(errs) == 0 {
		t.Error("one-sided stock journal accepted")
	}
}
