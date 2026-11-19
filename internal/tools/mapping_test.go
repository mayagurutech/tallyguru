package tools

import (
	"strings"
	"testing"

	"github.com/mayagurutech/tallyguru/internal/tally"
)

func TestMappingFlow(t *testing.T) {
	e, db := env(t)
	authorise(t, e)
	db.ReplaceGroups([]tally.Group{{Name: "Current Assets"}, {Name: "Sundry Debtors", Parent: "Current Assets"},
		{Name: "Cash-in-Hand", Parent: "Current Assets"}, {Name: "Sales Accounts"}, {Name: "Indirect Expenses"}})
	db.ReplaceLedgers([]tally.Ledger{
		{Name: "Cash", GUID: "1", Parent: "Cash-in-Hand"},
		{Name: "Ram Traders", GUID: "2", Parent: "Sundry Debtors"},
		{Name: "Sales", GUID: "3", Parent: "Sales Accounts"},
		{Name: "Office Rent", GUID: "4", Parent: "Indirect Expenses"},
		{Name: "Misc Charges", GUID: "5", Parent: "Indirect Expenses"},
	})
	st, err := e.mappingStatus(map[string]any{})
	// Unused ledgers are decided by the tool; nothing to ask.
	if err != nil || !strings.Contains(st, "Nothing needs the user") {
		t.Fatalf("status:\n%s %v", st, err)
	}
	rv, _ := e.mappingReview(map[string]any{})
	if !strings.HasPrefix(rv, "Nothing pending") {
		t.Fatalf("review:\n%s", rv)
	}
	if _, err := e.mappingSet(map[string]any{"family": "tds", "tag": "194Z", "ledgers": []any{"Misc Charges"}}); err == nil {
		t.Fatal("invalid tag accepted")
	}
	out, _ := e.mappingSet(map[string]any{"family": "tds", "tag": "194c contractor", "ledgers": []any{"misc charges", "Cash"}})
	if !strings.Contains(out, "for 1 ledger") || !strings.Contains(out, "Cash") {
		t.Fatalf("set: %s", out)
	}
	if st, _ = e.mappingStatus(map[string]any{}); !strings.Contains(st, "tds | 1 |") {
		t.Fatalf("after set:\n%s", st)
	}
	// Rename keeps the confirmed tag; a new ledger shows as pending.
	db.ReplaceLedgers([]tally.Ledger{
		{Name: "Cash", GUID: "1", Parent: "Cash-in-Hand"}, {Name: "Ram Traders", GUID: "2", Parent: "Sundry Debtors"},
		{Name: "Sales", GUID: "3", Parent: "Sales Accounts"}, {Name: "Office Rent", GUID: "4", Parent: "Indirect Expenses"},
		{Name: "Sundry Charges", GUID: "5", Parent: "Indirect Expenses"}, {Name: "Audit Fees", GUID: "6", Parent: "Indirect Expenses"},
	})
	e.mappingStatus(map[string]any{}) // refresh after the re-sync
	rs, _ := db.MapRows("tds", false)
	if len(rs) != 3 { // Office Rent, Sundry Charges, Audit Fees
		t.Fatalf("new ledger missing: %d tds rows", len(rs))
	}
	for _, r := range rs {
		if r.Name == "Sundry Charges" && r.Tag != "194C contractor" {
			t.Errorf("renamed ledger lost its tag: %+v", r)
		}
	}
	// Privacy mode: party shown as a code, and can be set by code.
	e.Cfg.PrivacyMode = true
	db.ReplaceLedgers([]tally.Ledger{{Name: "Shah & Sons", GUID: "7", Parent: "Sundry Debtors"}})
	rv, _ = e.mappingReview(map[string]any{"family": "flag"})
	if strings.Contains(rv, "Shah") {
		t.Fatalf("privacy leak:\n%s", rv)
	}
}

func TestReasonMask(t *testing.T) {
	cases := map[string]string{
		"credit taken on 100% (2.3 lakh a year) but looks blocked: likely blocked credit u/s 17(5)": "credit taken on # (#) but looks blocked: likely blocked credit u/s 17(5)",
		"rent; 45000 a year":                      "rent; #",
		"TDS deducted on 80% of it; 194J(b) fees": "TDS deducted on # of it; 194J(b) fees",
		"cash over 1.5 cr, see 40A(3)":            "cash over #, see 40A(3)",
	}
	for in, want := range cases {
		if got := reNum.ReplaceAllString(in, "#"); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
