// Package tools holds the MCP tools. The server does the maths; tools return short text
// tables (top N by default), never raw rows.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tbcheck"
)

// Env is what tools need.
type Env struct {
	Root  string
	Cfg   config.Config
	Box   *crypt.Box
	Now   func() time.Time
	Tally *tally.Client // nil: never contact Tally (tests, backups)
	// Ask shows the user a form through the AI app (MCP elicitation); nil or mcp.ErrNoElicit:
	// the app cannot, and OpenPanel (if set) opens the control panel instead.
	Ask       func(message string, schema json.RawMessage) (mcp.Elicitation, error)
	OpenPanel func() error
	// Confirm shows a Yes/No dialog on this PC, straight to the person (Windows); when set it
	// is used instead of Ask for the yearly authorisation and for allowing posting.
	Confirm func(title, text string) (bool, error)

	checked   map[string]time.Time // last quick check per company folder
	openNames []string             // companies open in Tally, cached (openInTally)
	openAt    time.Time
	note      string // appended to the current answer (e.g. stale data)
}

// RefreshEvery is how old the last check may be before an answer triggers a quick check.
const RefreshEvery = 60 * time.Second

// refresh runs the smart sync before answering about a live company, so answers are as
// fresh as Tally. Unchanged books cost one small request. On failure the answer still comes
// from the last sync, with a note saying how old it is.
func (e *Env) refresh(l store.Label) {
	if e.Tally == nil || e.Cfg.NoAutoRefresh || l.Source != store.Live {
		return
	}
	if e.checked == nil {
		e.checked = map[string]time.Time{}
	}
	k := l.Dir(e.Root)
	if time.Since(e.checked[k]) < RefreshEvery {
		return
	}
	if bg.busy() { // never wait behind a long first sync
		e.note = "Note: a sync is running in the background; figures are as of the last finished sync."
		return
	}
	e.checked[k] = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := syncjob.Run(ctx, e.Tally, syncjob.Job{Root: e.Root, Company: l.Company, Source: l.Source})
	switch {
	case err != nil && res.Mode == "":
		e.note = fmt.Sprintf("Note: could not check Tally (%v); figures are as of the last sync.", err)
	case err == nil && res.Mode != "unchanged":
		e.note = fmt.Sprintf("Note: refreshed from Tally first (%s, %d vouchers updated).", res.Mode, res.Stats.Fetched)
	}
}

// withNote wraps a handler so any refresh note is appended to its answer.
func (e *Env) withNote(h func(map[string]any) (string, error)) func(map[string]any) (string, error) {
	return func(a map[string]any) (string, error) {
		e.note = ""
		s, err := h(a)
		if e.note != "" && err == nil {
			s += "\n" + e.note
		}
		return s, err
	}
}

func (e *Env) syncNow(a map[string]any) (string, error) {
	ls, err := store.List(e.Root)
	if err != nil {
		return "", err
	}
	want := strings.ToLower(str(a, "company"))
	var l *store.Label
	for i := range ls {
		if ls[i].Source == store.Live && (want == "" && len(ls) == 1 || strings.EqualFold(ls[i].Company, want) ||
			want != "" && strings.Contains(strings.ToLower(ls[i].Company), want)) {
			l = &ls[i]
		}
	}
	if e.Tally == nil {
		return "", fmt.Errorf("syncing is not available here")
	}
	// Not set up here yet, or its authorisation is due again (new financial year): the
	// connect flow (Tally check, the user's own authorisation, first sync in the background).
	if l == nil {
		return e.companyConnect(a)
	}
	if _, err := (auth.Store{Dir: l.Dir(e.Root), Box: e.Box}).Valid(l.Company, e.Now()); err != nil {
		return e.companyConnect(map[string]any{"company": l.Company})
	}
	if running, text := bg.view(""); running {
		return text + "\nIt is already syncing; ask sync_status later.", nil
	}
	full, _ := a["full"].(bool)
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res, err := syncjob.Run(ctx, e.Tally, syncjob.Job{Root: e.Root, Company: l.Company, Source: l.Source, Full: full})
	if e.checked == nil {
		e.checked = map[string]time.Time{}
	}
	e.checked[l.Dir(e.Root)] = time.Now()
	if err != nil && res.Mode == "" {
		return "", err
	}
	return fmt.Sprintf("%s\n(%s)", res, time.Since(t0).Round(time.Millisecond)), nil
}

const companyProp = `"company":{"type":"string","description":"Company name (optional if only one)"}`

func schema(props ...string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + strings.Join(append([]string{companyProp}, props...), ",") + `}}`)
}

// Core returns the Phase 1 core tools.
func Core(e *Env) []mcp.Tool {
	ts := e.core()
	for i := range ts {
		ts[i].Handler = e.withNote(ts[i].Handler)
	}
	return ts
}

func (e *Env) core() []mcp.Tool {
	return []mcp.Tool{
		{Name: "sync_now", Description: "Sync from Tally; connects new companies (no name: list them).",
			InputSchema: schema(`"full":{"type":"boolean","description":"Full re-check of all months"}`), Handler: e.syncNow},
		{Name: "company_list", Description: "List synced companies with source, last sync and check status.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`), Handler: e.companyList},
		{Name: "sync_status", Description: "Sync and trial-balance check status for a company.",
			InputSchema: schema(), Handler: e.syncStatus},
		{Name: "trial_balance", Description: "Trial balance by group (Dr/Cr), matched with Tally.",
			InputSchema: schema(`"group":{"type":"string","description":"Show ledgers of this group"}`, `"top":{"type":"integer"}`), Handler: e.trialBalance},
		{Name: "ledger_balance", Description: "One ledger: closing, opening, totals. Optional month-wise table and last N entries.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp + `,"ledger":{"type":"string"},` +
				`"monthly":{"type":"boolean"},"entries":{"type":"integer","description":"Last N entries"}},"required":["ledger"]}`), Handler: e.ledgerBalance},
		{Name: "outstanding_summary", Description: "Receivables or payables: total and top parties.",
			InputSchema: schema(`"kind":{"type":"string","enum":["receivable","payable"]}`, `"top":{"type":"integer"}`), Handler: e.outstanding},
		{Name: "month_summary", Description: "Month-wise sales, purchases, expenses, GST, receipts, payments.",
			InputSchema: schema(`"months":{"type":"integer","description":"Last N months (default 12)"}`), Handler: e.monthSummary},
	}
}

func str(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return strings.TrimSpace(s)
}

func (e *Env) top(a map[string]any) int {
	if f, ok := a["top"].(float64); ok && f >= 1 {
		return min(int(f), 100)
	}
	return e.Cfg.TopN
}

// open picks the company, then enforces: dev guard, authorisation for this FY, and
// (if checked) a trial balance matching Tally.
func (e *Env) open(a map[string]any, needTB bool) (*store.DB, error) {
	ls, err := store.List(e.Root)
	if err != nil {
		return nil, err
	}
	if len(ls) == 0 {
		return nil, fmt.Errorf("no company connected yet: call sync_now (with the company name, if the user gave one) to connect the company open in TallyPrime")
	}
	want := strings.ToLower(str(a, "company"))
	var hits []store.Label
	for _, l := range ls {
		n := strings.ToLower(l.Company)
		if want == "" || n == want {
			hits = append(hits, l)
		}
	}
	if len(hits) == 0 && want != "" {
		for _, l := range ls {
			if strings.Contains(strings.ToLower(l.Company), want) {
				hits = append(hits, l)
			}
		}
	}
	if len(hits) == 0 && want != "" {
		return nil, fmt.Errorf("%q is not connected yet: call sync_now with company %q to connect it", str(a, "company"), str(a, "company"))
	}
	if len(hits) > 1 && want == "" { // no name given: the company open in TallyPrime now
		var open []store.Label
		for _, l := range hits {
			if l.Source == store.Live && slices.ContainsFunc(e.openInTally(), func(n string) bool { return strings.EqualFold(n, l.Company) }) {
				open = append(open, l)
			}
		}
		if len(open) == 1 {
			hits = open
		}
	}
	if len(hits) != 1 {
		var names []string
		for _, l := range ls {
			names = append(names, fmt.Sprintf("%s (%s)", l.Company, l.Source))
		}
		return nil, fmt.Errorf("say which company: %s", strings.Join(names, "; "))
	}
	l := hits[0]
	if _, err := auth.CoverFromAnnual(auth.Store{Dir: e.Root, Box: e.Box}, auth.Store{Dir: l.Dir(e.Root), Box: e.Box},
		l.Company, l.GSTIN, string(l.Source), e.Now()); err != nil {
		if l.Source == store.Live {
			return nil, fmt.Errorf("%s needs this year's authorisation: call sync_now with company %q (the user is asked)", l.Company, l.Company)
		}
		return nil, err
	}
	e.refresh(l)
	db, err := store.Open(e.Root, l)
	if err != nil {
		return nil, err
	}
	if needTB {
		if err := tbcheck.Gate(db); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func (e *Env) companyList(map[string]any) (string, error) {
	ls, err := store.List(e.Root)
	if err != nil || len(ls) == 0 {
		return "No company synced yet.", err
	}
	var b strings.Builder
	b.WriteString("Company | Source | GSTIN | Last sync | TB check\n")
	for _, l := range ls {
		db, err := store.Open(e.Root, l)
		if err != nil {
			fmt.Fprintf(&b, "%s | %s | %s | error: %v\n", l.Company, l.Source, dash(l.GSTIN), err)
			continue
		}
		ls, _ := db.Meta("last_sync")
		tb, _ := db.Meta("tb_status")
		db.Close()
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s\n", l.Company, l.Source, dash(l.GSTIN), when(ls), dash(tb))
	}
	return b.String(), nil
}

func (e *Env) syncStatus(a map[string]any) (string, error) {
	running, bgText := bg.view("")
	if running {
		return bgText, nil
	}
	db, err := e.open(a, false)
	if err != nil {
		if bgText != "" {
			return bgText, nil
		}
		return "", err
	}
	defer db.Close()
	var n, nl int
	var from, to string
	db.QueryRow(`SELECT COUNT(*), COALESCE(MIN(date),''), COALESCE(MAX(date),'') FROM vouchers`).Scan(&n, &from, &to)
	db.QueryRow(`SELECT COUNT(*) FROM ledgers`).Scan(&nl)
	last, _ := db.Meta("last_sync")
	tb, _ := db.Meta("tb_status")
	rec, _ := (auth.Store{Dir: db.Label.Dir(e.Root), Box: e.Box}).Valid(db.Label.Company, e.Now())
	s := fmt.Sprintf("%s (%s)\nLast sync: %s\nVouchers: %d (%s to %s), ledgers: %d\nTrial balance vs Tally: %s\nAuthorisation: %s FY %s",
		db.Label.Company, db.Label.Source, when(last), n, dash(from), dash(to), nl, dash(tb), rec.Kind, rec.FY)
	if tb != "ok" {
		d, _ := db.Meta("tb_detail")
		s += "\n" + d
	}
	return s, nil
}

// closings: computed closing per ledger = opening + lines (optional vouchers excluded).
const closingsSQL = `SELECT l.name, l.parent, l.opening + COALESCE(m.t,0) bal FROM ledgers l
 LEFT JOIN (SELECT vl.ledger, SUM(vl.amount) t FROM vlines vl JOIN acct_vouchers v ON v.guid=vl.voucher_guid
            GROUP BY vl.ledger) m ON m.ledger=l.name`

func (e *Env) trialBalance(a map[string]any) (string, error) {
	db, err := e.open(a, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var b strings.Builder
	var dr, cr int64
	if g := str(a, "group"); g != "" {
		list, err := nameBals(db, `SELECT name, bal FROM (`+closingsSQL+`) WHERE parent = ? COLLATE NOCASE AND bal<>0 ORDER BY ABS(bal) DESC`, g)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s, group %q (top %d)\nLedger | Balance\n", db.Label.Company, g, e.top(a))
		i, more := 0, 0
		for _, r := range list {
			n, v := r.name, r.bal
			if v < 0 {
				dr -= v
			} else {
				cr += v
			}
			if i++; i <= e.top(a) {
				b.WriteString(e.show(db, n, g) + " | " + side(v) + "\n")
			} else {
				more++
			}
		}
		if i == 0 {
			return fmt.Sprintf("No ledgers with a balance in group %q.", g), nil
		}
		if more > 0 {
			fmt.Fprintf(&b, "(%d more ledgers)\n", more)
		}
		fmt.Fprintf(&b, "Group total | %s | %s\n", amt(dr), amt(cr))
		return b.String(), nil
	}
	rows, err := db.Query(`SELECT parent, SUM(CASE WHEN bal<0 THEN -bal ELSE 0 END), SUM(CASE WHEN bal>0 THEN bal ELSE 0 END), COUNT(*)
		FROM (` + closingsSQL + `) WHERE bal<>0 GROUP BY parent ORDER BY parent`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	fmt.Fprintf(&b, "%s trial balance (matched with Tally)\nGroup | Ledgers | Dr | Cr\n", db.Label.Company)
	for rows.Next() {
		var g string
		var d, c int64
		var n int
		rows.Scan(&g, &d, &c, &n)
		if g == "" {
			g = "(primary)"
		}
		dr, cr = dr+d, cr+c
		fmt.Fprintf(&b, "%s | %d | %s | %s\n", g, n, amt(d), amt(c))
	}
	fmt.Fprintf(&b, "Total | | %s | %s\n", amt(dr), amt(cr))
	b.WriteString(stockNote(db))
	return b.String(), nil
}

// stockNote gives Tally's closing stock value of Stock-in-Hand ledgers whose value differs from
// their entries (closing stock entered per date, or valued from items). The rows above show
// entries only; the balance sheet and any carry-forward must use Tally's value.
func stockNote(db *store.DB) string {
	sl, _ := db.StockLedgers()
	var parts []string
	for n, tallyVal := range sl {
		var ours int64
		db.QueryRow(`SELECT bal FROM (`+closingsSQL+`) WHERE name = ?`, n).Scan(&ours)
		if tallyVal != ours {
			parts = append(parts, fmt.Sprintf("%s %s (entries only: %s)", n, side(tallyVal), side(ours)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return "Closing stock, Tally's value (use this for the balance sheet and carry-forward; never put the difference in Profit & Loss): " +
		strings.Join(parts, "; ") + "\n"
}

func (e *Env) ledgerBalance(a map[string]any) (string, error) {
	q := str(a, "ledger")
	if q == "" {
		return "", fmt.Errorf("give a ledger name")
	}
	db, err := e.open(a, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	if real := db.PartyName(q); real != "" { // privacy-mode code such as P001
		q = real
	}
	var name, parent string
	var opening int64
	err = db.QueryRow(`SELECT name, parent, opening FROM ledgers WHERE name = ? COLLATE NOCASE`, q).Scan(&name, &parent, &opening)
	if err != nil {
		rows, _ := db.Query(`SELECT name, parent FROM ledgers WHERE name LIKE ? ORDER BY name LIMIT 6`, "%"+q+"%")
		var c, parents, shown []string
		for rows.Next() {
			var n, p string
			rows.Scan(&n, &p)
			c, parents = append(c, n), append(parents, p)
		}
		rows.Close() // close before show(): it may query the DB
		for i := range c {
			shown = append(shown, e.show(db, c[i], parents[i]))
		}
		if len(c) > 1 {
			c = shown
		}
		switch len(c) {
		case 0:
			return "", fmt.Errorf("no ledger matching %q", q)
		case 1:
			a["ledger"] = c[0]
			db.Close()
			return e.ledgerBalance(a)
		default:
			return "", fmt.Errorf("several ledgers match %q: %s", q, strings.Join(c, "; "))
		}
	}
	shownName := e.show(db, name, parent) // before opening a cursor (single DB connection)
	type month struct {
		m       string
		d, c    int64
		entries int
	}
	var ms []month
	rows, err := db.Query(`SELECT substr(v.date,1,7), SUM(CASE WHEN vl.amount<0 THEN -vl.amount ELSE 0 END),
		SUM(CASE WHEN vl.amount>0 THEN vl.amount ELSE 0 END), COUNT(*)
		FROM vlines vl JOIN acct_vouchers v ON v.guid=vl.voucher_guid
		WHERE vl.ledger=? GROUP BY 1 ORDER BY 1`, name)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var x month
		rows.Scan(&x.m, &x.d, &x.c, &x.entries)
		ms = append(ms, x)
	}
	rows.Close()
	var dr, cr int64
	n := 0
	for _, x := range ms {
		dr, cr, n = dr+x.d, cr+x.c, n+x.entries
	}
	bal := opening + cr - dr

	var b strings.Builder
	// Default answer is one short block; month-wise and entries only when asked.
	stockVal, isStock := map[string]int64{}, false
	if sl, err := db.StockLedgers(); err == nil {
		stockVal = sl
		_, isStock = sl[name]
	}
	if isStock && stockVal[name] != bal {
		fmt.Fprintf(&b, "%s (under %s, Stock-in-Hand): closing stock %s (Tally's value: use this for the balance sheet and carry-forward)\nEntries only: closing %s | ",
			shownName, dash(parent), side(stockVal[name]), side(bal))
		fmt.Fprintf(&b, "Opening %s | Dr %s | Cr %s | %d entries", side(opening), amt(dr), amt(cr), n)
	} else {
		fmt.Fprintf(&b, "%s (under %s): closing %s\nOpening %s | Dr %s | Cr %s | %d entries",
			shownName, dash(parent), side(bal), side(opening), amt(dr), amt(cr), n)
	}
	if n == 1 {
		s := strings.Replace(b.String(), "| 1 entries", "| 1 entry", 1)
		b.Reset()
		b.WriteString(s)
	}
	if len(ms) > 0 {
		f, _ := time.Parse("2006-01", ms[0].m)
		l, _ := time.Parse("2006-01", ms[len(ms)-1].m)
		fmt.Fprintf(&b, " (%s to %s)", f.Format("Jan 2006"), l.Format("Jan 2006"))
	}
	if on, _ := a["monthly"].(bool); on {
		b.WriteString("\nMonth | Entries | Dr | Cr")
		for _, x := range ms {
			t, _ := time.Parse("2006-01", x.m)
			fmt.Fprintf(&b, "\n%s | %d | %s | %s", t.Format("Jan 2006"), x.entries, amt(x.d), amt(x.c))
		}
	}
	if k, ok := a["entries"].(float64); ok && k >= 1 {
		s, err := e.lastEntries(db, name, min(int(k), 50))
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// lastEntries lists the latest k entries of a ledger, newest first, each with its main
// opposite ledger (the largest other line in that voucher).
func (e *Env) lastEntries(db *store.DB, ledger string, k int) (string, error) {
	type ent struct {
		date, typ, num, other, otherParent string
		amt                                int64
	}
	var es []ent
	rows, err := db.Query(`SELECT v.date, v.type, v.number, vl.amount,
		COALESCE((SELECT o.ledger FROM vlines o WHERE o.voucher_guid = v.guid AND o.ledger <> ?1
		          ORDER BY ABS(o.amount) DESC LIMIT 1), ''),
		COALESCE((SELECT l.parent FROM vlines o JOIN ledgers l ON l.name = o.ledger
		          WHERE o.voucher_guid = v.guid AND o.ledger <> ?1 ORDER BY ABS(o.amount) DESC LIMIT 1), '')
		FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
		WHERE vl.ledger = ?1 ORDER BY v.date DESC, v.alter_id DESC LIMIT ?2`, ledger, k)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var x ent
		rows.Scan(&x.date, &x.typ, &x.num, &x.amt, &x.other, &x.otherParent)
		es = append(es, x)
	}
	rows.Close() // before show(), which may query
	var b strings.Builder
	fmt.Fprintf(&b, "\nLast %d entries:\nDate | Type | No. | Against | Amount", len(es))
	for _, x := range es {
		t, _ := time.Parse(time.DateOnly, x.date)
		fmt.Fprintf(&b, "\n%s | %s | %s | %s | %s", t.Format("02-01-2006"), x.typ, dash(x.num),
			dash(e.show(db, x.other, x.otherParent)), side(x.amt))
	}
	return b.String(), nil
}

func (e *Env) outstanding(a map[string]any) (string, error) {
	db, err := e.open(a, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	group, title, sign := "Sundry Debtors", "Receivables", int64(-1) // debtors normally Dr
	if str(a, "kind") == "payable" {
		group, title, sign = "Sundry Creditors", "Payables", 1
	}
	// Includes ledgers in sub-groups of Sundry Debtors/Creditors.
	list, err := nameBals(db, `SELECT name, bal FROM (`+closingsSQL+`)
		WHERE (parent = ?1 OR parent IN (SELECT name FROM group_anc WHERE ancestor = ?1))
		AND bal<>0 ORDER BY bal*?2 DESC`, group, sign)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	var total, adv int64
	var n, nadv int
	var lines []string
	for _, r := range list {
		p, v := r.name, r.bal
		if v*sign > 0 {
			total += v * sign
			n++
			if len(lines) < e.top(a) {
				lines = append(lines, fmt.Sprintf("%s | %s", e.show(db, p, group), amt(v*sign)))
			}
		} else {
			adv += -v * sign
			nadv++
		}
	}
	fmt.Fprintf(&b, "%s, %s (%s): %s from %s\n", db.Label.Company, title, group, amt(total), parties(n))
	if n > 0 {
		fmt.Fprintf(&b, "Top %d:\nParty | Amount\n%s\n", len(lines), strings.Join(lines, "\n"))
	}
	if nadv > 0 {
		fmt.Fprintf(&b, "Opposite balances (advances): %s from %s\n", amt(adv), parties(nadv))
	}
	s, err := e.billAgeing(db, group, sign)
	if err != nil {
		return "", err
	}
	b.WriteString(s)
	return b.String(), nil
}

// billAgeing adds bill-wise ageing for parties Tally tracks bill-wise (from Tally's own
// outstanding bills, synced each time). Parties not tracked bill-wise stay balance-only.
func (e *Env) billAgeing(db *store.DB, group string, sign int64) (string, error) {
	type bill struct {
		party, name, date string
		amt               int64
	}
	var bs []bill
	rows, err := db.Query(`SELECT b.party, b.name, b.bill_date, b.closing * ?2 FROM bills b JOIN ledgers l ON l.name = b.party
		WHERE (l.parent = ?1 OR l.parent IN (SELECT name FROM group_anc WHERE ancestor = ?1)) AND b.closing * ?2 > 0
		ORDER BY b.closing * ?2 DESC`, group, sign)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var x bill
		rows.Scan(&x.party, &x.name, &x.date, &x.amt)
		bs = append(bs, x)
	}
	rows.Close()
	if len(bs) == 0 {
		return "No bill-wise details in Tally for these parties (balances only).", nil
	}
	buckets := []struct {
		label string
		max   int
	}{{"0-30", 30}, {"31-60", 60}, {"61-90", 90}, {"91-180", 180}, {"181-365", 365}, {"over 365", 1 << 30}}
	sums := make([]int64, len(buckets))
	counts := make([]int, len(buckets))
	today := e.Now()
	age := func(d string) int {
		t, err := time.Parse(time.DateOnly, d)
		if err != nil {
			return 1 << 29
		}
		return int(today.Sub(t).Hours() / 24)
	}
	var tot int64
	for _, x := range bs {
		days := age(x.date)
		for i, bk := range buckets {
			if days <= bk.max {
				sums[i] += x.amt
				counts[i]++
				break
			}
		}
		tot += x.amt
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Bill-wise (Tally's open bills): %s in %d bills\nAge (days) | Bills | Amount", amt(tot), len(bs))
	for i, bk := range buckets {
		if counts[i] > 0 {
			fmt.Fprintf(&b, "\n%s | %d | %s", bk.label, counts[i], amt(sums[i]))
		}
	}
	k := min(5, len(bs))
	fmt.Fprintf(&b, "\nLargest %d bills:\nParty | Bill | Date | Days | Amount", k)
	for _, x := range bs[:k] {
		t, _ := time.Parse(time.DateOnly, x.date)
		fmt.Fprintf(&b, "\n%s | %s | %s | %d | %s", e.show(db, x.party, group), x.name, t.Format("02-01-2006"), age(x.date), amt(x.amt))
	}
	return b.String(), nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func when(rfc string) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return "never"
	}
	return t.Format("02-01-2006 15:04")
}

// amt formats positive paise as Indian rupees without side.
func amt(p int64) string {
	s := tbcheck.Rs(p)
	return strings.TrimSuffix(strings.TrimSuffix(s, " Cr"), " Dr")
}

// side shows a Tally-signed balance as "x Dr" / "x Cr"; columns use amt.
func side(p int64) string {
	if p == 0 {
		return "0.00"
	}
	return tbcheck.Rs(p)
}

func parties(n int) string {
	if n == 1 {
		return "1 party"
	}
	return fmt.Sprintf("%d parties", n)
}

// partyGroups hold ledgers whose names are personal/business identities.
var partyGroups = map[string]bool{"Sundry Debtors": true, "Sundry Creditors": true}

// show returns the name to send to the AI: a stable code in privacy mode for parties.
func (e *Env) show(db *store.DB, name, parent string) string {
	if !e.Cfg.PrivacyMode {
		return name
	}
	if !partyGroups[canonGroup(parent)] {
		// Sub-groups under debtors/creditors are parties too.
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM group_anc WHERE name = ? COLLATE NOCASE
			AND ancestor IN ('Sundry Debtors','Sundry Creditors')`, parent).Scan(&n)
		if n == 0 {
			return name
		}
	}
	if c, err := db.PartyCode(name); err == nil {
		return c
	}
	return "(party)" // never fall back to the real name
}

// canonGroup matches group names case-insensitively ("sundry debtors" typed by the AI).
func canonGroup(g string) string {
	for k := range partyGroups {
		if strings.EqualFold(strings.TrimSpace(g), k) {
			return k
		}
	}
	return g
}

type nameBal struct {
	name string
	bal  int64
}

// nameBals reads all rows and closes the cursor before returning. The DB has one
// connection, so nothing (e.g. a party-code lookup) may query while a cursor is open.
func nameBals(db *store.DB, q string, args ...any) ([]nameBal, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []nameBal
	for rows.Next() {
		var r nameBal
		if err := rows.Scan(&r.name, &r.bal); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
