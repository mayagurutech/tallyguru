package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/bank"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/write"
	"github.com/mayagurutech/tallyguru/internal/xlsx"
)

// Bank returns the bank statement tool (module "bank"). The AI reads the statement and sends
// it page by page; the server checks balances, matches with the books and drafts suggestions.
func Bank(e *Env) []mcp.Tool {
	return []mcp.Tool{{Name: "bank_match",
		Description: "Bank statement read by you, sent page by page (action page: opening, closing, lines). Scans/photos: first tell the user " +
			"\"Reading scanned or photographed statements uses more AI tokens than PDF or Excel. Continue?\". " +
			"action match: match with books, reco statement, draft suggestions; clear: start a new statement.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp +
			`,"action":{"type":"string","enum":["page","match","clear"]},"ledger":{"type":"string","description":"Bank ledger"},"page":{"type":"integer"}` +
			`,"opening":{"type":"number"},"closing":{"type":"number"},"lines":{"type":"array","items":{"type":"object","properties":{` +
			`"date":{"type":"string"},"narration":{"type":"string"},"ref":{"type":"string"},"debit":{"type":"number"},"credit":{"type":"number"},"balance":{"type":"number"}}}}` +
			`,"excel":{"type":"boolean"}},"required":["action","ledger"]}`),
		Handler: e.bankMatch}}
}

// Rules returns the rules-memory tool (loaded with bulk or bank).
func Rules(e *Env) []mcp.Tool {
	return []mcp.Tool{{Name: "rules",
		Description: "This client's remembered ledger rules (narration words -> ledger). Add only when the user says yes to remembering a correction.",
		InputSchema: schema(`"action":{"type":"string","enum":["list","add","delete"]}`, `"id":{"type":"integer"}`, `"match":{"type":"string"}`,
			`"ledger":{"type":"string"}`, `"gst":{"type":"string"}`, `"tds":{"type":"string"}`),
		Handler: e.rules}}
}

func (e *Env) rules(a map[string]any) (string, error) {
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		return "", err
	}
	switch str(a, "action") {
	case "add":
		r, err := bk.AddRule(write.Rule{Match: str(a, "match"), Ledger: str(a, "ledger"), GST: str(a, "gst"), TDS: str(a, "tds")})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Rule %d saved for %s: %q -> %s.", r.ID, db.Label.Company, r.Match, r.Ledger), nil
	case "delete":
		id, _ := a["id"].(float64)
		if err := bk.DeleteRule(int64(id)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Rule %d deleted.", int64(id)), nil
	}
	rs, err := bk.Rules()
	if err != nil {
		return "", err
	}
	if len(rs) == 0 {
		return "No rules saved for " + db.Label.Company + " yet.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d rules\nId | Words | Ledger | GST | TDS | Used\n", db.Label.Company, len(rs))
	for _, r := range rs {
		l := r.Ledger
		if r.Broken {
			l += " (ledger not in Tally: rule not used)"
		}
		fmt.Fprintf(&b, "%d | %s | %s | %s | %s | %d\n", r.ID, r.Match, l, dash(r.GST), dash(r.TDS), r.Uses)
	}
	return strings.TrimSpace(b.String()), nil
}

func isoDate(s string) string {
	s = strings.TrimSpace(s)
	for _, f := range []string{time.DateOnly, "02-01-2006", "02/01/2006", "02-01-06", "02/01/06", "2-Jan-2006", "02 Jan 2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return t.Format(time.DateOnly)
		}
	}
	return s
}

func (e *Env) bankMatch(a map[string]any) (string, error) {
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		return "", err
	}
	ledger, ok := bk.BankLedger(str(a, "ledger"))
	if !ok {
		return "", fmt.Errorf("%q is not a bank ledger in Tally", str(a, "ledger"))
	}
	switch str(a, "action") {
	case "clear":
		bank.Clear(db.DB, ledger)
		return "Statement for " + ledger + " cleared; send page 1 of the new statement.", nil
	case "page":
		return e.bankPage(db, ledger, a)
	case "match":
		return e.bankRun(db, bk, ledger, a)
	}
	return "", fmt.Errorf("action is page, match or clear")
}

func (e *Env) bankPage(db *store.DB, ledger string, a map[string]any) (string, error) {
	n, _ := a["page"].(float64)
	p := bank.Page{Ledger: ledger, No: int(n)}
	if p.No < 1 {
		return "", fmt.Errorf("give the page number (1, 2, ...)")
	}
	var err error
	if p.Opening, err = write.Rupees(a["opening"]); err != nil {
		return "", err
	}
	if p.Closing, err = write.Rupees(a["closing"]); err != nil {
		return "", err
	}
	ls, _ := a["lines"].([]any)
	for i, x := range ls {
		m, _ := x.(map[string]any)
		l := bank.Line{Date: isoDate(fmt.Sprint(m["date"])), Narration: strings.TrimSpace(fmt.Sprint(m["narration"]))}
		if s, ok := m["ref"].(string); ok {
			l.Ref = strings.TrimSpace(s)
		}
		var e1, e2, e3 error
		l.Debit, e1 = write.Rupees(m["debit"])
		l.Credit, e2 = write.Rupees(m["credit"])
		if m["balance"] != nil {
			l.Balance, e3 = write.Rupees(m["balance"])
			l.HasBalance = true
		}
		if err := firstErr(e1, e2, e3); err != nil {
			return "", fmt.Errorf("line %d: %v", i+1, err)
		}
		p.Lines = append(p.Lines, l)
	}
	if err := bank.Check(db.DB, p); err != nil {
		return "Page not saved: " + err.Error(), nil
	}
	if err := bank.Save(db.DB, p); err != nil {
		return "", err
	}
	return fmt.Sprintf("Page %d of %s saved: %d lines, %s to %s, every running balance and opening/closing check out. Send the next page, or action match.",
		p.No, ledger, len(p.Lines), p.Lines[0].Date, p.Lines[len(p.Lines)-1].Date), nil
}

func firstErr(es ...error) error {
	for _, e := range es {
		if e != nil {
			return e
		}
	}
	return nil
}

func (e *Env) bankRun(db *store.DB, bk *write.Book, ledger string, a map[string]any) (string, error) {
	r, err := bank.Match(db.DB, ledger)
	if err != nil {
		return "", err
	}
	open := bank.BookBalance(db.DB, ledger, shiftDay(r.From, -1))
	reco := bank.Reconcile(r, bank.BookBalance(db.DB, ledger, r.To), open)

	// Suggestions for statement lines not in the books. Bank charges/interest are always asked.
	grp := fmt.Sprintf("Bank %s to %s", ledger, r.To)
	if ids, err := write.GroupIDs(db.DB, grp); err == nil { // re-run: replace earlier suggestions
		for _, id := range ids {
			write.Mark(db.DB, id, "discarded", "", "replaced by a new match run")
		}
	}
	type sug struct {
		line    bank.Line
		text    string
		draftID int64
	}
	var drafted, ask []sug
	for _, l := range r.Lines {
		if l.Status != "unmatched" {
			continue
		}
		s := sug{line: l}
		sg, ok := bk.Suggest(l.Narration, ledger)
		// Bank charges/interest are always asked (author), unless the user's own rule covers them.
		if bank.BankCharge(l.Narration) && (!ok || sg.Rule == nil) {
			s.text = "bank charge/interest: ask the user which ledger"
			ask = append(ask, s)
			continue
		}
		if !ok {
			s.text = "no suggestion: ask the user"
			ask = append(ask, s)
			continue
		}
		v := write.Voucher{Date: l.Date, Narr: l.Narration, Reference: ""}
		if l.Debit > 0 {
			v.Type = bk.TypeFor("Payment")
			v.Lines = []write.Line{{Ledger: sg.Ledger, Amount: -l.Debit}, {Ledger: ledger, Amount: l.Debit}}
		} else {
			v.Type = bk.TypeFor("Receipt")
			v.Lines = []write.Line{{Ledger: ledger, Amount: -l.Credit}, {Ledger: sg.Ledger, Amount: l.Credit}}
		}
		if errs, _ := bk.Check(&v); len(errs) > 0 {
			s.text = sg.Ledger + " (" + sg.Source + ") but: " + strings.Join(errs, "; ")
			ask = append(ask, s)
			continue
		}
		write.Save(db.DB, &v)
		write.SetGroup(db.DB, v.ID, grp)
		s.text, s.draftID = sg.Ledger+" ("+sg.Source+")", v.ID
		drafted = append(drafted, s)
	}

	if x, _ := a["excel"].(bool); x {
		return e.bankExcel(db, r, reco)
	}
	var b strings.Builder
	unB, unL := 0, 0
	for _, l := range r.Lines {
		if l.Status == "unmatched" {
			unL++
		}
	}
	for _, en := range r.Book {
		if en.Status == "unmatched" && en.Date >= r.From && en.Date <= r.To {
			unB++
		}
	}
	fmt.Fprintf(&b, "%s, statement %s to %s: %d lines. Matched %d, probable %d (confirm), not in books %d; book entries not in statement %d.\n",
		ledger, r.From, r.To, len(r.Lines), r.Matched, r.Probable, unL, unB)
	fmt.Fprintf(&b, "Reco as on %s: balance per books %s; + cheques issued not cleared %s; − deposits not credited %s; − bank debits not in books %s; + bank credits not in books %s",
		reco.AsOn, bank.Rs(reco.Books), bank.Rs(sumE(reco.IssuedNotCleared)), bank.Rs(-sumE(reco.DepositedNotCredited)),
		bank.Rs(sumL(reco.BankOnlyDebits, true)), bank.Rs(sumL(reco.BankOnlyCr, false)))
	if reco.OpeningGap != 0 {
		fmt.Fprintf(&b, "; older items before the statement %s", bank.Rs(reco.OpeningGap))
	}
	fmt.Fprintf(&b, " = balance per bank %s.", bank.Rs(reco.Bank))
	if reco.Difference != 0 {
		fmt.Fprintf(&b, " UNEXPLAINED DIFFERENCE %s: check the statement pages and book entries.", bank.Rs(reco.Difference))
	} else {
		b.WriteString(" Ties.")
	}
	b.WriteString("\n")
	top := e.top(a)
	if r.Probable > 0 {
		b.WriteString("Probable matches (same amount, several entries; confirm):\n")
		k := 0
		for _, l := range r.Lines {
			if l.Status == "probable" && k < top {
				fmt.Fprintf(&b, "- %s %s %s\n", l.Date, clip(l.Narration), bank.Rs(l.Debit+l.Credit))
				k++
			}
		}
	}
	if len(drafted) > 0 {
		fmt.Fprintf(&b, "Drafted for statement lines not in books (group %q; preview with drafts, post after the user confirms; if the user changes a ledger, ask whether to remember it with rules add):\n", grp)
		for i, s := range drafted {
			if i == top {
				fmt.Fprintf(&b, "... %d more\n", len(drafted)-i)
				break
			}
			fmt.Fprintf(&b, "- draft %d: %s %s %s -> %s\n", s.draftID, s.line.Date, clip(s.line.Narration), bank.Rs(s.line.Debit+s.line.Credit), s.text)
		}
	}
	if len(ask) > 0 {
		b.WriteString("Need the user's choice:\n")
		for i, s := range ask {
			if i == top {
				fmt.Fprintf(&b, "... %d more\n", len(ask)-i)
				break
			}
			fmt.Fprintf(&b, "- %s %s %s: %s\n", s.line.Date, clip(s.line.Narration), bank.Rs(s.line.Debit+s.line.Credit), s.text)
		}
	}
	b.WriteString("excel: true gives every line, the book entries and the reco statement.")
	return b.String(), nil
}

func sumE(es []bank.Entry) (t int64) {
	for _, e := range es {
		t += e.Amount
	}
	return
}

func sumL(ls []bank.Line, debit bool) (t int64) {
	for _, l := range ls {
		if debit {
			t += l.Debit
		} else {
			t += l.Credit
		}
	}
	return
}

func clip(s string) string {
	r := []rune(s)
	if len(r) > 32 {
		return string(r[:32]) + "…"
	}
	return s
}

func shiftDay(d string, n int) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

func (e *Env) bankExcel(db *store.DB, r *bank.Result, c bank.Reco) (string, error) {
	w, p, err := e.newBook(db.Label.Company, fmt.Sprintf("Bank reco %s %s", r.Ledger, r.To))
	if err != nil {
		return "", err
	}
	w.Sheet("Reco", "Item", "Amount")
	w.Row("Balance per books as on "+ddmmyyyy(c.AsOn), xlsx.Money(c.Books))
	w.Row("Add: cheques issued, not cleared", xlsx.Money(sumE(c.IssuedNotCleared)))
	w.Row("Less: deposits not credited", xlsx.Money(sumE(c.DepositedNotCredited)))
	w.Row("Less: bank debits not in books", xlsx.Money(-sumL(c.BankOnlyDebits, true)))
	w.Row("Add: bank credits not in books", xlsx.Money(sumL(c.BankOnlyCr, false)))
	w.Row("Older items before the statement", xlsx.Money(c.OpeningGap))
	w.Row("Balance per bank", xlsx.Money(c.Bank))
	w.Row("Unexplained difference", xlsx.Money(c.Difference))
	w.Sheet("Statement", "Date", "Narration", "Ref", "Debit", "Credit", "Balance", "Status")
	for _, l := range r.Lines {
		w.Row(ddmmyyyy(l.Date), l.Narration, l.Ref, xlsx.Money(l.Debit), xlsx.Money(l.Credit), xlsx.Money(l.Balance), l.Status)
	}
	w.Sheet("Books", "Date", "Type", "No.", "Against", "Narration", "Withdrawal", "Deposit", "Status")
	for _, en := range r.Book {
		var wd, dp int64
		if en.Amount > 0 {
			wd = en.Amount
		} else {
			dp = -en.Amount
		}
		w.Row(ddmmyyyy(en.Date), en.Type, en.Number, en.Other, en.Narration, xlsx.Money(wd), xlsx.Money(dp), en.Status)
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return "Saved the bank reco for " + r.Ledger + " to:\n" + p, nil
}
