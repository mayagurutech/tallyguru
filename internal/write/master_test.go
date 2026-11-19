package write

import (
	"strings"
	"testing"
)

func TestMasterChecksAndXML(t *testing.T) {
	_, bk := testBook(t)
	bad := map[string]map[string]any{
		"already exists":         {"kind": "ledger", "name": "rent", "parent": "Indirect Expenses"},
		"group \"Indirect Exp\"": {"kind": "ledger", "name": "Electricity", "parent": "Indirect Exp"},
		"check digit":            {"kind": "ledger", "name": "New Party", "parent": "Sundry Debtors", "gstin": "23AAPFU0939F1ZV"},
		"GSTIN is for":           {"kind": "ledger", "name": "New Party", "parent": "Sundry Debtors", "gstin": "23AAPFU0939F1Z3", "state": "Gujarat"},
		"not found to alter":     {"kind": "ledger", "action": "alter", "name": "Nobody", "parent": "Sundry Debtors"},
		"unit is required":       {"kind": "stock_item", "name": "Copper Wire"},
		"nothing to change":      {"kind": "ledger", "action": "alter", "name": "Rent"},
	}
	for want, a := range bad {
		m, err := MasterFromArgs(a)
		if err != nil {
			t.Fatal(err)
		}
		errs, _ := bk.CheckMaster(&m)
		if !strings.Contains(strings.Join(errs, ";"), want) {
			t.Errorf("want %q, got %v", want, errs)
		}
	}
	// Good create: state taken from the GSTIN.
	m, _ := MasterFromArgs(map[string]any{"kind": "ledger", "name": "Gupta & Sons", "parent": "sundry debtors",
		"gstin": "23aapfu0939f1z3", "opening_dr": 5000.0})
	if errs, _ := bk.CheckMaster(&m); len(errs) > 0 {
		t.Fatal(errs)
	}
	x := MasterXML("TEST MCP", m)
	for _, want := range []string{`<LEDGER NAME="Gupta &amp; Sons" ACTION="Create">`, `<PARENT>Sundry Debtors</PARENT>`,
		`<OPENINGBALANCE>-5000.00</OPENINGBALANCE><ISDEEMEDPOSITIVE>Yes</ISDEEMEDPOSITIVE>`,
		`<LEDSTATENAME>Madhya Pradesh</LEDSTATENAME>`, `<PARTYGSTIN>23AAPFU0939F1Z3</PARTYGSTIN>`, `<REPORTNAME>All Masters</REPORTNAME>`} {
		if !strings.Contains(x, want) {
			t.Errorf("XML missing %s\n%s", want, x)
		}
	}
	// Alter keeps the before values for undo and renames via NAME attr.
	m, _ = MasterFromArgs(map[string]any{"kind": "ledger", "action": "alter", "name": "ram traders", "new_name": "Ram Traders Pvt Ltd"})
	if errs, _ := bk.CheckMaster(&m); len(errs) > 0 {
		t.Fatal(errs)
	}
	if !strings.Contains(m.Before, "parent Sundry Debtors") || !strings.Contains(m.Preview(), "Now: ") {
		t.Errorf("before %q", m.Before)
	}
	if x := MasterXML("TEST MCP", m); !strings.Contains(x, `<LEDGER NAME="Ram Traders" ACTION="Alter"><NAME.LIST><NAME>Ram Traders Pvt Ltd</NAME>`) {
		t.Error(x)
	}
}
