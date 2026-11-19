package store

import (
	"database/sql"

	"github.com/mayagurutech/tallyguru/internal/tally"
)

const mastersSchema = `
CREATE TABLE IF NOT EXISTS groups (name TEXT PRIMARY KEY, parent TEXT NOT NULL, is_revenue INTEGER NOT NULL, affects_gp INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS group_primary (name TEXT PRIMARY KEY, primary_group TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS group_anc (name TEXT NOT NULL, ancestor TEXT NOT NULL, PRIMARY KEY(name, ancestor));
CREATE TABLE IF NOT EXISTS vtypes (name TEXT PRIMARY KEY, parent TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS vtype_base (name TEXT PRIMARY KEY, base TEXT NOT NULL);
DROP VIEW IF EXISTS acct_vouchers;
-- Vouchers that affect the books. Excludes optional vouchers and voucher types Tally keeps
-- outside the accounts (found on a real pilot: 14 Delivery Notes broke the trial balance).
CREATE VIEW acct_vouchers AS SELECT * FROM vouchers WHERE optional = 0 AND type NOT IN (
  SELECT name FROM vtype_base WHERE base IN ('Delivery Note','Receipt Note','Sales Order',
  'Purchase Order','Rejections In','Rejections Out','Memorandum','Reversing Journal',
  'Physical Stock','Job Work In Order','Job Work Out Order','Material In','Material Out'));
CREATE TABLE IF NOT EXISTS bills (party TEXT NOT NULL, name TEXT NOT NULL, bill_date TEXT NOT NULL, closing INTEGER NOT NULL, advance INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS ix_bills_party ON bills(party);
CREATE TABLE IF NOT EXISTS stock_items (name TEXT PRIMARY KEY, parent TEXT NOT NULL, unit TEXT NOT NULL, closing_qty TEXT NOT NULL, closing_value INTEGER NOT NULL, opening_value INTEGER NOT NULL DEFAULT 0);
`

func replaceAll(db *sql.DB, del string, ins string, n int, row func(i int) []any, after ...string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(del); err != nil {
		return err
	}
	st, err := tx.Prepare(ins)
	if err != nil {
		return err
	}
	defer st.Close()
	for i := 0; i < n; i++ {
		if _, err := st.Exec(row(i)...); err != nil {
			return err
		}
	}
	for _, q := range after {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceGroups stores groups and recomputes each group's primary group.
func (s *DB) ReplaceGroups(gs []tally.Group) error {
	return replaceAll(s.DB, `DELETE FROM groups`, `INSERT OR REPLACE INTO groups VALUES(?,?,?,?)`, len(gs),
		func(i int) []any {
			g := gs[i]
			return []any{g.Name, g.Parent, b2i(g.IsRevenue), b2i(g.AffectsGrossProfit)}
		},
		`DELETE FROM group_primary`,
		`INSERT INTO group_primary
		 WITH RECURSIVE gp(name, primary_group, depth) AS (
		   SELECT name, name, 0 FROM groups WHERE parent = ''
		   UNION ALL
		   SELECT g.name, gp.primary_group, gp.depth+1 FROM groups g JOIN gp ON g.parent = gp.name WHERE gp.depth < 50)
		 SELECT name, primary_group FROM gp`,
		// Every group with each of its ancestors, itself included. Needed because
		// e.g. Duties & Taxes sits under Current Liabilities and Sundry Debtors under
		// Current Assets: "is X under Y?" must look at the whole chain.
		`DELETE FROM group_anc`,
		`INSERT OR IGNORE INTO group_anc
		 WITH RECURSIVE ga(name, ancestor, depth) AS (
		   SELECT name, name, 0 FROM groups
		   UNION ALL
		   SELECT ga.name, g.parent, ga.depth+1 FROM ga JOIN groups g ON g.name = ga.ancestor
		   WHERE g.parent <> '' AND ga.depth < 50)
		 SELECT name, ancestor FROM ga`)
}

// ReplaceVoucherTypes stores voucher types and each one's base type.
func (s *DB) ReplaceVoucherTypes(vs []tally.VoucherType) error {
	return replaceAll(s.DB, `DELETE FROM vtypes`, `INSERT OR REPLACE INTO vtypes VALUES(?,?)`, len(vs),
		func(i int) []any { return []any{vs[i].Name, vs[i].Parent} },
		`DELETE FROM vtype_base`,
		`INSERT INTO vtype_base
		 WITH RECURSIVE vb(name, base, depth) AS (
		   SELECT name, name, 0 FROM vtypes WHERE parent = name OR parent = ''
		   UNION ALL
		   SELECT v.name, vb.base, vb.depth+1 FROM vtypes v JOIN vb ON v.parent = vb.name
		   WHERE v.parent <> v.name AND vb.depth < 50)
		 SELECT name, base FROM vb`)
}

// ReplaceStockItems stores stock closing positions.
func (s *DB) ReplaceStockItems(ss []tally.StockItem) error {
	return replaceAll(s.DB, `DELETE FROM stock_items`, `INSERT OR REPLACE INTO stock_items VALUES(?,?,?,?,?,?)`, len(ss),
		func(i int) []any {
			x := ss[i]
			return []any{x.Name, x.Parent, x.Unit, x.ClosingQty, int64(x.ClosingValue), int64(x.OpeningValue)}
		})
}

// ReplaceBills stores Tally's outstanding bills.
func (s *DB) ReplaceBills(bs []tally.Bill) error {
	return replaceAll(s.DB, `DELETE FROM bills`, `INSERT INTO bills VALUES(?,?,?,?,?)`, len(bs),
		func(i int) []any {
			b := bs[i]
			d := ""
			if !b.Date.IsZero() {
				d = b.Date.Format("2006-01-02")
			}
			return []any{b.Party, b.Name, d, int64(b.Closing), b2i(b.Advance)}
		})
}
