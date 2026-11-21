package tally

import (
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func d(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func TestParseAmount(t *testing.T) {
	cases := map[string]Paise{"": 0, "0": 0, "1234.50": 123450, "-1,234.5": -123450, "12": 1200,
		".75": 75, "-0.01": -1, "1.005": 101, "-1.005": -101, "+3.10": 310,
		"-$100.00 @ ₹83.00/$ = -₹8,300.00": -830000, "€ 50.00 @ ₹ 90.5/€ = ₹ 4525.00": 452500, "₹ -1,000.00": -100000}
	for in, want := range cases {
		got, err := ParseAmount(in)
		if err != nil || got != want {
			t.Errorf("ParseAmount(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseAmount("12a"); err == nil {
		t.Error("expected error for 12a")
	}
	if _, err := ParseAmount("$100.00 @ 83.00/$"); err == nil {
		t.Error("forex amount without a rupee value must be refused")
	}
}

func TestVoucherRequestBuild(t *testing.T) {
	x := VoucherRequest("A & B <Co>", d("2025-04-01"), d("2025-04-30"), 120).Build()
	for _, want := range []string{
		"<SVCURRENTCOMPANY>A &amp; B &lt;Co&gt;</SVCURRENTCOMPANY>",
		`<SVFROMDATE TYPE="Date">1-Apr-2025</SVFROMDATE>`, `<SVTODATE TYPE="Date">30-Apr-2025</SVTODATE>`,
		"<TYPE>Voucher</TYPE>", "<FILTER>GuruF0</FILTER>", "<FILTER>GuruF1</FILTER>",
		`<SYSTEM TYPE="Formulae" NAME="GuruF0">$Date &gt;= $$Date:&quot;01-Apr-2025&quot; AND $Date &lt;= $$Date:&quot;30-Apr-2025&quot;</SYSTEM>`,
		`<SYSTEM TYPE="Formulae" NAME="GuruF1">$AlterID &gt; 120</SYSTEM>`,
		"AllLedgerEntries.Amount",
	} {
		if !strings.Contains(x, want) {
			t.Errorf("request missing %q", want)
		}
	}
	// Every voucher request must carry the date filter (Tally ignores SVFROMDATE).
	if !strings.Contains(VoucherIDsRequest("X", d("2026-05-01"), d("2026-05-31")).Build(), `$$Date:&quot;31-May-2026&quot;`) {
		t.Error("ID request missing date filter")
	}
	if strings.Contains(LedgerRequest("X", d("2025-04-01"), d("2026-03-31")).Build(), "<FILTER>") {
		t.Error("full ledger request should have no filter")
	}
}

func TestChunks(t *testing.T) {
	m := Chunks(d("2025-04-01"), d("2026-03-31"), false)
	if len(m) != 12 || !m[0].To.Equal(d("2025-04-30")) || !m[10].To.Equal(d("2026-02-28")) {
		t.Fatalf("monthly chunks wrong: %v", m)
	}
	w := Chunks(d("2025-04-01"), d("2025-04-30"), true)
	if len(w) != 5 || !w[4].From.Equal(d("2025-04-29")) || !w[4].To.Equal(d("2025-04-30")) {
		t.Fatalf("weekly chunks wrong: %v", w)
	}
	for _, p := range m {
		if p.To.Sub(p.From) > 31*24*time.Hour {
			t.Fatal("chunk longer than a month")
		}
	}
}

func TestSanitizer(t *testing.T) {
	in := "<A>x&#4;y\x01z &amp; &#65; &#13;</A>"
	b, _ := io.ReadAll(NewSanitizer(strings.NewReader(in)))
	if got, want := string(b), "<A>xyz &amp; &#65; &#13;</A>"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

const voucherXML = `<ENVELOPE><BODY><DATA><COLLECTION>
<VOUCHER REMOTEID="g1" VCHTYPE="Sales">
 <DATE>20250405</DATE><GUID>g1</GUID><ALTERID>501</ALTERID><MASTERID>9</MASTERID>
 <VOUCHERTYPENAME>Sales</VOUCHERTYPENAME><VOUCHERNUMBER>S/1</VOUCHERNUMBER>
 <PARTYLEDGERNAME>Ram &amp; Co</PARTYLEDGERNAME><ISCANCELLED>No</ISCANCELLED><NARRATION>a&#4;b</NARRATION>
 <ALLLEDGERENTRIES.LIST><LEDGERNAME>Ram &amp; Co</LEDGERNAME><AMOUNT>-1180.00</AMOUNT></ALLLEDGERENTRIES.LIST>
 <ALLLEDGERENTRIES.LIST><LEDGERNAME>Sales 18%</LEDGERNAME><AMOUNT>1000.00</AMOUNT></ALLLEDGERENTRIES.LIST>
 <ALLLEDGERENTRIES.LIST><LEDGERNAME>Output IGST</LEDGERNAME><AMOUNT>180.00</AMOUNT></ALLLEDGERENTRIES.LIST>
</VOUCHER>
<VOUCHER><DATE>20250406</DATE><GUID>g2</GUID><ALTERID>502</ALTERID><ISCANCELLED>Yes</ISCANCELLED></VOUCHER>
</COLLECTION></DATA></BODY></ENVELOPE>`

func TestParseVouchers(t *testing.T) {
	var got []Voucher
	err := ParseVouchers(NewSanitizer(strings.NewReader(voucherXML)), func(v Voucher) error {
		got = append(got, v)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 vouchers, got %d", len(got))
	}
	v := got[0]
	if v.Party != "Ram & Co" || v.AlterID != 501 || len(v.Lines) != 3 || v.Narration != "ab" || !v.Date.Equal(d("2025-04-05")) {
		t.Fatalf("bad voucher: %+v", v)
	}
	var sum Paise
	for _, l := range v.Lines {
		sum += l.Amount
	}
	if sum != 0 {
		t.Errorf("voucher not balanced: %d", sum)
	}
	if !got[1].Cancelled {
		t.Error("second voucher should be cancelled")
	}
}

func TestParseCompaniesAndError(t *testing.T) {
	x := `<ENVELOPE><COMPANY NAME="TEST MCP"><GUID>c1</GUID><BOOKSFROM>20250401</BOOKSFROM>
<GSTREGISTRATIONNUMBER>27AAAAA0000A1Z5</GSTREGISTRATIONNUMBER></COMPANY></ENVELOPE>`
	var cs []Company
	if err := ParseCompanies(strings.NewReader(x), func(c Company) error { cs = append(cs, c); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Name != "TEST MCP" || cs[0].GSTIN != "27AAAAA0000A1Z5" {
		t.Fatalf("bad companies %+v", cs)
	}
	// Real empty response seen from TallyPrime: CMPINFO count tags must be ignored.
	empty := `<ENVELOPE><BODY><DESC><CMPINFO><COMPANY>0</COMPANY><LEDGER>0</LEDGER><VOUCHER>0</VOUCHER></CMPINFO></DESC><DATA><COLLECTION> </COLLECTION></DATA></BODY></ENVELOPE>`
	n := 0
	ParseCompanies(strings.NewReader(empty), func(Company) error { n++; return nil })
	ParseVouchers(strings.NewReader(empty), func(Voucher) error { n++; return nil })
	if n != 0 {
		t.Errorf("CMPINFO tags parsed as data: %d", n)
	}
	e := `<ENVELOPE><LINEERROR>Could not find company</LINEERROR></ENVELOPE>`
	if err := ParseCompanies(strings.NewReader(e), func(Company) error { return nil }); err == nil {
		t.Error("expected tally error")
	}
}

func TestParseGroupsTypesStock(t *testing.T) {
	x := `<ENVELOPE><BODY><DESC><CMPINFO><GROUP>0</GROUP><STOCKITEM>0</STOCKITEM></CMPINFO></DESC><DATA><COLLECTION>
<GROUP NAME="Sales Accounts"><PARENT>&#4; Primary</PARENT><ISREVENUE>Yes</ISREVENUE><AFFECTSGROSSPROFIT>Yes</AFFECTSGROSSPROFIT></GROUP>
<GROUP NAME="Bank OD A/c"><PARENT>Loans (Liability)</PARENT></GROUP>
<GROUP NAME="Closing Stock" RESERVEDNAME="Stock-in-Hand"><PARENT>Current Assets</PARENT></GROUP>
<VOUCHERTYPE NAME="GST Sales"><PARENT>Sales</PARENT></VOUCHERTYPE>
<STOCKITEM NAME="Steel Rod"><PARENT>Steel</PARENT><BASEUNITS>Kg</BASEUNITS><CLOSINGBALANCE> 120 Kg</CLOSINGBALANCE><CLOSINGVALUE>-6000.00</CLOSINGVALUE></STOCKITEM>
</COLLECTION></DATA></BODY></ENVELOPE>`
	var gs []Group
	ParseGroups(NewSanitizer(strings.NewReader(x)), func(g Group) error { gs = append(gs, g); return nil })
	if len(gs) != 3 || gs[0].Parent != "" || !gs[0].IsRevenue || gs[1].Parent != "Loans (Liability)" || gs[2].Reserved != "Stock-in-Hand" {
		t.Fatalf("groups: %+v", gs)
	}
	var vt []VoucherType
	ParseVoucherTypes(NewSanitizer(strings.NewReader(x)), func(v VoucherType) error { vt = append(vt, v); return nil })
	if len(vt) != 1 || vt[0].Parent != "Sales" {
		t.Fatalf("vtypes: %+v", vt)
	}
	var si []StockItem
	ParseStockItems(NewSanitizer(strings.NewReader(x)), func(s StockItem) error { si = append(si, s); return nil })
	if len(si) != 1 || si[0].ClosingQty != "120 Kg" || si[0].ClosingValue != -600000 {
		t.Fatalf("stock: %+v", si)
	}
}

func TestSanitizerRepairsInvalidUTF8(t *testing.T) {
	// 0x96 = en dash and 0xA0 = nbsp in Windows-1252, invalid as UTF-8; ₹ and हि are valid.
	in := "<V><NARRATION>Rent\x96Nov ₹500\xa0हि</NARRATION></V>"
	b, _ := io.ReadAll(NewSanitizer(strings.NewReader(in)))
	if !utf8.Valid(b) {
		t.Fatalf("output still invalid UTF-8: %q", b)
	}
	if got, want := string(b), "<V><NARRATION>Rent–Nov ₹500 हि</NARRATION></V>"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	var vs []Voucher
	x := `<ENVELOPE><VOUCHER><DATE>20260801</DATE><GUID>g</GUID><NARRATION>a` + "\x96" + `b</NARRATION></VOUCHER></ENVELOPE>`
	if err := ParseVouchers(NewSanitizer(strings.NewReader(x)), func(v Voucher) error { vs = append(vs, v); return nil }); err != nil || len(vs) != 1 {
		t.Fatalf("voucher with bad byte must parse: %v", err)
	}
}

func TestParseBills(t *testing.T) {
	x := `<ENVELOPE><DESC><CMPINFO><BILL>0</BILL></CMPINFO></DESC><DATA><COLLECTION>
<BILL NAME="INV/12"><BILLDATE TYPE="Date">20260105</BILLDATE><PARENT>Ram Traders</PARENT><CLOSINGBALANCE>-11800.00</CLOSINGBALANCE><ISADVANCE>No</ISADVANCE></BILL>
</COLLECTION></DATA></ENVELOPE>`
	var bs []Bill
	if err := ParseBills(strings.NewReader(x), func(b Bill) error { bs = append(bs, b); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Name != "INV/12" || bs[0].Party != "Ram Traders" || bs[0].Closing != -1180000 || !bs[0].Date.Equal(d("2026-01-05")) {
		t.Fatalf("bills: %+v", bs)
	}
}

// Current TallyPrime keeps GSTIN in a dated registration list; the latest entry wins.
func TestParseLedgerParty(t *testing.T) {
	x := `<ENVELOPE><BODY><DATA><COLLECTION>
<LEDGER NAME="Gupta Suppliers"><GUID>g1</GUID><PARENT>Sundry Creditors</PARENT><OPENINGBALANCE></OPENINGBALANCE><CLOSINGBALANCE></CLOSINGBALANCE>
<LEDGSTREGDETAILS.LIST><APPLICABLEFROM>20170701</APPLICABLEFROM><GSTIN>24AAAAA0000A1Z5</GSTIN><STATE>Gujarat</STATE><GSTREGISTRATIONTYPE>Regular</GSTREGISTRATIONTYPE><ISTRANSPORTER>No</ISTRANSPORTER></LEDGSTREGDETAILS.LIST>
<LEDGSTREGDETAILS.LIST><APPLICABLEFROM>20240401</APPLICABLEFROM><GSTIN>27AAPFU0939F1ZV</GSTIN><STATE>Maharashtra</STATE><GSTREGISTRATIONTYPE>Regular</GSTREGISTRATIONTYPE><ISTRANSPORTER>Yes</ISTRANSPORTER></LEDGSTREGDETAILS.LIST>
<COUNTRYOFRESIDENCE>India</COUNTRYOFRESIDENCE><CURRENCYNAME>?</CURRENCYNAME></LEDGER>
<LEDGER NAME="Old Style"><GUID>g2</GUID><PARENT>Sundry Debtors</PARENT><PARTYGSTIN>09ABCDE1234F1Z5</PARTYGSTIN><LEDSTATENAME>Uttar Pradesh</LEDSTATENAME></LEDGER>
</COLLECTION></DATA></BODY></ENVELOPE>`
	var ls []Ledger
	if err := ParseLedgers(strings.NewReader(x), func(l Ledger) error { ls = append(ls, l); return nil }); err != nil {
		t.Fatal(err)
	}
	p := ls[0].Party
	if p.GSTIN != "27AAPFU0939F1ZV" || p.State != "Maharashtra" || !p.Transporter || p.Country != "India" || p.Currency != "" {
		t.Errorf("latest registration: %+v", p)
	}
	if q := ls[1].Party; q.GSTIN != "09ABCDE1234F1Z5" || q.State != "Uttar Pradesh" {
		t.Errorf("older fields: %+v", q)
	}
}
