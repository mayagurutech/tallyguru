package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/xlsx"
)

// ExportDir is where Excel files go: Documents\LedgerGuru\<company>. Files stay on
// this PC with real names (privacy mode only changes what the AI is told).
func (e *Env) exportDir(company string) (string, error) {
	d := filepath.Join(e.Cfg.ExportBase(), SafeName(company))
	return d, os.MkdirAll(d, 0o700)
}

// SafeName makes a company or report name safe as a Windows file name.
func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, s)
	return strings.TrimRight(strings.TrimSpace(s), ".")
}

// newBook creates "<report> <period> <time>.xlsx" in the company's export folder.
func (e *Env) newBook(company, title string) (*xlsx.Workbook, string, error) {
	d, err := e.exportDir(company)
	if err != nil {
		return nil, "", err
	}
	p := filepath.Join(d, SafeName(fmt.Sprintf("%s %s.xlsx", title, e.Now().Format("2006-01-02 1504"))))
	w, err := xlsx.Create(p)
	return w, p, err
}

func ddmmyyyy(d string) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return d
	}
	return t.Format("02-01-2006")
}

func (e *Env) dayBook(db *store.DB, a map[string]any, p report.Period) (string, error) {
	f := report.DayBookFilter{P: p, Type: str(a, "type")}
	if x, _ := a["excel"].(bool); x {
		return e.dayBookExcel(db, f)
	}
	types, largest, n, err := report.DayBook(db.DB, f, e.top(a))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, Day Book, %s", db.Label.Company, p.Label())
	if f.Type != "" {
		fmt.Fprintf(&b, ", type %q", f.Type)
	}
	fmt.Fprintf(&b, ": %d vouchers", n)
	if n == 0 {
		return b.String(), nil
	}
	b.WriteString("\nType | Vouchers | Value")
	for _, t := range types {
		fmt.Fprintf(&b, "\n%s | %d | %s", t.Type, t.N, amt(t.Value))
	}
	fmt.Fprintf(&b, "\nLargest %d:\nDate | Type | No. | Party / against | Value", len(largest))
	for _, v := range largest {
		fmt.Fprintf(&b, "\n%s | %s | %s | %s | %s", ddmmyyyy(v.Date), v.Type, dash(v.Number), e.show(db, v.Against, v.AgainstParent), amt(v.Value))
	}
	b.WriteString("\nFull list with every ledger line: ask for excel.")
	return b.String(), nil
}

func (e *Env) dayBookExcel(db *store.DB, f report.DayBookFilter) (string, error) {
	title := "Day Book " + f.P.Label()
	if f.Type != "" {
		title += " " + f.Type
	}
	w, path, err := e.newBook(db.Label.Company, title)
	if err != nil {
		return "", err
	}
	n, lines := 0, 0
	err = func() error {
		if err := w.Sheet("Vouchers", "Date", "Type", "No.", "Party / against", "Value", "Narration", "Cancelled"); err != nil {
			return err
		}
		if err := report.EachVoucher(db.DB, f, func(v report.Voucher) error {
			n++
			c := ""
			if v.Cancelled {
				c = "yes"
			}
			return w.Row(ddmmyyyy(v.Date), v.Type, v.Number, v.Against, xlsx.Money(v.Value), v.Narration, c)
		}); err != nil {
			return err
		}
		if err := w.Sheet("Lines", "Date", "Type", "No.", "Ledger", "Group", "Debit", "Credit"); err != nil {
			return err
		}
		return report.EachLine(db.DB, f, func(x report.VLine) error {
			lines++
			var dr, cr any
			if x.Amount < 0 {
				dr = xlsx.Money(-x.Amount)
			} else {
				cr = xlsx.Money(x.Amount)
			}
			return w.Row(ddmmyyyy(x.Date), x.Type, x.Number, x.Ledger, x.Parent, dr, cr)
		})
	}()
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return fmt.Sprintf("Saved %d vouchers and %d ledger lines (%s) to:\n%s", n, lines, f.P.Label(), path), nil
}

func (e *Env) ageingExcel(db *store.DB, r *report.Ageing, title string) (string, error) {
	w, path, err := e.newBook(db.Label.Company, title+" ageing")
	if err != nil {
		return "", err
	}
	head := []string{"Party", "Group", "Total"}
	for _, bk := range report.Buckets {
		head = append(head, bk.Label+" days")
	}
	head = append(head, "Method", "MSME over 45 days", "Not set against bills")
	err = w.Sheet(title, head...)
	for _, p := range r.Parties {
		if err != nil {
			break
		}
		row := []any{p.Name, p.Parent, xlsx.Money(p.Total)}
		for _, v := range p.By {
			row = append(row, xlsx.Money(v))
		}
		m := "FIFO on ledger"
		if p.BillWise {
			m = "Tally bills"
		}
		row = append(row, m, xlsx.Money(p.OverMSME), xlsx.Money(p.Unadjusted))
		err = w.Row(row...)
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return fmt.Sprintf("Saved %s ageing for %s to:\n%s", strings.ToLower(title), parties(len(r.Parties)), path), nil
}
