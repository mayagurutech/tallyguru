package report

import (
	"database/sql"
	"sort"
)

// TypeTotal is one voucher type's count and value in the Day Book.
type TypeTotal struct {
	Type  string
	N     int
	Value int64 // sum of credit lines (= sum of debit lines)
}

// Voucher is one Day Book row. Against is the party, or the largest other ledger.
type Voucher struct {
	GUID, Date, Type, Number, Against, AgainstParent, Narration string
	Value                                                       int64
	Cancelled                                                   bool
}

// DayBookFilter limits the Day Book to a voucher type (base type or own name).
type DayBookFilter struct {
	P    Period
	Type string
}

const dayBookSQL = `SELECT v.guid, v.date, v.type, v.number, v.narration, v.cancelled,
	COALESCE((SELECT SUM(amount) FROM vlines WHERE voucher_guid=v.guid AND amount>0),0),
	COALESCE(NULLIF(v.party,''), (SELECT ledger FROM vlines WHERE voucher_guid=v.guid ORDER BY ABS(amount) DESC LIMIT 1), ''),
	COALESCE((SELECT parent FROM ledgers WHERE name = COALESCE(NULLIF(v.party,''),
		(SELECT ledger FROM vlines WHERE voucher_guid=v.guid ORDER BY ABS(amount) DESC LIMIT 1))), '')
	FROM acct_vouchers v LEFT JOIN vtype_base vb ON vb.name=v.type
	WHERE substr(v.date,1,7) BETWEEN ?1 AND ?2 AND (?3 = '' OR v.type = ?3 COLLATE NOCASE OR vb.base = ?3 COLLATE NOCASE)
	ORDER BY v.date, v.alter_id`

// EachVoucher streams the Day Book in date order (for Excel: never all in memory).
func EachVoucher(db *sql.DB, f DayBookFilter, fn func(Voucher) error) error {
	rows, err := db.Query(dayBookSQL, f.P.From, f.P.To, f.Type)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v Voucher
		if err := rows.Scan(&v.GUID, &v.Date, &v.Type, &v.Number, &v.Narration, &v.Cancelled, &v.Value, &v.Against, &v.AgainstParent); err != nil {
			return err
		}
		if err := fn(v); err != nil {
			return err
		}
	}
	return rows.Err()
}

// DayBook summarises the period: totals by voucher type and the largest vouchers.
func DayBook(db *sql.DB, f DayBookFilter, top int) (types []TypeTotal, largest []Voucher, n int, err error) {
	byType := map[string]*TypeTotal{}
	err = EachVoucher(db, f, func(v Voucher) error {
		n++
		t := byType[v.Type]
		if t == nil {
			t = &TypeTotal{Type: v.Type}
			byType[v.Type] = t
		}
		t.N++
		t.Value += v.Value
		// Keep only the largest `top` (small slice, no full list in memory).
		if len(largest) < top || v.Value > largest[len(largest)-1].Value {
			largest = append(largest, v)
			sort.SliceStable(largest, func(i, j int) bool { return largest[i].Value > largest[j].Value })
			if len(largest) > top {
				largest = largest[:top]
			}
		}
		return nil
	})
	for _, t := range byType {
		types = append(types, *t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Value > types[j].Value })
	return types, largest, n, err
}

// VLine is one ledger line of a voucher, for the Excel "Lines" sheet.
type VLine struct {
	Date, Type, Number, Ledger, Parent string
	Amount                             int64 // Tally sign
}

// EachLine streams every ledger line of the Day Book's vouchers.
func EachLine(db *sql.DB, f DayBookFilter, fn func(VLine) error) error {
	rows, err := db.Query(`SELECT v.date, v.type, v.number, vl.ledger, COALESCE(l.parent,''), vl.amount
		FROM acct_vouchers v JOIN vlines vl ON vl.voucher_guid=v.guid LEFT JOIN ledgers l ON l.name=vl.ledger
		LEFT JOIN vtype_base vb ON vb.name=v.type
		WHERE substr(v.date,1,7) BETWEEN ?1 AND ?2 AND (?3 = '' OR v.type = ?3 COLLATE NOCASE OR vb.base = ?3 COLLATE NOCASE)
		ORDER BY v.date, v.alter_id, vl.seq`, f.P.From, f.P.To, f.Type)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x VLine
		if err := rows.Scan(&x.Date, &x.Type, &x.Number, &x.Ledger, &x.Parent, &x.Amount); err != nil {
			return err
		}
		if err := fn(x); err != nil {
			return err
		}
	}
	return rows.Err()
}
