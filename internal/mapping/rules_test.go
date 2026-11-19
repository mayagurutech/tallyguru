package mapping

import "testing"

var exp = []string{"Indirect Expenses"}

func get(l Ledger, fam string) Suggestion {
	for _, s := range Suggest(l) {
		if s.Family == fam {
			return s
		}
	}
	return Suggestion{}
}

func TestTDSRules(t *testing.T) {
	for name, want := range map[string]string{
		"Office Rent":                "194I(b) building rent",
		"Godown Kiraya":              "194I(b) building rent",
		"Machinery Hire Charges":     "194I(a) plant rent",
		"Professional Fees":          "194J(b) professional",
		"Legal Expenses":             "194J(b) professional",
		"Audit Fees":                 "194J(b) professional",
		"Software Development":       "194J(a) technical",
		"Contractor Payment":         "194C contractor",
		"Labour Charges":             "194C contractor",
		"Freight Inward":             "194C contractor",
		"Commission on Sales":        "194H commission",
		"Interest on Unsecured Loan": "194A interest",
		"Staff Salary":               "192 salary",
		"Electricity Charges":        "none",
		"Current Account Charges":    "none", // "rent" must not match inside "current"
	} {
		l := Ledger{Name: name, Groups: exp}
		if name == "Current Account Charges" {
			if s := get(l, TDS); s.Tag == "194I(b) building rent" {
				t.Errorf("%s matched rent", name)
			}
			continue
		}
		if s := get(l, TDS); s.Tag != want {
			t.Errorf("%s: got %q want %q", name, s.Tag, want)
		}
	}
}

func TestGSTRules(t *testing.T) {
	for name, want := range map[string]string{
		"Motor Car Expenses": "blocked 17(5)",
		"Staff Welfare":      "blocked 17(5)",
		"Diwali Gifts":       "blocked 17(5)",
		"Salary":             "non-gst",
		"Petrol Expenses":    "non-gst",
		"Legal Fees":         "rcm",
		"GTA Freight":        "rcm",
		"Office Rent":        "eligible",
		"Intrest on Capital": "non-gst",
		"Fabrication Work":   "eligible",
		"Repair @18%":        "eligible",
	} {
		if s := get(Ledger{Name: name, Groups: exp}, GST); s.Tag != want {
			t.Errorf("%s: got %q want %q", name, s.Tag, want)
		}
	}
}

func TestTaxAndFlags(t *testing.T) {
	dt := []string{"Duties & Taxes", "Current Liabilities"}
	for name, want := range map[string]string{
		"Input CGST @9%": "cgst input", "Output IGST": "igst output", "SGST Payable": "sgst output",
		"TDS on Contract 194C": "tds payable", "TCS Payable": "tcs", "GST RCM Payable": "gst rcm payable",
	} {
		if s := get(Ledger{Name: name, Groups: dt}, Tax); s.Tag != want {
			t.Errorf("%s: got %q want %q", name, s.Tag, want)
		}
	}
	// Goods/purchase ledgers never get a TDS question (194Q is per supplier).
	if s := get(Ledger{Name: "Battery", Groups: []string{"Consumable Goods", "Purchase Accounts"}}, TDS); s.Tag != "" {
		t.Errorf("purchase ledger got TDS %v", s)
	}
	if s := get(Ledger{Name: "P.CGST@6%", Groups: dt}, Tax); s.Tag != "cgst input" {
		t.Errorf("P.CGST: %v", s)
	}
	// A ledger named after a person under the user's own "Salary" group.
	if s := get(Ledger{Name: "Vipul", Groups: []string{"Salary", "Indirect Expenses"}}, TDS); s.Tag != "192 salary" || s.Conf != Medium {
		t.Errorf("group match: %v", s)
	}
	if s := get(Ledger{Name: "Intrest on Capital", Groups: exp}, TDS); s.Tag != "none" {
		t.Errorf("interest on capital: %v", s)
	}
	if s := get(Ledger{Name: "X", Groups: []string{"Mumbai Debtors", "Sundry Debtors", "Current Assets"}}, Flag); s.Tag != "debtor" {
		t.Errorf("sub-group debtor: %v", s)
	}
	if s := get(Ledger{Name: "HDFC", Groups: []string{"Bank Accounts"}}, Flag); s.Tag != "bank" || s.Conf != High {
		t.Errorf("bank: %v", s)
	}
	// Families that do not apply are absent.
	if n := len(Suggest(Ledger{Name: "Ram Traders", Groups: []string{"Sundry Debtors"}})); n != 1 {
		t.Errorf("debtor should only have a flag, got %d suggestions", n)
	}
	// Every suggested tag is a valid tag.
	for _, l := range []Ledger{{"Office Rent", exp}, {"Input CGST", dt}, {"Cash", []string{"Cash-in-Hand"}}, {"Sale", []string{"Purchase Accounts"}}} {
		for _, s := range Suggest(l) {
			if _, ok := Valid(s.Family, s.Tag); !ok {
				t.Errorf("%s: invalid tag %q in %s", l.Name, s.Tag, s.Family)
			}
		}
	}
}

func TestTDSThresholds(t *testing.T) {
	cases := []struct {
		tag         string
		pay, before int64
		want        bool
	}{
		{"194C contractor", 30000, 0, false},     // single exactly at limit
		{"194C contractor", 30001, 0, true},      // single over
		{"194C contractor", 20000, 80000, false}, // annual exactly 1 lakh
		{"194C contractor", 20000, 80001, true},
		{"194J(b) professional", 50000, 0, false},
		{"194J(b) professional", 10000, 40001, true},
		{"194H commission", 20001, 0, true},
		{"194H commission", 15000, 0, false},
		{"194I(b) building rent", 50000, 0, false}, // per month
		{"194I(b) building rent", 50001, 0, true},
		{"194A interest", 10001, 0, true},
		{"194Q purchase", 1000000, 4000000, false},
		{"194Q purchase", 1000000, 4000001, true},
		{"none", 9999999, 0, false},
	}
	for _, c := range cases {
		if got := Due(c.tag, c.pay, c.before); got != c.want {
			t.Errorf("%s pay %d before %d: got %v", c.tag, c.pay, c.before, got)
		}
	}
	for tag := range Thresholds {
		if _, ok := Valid(TDS, tag); !ok && tag != "194Q purchase" { // 194Q is per supplier, not a ledger tag
			t.Errorf("threshold tag %q not a TDS tag", tag)
		}
	}
}

// Found on the pilot (16 Nov 2026).
func TestPilotTaxCases(t *testing.T) {
	prov := []string{"Provisions", "Current Liabilities"}
	if s := get(Ledger{Name: "TDS Payble", Groups: prov}, Tax); s.Tag != "tds payable" {
		t.Errorf("TDS under Provisions: %+v", s)
	}
	if s := get(Ledger{Name: "TDS Receivable", Groups: []string{"Current Assets"}}, Tax); s.Tag != "" {
		t.Errorf("TDS receivable is an asset, not a tax payable: %+v", s)
	}
	if s := get(Ledger{Name: "Interest on TDS", Groups: exp}, TDS); s.Tag != "none" {
		t.Errorf("interest on late TDS: %+v", s)
	}
	// Name says output: usage in one purchase entry must not flip it to input.
	s := get(Ledger{Name: "CGST Output", Groups: []string{"Duties & Taxes", "Current Liabilities"}}, Tax)
	if d := Decide(s, &Evidence{Entries: 1, WithCost: 1}); d.Tag != "cgst output" {
		t.Errorf("CGST Output decided %+v", d)
	}
}
