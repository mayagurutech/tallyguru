package report

import (
	"database/sql"
)

// GSTMonth is one month's GST from the books (natural side positive).
type GSTMonth struct {
	Month              string
	Output, Input, RCM int64 // booked in sales/purchase/expense/asset vouchers
	Settled            int64 // set-off journals and tax payments (net Dr to GST ledgers)
}

// Net is output tax plus RCM payable less input credit for the month.
func (m GSTMonth) Net() int64 { return m.Output + m.RCM - m.Input }

// GSTSummary reads GST by month for period p. The tax tag only says a ledger is a GST
// ledger; output or input is decided per voucher, because books often use one ledger for
// both (seen on a pilot). A voucher touching an income ledger is a sale (output; credit
// notes reduce it); one touching an expense, purchase or fixed-asset ledger is a purchase
// (input; debit notes reduce it). Other vouchers on GST ledgers (set-off journals, challans,
// transfers) are settlements. tagged is the number of GST-tagged ledgers (0: no mapping).
func GSTSummary(db *sql.DB, p Period) (ms []GSTMonth, tagged int, err error) {
	db.QueryRow(`SELECT COUNT(*) FROM ledger_map WHERE family='tax' AND (tag LIKE '%gst%' OR tag LIKE 'cess%')`).Scan(&tagged)
	rows, err := db.Query(`
		WITH tl AS (SELECT l.name, m.tag FROM ledgers l JOIN ledger_map m ON m.guid=l.guid AND m.family='tax'
		            WHERE m.tag LIKE '%gst%' OR m.tag LIKE 'cess%'),
		inc AS (SELECT name FROM group_anc WHERE ancestor IN ('Sales Accounts','Direct Incomes','Indirect Incomes')),
		cost AS (SELECT g.name FROM groups g WHERE g.is_revenue=1 AND g.name NOT IN (SELECT name FROM inc)
		         UNION SELECT name FROM group_anc WHERE ancestor='Fixed Assets'),
		-- Each ledger classified once: 2 income, 1 cost/asset, 0 other.
		lk AS (SELECT l.name, CASE WHEN l.parent IN (SELECT name FROM inc) THEN 2
		                           WHEN l.parent IN (SELECT name FROM cost) THEN 1 ELSE 0 END k FROM ledgers l),
		-- Only vouchers in the period that touch a GST ledger.
		gv AS (SELECT DISTINCT vl.voucher_guid g FROM vlines vl JOIN tl ON tl.name=vl.ledger
		       JOIN vouchers v ON v.guid=vl.voucher_guid WHERE substr(v.date,1,7) BETWEEN ?1 AND ?2),
		side AS (SELECT vl.voucher_guid g, MAX(lk.k) s FROM vlines vl JOIN lk ON lk.name=vl.ledger
		         WHERE vl.voucher_guid IN (SELECT g FROM gv) GROUP BY vl.voucher_guid)
		SELECT substr(v.date,1,7), tl.tag, side.s, SUM(vl.amount)
		FROM vlines vl JOIN acct_vouchers v ON v.guid=vl.voucher_guid JOIN tl ON tl.name=vl.ledger
		JOIN side ON side.g=vl.voucher_guid
		WHERE v.cancelled=0 AND substr(v.date,1,7) BETWEEN ?1 AND ?2
		GROUP BY 1, 2, 3 ORDER BY 1`, p.From, p.To)
	if err != nil {
		return nil, tagged, err
	}
	defer rows.Close()
	idx := map[string]int{}
	for rows.Next() {
		var m, tag string
		var s int     // 2 sale, 1 purchase/expense, 0 settlement
		var amt int64 // Tally sign: Cr positive
		if err := rows.Scan(&m, &tag, &s, &amt); err != nil {
			return nil, tagged, err
		}
		i, ok := idx[m]
		if !ok {
			i = len(ms)
			idx[m] = i
			ms = append(ms, GSTMonth{Month: m})
		}
		x := &ms[i]
		switch {
		case s == 0:
			x.Settled += -amt
		case tag == "gst rcm payable":
			x.RCM += amt
		case s == 2:
			x.Output += amt
		default:
			x.Input += -amt
		}
	}
	return ms, tagged, rows.Err()
}

// TDSRow is one TDS payable ledger: deducted (Cr) and deposited (Dr) in the period.
type TDSRow struct {
	Ledger              string
	Deducted, Deposited int64
	Closing             int64 // payable at the period end (Cr positive)
}

// TDSMonth is the month-wise total over all TDS payable ledgers.
type TDSMonth struct {
	Month               string
	Deducted, Deposited int64
}

// TDSSummary reads TDS payable ledgers (tax tag "tds payable") for period p.
func TDSSummary(db *sql.DB, p Period) (ls []TDSRow, ms []TDSMonth, err error) {
	rows, err := db.Query(`SELECT l.name,
		COALESCE(SUM(CASE WHEN ml.month BETWEEN ?1 AND ?2 THEN ml.cr END),0),
		COALESCE(SUM(CASE WHEN ml.month BETWEEN ?1 AND ?2 THEN ml.dr END),0),
		l.opening + COALESCE(SUM(CASE WHEN ml.month <= ?2 THEN ml.cr-ml.dr END),0)
		FROM ledgers l JOIN ledger_map m ON m.guid=l.guid AND m.family='tax' AND m.tag='tds payable'
		LEFT JOIN month_ledger ml ON ml.ledger=l.name
		GROUP BY l.name ORDER BY 2 DESC`, p.From, p.To)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var r TDSRow
		if err := rows.Scan(&r.Ledger, &r.Deducted, &r.Deposited, &r.Closing); err != nil {
			rows.Close()
			return nil, nil, err
		}
		ls = append(ls, r)
	}
	rows.Close()
	rows, err = db.Query(`SELECT ml.month, SUM(ml.cr), SUM(ml.dr) FROM month_ledger ml JOIN ledgers l ON l.name=ml.ledger
		JOIN ledger_map m ON m.guid=l.guid AND m.family='tax' AND m.tag='tds payable'
		WHERE ml.month BETWEEN ? AND ? GROUP BY 1 ORDER BY 1`, p.From, p.To)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x TDSMonth
		rows.Scan(&x.Month, &x.Deducted, &x.Deposited)
		ms = append(ms, x)
	}
	return ls, ms, rows.Err()
}
