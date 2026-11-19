package store

import (
	"strings"

	"github.com/mayagurutech/tallyguru/internal/mapping"
)

// Evidence reads what the books show for every ledger, so mapping can decide from facts
// and only ask where the books leave real doubt. Runs on all vouchers, locally.
func (s *DB) Evidence() (map[string]*mapping.Evidence, error) {
	ev := map[string]*mapping.Evidence{}
	get := func(n string) *mapping.Evidence {
		k := strings.ToLower(n)
		if ev[k] == nil {
			ev[k] = &mapping.Evidence{}
		}
		return ev[k]
	}
	// Ledger classes used below.
	const cls = `CREATE TEMP TABLE IF NOT EXISTS lcls (name TEXT PRIMARY KEY, c TEXT);
	DELETE FROM lcls;
	INSERT OR IGNORE INTO lcls SELECT l.name,
	  CASE WHEN EXISTS(SELECT 1 FROM group_anc a WHERE a.name=l.parent AND a.ancestor='Duties & Taxes') THEN
	         CASE WHEN lower(l.name) LIKE '%tds%' THEN 'tds' WHEN lower(l.name) LIKE '%gst%' OR lower(l.name) LIKE '%cess%' THEN 'gst' ELSE 'duty' END
	       WHEN lower(l.name) LIKE '%tds%' AND lower(l.name) LIKE '%pay%' THEN 'tds'
	       WHEN EXISTS(SELECT 1 FROM group_anc a WHERE a.name=l.parent AND a.ancestor='Sales Accounts') THEN 'sales'
	       WHEN EXISTS(SELECT 1 FROM group_anc a WHERE a.name=l.parent AND a.ancestor IN ('Purchase Accounts','Direct Expenses','Indirect Expenses','Fixed Assets')) THEN 'cost'
	       WHEN EXISTS(SELECT 1 FROM group_anc a WHERE a.name=l.parent AND a.ancestor IN ('Sundry Creditors','Sundry Debtors')) THEN 'party'
	       ELSE '' END
	  FROM ledgers l;`
	if _, err := s.Exec(cls); err != nil {
		return nil, err
	}
	// Per voucher: does it carry TDS, GST, a sales line, a cost line.
	if _, err := s.Exec(`CREATE TEMP TABLE IF NOT EXISTS vflag AS SELECT '' g, 0 tds, 0 gst, 0 sal, 0 cost WHERE 0;
	DELETE FROM vflag;
	INSERT INTO vflag SELECT vl.voucher_guid, MAX(c.c='tds'), MAX(c.c='gst'), MAX(c.c='sales'), MAX(c.c='cost')
	  FROM vlines vl JOIN acct_vouchers v ON v.guid=vl.voucher_guid LEFT JOIN lcls c ON c.name=vl.ledger GROUP BY vl.voucher_guid;
	CREATE INDEX IF NOT EXISTS temp.ix_vflag ON vflag(g);`); err != nil {
		return nil, err
	}
	// Parties that ever had TDS deducted (TDS line in a voucher with that party's line).
	if _, err := s.Exec(`CREATE TEMP TABLE IF NOT EXISTS tdsparty AS SELECT '' name WHERE 0; DELETE FROM tdsparty;
	INSERT INTO tdsparty SELECT DISTINCT vl.ledger FROM vlines vl JOIN vflag f ON f.g=vl.voucher_guid AND f.tds=1
	  JOIN lcls c ON c.name=vl.ledger AND c.c='party';`); err != nil {
		return nil, err
	}
	// Cost ledgers: yearly debit (largest FY), share with GST, share with TDS (same voucher
	// or the same party had TDS deducted anywhere).
	rows, err := s.Query(`WITH x AS (
	  SELECT vl.ledger, vl.voucher_guid g, -vl.amount amt, substr(date(v.date,'-3 months'),1,4) fy, f.gst, f.tds,
	    EXISTS(SELECT 1 FROM vlines p JOIN tdsparty t ON t.name=p.ledger WHERE p.voucher_guid=vl.voucher_guid) ptds
	  FROM vlines vl JOIN acct_vouchers v ON v.guid=vl.voucher_guid JOIN vflag f ON f.g=vl.voucher_guid
	  JOIN lcls c ON c.name=vl.ledger AND c.c='cost' WHERE vl.amount < 0)
	SELECT ledger, COUNT(*), SUM(amt), SUM(CASE WHEN gst=1 THEN amt ELSE 0 END),
	  SUM(CASE WHEN tds=1 OR ptds=1 THEN amt ELSE 0 END),
	  (SELECT MAX(t) FROM (SELECT SUM(amt) t FROM x x2 WHERE x2.ledger=x.ledger GROUP BY fy))
	FROM x GROUP BY ledger`)
	if err != nil {
		return nil, err
	}
	type r struct {
		n                  string
		cnt                int
		tot, gst, tds, mfy int64
	}
	var rs []r
	for rows.Next() {
		var x r
		rows.Scan(&x.n, &x.cnt, &x.tot, &x.gst, &x.tds, &x.mfy)
		rs = append(rs, x)
	}
	rows.Close()
	for _, x := range rs {
		e := get(x.n)
		e.Entries, e.Total, e.MaxYear = x.cnt, x.tot, x.mfy
		if x.tot > 0 {
			e.GSTShare, e.TDSShare = float64(x.gst)/float64(x.tot), float64(x.tds)/float64(x.tot)
		}
	}
	// Tax ledgers: used next to sales lines or cost lines?
	rows, err = s.Query(`SELECT vl.ledger, SUM(f.sal=1 AND f.cost=0), SUM(f.cost=1 AND f.sal=0), COUNT(*)
	  FROM vlines vl JOIN vflag f ON f.g=vl.voucher_guid JOIN lcls c ON c.name=vl.ledger AND c.c IN ('gst','tds','duty')
	  GROUP BY vl.ledger`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		var sal, cost, cnt int
		rows.Scan(&n, &sal, &cost, &cnt)
		e := get(n)
		e.WithSales, e.WithCost, e.Entries = sal, cost, cnt
	}
	rows.Close()
	// Every other ledger: just the entry count.
	rows, err = s.Query(`SELECT ledger, COUNT(*) FROM vlines GROUP BY ledger`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		var c int
		rows.Scan(&n, &c)
		if e := get(n); e.Entries == 0 {
			e.Entries = c
		}
	}
	rows.Close()
	return ev, nil
}
