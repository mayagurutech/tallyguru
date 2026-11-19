// Package bank checks bank statement pages sent by the AI (the server never reads the file),
// matches statement lines with the bank ledger in the local books, and builds the bank
// reconciliation statement. Amounts in paise.
package bank

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MatchDays is how far apart the statement and book dates may be (author: ±3 days).
const MatchDays = 3

const schema = `
CREATE TABLE IF NOT EXISTS bank_lines (ledger TEXT NOT NULL, page INTEGER NOT NULL, seq INTEGER NOT NULL,
  date TEXT NOT NULL, narration TEXT NOT NULL, ref TEXT NOT NULL, debit INTEGER NOT NULL, credit INTEGER NOT NULL,
  balance INTEGER NOT NULL, status TEXT NOT NULL DEFAULT '', guid TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(ledger, page, seq));
CREATE TABLE IF NOT EXISTS bank_pages (ledger TEXT NOT NULL, page INTEGER NOT NULL, opening INTEGER NOT NULL,
  closing INTEGER NOT NULL, first TEXT NOT NULL, last TEXT NOT NULL, PRIMARY KEY(ledger, page));`

// Line is one statement line. Debit = money out of the account, credit = money in.
// Balance follows the bank's sign (positive = money in the account).
type Line struct {
	Date, Narration, Ref string
	Debit, Credit        int64
	Balance              int64
	HasBalance           bool
	Status, GUID         string // after matching: matched | probable | unmatched
}

// Page is one statement page as read by the AI.
type Page struct {
	Ledger           string
	No               int
	Opening, Closing int64
	Lines            []Line
}

// Check validates a page: every running balance, opening + credits − debits = closing, and
// continuity with the previous page saved for this ledger. Returns the first problem.
func Check(db *sql.DB, p Page) error {
	db.Exec(schema)
	if len(p.Lines) == 0 {
		return fmt.Errorf("page %d has no lines", p.No)
	}
	run := p.Opening
	for i, l := range p.Lines {
		if l.Debit < 0 || l.Credit < 0 || (l.Debit > 0 && l.Credit > 0) {
			return fmt.Errorf("page %d line %d: give either a debit or a credit", p.No, i+1)
		}
		if _, err := time.Parse(time.DateOnly, l.Date); err != nil {
			return fmt.Errorf("page %d line %d: date %q (use YYYY-MM-DD)", p.No, i+1, l.Date)
		}
		run += l.Credit - l.Debit
		if l.HasBalance && l.Balance != run {
			return fmt.Errorf("page %d line %d (%s, %s): running balance should be %s but the page shows %s; reread this line",
				p.No, i+1, l.Date, short(l.Narration), rs(run), rs(l.Balance))
		}
	}
	if run != p.Closing {
		return fmt.Errorf("page %d: opening %s + credits − debits = %s, but the closing shown is %s (difference %s); reread the page",
			p.No, rs(p.Opening), rs(run), rs(p.Closing), rs(p.Closing-run))
	}
	var prevClose int64
	err := db.QueryRow(`SELECT closing FROM bank_pages WHERE ledger=? AND page=?`, p.Ledger, p.No-1).Scan(&prevClose)
	if err == nil && prevClose != p.Opening {
		return fmt.Errorf("page %d opens at %s but page %d closed at %s: a page or line may be missing",
			p.No, rs(p.Opening), p.No-1, rs(prevClose))
	}
	return nil
}

// Save stores a checked page (replacing the same page number if sent again).
func Save(db *sql.DB, p Page) error {
	db.Exec(schema)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM bank_lines WHERE ledger=? AND page=?`, p.Ledger, p.No)
	for i, l := range p.Lines {
		if _, err := tx.Exec(`INSERT INTO bank_lines(ledger,page,seq,date,narration,ref,debit,credit,balance) VALUES(?,?,?,?,?,?,?,?,?)`,
			p.Ledger, p.No, i+1, l.Date, l.Narration, l.Ref, l.Debit, l.Credit, l.Balance); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO bank_pages VALUES(?,?,?,?,?,?)`, p.Ledger, p.No, p.Opening, p.Closing,
		p.Lines[0].Date, p.Lines[len(p.Lines)-1].Date); err != nil {
		return err
	}
	return tx.Commit()
}

// Clear forgets the saved statement for a ledger (to start a new month).
func Clear(db *sql.DB, ledger string) {
	db.Exec(schema)
	db.Exec(`DELETE FROM bank_lines WHERE ledger=?`, ledger)
	db.Exec(`DELETE FROM bank_pages WHERE ledger=?`, ledger)
}

// Entry is one book entry on the bank ledger. Amount: Tally sign (negative = Dr = money in).
type Entry struct {
	GUID, Date, Type, Number, Narration, Other string
	Amount                                     int64
	Status                                     string
}

// Result of matching all saved pages of a ledger.
type Result struct {
	Ledger            string
	From, To          string
	Opening, Closing  int64 // statement
	Lines             []Line
	Book              []Entry // book entries in the period (and up to MatchDays after)
	Matched, Probable int
}

// Match pairs saved statement lines with book entries: same amount and side, date within
// MatchDays. One candidate: matched. Several: the one sharing the cheque/ref or closest in
// date, marked probable for the user to confirm.
func Match(db *sql.DB, ledger string) (*Result, error) {
	db.Exec(schema)
	r := &Result{Ledger: ledger}
	if err := db.QueryRow(`SELECT MIN(first), MAX(last) FROM bank_pages WHERE ledger=?`, ledger).Scan(&r.From, &r.To); err != nil || r.From == "" {
		return nil, fmt.Errorf("no statement pages saved for %s yet", ledger)
	}
	db.QueryRow(`SELECT opening FROM bank_pages WHERE ledger=? ORDER BY page LIMIT 1`, ledger).Scan(&r.Opening)
	db.QueryRow(`SELECT closing FROM bank_pages WHERE ledger=? ORDER BY page DESC LIMIT 1`, ledger).Scan(&r.Closing)
	rows, err := db.Query(`SELECT date, narration, ref, debit, credit, balance FROM bank_lines WHERE ledger=? ORDER BY page, seq`, ledger)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l Line
		rows.Scan(&l.Date, &l.Narration, &l.Ref, &l.Debit, &l.Credit, &l.Balance)
		r.Lines = append(r.Lines, l)
	}
	rows.Close()
	from := shift(r.From, -MatchDays-60) // cheques issued before the statement may clear in it
	to := shift(r.To, MatchDays)
	rows, err = db.Query(`SELECT v.guid, v.date, v.type, v.number, v.narration, vl.amount,
		COALESCE((SELECT o.ledger FROM vlines o WHERE o.voucher_guid=v.guid AND o.ledger<>vl.ledger ORDER BY abs(o.amount) DESC LIMIT 1),'')
		FROM vouchers v JOIN vlines vl ON vl.voucher_guid=v.guid
		WHERE vl.ledger=? AND v.cancelled=0 AND v.optional=0 AND v.date BETWEEN ? AND ? ORDER BY v.date`, ledger, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e Entry
		rows.Scan(&e.GUID, &e.Date, &e.Type, &e.Number, &e.Narration, &e.Amount, &e.Other)
		r.Book = append(r.Book, e)
	}
	rows.Close()
	used := map[int]bool{}
	for i := range r.Lines {
		l := &r.Lines[i]
		want := l.Debit - l.Credit // money out = Cr in books (positive)
		var cands []int
		for j, e := range r.Book {
			if !used[j] && e.Amount == want && days(e.Date, l.Date) <= MatchDays {
				cands = append(cands, j)
			}
		}
		if len(cands) == 0 {
			l.Status = "unmatched"
			continue
		}
		best := cands[0]
		refHit := false
		for _, j := range cands {
			if hit := refMatch(l, r.Book[j]); hit && !refHit {
				best, refHit = j, true
			} else if hit == refHit && days(r.Book[j].Date, l.Date) < days(r.Book[best].Date, l.Date) {
				best = j
			}
		}
		used[best] = true
		l.GUID = r.Book[best].GUID
		if len(cands) == 1 || refHit {
			l.Status = "matched"
			r.Matched++
		} else {
			l.Status = "probable"
			r.Probable++
		}
		r.Book[best].Status = l.Status
	}
	for j := range r.Book {
		if r.Book[j].Status == "" {
			r.Book[j].Status = "unmatched"
		}
	}
	// Remember the outcome on the saved lines (for the Excel and later runs).
	for i, l := range r.Lines {
		_ = i
		db.Exec(`UPDATE bank_lines SET status=?, guid=? WHERE ledger=? AND date=? AND debit=? AND credit=? AND narration=?`,
			l.Status, l.GUID, ledger, l.Date, l.Debit, l.Credit, l.Narration)
	}
	return r, nil
}

func refMatch(l *Line, e Entry) bool {
	ref := strings.TrimLeft(strings.TrimSpace(l.Ref), "0")
	if len(ref) < 3 {
		return false
	}
	return strings.Contains(e.Narration, ref) || strings.TrimLeft(e.Number, "0") == ref
}

// Reco is the bank reconciliation statement as on the statement's last date.
type Reco struct {
	AsOn                       string
	Books, Bank                int64 // balance per books (bank sign: + = money in account) and per statement
	IssuedNotCleared           []Entry
	DepositedNotCredited       []Entry
	BankOnlyDebits, BankOnlyCr []Line // in the statement, not in the books (charges, interest, direct receipts)
	OpeningGap                 int64  // statement opening vs books at the day before, after entries clearing in this statement
	Difference                 int64  // what is still unexplained (0 when it ties)
}

// Reconcile builds the statement from a match result. bookBal is the ledger balance as on
// AsOn and bookOpen the balance the day before the statement starts, both in Tally sign
// (negative = Dr = money in the account). Book entries before the statement period count only
// when they cleared in it; older uncleared items show as an opening gap.
func Reconcile(r *Result, bookBal, bookOpen int64) Reco {
	c := Reco{AsOn: r.To, Books: -bookBal, Bank: r.Closing}
	gap := r.Opening + bookOpen
	for _, e := range r.Book {
		if e.Date < r.From && e.Status != "unmatched" {
			gap -= e.Amount
		}
		if e.Status != "unmatched" || e.Date > r.To || e.Date < r.From {
			continue
		}
		if e.Amount > 0 {
			c.IssuedNotCleared = append(c.IssuedNotCleared, e)
		} else {
			c.DepositedNotCredited = append(c.DepositedNotCredited, e)
		}
	}
	for _, l := range r.Lines {
		if l.Status != "unmatched" {
			continue
		}
		if l.Debit > 0 {
			c.BankOnlyDebits = append(c.BankOnlyDebits, l)
		} else {
			c.BankOnlyCr = append(c.BankOnlyCr, l)
		}
	}
	// Books + issued not cleared − deposited not credited − bank-only debits + bank-only credits = bank.
	x := c.Books
	for _, e := range c.IssuedNotCleared {
		x += e.Amount
	}
	for _, e := range c.DepositedNotCredited {
		x += e.Amount // negative
	}
	for _, l := range c.BankOnlyDebits {
		x -= l.Debit
	}
	for _, l := range c.BankOnlyCr {
		x += l.Credit
	}
	c.OpeningGap = gap
	c.Difference = c.Bank - x - gap
	sort.Slice(c.IssuedNotCleared, func(i, j int) bool { return c.IssuedNotCleared[i].Amount > c.IssuedNotCleared[j].Amount })
	return c
}

// BookBalance is the bank ledger balance at the end of the day in Tally sign.
func BookBalance(db *sql.DB, ledger, asOn string) int64 {
	var opening, moves int64
	db.QueryRow(`SELECT opening FROM ledgers WHERE name=?`, ledger).Scan(&opening)
	db.QueryRow(`SELECT COALESCE(SUM(vl.amount),0) FROM vlines vl JOIN vouchers v ON v.guid=vl.voucher_guid
		WHERE vl.ledger=? AND v.cancelled=0 AND v.optional=0 AND v.date<=?`, ledger, asOn).Scan(&moves)
	return opening + moves
}

// BankCharge reports lines that look like bank charges or interest (always asked, never auto-drafted).
func BankCharge(narration string) bool {
	n := strings.ToLower(narration)
	for _, w := range []string{"charge", "chrg", "chg", "commission", "interest", "int.pd", "int pd", "sms", "gst on", "annual fee", "min bal", "penal"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func shift(d string, n int) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

func days(a, b string) int {
	ta, _ := time.Parse(time.DateOnly, a)
	tb, _ := time.Parse(time.DateOnly, b)
	d := int(ta.Sub(tb).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

func short(s string) string {
	if len(s) > 30 {
		return s[:30] + "…"
	}
	return s
}

func rs(p int64) string {
	neg := p < 0
	if neg {
		p = -p
	}
	s := fmt.Sprintf("%d", p/100)
	if len(s) > 3 {
		head, tail := s[:len(s)-3], s[len(s)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		s = strings.Join(parts, ",") + "," + tail
	}
	s = fmt.Sprintf("%s.%02d", s, p%100)
	if neg {
		return "-" + s
	}
	return s
}

// Rs formats paise for answers.
func Rs(p int64) string { return rs(p) }
