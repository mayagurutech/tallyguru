package tools

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tbcheck"
)

var now = time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

func env(t *testing.T) (*Env, *store.DB) {
	root := t.TempDir()
	box, _ := crypt.New(bytes.Repeat([]byte{3}, 32))
	l := store.Label{Company: "TEST MCP", Source: store.Live}
	db, err := store.Open(root, l)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.ReplaceLedgers([]tally.Ledger{
		{Name: "Cash", GUID: "1", Parent: "Cash-in-Hand", Closing: -50000},
		{Name: "Ram Traders", GUID: "2", Parent: "Sundry Debtors", Closing: -118000},
		{Name: "Sales", GUID: "3", Parent: "Sales Accounts", Closing: 168000},
	})
	tx, _ := db.BeginVouchers()
	tx.Put(tally.Voucher{GUID: "v1", Date: d("2026-04-03"), Lines: []tally.LedgerLine{{Ledger: "Ram Traders", Amount: -118000}, {Ledger: "Sales", Amount: 118000}}})
	tx.Put(tally.Voucher{GUID: "v2", Date: d("2026-05-03"), Lines: []tally.LedgerLine{{Ledger: "Cash", Amount: -50000}, {Ledger: "Sales", Amount: 50000}}})
	tx.Commit()
	return &Env{Root: root, Cfg: config.Default(), Box: box, Now: func() time.Time { return now }}, db
}

func d(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func authorise(t *testing.T, e *Env) {
	l := store.Label{Company: "TEST MCP", Source: store.Live}
	if err := (auth.Store{Dir: l.Dir(e.Root), Box: e.Box}).Add(auth.Record{Company: "TEST MCP", Kind: auth.Owner, FY: auth.FY(now)}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestToolsRefuseWithoutAuthorisation(t *testing.T) {
	e, db := env(t)
	tbcheck.Run(db)
	if _, err := e.trialBalance(map[string]any{}); err == nil || !strings.Contains(err.Error(), "no authorisation") {
		t.Fatalf("expected authorisation refusal, got %v", err)
	}
}

func TestToolsRefuseOnTBMismatch(t *testing.T) {
	e, db := env(t)
	authorise(t, e)
	db.ReplaceLedgers([]tally.Ledger{{Name: "Cash", GUID: "1", Closing: -1}, {Name: "Ram Traders", GUID: "2", Closing: -118000}, {Name: "Sales", GUID: "3", Closing: 168000}})
	tbcheck.Run(db)
	for name, f := range map[string]func(map[string]any) (string, error){"trial_balance": e.trialBalance, "outstanding": e.outstanding} {
		if _, err := f(map[string]any{"ledger": "Cash"}); err == nil || !strings.Contains(err.Error(), "does NOT match") {
			t.Errorf("%s must refuse on mismatch, got %v", name, err)
		}
	}
}

func TestAnswers(t *testing.T) {
	e, db := env(t)
	authorise(t, e)
	if r, _ := tbcheck.Run(db); !r.OK() {
		t.Fatalf("setup TB: %s", r)
	}
	tb, err := e.trialBalance(map[string]any{})
	if err != nil || !strings.Contains(tb, "Total | | 1,680.00 | 1,680.00") {
		t.Fatalf("trial balance:\n%s %v", tb, err)
	}
	lb, err := e.ledgerBalance(map[string]any{"ledger": "ram"})
	if err != nil || !strings.Contains(lb, "closing 1,180.00 Dr") || !strings.Contains(lb, "| 1 entry") || strings.Contains(lb, "Month |") {
		t.Fatalf("ledger balance (concise):\n%s %v", lb, err)
	}
	lb, err = e.ledgerBalance(map[string]any{"ledger": "ram", "monthly": true, "entries": float64(5)})
	if err != nil || !strings.Contains(lb, "Apr 2026 | 1 | 1,180.00 | 0.00") ||
		!strings.Contains(lb, "03-04-2026 |  | - | Sales | 1,180.00 Dr") {
		t.Fatalf("ledger balance (monthly + entries):\n%s %v", lb, err)
	}
	os, err := e.outstanding(map[string]any{"kind": "receivable"})
	if err != nil || !strings.Contains(os, "1,180.00 from 1 party") {
		t.Fatalf("outstanding:\n%s %v", os, err)
	}
}

func TestPrivacyMode(t *testing.T) {
	e, db := env(t)
	authorise(t, e)
	tbcheck.Run(db)
	e.Cfg.PrivacyMode = true
	type step struct {
		name string
		call func() (string, error)
	}
	for _, st := range []step{ // fixed order: a code exists only after an answer showed it
		{"outstanding", func() (string, error) { return e.outstanding(map[string]any{"kind": "receivable"}) }},
		{"tb group (case)", func() (string, error) { return e.trialBalance(map[string]any{"group": "sundry debtors"}) }},
		{"ledger by name", func() (string, error) { return e.ledgerBalance(map[string]any{"ledger": "Ram Traders"}) }},
		{"ledger by code", func() (string, error) { return e.ledgerBalance(map[string]any{"ledger": "p001"}) }},
	} {
		name, call := st.name, st.call
		out, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(out, "Ram") || !strings.Contains(out, "P001") {
			t.Errorf("%s leaked or missing code:\n%s", name, out)
		}
	}
	// A party in a sub-group of Sundry Debtors (itself under Current Assets) is coded too,
	// and counted in outstanding.
	db.ReplaceGroups([]tally.Group{{Name: "Current Assets"}, {Name: "Sundry Debtors", Parent: "Current Assets"},
		{Name: "Mumbai Debtors", Parent: "Sundry Debtors"}})
	if got := e.show(db, "Shah & Sons", "Mumbai Debtors"); got == "Shah & Sons" {
		t.Error("sub-group party leaked its name in privacy mode")
	}
	// Non-party ledgers keep their names.
	if out, _ := e.ledgerBalance(map[string]any{"ledger": "Cash"}); !strings.Contains(out, "Cash") {
		t.Error("non-party ledger should keep its name")
	}
	// Code is stable.
	if c, _ := db.PartyCode("Ram Traders"); c != "P001" {
		t.Errorf("code changed: %s", c)
	}
}

func TestBillAgeing(t *testing.T) {
	e, db := env(t)
	authorise(t, e)
	tbcheck.Run(db)
	db.ReplaceBills([]tally.Bill{
		{Name: "INV/1", Party: "Ram Traders", Date: now.AddDate(0, 0, -10), Closing: -100000},
		{Name: "INV/2", Party: "Ram Traders", Date: now.AddDate(0, 0, -200), Closing: -18000},
	})
	out, err := e.outstanding(map[string]any{"kind": "receivable"})
	if err != nil || !strings.Contains(out, "1,180.00 in 2 bills") || !strings.Contains(out, "0-30 | 1 | 1,000.00") ||
		!strings.Contains(out, "181-365 | 1 | 180.00") || !strings.Contains(out, "Ram Traders | INV/1") {
		t.Fatalf("ageing:\n%s %v", out, err)
	}
	e.Cfg.PrivacyMode = true
	if out, _ = e.outstanding(map[string]any{"kind": "receivable"}); strings.Contains(out, "Ram") {
		t.Fatalf("privacy mode leaked party in bills:\n%s", out)
	}
}

func TestToolDefinitionSize(t *testing.T) {
	e, _ := env(t)
	b, _ := json.Marshal(Core(e))
	// ~4 characters per token; target < 1,500 tokens per profile.
	if tok := len(b) / 4; tok > 1500 {
		t.Fatalf("core tool definitions ~%d tokens, over 1,500 budget", tok)
	} else {
		t.Logf("core tool definitions: %d bytes, ~%d tokens", len(b), tok)
	}
}

func TestDayBookExcel(t *testing.T) {
	e, db := env(t)
	authorise(t, e)
	db.RebuildSummaries()
	tbcheck.Run(db)
	e.Cfg.ExportDir = t.TempDir()
	out, err := e.runReport(map[string]any{"name": "daybook", "fy": "2026-27", "excel": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Saved 2 vouchers and 4 ledger lines") || !strings.Contains(out, e.Cfg.ExportDir) {
		t.Errorf("%s", out)
	}
	sum, err := e.runReport(map[string]any{"name": "daybook", "fy": "2026-27"})
	if err != nil || !strings.Contains(sum, "2 vouchers") || strings.Contains(sum, "Saved") {
		t.Errorf("%v %s", err, sum)
	}
}
