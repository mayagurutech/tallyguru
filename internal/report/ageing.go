package report

import (
	"database/sql"
	"sort"
	"time"
)

// Buckets are the ageing columns, in days.
var Buckets = []struct {
	Label string
	Max   int
}{{"0-30", 30}, {"31-60", 60}, {"61-90", 90}, {"91-180", 180}, {"over 180", 1 << 30}}

// MSMEDays is the payment limit for MSME suppliers (43B(h); flagged, judged in audit).
const MSMEDays = 45

// PartyAge is one party's balance split by age.
type PartyAge struct {
	Name, Parent string
	Total        int64   // natural side positive (receivable for debtors, payable for creditors)
	By           []int64 // per bucket
	BillWise     bool    // from Tally's open bills; else FIFO on ledger entries
	MSME         bool
	OverMSME     int64 // MSME creditor amount older than 45 days
	Unadjusted   int64 // open bills in Tally beyond the balance (entries not set against bills)
}

// Ageing is the result for debtors or creditors.
type Ageing struct {
	AsOf         time.Time
	Parties      []PartyAge // with a balance on the natural side, largest first
	By           []int64
	Total        int64
	Advances     int64 // opposite-side balances (advances), not aged
	NAdv         int
	NBill, NFIFO int
	MSMEOver     int64
	NMSMEOver    int
	Unadjusted   int64 // open bills beyond balances: entries not set against bills in Tally
	NUnadjusted  int
}

func bucket(days int) int {
	for i, b := range Buckets {
		if days <= b.Max {
			return i
		}
	}
	return len(Buckets) - 1
}

// AgeParties ages every party under group (Sundry Debtors or Sundry Creditors, with
// sub-groups) as on asOf. sign is -1 for debtors (Dr natural), +1 for creditors.
// Parties Tally tracks bill-wise use Tally's open bills; the rest use FIFO: the balance is
// made of the latest entries on its side, older ones are taken as settled.
func AgeParties(db *sql.DB, group string, sign int64, asOf time.Time) (*Ageing, error) {
	r := &Ageing{AsOf: asOf, By: make([]int64, len(Buckets))}
	booksFrom := ""
	db.QueryRow(`SELECT value FROM meta WHERE key='books_from'`).Scan(&booksFrom)

	type party struct {
		name, parent string
		bal          int64 // Tally sign, computed closing
		msme         bool
	}
	var ps []party
	// As on a past date (e.g. the year end for the MSME check): balances from entries up to
	// that date, and Tally's open bills (which are as of now) are not used; FIFO for all.
	var last string
	db.QueryRow(`SELECT COALESCE(MAX(date),'') FROM vouchers`).Scan(&last)
	asOfDay := asOf.Format(time.DateOnly)
	current := asOfDay >= last
	// Current: balances from month_ledger in one pass. Past: from lines up to the date.
	mv := `SELECT ledger, SUM(cr-dr) t FROM month_ledger WHERE ledger IN (SELECT name FROM pl) GROUP BY ledger`
	if !current {
		mv = `SELECT vl.ledger, SUM(vl.amount) t FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
		      WHERE vl.ledger IN (SELECT name FROM pl) AND v.date <= ?2 GROUP BY vl.ledger`
	}
	rows, err := db.Query(`WITH pl AS (SELECT name, guid, parent, opening FROM ledgers
		  WHERE parent = ?1 OR parent IN (SELECT name FROM group_anc WHERE ancestor = ?1)),
		mv AS (`+mv+`)
		SELECT pl.name, pl.parent, pl.opening + COALESCE(mv.t, 0),
		EXISTS(SELECT 1 FROM ledger_map m WHERE m.guid = pl.guid AND m.family = 'flag' AND m.tag = 'creditor msme')
		FROM pl LEFT JOIN mv ON mv.ledger = pl.name`, group, asOfDay)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p party
		if err := rows.Scan(&p.name, &p.parent, &p.bal, &p.msme); err != nil {
			rows.Close()
			return nil, err
		}
		ps = append(ps, p)
	}
	rows.Close()

	bills := map[string][][2]any{} // party -> (date, amount natural side)
	if current {
		rows, err = db.Query(`SELECT party, bill_date, closing * ? FROM bills`, sign)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var p, d string
			var a int64
			rows.Scan(&p, &d, &a)
			bills[p] = append(bills[p], [2]any{d, a})
		}
		rows.Close()
	}

	days := func(d string) int {
		t, err := time.Parse(time.DateOnly, d)
		if err != nil {
			return 1 << 29 // unknown date: oldest bucket
		}
		return int(asOf.Sub(t).Hours() / 24)
	}

	for _, p := range ps {
		nat := p.bal * sign // natural side positive
		if nat == 0 {
			continue
		}
		if nat < 0 {
			r.Advances += -nat
			r.NAdv++
			continue
		}
		pa := PartyAge{Name: p.name, Parent: p.parent, Total: nat, By: make([]int64, len(Buckets)), MSME: p.msme}
		add := func(d string, a int64) {
			n := days(d)
			pa.By[bucket(n)] += a
			if p.msme && n > MSMEDays {
				pa.OverMSME += a
			}
		}
		bs := bills[p.name]
		var inBills int64
		for _, b := range bs {
			if a := b[1].(int64); a > 0 {
				inBills += a
			}
		}
		if inBills > nat {
			// Open bills exceed the balance: receipts/payments were not set against bills in
			// Tally, so the bills' dates are not the dues' age (author confirmed on the pilot:
			// the balance was recent). Age by FIFO on entries instead; report the excess.
			pa.Unadjusted = inBills - nat
			r.Unadjusted += pa.Unadjusted
			r.NUnadjusted++
			bs = nil
		}
		if len(bs) > 0 {
			pa.BillWise = true
			r.NBill++
			// The balance is made of the newest open bills.
			sort.Slice(bs, func(i, j int) bool { return bs[i][0].(string) > bs[j][0].(string) })
			left := nat
			for _, b := range bs {
				a := b[1].(int64)
				if a <= 0 {
					continue
				}
				if take := min(a, left); take > 0 {
					add(b[0].(string), take)
					left -= take
				}
			}
			// Balance not covered by open bills (on-account entries): aged by FIFO like any
			// other party, so the party total still equals the ledger.
			if left > 0 {
				if err := fifo(db, p.name, sign, left, booksFrom, asOfDay, add); err != nil {
					return nil, err
				}
			}
		} else {
			r.NFIFO++
			if err := fifo(db, p.name, sign, nat, booksFrom, asOfDay, add); err != nil {
				return nil, err
			}
		}
		r.Parties = append(r.Parties, pa)
	}
	sort.Slice(r.Parties, func(i, j int) bool { return r.Parties[i].Total > r.Parties[j].Total })
	for _, p := range r.Parties {
		r.Total += p.Total
		for i, v := range p.By {
			r.By[i] += v
		}
		if p.OverMSME > 0 {
			r.MSMEOver += p.OverMSME
			r.NMSMEOver++
		}
	}
	return r, nil
}

// fifo spreads amount over the party's latest natural-side entries, newest first; anything
// left over is the opening balance, dated at the start of the books.
func fifo(db *sql.DB, ledger string, sign, amount int64, booksFrom, asOf string, add func(string, int64)) error {
	rows, err := db.Query(`SELECT v.date, vl.amount * ? FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
		WHERE vl.ledger = ? AND vl.amount * ? > 0 AND v.date <= ? ORDER BY v.date DESC, v.alter_id DESC`, sign, ledger, sign, asOf)
	if err != nil {
		return err
	}
	type ent struct {
		d string
		a int64
	}
	var es []ent
	for rows.Next() {
		var e ent
		rows.Scan(&e.d, &e.a)
		es = append(es, e)
	}
	rows.Close()
	for _, e := range es {
		if amount <= 0 {
			return nil
		}
		take := min(e.a, amount)
		add(e.d, take)
		amount -= take
	}
	if amount > 0 {
		add(booksFrom, amount)
	}
	return nil
}
