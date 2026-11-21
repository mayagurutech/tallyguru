package tools

import (
	"strings"
	"testing"

	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tbcheck"
)

// Seen on a client: a value-basis closing-stock ledger (closing stock entered per year in Tally,
// group renamed) was reported as opening + entries; carried forward, the gap went into Profit &
// Loss A/c opening. Answers must give Tally's stock value, and openings must be checked.
func TestStockLedgerValueAndOpenings(t *testing.T) {
	e, db := env(t)
	authorise(t, e)
	db.ReplaceGroups([]tally.Group{{Name: "Current Assets", Reserved: "Current Assets"},
		{Name: "Closing Stock", Parent: "Current Assets", Reserved: "Stock-in-Hand"},
		{Name: "Cash-in-Hand", Parent: "Current Assets"}, {Name: "Sundry Debtors", Parent: "Current Assets"}, {Name: "Sales Accounts"}})
	db.ReplaceLedgers([]tally.Ledger{
		{Name: "Cash", GUID: "1", Parent: "Cash-in-Hand", Closing: -50000},
		{Name: "Ram Traders", GUID: "2", Parent: "Sundry Debtors", Closing: -118000},
		{Name: "Sales", GUID: "3", Parent: "Sales Accounts", Closing: 168000},
		{Name: "STOCK", GUID: "4", Parent: "Closing Stock", Opening: -100000, Closing: -1109575200},
		{Name: tbcheck.PLLedger, GUID: "5", Opening: 100000, Closing: 100000},
	})
	if r, _ := tbcheck.Run(db); !r.OK() {
		t.Fatalf("setup TB: %s", r)
	}
	tb, err := e.trialBalance(map[string]any{})
	if err != nil || !strings.Contains(tb, "Closing stock, Tally's value") || !strings.Contains(tb, "STOCK 1,10,95,752.00 Dr (entries only: 1,000.00 Dr)") {
		t.Fatalf("trial balance must give Tally's stock value:\n%s %v", tb, err)
	}
	lb, err := e.ledgerBalance(map[string]any{"ledger": "STOCK"})
	if err != nil || !strings.Contains(lb, "closing stock 1,10,95,752.00 Dr (Tally's value") || !strings.Contains(lb, "Entries only: closing 1,000.00 Dr") {
		t.Fatalf("ledger balance must give Tally's stock value:\n%s %v", lb, err)
	}
	// A P&L opening is drafted with a warning; a stock ledger opening is refused when the company keeps inventory.
	pl, err := e.draftMaster(map[string]any{"kind": "ledger", "action": "alter", "name": tbcheck.PLLedger, "opening_cr": float64(5077862)})
	if err != nil || !strings.Contains(pl, "never a balancing figure") {
		t.Fatalf("P&L opening needs the warning:\n%s %v", pl, err)
	}
	db.ReplaceStockItems([]tally.StockItem{{Name: "Granules", Unit: "Kg", ClosingValue: -100000}})
	st, err := e.draftMaster(map[string]any{"kind": "ledger", "action": "alter", "name": "STOCK", "opening_dr": float64(11095752)})
	if err != nil || !strings.Contains(st, "Not drafted") || !strings.Contains(st, "stock items' opening") {
		t.Fatalf("stock ledger opening in an inventory company must be refused:\n%s %v", st, err)
	}
}
