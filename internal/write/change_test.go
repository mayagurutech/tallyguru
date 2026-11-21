package write

import (
	"strings"
	"testing"
)

func TestChangeXML(t *testing.T) {
	_, bk := testBook(t)
	before := Voucher{Type: "Payment", Base: "Payment", Number: "14", Date: "2026-07-01",
		Lines: []Line{{"Rent", -2500000}, {"HDFC Bank", 2500000}}}
	c, errs, _ := bk.NewChange("delete", before, "g-1", nil)
	if len(errs) > 0 || !strings.HasPrefix(c.Word, "DELETE-") {
		t.Fatal(errs, c.Word)
	}
	if x := ChangeXML("TEST MCP", c); !strings.Contains(x, `<VOUCHER DATE="1-Jul-2026" TAGNAME="Voucher Number" TAGVALUE="14" VCHTYPE="Payment" ACTION="Delete"><DATE>1-Jul-2026</DATE><VOUCHERTYPENAME>Payment</VOUCHERTYPENAME><VOUCHERNUMBER>14</VOUCHERNUMBER></VOUCHER>`) {
		t.Error(x)
	}
	after, _ := FromArgs(map[string]any{"type": "Payment", "date": "2026-07-01", "lines": []any{
		map[string]any{"ledger": "Rent", "dr": 26000.0}, map[string]any{"ledger": "HDFC Bank", "cr": 26000.0}}})
	c, errs, _ = bk.NewChange("alter", before, "g-1", &after)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	c.ID = 7
	x := ChangeXML("TEST MCP", c)
	for _, want := range []string{`ACTION="Alter">`, `<AMOUNT>-26000.00</AMOUNT>`, `Altered via LedgerGuru [TMG-7]`} {
		if !strings.Contains(x, want) {
			t.Errorf("missing %s\n%s", want, x)
		}
	}
	if strings.Count(x, "<VOUCHER ") != 1 {
		t.Error("two voucher tags")
	}
	// Sales cannot be altered here (stock lines not in the local books).
	before.Type, before.Base = "Sales", "Sales"
	if _, errs, _ := bk.NewChange("alter", before, "g-1", &after); len(errs) == 0 {
		t.Error("sales alter allowed")
	}
}

func TestDiscardChangeDraft(t *testing.T) {
	db, bk := testBook(t)
	c, _, _ := bk.NewChange("cancel", Voucher{Type: "Payment", Base: "Payment", Number: "1", Date: "2026-07-01"}, "g", nil)
	if err := SaveChange(db.DB, &c); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(db.DB, []int64{c.ID}); err == nil {
		t.Error("a change draft must not load as a postable draft")
	}
	if n, err := Discard(db.DB, []int64{c.ID}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := LoadChange(db.DB, c.ID); err == nil {
		t.Error("discarded change still waiting")
	}
}
