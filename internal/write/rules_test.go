package write

import "testing"

func TestRulesAndSuggest(t *testing.T) {
	db, bk := testBook(t)
	// Past entries: two rent payments with "landlord sharma".
	for i, g := range []string{"p1", "p2"} {
		db.Exec(`INSERT INTO vouchers VALUES (?, '2026-05-0'||?, 'Payment', ?, '', '', 'NEFT landlord Sharma rent', 0, 0, 0, 0)`, g, i+1, g)
		db.Exec(`INSERT INTO vlines VALUES (?, 1, 'Rent', -100)`, g)
		db.Exec(`INSERT INTO vlines VALUES (?, 2, 'HDFC Bank', 100)`, g)
	}
	s, ok := bk.Suggest("NEFT-SHARMA-RENT JUL", "HDFC Bank")
	if !ok || s.Ledger != "Rent" || s.Rule != nil {
		t.Fatalf("past-entry suggestion: %+v %v", s, ok)
	}
	// A rule wins over past entries.
	r, err := bk.AddRule(Rule{Match: "Sharma", Ledger: "ram traders"})
	if err != nil || r.Ledger != "Ram Traders" {
		t.Fatal(r, err)
	}
	if s, _ := bk.Suggest("NEFT-SHARMA-RENT JUL", "HDFC Bank"); s.Ledger != "Ram Traders" || s.Rule == nil {
		t.Errorf("rule not used first: %+v", s)
	}
	if _, err := bk.AddRule(Rule{Match: "jio", Ledger: "No Such Ledger"}); err == nil {
		t.Error("rule to a missing ledger accepted")
	}
	if err := bk.DeleteRule(r.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := bk.Suggest("UPI 99881 xyz", "HDFC Bank"); s.Ledger != "" {
		t.Errorf("guessed from nothing: %+v", s)
	}
}
