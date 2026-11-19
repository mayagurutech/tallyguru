package write

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/gst"
)

// Line is one accounting line. Amount in paise, Tally sign (negative = debit).
type Line struct {
	Ledger string `json:"ledger"`
	Amount int64  `json:"amount"`
}

// Item is one stock line of an item invoice. Rate in paise per unit; Amount with the sign
// of its accounting line; Ledger is the sales/purchase ledger it is allocated to.
type Item struct {
	Item   string `json:"item"`
	Qty    string `json:"qty"`
	Unit   string `json:"unit"`
	Rate   int64  `json:"rate"`
	Amount int64  `json:"amount"`
	Ledger string `json:"ledger"`
	Out    bool   `json:"out,omitempty"` // stock journal: going out
}

// Voucher is a draft entry.
type Voucher struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Base      string `json:"base"` // Tally base type (Sales, Payment...)
	Date      string `json:"date"` // YYYY-MM-DD
	Number    string `json:"number,omitempty"`
	Party     string `json:"party,omitempty"`
	Reference string `json:"reference,omitempty"`
	Narr      string `json:"narration,omitempty"`
	Lines     []Line `json:"lines"`
	Items     []Item `json:"items,omitempty"`
	Override  bool   `json:"override_duplicate,omitempty"`
}

// Schema for the drafts kept per company.
const Schema = `
CREATE TABLE IF NOT EXISTS drafts (id INTEGER PRIMARY KEY, created TEXT NOT NULL, kind TEXT NOT NULL,
  body TEXT NOT NULL, status TEXT NOT NULL, guid TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '');`

// Rupees parses a rupee amount ("1,18,000.50" or a number) into paise.
func Rupees(v any) (int64, error) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case string:
		s := strings.ReplaceAll(strings.TrimSpace(x), ",", "")
		var err error
		if f, err = strconv.ParseFloat(s, 64); err != nil {
			return 0, fmt.Errorf("amount %q is not a number", x)
		}
	case nil:
		return 0, nil
	default:
		return 0, fmt.Errorf("amount %v is not a number", v)
	}
	return int64(math.Round(f * 100)), nil
}

// FromArgs reads a draft from tool arguments:
// lines: [{ledger, dr|cr}], items: [{item, qty, unit, rate, ledger}] (amount = qty × rate).
func FromArgs(a map[string]any) (Voucher, error) {
	s := func(k string) string { x, _ := a[k].(string); return strings.TrimSpace(x) }
	v := Voucher{Type: s("type"), Date: s("date"), Number: s("number"), Party: s("party"),
		Reference: s("reference"), Narr: s("narration")}
	v.Override, _ = a["override_duplicate"].(bool)
	if v.Type == "" {
		return v, fmt.Errorf("type is required (e.g. Payment, Receipt, Journal, Contra, Sales, Purchase)")
	}
	if t, err := time.Parse(time.DateOnly, v.Date); err != nil {
		if t2, err2 := time.Parse("02-01-2006", v.Date); err2 == nil {
			v.Date = t2.Format(time.DateOnly)
		} else {
			return v, fmt.Errorf("date %q: use YYYY-MM-DD or DD-MM-YYYY", v.Date)
		}
	} else {
		v.Date = t.Format(time.DateOnly)
	}
	ls, _ := a["lines"].([]any)
	for i, x := range ls {
		m, _ := x.(map[string]any)
		name, _ := m["ledger"].(string)
		dr, err1 := Rupees(m["dr"])
		cr, err2 := Rupees(m["cr"])
		if err1 != nil || err2 != nil {
			return v, fmt.Errorf("line %d: %v", i+1, firstErr(err1, err2))
		}
		if strings.TrimSpace(name) == "" || (dr == 0) == (cr == 0) || dr < 0 || cr < 0 {
			return v, fmt.Errorf("line %d: give a ledger and one positive amount, dr or cr", i+1)
		}
		v.Lines = append(v.Lines, Line{Ledger: strings.TrimSpace(name), Amount: cr - dr})
	}
	its, _ := a["items"].([]any)
	for i, x := range its {
		m, _ := x.(map[string]any)
		it := Item{}
		it.Item, _ = m["item"].(string)
		it.Unit, _ = m["unit"].(string)
		it.Ledger, _ = m["ledger"].(string)
		var q float64
		switch y := m["qty"].(type) {
		case float64:
			q = y
		case string:
			q, _ = strconv.ParseFloat(strings.TrimSpace(y), 64)
		}
		rate, err := Rupees(m["rate"])
		if err != nil || q == 0 || rate <= 0 || it.Item == "" {
			return v, fmt.Errorf("item %d: give item, qty and rate > 0 (stock journal: qty < 0 for items going out)", i+1)
		}
		if q < 0 {
			it.Out, q = true, -q
		}
		it.Qty = strconv.FormatFloat(q, 'f', -1, 64)
		it.Rate = rate
		it.Amount = int64(math.Round(q * float64(rate)))
		v.Items = append(v.Items, it)
	}
	if len(v.Lines)+len(v.Items) < 2 {
		return v, fmt.Errorf("an entry needs at least two lines")
	}
	return v, nil
}

func firstErr(es ...error) error {
	for _, e := range es {
		if e != nil {
			return e
		}
	}
	return nil
}

// Book is what the checks need from the company's synced data.
type Book struct {
	db                 *sql.DB
	ledgers            map[string]ledgerInfo
	lower              map[string]string
	vtypes             map[string]string // type -> base
	items              map[string]string // stock item -> base unit
	companyState, from string
}

type ledgerInfo struct {
	name, parent, state, gstin, tax, tds, flag string
	party, cash, bank, foreign                 bool
}

// LoadBook reads masters needed for checks.
func LoadBook(db *sql.DB) (*Book, error) {
	b := &Book{db: db, ledgers: map[string]ledgerInfo{}, lower: map[string]string{}, vtypes: map[string]string{}, items: map[string]string{}}
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
	rows, err = db.Query(`SELECT l.name, l.parent, COALESCE(p.state,''), COALESCE(p.gstin,''), COALESCE(p.country,''),
		COALESCE((SELECT tag FROM ledger_map m WHERE m.guid=l.guid AND m.family='tax'),''),
		COALESCE((SELECT tag FROM ledger_map m WHERE m.guid=l.guid AND m.family='tds'),''),
		COALESCE((SELECT tag FROM ledger_map m WHERE m.guid=l.guid AND m.family='flag'),'')
		FROM ledgers l LEFT JOIN ledger_party p ON p.name=l.name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x ledgerInfo
		var country string
		rows.Scan(&x.name, &x.parent, &x.state, &x.gstin, &country, &x.tax, &x.tds, &x.flag)
		a := anc[x.parent]
		x.party = a["Sundry Debtors"] || a["Sundry Creditors"]
		x.cash = a["Cash-in-Hand"] || x.parent == "Cash-in-Hand"
		x.bank = a["Bank Accounts"] || a["Bank OD A/c"] || x.parent == "Bank Accounts"
		x.foreign = country != "" && !strings.EqualFold(country, "India")
		b.ledgers[x.name] = x
		b.lower[strings.ToLower(x.name)] = x.name
	}
	rows.Close()
	rows, err = db.Query(`SELECT t.name, COALESCE(b.base, t.name) FROM vtypes t LEFT JOIN vtype_base b ON b.name=t.name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n, base string
		rows.Scan(&n, &base)
		b.vtypes[n] = base
	}
	rows.Close()
	rows, err = db.Query(`SELECT name, unit FROM stock_items`)
	if err == nil {
		for rows.Next() {
			var n, u string
			rows.Scan(&n, &u)
			b.items[n] = u
		}
		rows.Close()
	}
	db.QueryRow(`SELECT value FROM meta WHERE key='company_state'`).Scan(&b.companyState)
	db.QueryRow(`SELECT value FROM meta WHERE key='books_from'`).Scan(&b.from)
	return b, nil
}

// Check validates a draft. Errors stop it; warnings are shown in the preview.
// Ledger and voucher type names are corrected to Tally's exact spelling when only the case differs.
func (b *Book) Check(v *Voucher) (errs, warns []string) {
	// Voucher type
	if base, ok := b.vtypes[v.Type]; ok {
		v.Base = base
	} else {
		for n, base := range b.vtypes {
			if strings.EqualFold(n, v.Type) {
				v.Type, v.Base = n, base
				break
			}
		}
		if v.Base == "" {
			errs = append(errs, fmt.Sprintf("voucher type %q is not in Tally", v.Type))
		}
	}
	// Ledgers
	fix := func(name string) (string, bool) {
		if _, ok := b.ledgers[name]; ok {
			return name, true
		}
		if n, ok := b.lower[strings.ToLower(name)]; ok {
			return n, true
		}
		return name, false
	}
	for i := range v.Lines {
		n, ok := fix(v.Lines[i].Ledger)
		v.Lines[i].Ledger = n
		if !ok {
			errs = append(errs, fmt.Sprintf("ledger %q not found%s", n, b.suggest(n)))
		}
	}
	sj := v.Base == "Stock Journal"
	if sj {
		errs = append(errs, b.checkStockJournal(v)...)
	}
	for i := range v.Items {
		if !sj {
			if v.Items[i].Out || v.Items[i].Ledger == "" {
				errs = append(errs, fmt.Sprintf("item %d: give the sales/purchase ledger and qty > 0", i+1))
			} else if n, ok := fix(v.Items[i].Ledger); !ok {
				errs = append(errs, fmt.Sprintf("ledger %q not found%s", n, b.suggest(n)))
			} else {
				v.Items[i].Ledger = n
			}
		}
		if u, ok := b.items[v.Items[i].Item]; !ok {
			errs = append(errs, fmt.Sprintf("stock item %q not found", v.Items[i].Item))
		} else if v.Items[i].Unit == "" {
			v.Items[i].Unit = u
		}
		// Sign of the item line follows the voucher: sales/credit note Cr, purchase/debit note Dr.
		switch {
		case sj && v.Items[i].Out:
			v.Items[i].Amount = abs(v.Items[i].Amount)
		case sj:
			v.Items[i].Amount = -abs(v.Items[i].Amount)
		case v.Base == "Purchase" || v.Base == "Credit Note":
			v.Items[i].Amount = -abs(v.Items[i].Amount)
		default:
			v.Items[i].Amount = abs(v.Items[i].Amount)
		}
	}
	if v.Party != "" {
		n, ok := fix(v.Party)
		v.Party = n
		if !ok {
			errs = append(errs, fmt.Sprintf("party %q not found%s", n, b.suggest(n)))
		}
	} else {
		v.Party = b.guessParty(v)
	}
	// Balance
	var sum int64
	for _, l := range v.Lines {
		sum += l.Amount
	}
	for _, it := range v.Items {
		sum += it.Amount
	}
	if sum != 0 && !sj {
		errs = append(errs, fmt.Sprintf("debits and credits differ by %s", rs(abs(sum))))
	}
	// Date
	if b.from != "" && v.Date < b.from {
		errs = append(errs, fmt.Sprintf("date %s is before the books begin (%s)", v.Date, b.from))
	}
	if t, _ := time.Parse(time.DateOnly, v.Date); t.After(time.Now().AddDate(0, 0, 1)) {
		warns = append(warns, "date is in the future")
	}
	// Payment/receipt/contra need a cash or bank line
	if v.Base == "Payment" || v.Base == "Receipt" || v.Base == "Contra" {
		has := false
		for _, l := range v.Lines {
			if x := b.ledgers[l.Ledger]; x.cash || x.bank {
				has = true
			}
		}
		if !has {
			errs = append(errs, v.Base+" needs a cash or bank ledger")
		}
	}
	if len(errs) > 0 {
		return
	}
	if w := b.duplicate(v); w != "" {
		if v.Override {
			warns = append(warns, w+" (posting anyway: override given)")
		} else {
			errs = append(errs, w+"; send override_duplicate: true only if it is really a new entry")
		}
	}
	warns = append(warns, b.gstWarnings(v)...)
	warns = append(warns, b.taxWarnings(v)...)
	return
}

func (b *Book) suggest(name string) string {
	low := strings.ToLower(name)
	var hits []string
	for _, x := range b.ledgers {
		n := strings.ToLower(x.name)
		if strings.Contains(n, low) || strings.Contains(low, n) || sharedWord(n, low) {
			hits = append(hits, x.name)
		}
	}
	sort.Strings(hits)
	if len(hits) == 0 {
		return ""
	}
	if len(hits) > 5 {
		hits = hits[:5]
	}
	return " (did you mean: " + strings.Join(hits, ", ") + "?)"
}

func sharedWord(a, b string) bool {
	for _, w := range strings.Fields(a) {
		if len(w) >= 4 && strings.Contains(b, w) {
			return true
		}
	}
	return false
}

// guessParty picks the customer/supplier ledger for sales/purchase/notes when none is given.
func (b *Book) guessParty(v *Voucher) string {
	switch v.Base {
	case "Sales", "Purchase", "Credit Note", "Debit Note":
	default:
		return ""
	}
	for _, l := range v.Lines {
		if b.ledgers[l.Ledger].party {
			return l.Ledger
		}
	}
	return ""
}

// duplicate: same party + same reference (invoice no.) in the same FY, in books or drafts.
func (b *Book) duplicate(v *Voucher) string {
	if v.Party == "" || v.Reference == "" {
		return ""
	}
	fyFrom, fyTo := fy(v.Date)
	var n int
	b.db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE cancelled=0 AND party=? AND lower(trim(reference))=lower(trim(?)) AND date BETWEEN ? AND ?`,
		v.Party, v.Reference, fyFrom, fyTo).Scan(&n)
	if n > 0 {
		return fmt.Sprintf("duplicate: %s already has invoice/reference %q in FY %s in Tally", v.Party, v.Reference, fyFrom[:4])
	}
	rows, err := b.db.Query(`SELECT id, body FROM drafts WHERE status='draft' AND kind='voucher'`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var body string
		rows.Scan(&id, &body)
		var d Voucher
		if json.Unmarshal([]byte(body), &d) == nil && id != v.ID && d.Party == v.Party &&
			strings.EqualFold(strings.TrimSpace(d.Reference), strings.TrimSpace(v.Reference)) && d.Date >= fyFrom && d.Date <= fyTo {
			return fmt.Sprintf("duplicate: draft %d has the same party and reference %q", id, v.Reference)
		}
	}
	return ""
}

func fy(date string) (string, string) {
	y, _ := strconv.Atoi(date[:4])
	if date[5:7] < "04" {
		y--
	}
	return fmt.Sprintf("%d-04-01", y), fmt.Sprintf("%d-03-31", y+1)
}

// gstWarnings: tax type vs party state, and tax amount vs taxable × rate.
func (b *Book) gstWarnings(v *Voucher) []string {
	var igst, local, taxable int64
	var rates = map[string]bool{}
	for _, l := range v.Lines {
		x := b.ledgers[l.Ledger]
		t := x.tax
		switch {
		case strings.HasPrefix(t, "igst"):
			igst += abs(l.Amount)
		case strings.HasPrefix(t, "cgst"), strings.HasPrefix(t, "sgst"):
			local += abs(l.Amount)
		}
	}
	if igst == 0 && local == 0 {
		return nil
	}
	var w []string
	if b.companyState == "" {
		w = append(w, "company state not set in Tally: IGST vs CGST+SGST not checked")
	}
	if v.Party != "" && b.companyState != "" {
		p := b.ledgers[v.Party]
		st := p.state
		if p.gstin != "" && gst.Problem(p.gstin) == "" {
			st = gst.StateOf(p.gstin)
		}
		switch {
		case p.foreign:
		case st == "":
			w = append(w, "party state not known in Tally: check IGST vs CGST+SGST")
		case gst.SameState(st, b.companyState) && igst > 0:
			w = append(w, fmt.Sprintf("IGST charged but the party is in %s, same state as the company", st))
		case !gst.SameState(st, b.companyState) && local > 0:
			w = append(w, fmt.Sprintf("CGST+SGST charged but the party is in %s, company in %s", st, b.companyState))
		}
	}
	// Tax vs taxable: taxable = sales/purchase/income/expense lines (not party, tax, cash, bank).
	for _, l := range v.Lines {
		x := b.ledgers[l.Ledger]
		if x.party || x.cash || x.bank || x.tax != "" || strings.EqualFold(x.parent, "Duties & Taxes") {
			continue
		}
		taxable += abs(l.Amount)
	}
	for _, it := range v.Items {
		taxable += abs(it.Amount)
	}
	tax := igst + local
	if taxable > 0 {
		for _, r := range []int64{5, 12, 18, 28, 3, 1} { // common GST rates
			if abs(taxable*r-tax*100) <= 100*100 { // within 1 rupee
				rates[fmt.Sprint(r)] = true
			}
		}
		if len(rates) == 0 {
			w = append(w, fmt.Sprintf("GST %s on taxable %s is %.2f%%: not a usual rate, check it", rs(tax), rs(taxable), float64(tax)*100/float64(taxable)))
		}
	}
	if local > 0 {
		var c, s int64
		for _, l := range v.Lines {
			t := b.ledgers[l.Ledger].tax
			if strings.HasPrefix(t, "cgst") {
				c += abs(l.Amount)
			} else if strings.HasPrefix(t, "sgst") {
				s += abs(l.Amount)
			}
		}
		if c != s {
			w = append(w, fmt.Sprintf("CGST %s and SGST %s differ", rs(c), rs(s)))
		}
	}
	return w
}

// taxWarnings: TDS tag with no TDS line; ITC on a 17(5) ledger.
func (b *Book) taxWarnings(v *Voucher) []string {
	var w []string
	hasTDS, blocked := false, false
	var tdsLedger string
	for _, l := range v.Lines {
		x := b.ledgers[l.Ledger]
		if x.tax == "tds payable" {
			hasTDS = true
		}
		if x.tds != "" && x.tds != "none" && l.Amount < 0 && tdsLedger == "" {
			tdsLedger = x.name + " (" + x.tds + ")"
		}
		if strings.Contains(x.tax, "17(5)") || x.flag == "blocked credit" {
			blocked = true
		}
	}
	if tdsLedger != "" && !hasTDS && (v.Base == "Journal" || v.Base == "Purchase" || v.Base == "Payment") {
		w = append(w, "no TDS line for "+tdsLedger+": check the threshold")
	}
	if blocked {
		w = append(w, "a ledger is tagged blocked credit 17(5): check the ITC")
	}
	return w
}

func rs(p int64) string {
	s := tallyAmt(abs(p))
	whole, frac := s[:len(s)-3], s[len(s)-3:]
	if len(whole) > 3 {
		head, tail := whole[:len(whole)-3], whole[len(whole)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		whole = strings.Join(parts, ",") + "," + tail
	}
	return whole + frac
}

// Preview is the short text shown before posting.
func (v *Voucher) Preview() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Draft %d: %s, %s", v.ID, v.Type, v.Date)
	if v.Party != "" {
		fmt.Fprintf(&b, ", party %s", v.Party)
	}
	if v.Reference != "" {
		fmt.Fprintf(&b, ", ref %s", v.Reference)
	}
	b.WriteString("\n")
	for _, it := range v.Items {
		to := it.Ledger
		if v.Base == "Stock Journal" {
			to = "in"
			if it.Out {
				to = "out"
			}
		}
		fmt.Fprintf(&b, "  item %s: %s %s × %s = %s (%s)\n", it.Item, it.Qty, it.Unit, rs(it.Rate), rs(abs(it.Amount)), to)
	}
	var dr int64
	for _, l := range v.Lines {
		if l.Amount < 0 {
			fmt.Fprintf(&b, "  Dr %s %s\n", l.Ledger, rs(l.Amount))
			dr -= l.Amount
		} else {
			fmt.Fprintf(&b, "  Cr %s %s\n", l.Ledger, rs(l.Amount))
		}
	}
	for _, it := range v.Items {
		if it.Amount < 0 {
			dr -= it.Amount
		}
	}
	fmt.Fprintf(&b, "  Total %s. Narration: %s", rs(dr), v.Narration())
	return b.String()
}

// Total is the voucher's debit total.
func (v *Voucher) Total() int64 {
	var dr int64
	for _, l := range v.Lines {
		if l.Amount < 0 {
			dr -= l.Amount
		}
	}
	for _, it := range v.Items {
		if it.Amount < 0 {
			dr -= it.Amount
		}
	}
	return dr
}

// checkStockJournal: items only, with at least one going out and one coming in. Values may
// differ (Tally values the items itself), so they are not required to balance.
func (b *Book) checkStockJournal(v *Voucher) []string {
	var errs []string
	if len(v.Lines) > 0 {
		errs = append(errs, "a stock journal has stock items only (no ledger lines)")
	}
	in, out := 0, 0
	for _, it := range v.Items {
		if it.Out {
			out++
		} else {
			in++
		}
	}
	if in == 0 || out == 0 {
		errs = append(errs, "a stock journal needs items going out (qty < 0) and coming in (qty > 0)")
	}
	return errs
}
