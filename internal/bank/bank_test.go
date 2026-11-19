package bank

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE ledgers (guid TEXT, name TEXT, parent TEXT, opening INTEGER, closing INTEGER, alter_id INTEGER, master_id INTEGER)`,
		`CREATE TABLE vouchers (guid TEXT, date TEXT, type TEXT, number TEXT, party TEXT, reference TEXT, narration TEXT, cancelled INTEGER, optional INTEGER, alter_id INTEGER, master_id INTEGER)`,
		`CREATE TABLE vlines (voucher_guid TEXT, seq INTEGER, ledger TEXT, amount INTEGER)`,
		`INSERT INTO ledgers VALUES ('b','HDFC','Bank Accounts',-10000000,0,0,0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func vch(db *sql.DB, guid, date, num, narr, other string, bankAmt int64) {
	db.Exec(`INSERT INTO vouchers VALUES (?,?,'Payment',?,'','',?,0,0,0,0)`, guid, date, num, narr)
	db.Exec(`INSERT INTO vlines VALUES (?,1,'HDFC',?)`, guid, bankAmt)
	db.Exec(`INSERT INTO vlines VALUES (?,2,?,?)`, guid, other, -bankAmt)
}

func TestPageChecks(t *testing.T) {
	db := testDB(t)
	good := Page{Ledger: "HDFC", No: 1, Opening: 100000, Closing: 130000, Lines: []Line{
		{Date: "2026-07-01", Credit: 50000, Balance: 150000, HasBalance: true},
		{Date: "2026-07-02", Debit: 20000, Balance: 130000, HasBalance: true}}}
	if err := Check(db, good); err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.Lines = append([]Line{}, good.Lines...)
	bad.Lines[1].Balance = 131000
	if err := Check(db, bad); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("wrong running balance not caught: %v", err)
	}
	bad = good
	bad.Closing = 140000
	if err := Check(db, bad); err == nil || !strings.Contains(err.Error(), "closing shown") {
		t.Errorf("closing mismatch not caught: %v", err)
	}
	Save(db, good)
	gap := Page{Ledger: "HDFC", No: 2, Opening: 120000, Closing: 120000, Lines: []Line{{Date: "2026-07-03", Credit: 0, Debit: 0}}}
	if err := Check(db, gap); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("gap between pages not caught: %v", err)
	}
}

func TestMatchAndReco(t *testing.T) {
	db := testDB(t)
	// Books (bank opening 1,00,000 Dr = -10000000 paise):
	vch(db, "v1", "2026-07-02", "1", "Rent July", "Rent", 2500000)         // payment 25,000, clears 03-07
	vch(db, "v2", "2026-07-05", "2", "Chq 004512 Gupta", "Gupta", 1000000) // cheque 10,000, clears (ref)
	vch(db, "v3", "2026-07-05", "3", "Chq 004513 Mehta", "Mehta", 1000000) // cheque 10,000, NOT cleared
	vch(db, "v4", "2026-07-20", "4", "Received Ram", "Ram", -1500000)      // receipt 15,000, not credited
	vch(db, "v5", "2026-07-10", "5", "Salary", "Salary", 4000000)          // 40,000 on 10-07, clears 18-07 (8 days: no match)
	p := Page{Ledger: "HDFC", No: 1, Opening: 10000000, Lines: []Line{
		{Date: "2026-07-03", Narration: "NEFT RENT", Debit: 2500000},
		{Date: "2026-07-08", Narration: "CHQ PAID", Ref: "004512", Debit: 1000000},
		{Date: "2026-07-18", Narration: "SALARY", Debit: 4000000},
		{Date: "2026-07-31", Narration: "BANK CHARGES", Debit: 59000},
	}}
	run := p.Opening
	for _, l := range p.Lines {
		run += l.Credit - l.Debit
	}
	p.Closing = run
	if err := Check(db, p); err != nil {
		t.Fatal(err)
	}
	Save(db, p)
	r, err := Match(db, "HDFC")
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]string{}
	for _, l := range r.Lines {
		st[l.Narration] = l.Status + ":" + l.GUID
	}
	if st["NEFT RENT"] != "matched:v1" || st["CHQ PAID"] != "matched:v2" || !strings.HasPrefix(st["SALARY"], "unmatched") {
		t.Errorf("statuses %v", st)
	}
	c := Reconcile(r, BookBalance(db, "HDFC", r.To), BookBalance(db, "HDFC", shift(r.From, -1)))
	// Books: 1,00,000 - 25,000 - 10,000 - 10,000 + 15,000 - 40,000 = 30,000
	if c.Books != 3000000 {
		t.Errorf("books %d", c.Books)
	}
	if c.Difference != 0 {
		t.Errorf("reco does not tie: %+v", c)
	}
	if len(c.IssuedNotCleared) != 2 || len(c.DepositedNotCredited) != 1 || len(c.BankOnlyDebits) != 2 {
		t.Errorf("reco lists %d %d %d", len(c.IssuedNotCleared), len(c.DepositedNotCredited), len(c.BankOnlyDebits))
	}
	if !BankCharge("BANK CHARGES") || BankCharge("NEFT RENT") {
		t.Error("bank charge detection")
	}
}

func TestProbableWhenSameAmountTwice(t *testing.T) {
	db := testDB(t)
	vch(db, "a", "2026-07-01", "1", "Petrol", "Fuel", 200000)
	vch(db, "b", "2026-07-02", "2", "Petrol", "Fuel", 200000)
	p := Page{Ledger: "HDFC", No: 1, Opening: 0, Closing: -200000, Lines: []Line{{Date: "2026-07-02", Narration: "POS FUEL", Debit: 200000}}}
	Save(db, p)
	r, _ := Match(db, "HDFC")
	if r.Lines[0].Status != "probable" || r.Lines[0].GUID != "b" {
		t.Errorf("%+v", r.Lines[0])
	}
}
