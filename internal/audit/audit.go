// Package audit runs books-only audit checks on ALL entries of a period (never samples).
// Each check reports facts: what was found, where, how much. Whether something is a
// disallowance, a default or fine is the auditor's judgment; nothing here concludes.
package audit

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/gst"
	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/sections"
)

// Limits used by the checks, in paise. One table so they are easy to review and change.
var Limits = struct {
	CashPayment, CashReceipt, CashLoan int64
	CashTransporter                    int64
	RoundMultiple, RoundMin            int64
	DuplicateDays                      int
	DuplicateMin                       int64
	MSMEDays, OldDebtorDays            int
}{
	DuplicateMin:    10000 * 100,  // below this a duplicate needs the same bill reference too
	CashPayment:     10000 * 100,  // s. 36(4) (40A(3)): more than this in a day to one person
	CashTransporter: 35000 * 100,  // s. 36(6) (40A(3) proviso): goods carriage operators
	CashReceipt:     200000 * 100, // s. 186 (269ST): this or more from one person in a day
	CashLoan:        20000 * 100,  // s. 185 (269SS) / 269T: this or more
	RoundMultiple:   10000 * 100,  // round sums: exact multiples of this...
	RoundMin:        50000 * 100,  // ...of at least this
	DuplicateDays:   7,
	MSMEDays:        report.MSMEDays,
	OldDebtorDays:   180,
}

// Issue is one finding. Vouchers lists every voucher behind it (the "Working").
type Issue struct {
	Date, Party, Detail string
	Amount              int64
	Vouchers            []string // voucher GUIDs
}

// Result is one check's outcome.
type Result struct {
	ID, Title, Section, Note string
	Issues                   []Issue
	Count                    int
	Amount                   int64
	NotAvailable             bool
}

// Checks in display order (ID, title, section key).
var Checks = []struct{ ID, Title, Key string }{
	{"cash_payments", "Cash payments over 10,000 to one party in a day", "40A(3)"},
	{"cash_receipts", "Cash received of 2 lakh or more from one party in a day", "269ST"},
	{"cash_loans_taken", "Loans or deposits taken in cash of 20,000 or more", "269SS"},
	{"cash_loans_repaid", "Loans or deposits repaid in cash of 20,000 or more", "269T"},
	{"msme_45_days", "MSME creditors unpaid beyond 45 days at the period end", "43B(h)"},
	{"tds_not_deducted", "Expenses over the TDS threshold with no TDS seen for the party", "40(a)(ia)"},
	{"related_parties", "Entries with related parties", ""},
	{"negative_cash", "Days with negative cash in hand", ""},
	{"suspense", "Suspense balances and entries", ""},
	{"duplicates", "Possible duplicate entries", ""},
	{"round_sums", "Round-sum journals and payments", ""},
	{"sunday_entries", "Entries dated on Sundays", ""},
	{"old_debtors", "Receivables over 180 days and advances to suppliers", ""},
	{"invalid_gstin", "Party GSTINs that fail the format, state code or check digit", ""},
	{"igst_vs_state", "IGST on same-state parties, or CGST+SGST on other-state parties", ""},
	{"foreign_parties", "Entries with foreign parties (kept out of 194C and GST checks)", "195"},
	{"edit_log", "Edit log (changes to entries)", ""},
}

type ledger struct {
	name, parent                         string
	cash, bank, loan, capital, tax, rev  bool
	income, creditor, debtor, fixedAsset bool
	tdsTag                               string // TDS section tag, "" or "none" when not applicable
	tdsPayable, related, suspense        bool
	opening                              int64
	taxTag                               string // e.g. "igst output"
	gstin, state, country                string
	transporter, foreign                 bool
}

// partyState is the party's state: from a valid GSTIN first, else the ledger's state.
func (l *ledger) partyState() string {
	if l.gstin != "" && gst.Problem(l.gstin) == "" {
		return gst.StateOf(l.gstin)
	}
	return l.state
}

type line struct {
	ledger string
	amount int64 // Tally sign: Cr positive
}

type voucher struct {
	guid, date, typ, base, number, party, reference, narration string
	lines                                                      []line
	value                                                      int64
}

// Run runs every check for the period. asOf is the period end (for ageing-based checks).
func Run(db *sql.DB, p report.Period) ([]Result, error) {
	ls, err := loadLedgers(db)
	if err != nil {
		return nil, err
	}
	end, _ := time.Parse("02-01-2006", p.End())
	rs := map[string]*Result{}
	for _, c := range Checks {
		r := &Result{ID: c.ID, Title: c.Title}
		if c.Key != "" {
			r.Section = sections.Cite(c.Key)
		}
		rs[c.ID] = r
	}
	st := newState(ls)
	db.QueryRow(`SELECT value FROM meta WHERE key='company_state'`).Scan(&st.companyState)
	invalidGSTIN(ls, rs["invalid_gstin"])
	if err := eachVoucher(db, p, func(v voucher) { st.add(v) }); err != nil {
		return nil, err
	}
	st.finish(rs)
	if err := negativeCash(db, ls, p, rs["negative_cash"]); err != nil {
		return nil, err
	}
	if err := suspense(db, ls, p, rs["suspense"]); err != nil {
		return nil, err
	}
	if err := ageingChecks(db, end, rs["msme_45_days"], rs["old_debtors"]); err != nil {
		return nil, err
	}
	rs["edit_log"].NotAvailable = true
	rs["edit_log"].Note = "Not read from Tally yet (being checked which TallyPrime versions give it over XML)."
	if rs["tds_not_deducted"].Note == "" {
		rs["tds_not_deducted"].Note = "Threshold amounts are from the 1961 Act tables; confirm them for the 2025 Act."
	}
	var out []Result
	for _, c := range Checks {
		r := rs[c.ID]
		r.Count = len(r.Issues)
		for _, i := range r.Issues {
			r.Amount += i.Amount
		}
		sort.SliceStable(r.Issues, func(a, b int) bool { return r.Issues[a].Amount > r.Issues[b].Amount })
		out = append(out, *r)
	}
	return out, nil
}

func loadLedgers(db *sql.DB) (map[string]*ledger, error) {
	anc := map[string]map[string]bool{}
	rows, err := db.Query(`SELECT name, ancestor FROM group_anc`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n, a string
		rows.Scan(&n, &a)
		if anc[n] == nil {
			anc[n] = map[string]bool{}
		}
		anc[n][a] = true
	}
	rows.Close()
	tags := map[string]map[string]string{} // guid -> family -> tag
	rows, err = db.Query(`SELECT guid, family, tag FROM ledger_map`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g, f, t string
		rows.Scan(&g, &f, &t)
		if tags[g] == nil {
			tags[g] = map[string]string{}
		}
		tags[g][f] = t
	}
	rows.Close()
	rows, err = db.Query(`SELECT l.guid, l.name, l.parent, l.opening, COALESCE(g.is_revenue,0),
		COALESCE(p.gstin,''), COALESCE(p.state,''), COALESCE(p.country,''), COALESCE(p.currency,''), COALESCE(p.transporter,0)
		FROM ledgers l LEFT JOIN groups g ON g.name=l.parent LEFT JOIN ledger_party p ON p.name=l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ledger{}
	for rows.Next() {
		var guid string
		x := &ledger{}
		var currency string
		rows.Scan(&guid, &x.name, &x.parent, &x.opening, &x.rev, &x.gstin, &x.state, &x.country, &currency, &x.transporter)
		// Foreign: residence outside India (pilot Q4: a party paid in USD). Currency alone is
		// not used: Tally fills the company's own currency for most ledgers.
		x.foreign = x.country != "" && !gst.SameState(x.country, "India")
		a, t := anc[x.parent], tags[guid]
		x.taxTag = t["tax"]
		x.cash = a["Cash-in-Hand"] || t["flag"] == "cash"
		x.bank = a["Bank Accounts"] || a["Bank OD A/c"] || t["flag"] == "bank"
		x.loan = !x.bank && (a["Loans (Liability)"] || t["flag"] == "loan taken")
		x.capital = a["Capital Account"] || t["flag"] == "capital"
		x.tax = a["Duties & Taxes"]
		x.income = a["Sales Accounts"] || a["Direct Incomes"] || a["Indirect Incomes"]
		x.creditor, x.debtor = a["Sundry Creditors"], a["Sundry Debtors"]
		x.fixedAsset = a["Fixed Assets"]
		x.tdsTag = t["tds"]
		x.tdsPayable = t["tax"] == "tds payable"
		x.related = t["flag"] == "related party"
		x.suspense = a["Suspense A/c"] || t["flag"] == "suspense"
		out[x.name] = x
	}
	return out, rows.Err()
}

// eachVoucher streams accounting vouchers of the period with their lines, in date order.
func eachVoucher(db *sql.DB, p report.Period, fn func(voucher)) error {
	rows, err := db.Query(`SELECT v.guid, v.date, v.type, COALESCE(vb.base, v.type), v.number, v.party, v.reference, v.narration, vl.ledger, vl.amount
		FROM acct_vouchers v JOIN vlines vl ON vl.voucher_guid = v.guid LEFT JOIN vtype_base vb ON vb.name = v.type
		WHERE v.cancelled = 0 AND substr(v.date,1,7) BETWEEN ? AND ?
		ORDER BY v.date, v.guid, vl.seq`, p.From, p.To)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cur *voucher
	flush := func() {
		if cur != nil {
			for _, l := range cur.lines {
				if l.amount > 0 {
					cur.value += l.amount
				}
			}
			fn(*cur)
		}
	}
	for rows.Next() {
		var v voucher
		var l line
		if err := rows.Scan(&v.guid, &v.date, &v.typ, &v.base, &v.number, &v.party, &v.reference, &v.narration, &l.ledger, &l.amount); err != nil {
			return err
		}
		if cur == nil || cur.guid != v.guid {
			flush()
			cur = &v
		}
		cur.lines = append(cur.lines, l)
	}
	flush()
	return rows.Err()
}

func rupees(p int64) string {
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
	if neg {
		return "-" + s
	}
	return s
}

func ddmmyyyy(d string) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return d
	}
	return t.Format("02-01-2006")
}
