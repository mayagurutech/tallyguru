// Package report builds financial statements from the local summary tables. The server does
// every sum; the tools only format the result as a short text table.
//
// Signs follow Tally: positive = credit, negative = debit (paise).
package report

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Period is a range of whole months, "2026-04" to "2027-03".
type Period struct{ From, To string }

// Label is "Apr 2026 to Mar 2027".
func (p Period) Label() string {
	f, _ := time.Parse("2006-01", p.From)
	t, _ := time.Parse("2006-01", p.To)
	if p.From == p.To {
		return f.Format("Jan 2006")
	}
	return f.Format("Jan 2006") + " to " + t.Format("Jan 2006")
}

// End is the last day of the period, "31-03-2027".
func (p Period) End() string {
	t, _ := time.Parse("2006-01", p.To)
	return t.AddDate(0, 1, -1).Format("02-01-2006")
}

// FYOf gives the financial year (April to March) holding month m, e.g. "2026-27".
func FYOf(m string) string {
	t, _ := time.Parse("2006-01", m)
	y := t.Year()
	if t.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%d-%02d", y, (y+1)%100)
}

var reFY = regexp.MustCompile(`^(\d{4})-(\d{2})$`)
var reMonth = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// ParsePeriod reads the tool's fy/from/to. Missing values: the financial year of the latest
// entry, up to the latest month with entries (so a running year is "year to date").
func ParsePeriod(fy, from, to, first, last string) (Period, error) {
	if last == "" {
		return Period{}, fmt.Errorf("no entries in the synced books")
	}
	if fy != "" {
		m := reFY.FindStringSubmatch(fy)
		if m == nil {
			return Period{}, fmt.Errorf("fy must look like 2026-27")
		}
		var y int
		fmt.Sscan(m[1], &y)
		if fmt.Sprintf("%02d", (y+1)%100) != m[2] {
			return Period{}, fmt.Errorf("fy %s: second year must follow the first", fy)
		}
		p := Period{fmt.Sprintf("%d-04", y), fmt.Sprintf("%d-03", y+1)}
		if p.To > last && p.From <= last {
			p.To = last // running year: up to the latest month
		}
		if from == "" && to == "" {
			return p, nil
		}
		if from == "" {
			from = p.From
		}
		if to == "" {
			to = p.To
		}
	}
	if from == "" {
		from = FYOf(last)[:4] + "-04"
		if from < first {
			from = first
		}
	}
	if to == "" {
		to = last
	}
	if !reMonth.MatchString(from) || !reMonth.MatchString(to) {
		return Period{}, fmt.Errorf("months must look like 2026-04")
	}
	if from > to {
		return Period{}, fmt.Errorf("from %s is after to %s", from, to)
	}
	return Period{from, to}, nil
}

// Ledger is one ledger with its amounts for a period.
type Ledger struct {
	Name, Parent string
	Primary      string   // primary group ("" for ledgers at the top, e.g. Profit & Loss A/c)
	Anc          []string // parent group and every group above it
	Revenue, GP  bool     // P&L ledger; affects gross profit (trading section)
	Open         int64    // opening at the start of the books
	Before, In   int64    // net entries before the period, and inside it
	TallyClose   int64    // Tally's own closing (used for stock ledgers)
	Salary       bool     // audit-flag tag "salary" (Schedule III employee costs)
	MSME         bool     // audit-flag tag "creditor msme"
}

// Close is the balance at the end of the period.
func (l Ledger) Close() int64 { return l.Open + l.Before + l.In }

// Under says whether the ledger sits under group g (at any depth).
func (l Ledger) Under(g string) bool {
	for _, a := range l.Anc {
		if strings.EqualFold(a, g) {
			return true
		}
	}
	return false
}

// Books is everything a statement needs, loaded in one pass.
type Books struct {
	Company   string
	P         Period
	First     string // first month with entries
	BooksFrom string // "2026-04"
	SyncFrom  string // "2025-04" when only recent years are synced (earlier ones are in the openings)
	Last      string // latest month with entries
	Ledgers   []Ledger
}

// Load reads ledgers, groups and month totals for period p.
func Load(db *sql.DB, company string, p Period) (*Books, error) {
	b := &Books{Company: company, P: p}
	bf := ""
	db.QueryRow(`SELECT value FROM meta WHERE key='books_from'`).Scan(&bf)
	if len(bf) >= 7 {
		b.BooksFrom = bf[:7]
	}
	sf := ""
	db.QueryRow(`SELECT value FROM meta WHERE key='sync_from'`).Scan(&sf)
	if len(sf) >= 7 && sf[:7] > b.BooksFrom {
		b.SyncFrom = sf[:7]
	}
	anc := map[string][]string{}
	rows, err := db.Query(`SELECT name, ancestor FROM group_anc`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n, a string
		rows.Scan(&n, &a)
		anc[n] = append(anc[n], a)
	}
	rows.Close()
	// One pass over month_ledger (its key starts with the month, so a per-ledger lookup
	// would scan it once per ledger: 0.7 s on the pilot).
	rows, err = db.Query(`WITH mv AS (SELECT ledger,
		  SUM(CASE WHEN month < ?1 THEN cr-dr ELSE 0 END) b,
		  SUM(CASE WHEN month BETWEEN ?1 AND ?2 THEN cr-dr ELSE 0 END) i
		  FROM month_ledger WHERE month <= ?2 GROUP BY ledger),
		fl AS (SELECT guid, MAX(tag='salary') sal, MAX(tag='creditor msme') msme FROM ledger_map WHERE family='flag' GROUP BY guid)
		SELECT l.name, l.parent, COALESCE(gp.primary_group,''), COALESCE(g.is_revenue,0), COALESCE(g.affects_gp,0),
		l.opening, l.closing, COALESCE(mv.b,0), COALESCE(mv.i,0), COALESCE(fl.sal,0), COALESCE(fl.msme,0)
		FROM ledgers l LEFT JOIN groups g ON g.name=l.parent LEFT JOIN group_primary gp ON gp.name=l.parent
		LEFT JOIN mv ON mv.ledger=l.name LEFT JOIN fl ON fl.guid=l.guid`, p.From, p.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x Ledger
		if err := rows.Scan(&x.Name, &x.Parent, &x.Primary, &x.Revenue, &x.GP, &x.Open, &x.TallyClose, &x.Before, &x.In, &x.Salary, &x.MSME); err != nil {
			return nil, err
		}
		x.Anc = anc[x.Parent]
		b.Ledgers = append(b.Ledgers, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	db.QueryRow(`SELECT COALESCE(MIN(month),''), COALESCE(MAX(month),'') FROM month_ledger`).Scan(&b.First, &b.Last)
	return b, nil
}

// Line is one row of a statement. Amount is shown as positive on its natural side.
type Line struct {
	Label  string
	Amount int64
	Note   string
}

// Section is a titled block of lines with a total.
type Section struct {
	Title string
	Lines []Line
	Total int64
}

func (s *Section) add(label string, v int64, note string) {
	if v == 0 && note == "" {
		return
	}
	s.Lines = append(s.Lines, Line{label, v, note})
	s.Total += v
}

// fromStart says whether the period starts at the beginning of the books, so revenue
// ledgers' openings and the opening stock belong to it.
func (b *Books) fromStart() bool { return b.BooksFrom == "" || b.P.From <= b.BooksFrom }

// periodAmt is a revenue ledger's amount for the period (credit positive).
func (b *Books) periodAmt(l Ledger) int64 {
	if b.fromStart() {
		return l.Open + l.In
	}
	return l.In
}

func isStock(l Ledger) bool { return l.Under("Stock-in-Hand") }

// Stock gives opening and closing stock (positive values) and whether the closing is known
// for this period end. Tally keeps only the current closing value, so a period ending
// before the latest month has no closing stock (warned, never guessed).
func (b *Books) Stock() (open, close int64, known bool) {
	for _, l := range b.Ledgers {
		if isStock(l) {
			open += -l.Open
			close += -l.TallyClose
		}
	}
	return open, close, b.P.To >= b.Last
}

// Warnings that every statement for this period must show.
func (b *Books) Warnings() []string {
	var w []string
	open, close, known := b.Stock()
	if (open != 0 || close != 0) && !known {
		w = append(w, fmt.Sprintf("Closing stock is known only for the latest month (%s); this period's closing stock is NOT included.", b.Last))
	}
	if b.SyncFrom != "" && b.P.From < b.SyncFrom {
		w = append(w, fmt.Sprintf("Vouchers are synced from %s only; entries before that are NOT in this period's figures (sync_now with from=all reads the whole history).", b.SyncFrom))
	}
	if open != 0 && !b.fromStart() {
		w = append(w, "Opening stock for a period that starts after the books' first month is not known; it is NOT included.")
	}
	return w
}

// stockAdj is closing minus opening stock for the period, as far as known.
func (b *Books) stockAdj() (open, close int64) {
	o, c, known := b.Stock()
	if !b.fromStart() {
		o = 0
	}
	if !known {
		c = 0
	}
	return o, c
}

// Group is one primary group's total.
type Group struct {
	Name   string
	Amount int64 // natural side positive (income Cr, expense Dr)
	N      int
}

// TallyPL is the P&L by primary group, as Tally shows it.
type TallyPL struct {
	TradingExp, TradingInc, IndirectExp, IndirectInc []Group
	OpenStock, CloseStock                            int64
	GrossProfit, NetProfit                           int64
}

// PLByGroup builds the Tally-style P&L.
func (b *Books) PLByGroup() TallyPL {
	var r TallyPL
	r.OpenStock, r.CloseStock = b.stockAdj()
	sums := map[string]*Group{}
	order := []string{}
	for _, l := range b.Ledgers {
		if !l.Revenue {
			continue
		}
		v := b.periodAmt(l)
		if v == 0 {
			continue
		}
		g := sums[l.Primary]
		if g == nil {
			g = &Group{Name: l.Primary}
			sums[l.Primary] = g
			order = append(order, l.Primary)
		}
		g.Amount += v
		g.N++
	}
	sort.Strings(order)
	gp := map[string]bool{}
	for _, l := range b.Ledgers {
		if l.Revenue && l.GP {
			gp[l.Primary] = true
		}
	}
	var gross, net int64
	for _, k := range order {
		g := *sums[k]
		inc := g.Amount > 0 // credit balance: income side
		if !inc {
			g.Amount = -g.Amount
		}
		switch {
		case gp[k] && inc:
			r.TradingInc = append(r.TradingInc, g)
			gross += g.Amount
		case gp[k]:
			r.TradingExp = append(r.TradingExp, g)
			gross -= g.Amount
		case inc:
			r.IndirectInc = append(r.IndirectInc, g)
			net += g.Amount
		default:
			r.IndirectExp = append(r.IndirectExp, g)
			net -= g.Amount
		}
	}
	gross += r.CloseStock - r.OpenStock
	r.GrossProfit = gross
	r.NetProfit = gross + net
	return r
}

// ProfitToDate is the profit from the start of the books to the period end (the Profit &
// Loss A/c ledger's own opening is added separately in the balance sheet).
func (b *Books) ProfitToDate() (current int64) {
	for _, l := range b.Ledgers {
		if l.Revenue {
			current += l.Close()
		}
	}
	// Stock moves the profit only when the closing stock is known; otherwise the balance
	// sheet keeps stock at its opening value, so profit must not move by it either.
	if open, close, known := b.Stock(); known {
		current += close - open
	}
	return current
}

// bsLedger is one balance-sheet ledger with its closing at the period end.
type bsLedger struct {
	Ledger
	Bal int64
}

// bsLedgers returns every non-revenue ledger's closing, with stock at Tally's closing value
// (when known) and the Profit & Loss A/c ledger left out (shown as its own line).
func (b *Books) bsLedgers() (ls []bsLedger, plOpen int64) {
	_, _, known := b.Stock()
	for _, l := range b.Ledgers {
		if l.Revenue {
			continue
		}
		if l.Name == "Profit & Loss A/c" && l.Primary == "" {
			plOpen = l.Close()
			continue
		}
		bal := l.Close()
		if isStock(l) {
			if !known {
				bal = l.Open // closing stock not known: books value only (warned)
			} else {
				bal = l.TallyClose
			}
		}
		if bal != 0 {
			ls = append(ls, bsLedger{l, bal})
		}
	}
	return ls, plOpen
}

// BS is a balance sheet: two sides, each a list of sections.
type BS struct {
	Liab, Assets []Section
	Diff         int64 // liabilities minus assets; 0 when the books balance
	PLOpen, PL   int64 // P&L A/c opening and profit to the period end
}

// Liability-side primary groups in Tally; other primaries go by their balance.
var liabPrimary = map[string]bool{"Capital Account": true, "Loans (Liability)": true, "Current Liabilities": true,
	"Suspense A/c": true, "Branch / Divisions": true}
var assetPrimary = map[string]bool{"Fixed Assets": true, "Investments": true, "Current Assets": true, "Misc. Expenses (ASSET)": true}

// BSByGroup builds the Tally-style balance sheet by primary group.
func (b *Books) BSByGroup() BS {
	ls, plOpen := b.bsLedgers()
	profit := b.ProfitToDate()
	sum := map[string]int64{}
	var names []string
	for _, l := range ls {
		k := l.Primary
		if k == "" {
			k = l.Name
		}
		if _, ok := sum[k]; !ok {
			names = append(names, k)
		}
		sum[k] += l.Bal
	}
	sort.Strings(names)
	liab := Section{Title: "Liabilities"}
	asset := Section{Title: "Assets"}
	for _, k := range names {
		v := sum[k]
		if liabPrimary[k] || !isAssetName(k) && v > 0 {
			liab.add(k, v, "")
		} else {
			asset.add(k, -v, "")
		}
	}
	liab.add("Profit & Loss A/c", plOpen+profit, plNote(plOpen, profit))
	return BS{Liab: []Section{liab}, Assets: []Section{asset}, Diff: liab.Total - asset.Total, PLOpen: plOpen, PL: profit}
}

// plNote explains the Profit & Loss A/c line: profit since the books began, and entries
// passed in the P&L A/c ledger itself (e.g. earlier years' profit moved to capital).
func plNote(plLedger, profit int64) string {
	if plLedger == 0 {
		return "profit since the books began"
	}
	return fmt.Sprintf("profit since the books began %s; entries in the P&L A/c ledger %s, e.g. transfers to capital", money(profit), money(plLedger))
}

func isAssetName(k string) bool {
	for a := range assetPrimary {
		if strings.EqualFold(a, k) {
			return true
		}
	}
	return false
}

// money is a short signed rupee figure for notes (full formatting is in the tools).
func money(p int64) string {
	neg := p < 0
	if neg {
		p = -p
	}
	s := indian(p / 100)
	if neg {
		return "-" + s
	}
	return s
}

// indian groups digits the Indian way: 12,34,567.
func indian(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	head, tail := s[:len(s)-3], s[len(s)-3:]
	var parts []string
	for len(head) > 2 {
		parts = append([]string{head[len(head)-2:]}, parts...)
		head = head[:len(head)-2]
	}
	if head != "" {
		parts = append([]string{head}, parts...)
	}
	return strings.Join(parts, ",") + "," + tail
}

// GroupLedgers lists the ledgers under group g for the statement: balance-sheet closing,
// or the period amount for P&L groups. Natural side positive; largest first.
func (b *Books) GroupLedgers(g string) []Line {
	var out []Line
	if strings.EqualFold(g, "Profit & Loss A/c") {
		return nil
	}
	bs, _ := b.bsLedgers()
	for _, l := range bs {
		if l.Under(g) || strings.EqualFold(l.Primary, g) {
			out = append(out, Line{l.Name, l.Bal, l.Parent})
		}
	}
	for _, l := range b.Ledgers {
		if l.Revenue && (l.Under(g) || strings.EqualFold(l.Primary, g)) {
			if v := b.periodAmt(l); v != 0 {
				out = append(out, Line{l.Name, v, l.Parent})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return abs(out[i].Amount) > abs(out[j].Amount) })
	return out
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
