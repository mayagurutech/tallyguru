package store

// Precomputed summaries so common answers are instant. Rebuilt after every sync.
//
// GST figures here are a first approximation: ledgers under Duties & Taxes whose name
// contains "GST". Phase 2 ledger mapping replaces this with confirmed tags.
const summarySchema = `
CREATE TABLE IF NOT EXISTS month_ledger (month TEXT NOT NULL, ledger TEXT NOT NULL, dr INTEGER NOT NULL, cr INTEGER NOT NULL, n INTEGER NOT NULL, PRIMARY KEY(month, ledger));
CREATE TABLE IF NOT EXISTS month_summary (month TEXT NOT NULL, metric TEXT NOT NULL, amount INTEGER NOT NULL, n INTEGER NOT NULL, PRIMARY KEY(month, metric));
`

// Metrics in display order with labels.
var Metrics = []struct{ ID, Label string }{
	{"sales", "Sales"},
	{"purchases", "Purchases"},
	{"direct_exp", "Direct expenses"},
	{"indirect_exp", "Indirect expenses"},
	{"other_income", "Other income"},
	{"gst_cr", "GST ledgers Cr (mostly output tax)"},
	{"gst_dr", "GST ledgers Dr (mostly input tax)"},
	{"receipts", "Receipts"},
	{"payments", "Payments"},
}

// RebuildSummaries recomputes month_ledger and month_summary in one transaction.
func (s *DB) RebuildSummaries() error {
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM month_ledger`,
		`INSERT INTO month_ledger
		 SELECT substr(v.date,1,7), vl.ledger,
		        SUM(CASE WHEN vl.amount<0 THEN -vl.amount ELSE 0 END),
		        SUM(CASE WHEN vl.amount>0 THEN vl.amount ELSE 0 END), COUNT(*)
		 FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
		 GROUP BY 1, 2`,
		`DELETE FROM month_summary`,
		// Group-based metrics (via each ledger's primary group).
		`INSERT INTO month_summary
		 SELECT ml.month,
		   CASE gp.primary_group
		     WHEN 'Sales Accounts' THEN 'sales' WHEN 'Purchase Accounts' THEN 'purchases'
		     WHEN 'Direct Expenses' THEN 'direct_exp' WHEN 'Indirect Expenses' THEN 'indirect_exp'
		     ELSE 'other_income' END,
		   SUM(CASE WHEN gp.primary_group IN ('Sales Accounts','Direct Incomes','Indirect Incomes')
		            THEN ml.cr - ml.dr ELSE ml.dr - ml.cr END),
		   SUM(ml.n)
		 FROM month_ledger ml JOIN ledgers l ON l.name = ml.ledger
		 JOIN group_primary gp ON gp.name = l.parent
		 WHERE gp.primary_group IN ('Sales Accounts','Purchase Accounts','Direct Expenses','Indirect Expenses','Direct Incomes','Indirect Incomes')
		 GROUP BY 1, 2`,
		`INSERT INTO month_summary
		 SELECT ml.month, 'gst_cr', SUM(ml.cr), SUM(ml.n) FROM month_ledger ml JOIN ledgers l ON l.name = ml.ledger
		 JOIN group_primary gp ON gp.name = l.parent
		 WHERE l.parent IN (SELECT name FROM group_anc WHERE ancestor = 'Duties & Taxes') AND upper(l.name) LIKE '%GST%' GROUP BY 1`,
		`INSERT INTO month_summary
		 SELECT ml.month, 'gst_dr', SUM(ml.dr), SUM(ml.n) FROM month_ledger ml JOIN ledgers l ON l.name = ml.ledger
		 JOIN group_primary gp ON gp.name = l.parent
		 WHERE l.parent IN (SELECT name FROM group_anc WHERE ancestor = 'Duties & Taxes') AND upper(l.name) LIKE '%GST%' GROUP BY 1`,
		// Voucher-type metrics: total = sum of credit lines (= sum of debit lines).
		`INSERT INTO month_summary
		 SELECT substr(v.date,1,7), lower(vb.base) || 's', SUM(t.c), COUNT(*)
		 FROM acct_vouchers v JOIN vtype_base vb ON vb.name = v.type
		 JOIN (SELECT voucher_guid, SUM(CASE WHEN amount>0 THEN amount ELSE 0 END) c FROM vlines GROUP BY 1) t ON t.voucher_guid = v.guid
		 WHERE v.cancelled = 0 AND vb.base IN ('Receipt','Payment')
		 GROUP BY 1, 2`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
