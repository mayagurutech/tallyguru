// Package tbheal runs when the trial-balance check fails. For the ledgers that differ only, it
// asks Tally their balance at each month end, finds the months that differ, reads those months
// again and checks once more. If a ledger still differs, it names the month, the difference and
// any voucher of that month whose amount equals the difference, and writes the detail to Excel.
// It never changes numbers to force a match: answers stay blocked until the books tie.
package tbheal

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/sync"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tbcheck"
	"github.com/mayagurutech/tallyguru/internal/xlsx"
)

// MaxLedgers caps how many differing ledgers are traced (keeps Tally requests small).
const MaxLedgers = 50

// Month is one ledger's movement in one month, as Tally and as the local copy see it.
type Month struct {
	Ledger       string
	From, To     time.Time
	Tally, Local int64  // movement in the month, paise, Tally sign
	Hint         string // a local voucher whose amount equals the difference, if any
}

// Diff returns Tally minus local.
func (m Month) Diff() int64 { return m.Tally - m.Local }

// Result of a heal.
type Result struct {
	Refetched []tally.Period
	Fetched   int
	TB        tbcheck.Result
	Left      []Month // months still differing after the re-read
	Excel     string  // path of the detail file, if written
}

// Run heals what it can. from/to is the synced period (from = books-from); months are traced
// from start (later than from when only recent years are synced); exportDir is where the Excel file goes.
func Run(ctx context.Context, e *sync.Engine, r tbcheck.Result, from, start, to time.Time, exportDir string, say func(string)) (Result, error) {
	res := Result{TB: r}
	names := ledgers(r)
	if len(names) == 0 {
		return res, nil
	}
	say(fmt.Sprintf("Tracing %d differing ledgers month by month in Tally…", len(names)))
	months, err := compare(ctx, e, names, from, start, to)
	if err != nil {
		return res, err
	}
	bad := periods(months)
	if len(bad) > 0 {
		say(fmt.Sprintf("Reading %d months again: %s", len(bad), list(bad)))
		if res.Fetched, err = e.Refetch(ctx, bad); err != nil {
			return res, err
		}
		res.Refetched = bad
		if res.TB, err = tbcheck.Run(e.DB); err != nil {
			return res, err
		}
		if res.TB.OK() {
			say("Trial balance matches Tally after reading those months again.")
			return res, nil
		}
		if names = ledgers(res.TB); len(names) == 0 {
			return res, nil
		}
		if months, err = compare(ctx, e, names, from, start, to); err != nil {
			return res, err
		}
	}
	for _, m := range months {
		if m.Diff() != 0 {
			m.Hint = hint(e.DB, m)
			res.Left = append(res.Left, m)
		}
	}
	if len(res.Left) > 0 && exportDir != "" {
		res.Excel, err = excel(exportDir, e.DB.Label.Company, res.Left)
	}
	return res, err
}

func ledgers(r tbcheck.Result) []string {
	var n []string
	for _, m := range r.Mismatches {
		if m.Ledger != tbcheck.PLLedger && len(n) < MaxLedgers {
			n = append(n, m.Ledger)
		}
	}
	return n
}

// compare asks Tally each ledger's closing at every month end and compares month movements.
func compare(ctx context.Context, e *sync.Engine, names []string, from, start, to time.Time) ([]Month, error) {
	if start.Before(from) {
		start = from
	}
	partial := start.After(from)
	company := e.DB.Label.Company
	prevT, prevL := map[string]int64{}, map[string]int64{}
	first := true
	var out []Month
	for _, p := range tally.Chunks(start, to, false) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tallyBal := map[string]int64{}
		err := e.C.Do(ctx, tally.LedgerBalancesRequest(company, from, p.To, names).Build(), func(r io.Reader) error {
			return tally.ParseLedgers(r, func(l tally.Ledger) error {
				tallyBal[l.Name] = int64(l.Closing)
				if first {
					prevT[l.Name] = int64(l.Opening)
				}
				return nil
			})
		})
		if err != nil {
			return nil, fmt.Errorf("balances to %s: %w", p.To.Format(time.DateOnly), err)
		}
		for _, n := range names {
			var open, upto int64
			e.DB.QueryRow(`SELECT opening FROM ledgers WHERE name = ?`, n).Scan(&open)
			e.DB.QueryRow(`SELECT COALESCE(SUM(vl.amount), 0) FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
				WHERE vl.ledger = ? AND v.date <= ?`, n, p.To.Format(time.DateOnly)).Scan(&upto)
			local := open + upto
			if first {
				prevL[n] = open
				if _, ok := prevT[n]; !ok || partial { // partial: open is Tally's balance before start
					prevT[n] = open
				}
			}
			out = append(out, Month{Ledger: n, From: p.From, To: p.To,
				Tally: tallyBal[n] - prevT[n], Local: local - prevL[n]})
			prevT[n], prevL[n] = tallyBal[n], local
		}
		first = false
	}
	return out, nil
}

func periods(ms []Month) []tally.Period {
	seen := map[time.Time]bool{}
	var ps []tally.Period
	for _, m := range ms {
		if m.Diff() != 0 && !seen[m.From] {
			seen[m.From] = true
			ps = append(ps, tally.Period{From: m.From, To: m.To})
		}
	}
	return ps
}

// hint finds a voucher in that month touching the ledger whose line amount equals the difference
// (or its reverse): the likeliest single cause.
func hint(db *store.DB, m Month) string {
	d := m.Diff()
	var ty, no, date string
	var amt int64
	err := db.QueryRow(`SELECT v.type, v.number, v.date, vl.amount FROM vlines vl JOIN vouchers v ON v.guid = vl.voucher_guid
		WHERE vl.ledger = ? AND v.date BETWEEN ? AND ? AND (vl.amount = ? OR vl.amount = ?) LIMIT 1`,
		m.Ledger, m.From.Format(time.DateOnly), m.To.Format(time.DateOnly), d, -d).Scan(&ty, &no, &date, &amt)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s %s no. %s has %s on this ledger, the same as the difference", date, ty, no, tbcheck.Rs(amt))
}

func list(ps []tally.Period) string {
	var s []string
	for _, p := range ps {
		s = append(s, p.From.Format("Jan 2006"))
	}
	return strings.Join(s, ", ")
}

func excel(dir, company string, ms []Month) (string, error) {
	d := filepath.Join(dir, safe(company))
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(d, "TB difference "+time.Now().Format("2006-01-02 1504")+".xlsx")
	w, err := xlsx.Create(p)
	if err != nil {
		return "", err
	}
	w.Sheet("TB difference", "Ledger", "Month", "Tally movement", "Local movement", "Difference", "Likely voucher")
	for _, m := range ms {
		w.Row(m.Ledger, m.From.Format("Jan 2006"), xlsx.Money(m.Tally), xlsx.Money(m.Local), xlsx.Money(m.Diff()), m.Hint)
	}
	return p, w.Close()
}

func safe(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, s)
	return strings.TrimRight(strings.TrimSpace(s), ".")
}

// String is the short summary for the panel and the AI.
func (r Result) String() string {
	var b strings.Builder
	if len(r.Refetched) > 0 {
		fmt.Fprintf(&b, "Self-check: read %d months again (%s), %d vouchers.\n", len(r.Refetched), list(r.Refetched), r.Fetched)
	}
	if len(r.Left) == 0 {
		return b.String()
	}
	b.WriteString("Still differs in Tally's own figures for these months:\n")
	for i, m := range r.Left {
		if i == 10 {
			fmt.Fprintf(&b, "  ... and %d more (see the Excel file)\n", len(r.Left)-10)
			break
		}
		fmt.Fprintf(&b, "  %s, %s: Tally %s, local %s, difference %s", m.Ledger, m.From.Format("Jan 2006"),
			tbcheck.Rs(m.Tally), tbcheck.Rs(m.Local), tbcheck.Rs(m.Diff()))
		if m.Hint != "" {
			b.WriteString(". " + m.Hint)
		}
		b.WriteString("\n")
	}
	if r.Excel != "" {
		b.WriteString("Detail: " + r.Excel + "\n")
	}
	return b.String()
}
