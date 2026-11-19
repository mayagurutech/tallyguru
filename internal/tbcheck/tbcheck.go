// Package tbcheck proves the local database agrees with Tally before any answer is given.
//
// For every ledger: Tally's closing balance (fetched in the same sync) must equal
// opening + all voucher lines in the local DB (optional vouchers excluded; cancelled
// vouchers carry no lines). The local books must also total exactly zero.
//
// Stock-in-Hand ledgers are skipped per ledger too: with inventory, Tally reports their closing as
// the stock valuation (computed from items), not opening plus entries.
//
// "Profit & Loss A/c" is skipped per ledger: Tally reports its closing including the
// current period's profit (computed, not posted). It is still covered by the zero-total
// check, because every other ledger matching forces its posted lines to match too.
package tbcheck

import (
	"fmt"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
)

// PLLedger is Tally's reserved profit & loss ledger name.
const PLLedger = "Profit & Loss A/c"

// Mismatch is one ledger that differs.
type Mismatch struct {
	Ledger      string
	Tally, Ours int64 // paise, Tally sign (negative = debit)
}

// Result of a check.
type Result struct {
	Ledgers    int
	Mismatches []Mismatch
	Unknown    []string // ledger names used in vouchers but missing from the ledger list
	BooksTotal int64    // must be 0
	Stock      []Mismatch // Stock-in-Hand ledgers: Tally shows the inventory valuation, not checked
}

// OK reports whether answers may be given.
func (r Result) OK() bool { return len(r.Mismatches) == 0 && len(r.Unknown) == 0 && r.BooksTotal == 0 }

// Rs formats paise as Indian-style rupees with Dr/Cr.
func Rs(p int64) string {
	side := "Cr"
	if p < 0 {
		side, p = "Dr", -p
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
	return fmt.Sprintf("%s.%02d %s", s, p%100, side)
}

func (r Result) String() string {
	if r.OK() {
		s := fmt.Sprintf("Trial balance matches Tally: %d ledgers, books total 0.00", r.Ledgers)
		if len(r.Stock) > 0 {
			s += fmt.Sprintf(" (%d Stock-in-Hand ledgers show Tally's stock valuation and are not compared)", len(r.Stock))
		}
		return s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "WARNING: local data does NOT match Tally (%d of %d ledgers differ", len(r.Mismatches), r.Ledgers)
	if r.BooksTotal != 0 {
		fmt.Fprintf(&b, "; local books out by %s", Rs(r.BooksTotal))
	}
	b.WriteString("). Answers are blocked until a re-sync matches.\n")
	for i, m := range r.Mismatches {
		if i == 10 {
			fmt.Fprintf(&b, "  ... and %d more\n", len(r.Mismatches)-10)
			break
		}
		fmt.Fprintf(&b, "  %-30s Tally %18s  local %18s\n", m.Ledger, Rs(m.Tally), Rs(m.Ours))
	}
	for _, u := range r.Unknown {
		fmt.Fprintf(&b, "  ledger %q used in vouchers but not in ledger list\n", u)
	}
	return b.String()
}

// Run checks the DB and records the outcome in meta (tb_status, tb_checked, tb_detail).
func Run(db *store.DB) (Result, error) {
	var r Result
	rows, err := db.Query(`
		SELECT l.name, l.closing, l.opening + COALESCE(m.total, 0),
		       EXISTS(SELECT 1 FROM group_anc a WHERE a.name = l.parent AND a.ancestor = 'Stock-in-Hand') OR l.parent = 'Stock-in-Hand'
		FROM ledgers l
		LEFT JOIN (SELECT vl.ledger, SUM(vl.amount) total
		           FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
		           GROUP BY vl.ledger) m ON m.ledger = l.name
		ORDER BY ABS(l.closing - (l.opening + COALESCE(m.total, 0))) DESC, l.name`)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var name string
		var tallyBal, ours int64
		var stock bool
		if err := rows.Scan(&name, &tallyBal, &ours, &stock); err != nil {
			rows.Close()
			return r, err
		}
		r.Ledgers++
		r.BooksTotal += ours
		if stock {
			r.Stock = append(r.Stock, Mismatch{name, tallyBal, ours})
			continue
		}
		if name != PLLedger && tallyBal != ours {
			r.Mismatches = append(r.Mismatches, Mismatch{name, tallyBal, ours})
		}
	}
	rows.Close()

	// Only lines that carry an amount in vouchers affecting the books (seen on a pilot:
	// zero-amount blank-ledger lines in stock journals and cancelled sales).
	urows, err := db.Query(`SELECT DISTINCT vl.ledger FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
		LEFT JOIN ledgers l ON l.name = vl.ledger WHERE l.name IS NULL AND vl.amount <> 0`)
	if err != nil {
		return r, err
	}
	for urows.Next() {
		var n string
		urows.Scan(&n)
		r.Unknown = append(r.Unknown, n)
	}
	urows.Close()

	status := "ok"
	if !r.OK() {
		status = "mismatch"
	}
	db.SetMeta("tb_status", status)
	db.SetMeta("tb_checked", time.Now().Format(time.RFC3339))
	db.SetMeta("tb_detail", r.String())
	return r, nil
}

// Gate returns an error (with the warning text) unless the last check passed.
// Every answering tool must call this first.
func Gate(db *store.DB) error {
	s, _ := db.Meta("tb_status")
	switch s {
	case "ok":
		return nil
	case "":
		return fmt.Errorf("no trial-balance check yet for %q; run a sync first", db.Label.Company)
	default:
		d, _ := db.Meta("tb_detail")
		return fmt.Errorf("%s", d)
	}
}
