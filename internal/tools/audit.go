package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/audit"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/xlsx"
)

// Audit returns the Phase 4 audit tool: one tool with a short catalog.
func Audit(e *Env) []mcp.Tool {
	var ids []string
	for _, c := range audit.Checks {
		ids = append(ids, `"`+c.ID+`"`)
	}
	ts := []mcp.Tool{{Name: "run_audit",
		Description: "Books-only audit checks on all entries (facts, not conclusions; the auditor judges). " +
			"check: all (one line per check) or one id for its top cases. Period: fy like 2026-27 (default: latest year). " +
			"excel: workbook with Summary, Issues, Working on this PC.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp +
			`,"check":{"type":"string","enum":["all",` + strings.Join(ids, ",") + `]},"fy":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},` +
			`"excel":{"type":"boolean"},"top":{"type":"integer"}}}`),
		Handler: e.runAudit}}
	for i := range ts {
		ts[i].Handler = e.withNote(ts[i].Handler)
	}
	return ts
}

func (e *Env) runAudit(a map[string]any) (string, error) {
	db, err := e.open(a, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var first, last string
	db.QueryRow(`SELECT COALESCE(MIN(month),''), COALESCE(MAX(month),'') FROM month_ledger`).Scan(&first, &last)
	fy := str(a, "fy")
	if fy == "" && str(a, "from") == "" {
		fy = report.FYOf(last) // audit default: the whole latest financial year
	}
	p, err := report.ParsePeriod(fy, str(a, "from"), str(a, "to"), first, last)
	if err != nil {
		return "", err
	}
	rs, err := audit.Run(db.DB, p)
	if err != nil {
		return "", err
	}
	if x, _ := a["excel"].(bool); x {
		return e.auditExcel(db, p, rs)
	}
	var b strings.Builder
	check := str(a, "check")
	if check == "" || check == "all" {
		fmt.Fprintf(&b, "%s, audit checks, %s (all entries; facts for the auditor to judge)\nCheck | Cases | Amount\n", db.Label.Company, p.Label())
		for _, r := range rs {
			fmt.Fprintf(&b, "%s | %s | %s\n", title(r), cases(r), amt(r.Amount))
		}
		b.WriteString("Ask for one check to see its cases, or excel for every case with its entries.")
		return b.String(), nil
	}
	for _, r := range rs {
		if r.ID != check {
			continue
		}
		fmt.Fprintf(&b, "%s, %s, %s: %s, %s\n", db.Label.Company, title(r), p.Label(), cases(r), amt(r.Amount))
		top := min(e.top(a), len(r.Issues))
		if top > 0 {
			b.WriteString("Date | Party / ledger | Amount | Detail\n")
			for _, i := range r.Issues[:top] {
				fmt.Fprintf(&b, "%s | %s | %s | %s\n", dash(i.Date), dash(e.showParty(db, i.Party)), amt(i.Amount), i.Detail)
			}
			if len(r.Issues) > top {
				fmt.Fprintf(&b, "(%d more: ask for excel)\n", len(r.Issues)-top)
			}
		}
		if r.Note != "" {
			b.WriteString("Note: " + r.Note + "\n")
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	return "", fmt.Errorf("unknown check %q", check)
}

func title(r audit.Result) string {
	if r.Section != "" {
		return r.Title + ", " + r.Section
	}
	return r.Title
}

func cases(r audit.Result) string {
	if r.NotAvailable {
		return "not available"
	}
	if r.Count == 1 {
		return "1 case"
	}
	return fmt.Sprintf("%d cases", r.Count)
}

// showParty applies privacy mode to a ledger name whose group is not known here.
func (e *Env) showParty(db *store.DB, name string) string {
	if name == "" || !e.Cfg.PrivacyMode {
		return name
	}
	var parent string
	db.QueryRow(`SELECT parent FROM ledgers WHERE name = ?`, name).Scan(&parent)
	return e.show(db, name, parent)
}

// auditExcel writes one workbook: Summary, Issues, Working (every entry behind each issue).
func (e *Env) auditExcel(db *store.DB, p report.Period, rs []audit.Result) (string, error) {
	w, path, err := e.newBook(db.Label.Company, "Audit checks "+p.Label())
	if err != nil {
		return "", err
	}
	n := 0
	err = func() error {
		if err := w.Sheet("Summary", "No.", "Check", "Section (Income-tax Act 2025, 1961 in brackets)", "Cases", "Amount", "Note"); err != nil {
			return err
		}
		w.Row("", db.Label.Company+", "+p.Label()+". Facts found on all entries; judgment and sign-off stay with the auditor.")
		for i, r := range rs {
			c := any(r.Count)
			if r.NotAvailable {
				c = "not available"
			}
			if err := w.Row(i+1, r.Title, r.Section, c, xlsx.Money(r.Amount), r.Note); err != nil {
				return err
			}
		}
		if err := w.Sheet("Issues", "Issue", "Check", "Date", "Party / ledger", "Amount", "Detail", "Auditor's remark"); err != nil {
			return err
		}
		for _, r := range rs {
			for _, i := range r.Issues {
				n++
				if err := w.Row(n, r.Title, i.Date, i.Party, xlsx.Money(i.Amount), i.Detail, ""); err != nil {
					return err
				}
			}
		}
		if err := w.Sheet("Working", "Issue", "Date", "Type", "No.", "Ledger", "Debit", "Credit", "Narration"); err != nil {
			return err
		}
		k := 0
		for _, r := range rs {
			for _, i := range r.Issues {
				k++
				for _, g := range i.Vouchers {
					if err := workingRows(w, db, k, g); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}()
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return fmt.Sprintf("Saved %d checks with %d issues (and every entry behind them) for %s to:\n%s", len(rs), n, p.Label(), path), nil
}

func workingRows(w *xlsx.Workbook, db *store.DB, issue int, guid string) error {
	rows, err := db.Query(`SELECT v.date, v.type, v.number, v.narration, vl.ledger, vl.amount FROM vouchers v
		JOIN vlines vl ON vl.voucher_guid = v.guid WHERE v.guid = ? ORDER BY vl.seq`, guid)
	if err != nil {
		return err
	}
	type r struct {
		date, typ, num, narr, ledger string
		amt                          int64
	}
	var out []r
	for rows.Next() {
		var x r
		rows.Scan(&x.date, &x.typ, &x.num, &x.narr, &x.ledger, &x.amt)
		out = append(out, x)
	}
	rows.Close()
	for i, x := range out {
		var dr, cr any
		if x.amt < 0 {
			dr = xlsx.Money(-x.amt)
		} else {
			cr = xlsx.Money(x.amt)
		}
		narr := ""
		if i == 0 {
			narr = x.narr
		}
		if err := w.Row(issue, ddmmyyyy(x.date), x.typ, x.num, x.ledger, dr, cr, narr); err != nil {
			return err
		}
	}
	return nil
}
