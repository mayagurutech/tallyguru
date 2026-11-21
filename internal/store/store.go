// Package store keeps one SQLite file per company on disk.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/tally"
	_ "modernc.org/sqlite"
)

// Source says where the books came from.
type Source string

const (
	Live   Source = "live"   // TallyPrime on the client's PC
	Backup Source = "backup" // client backup restored on the CA's PC
)

// Label identifies one company's data. Different labels never share a file.
type Label struct {
	Company, GSTIN string
	Source         Source
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Dir returns the folder for this company under root, e.g. test-mcp__nogstin__live.
func (l Label) Dir(root string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(l.Company), "-"), "-")
	g := strings.ToLower(strings.TrimSpace(l.GSTIN))
	if g == "" {
		g = "nogstin"
	}
	return filepath.Join(root, fmt.Sprintf("%s__%s__%s", slug, g, l.Source))
}

// DefaultRoot is %LOCALAPPDATA%\TallyGuru.
// DefaultRoot is the data folder: TALLYGURU_DATA if set (the installer sets it, so the AI app,
// the control panel and the command line share one folder; the Microsoft Store Claude app
// redirects AppData), else %LOCALAPPDATA%TallyGuru.
func DefaultRoot() string {
	if d := os.Getenv("TALLYGURU_DATA"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil { // LOCALAPPDATA on Windows
		return filepath.Join(d, "TallyGuru")
	}
	return "TallyGuru"
}

// DB is one company's database.
type DB struct {
	*sql.DB
	Label Label
	Path  string
}

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ledgers (
  guid TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, parent TEXT NOT NULL,
  opening INTEGER NOT NULL, closing INTEGER NOT NULL, alter_id INTEGER NOT NULL, master_id INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS vouchers (
  guid TEXT PRIMARY KEY, date TEXT NOT NULL, type TEXT NOT NULL, number TEXT NOT NULL,
  party TEXT NOT NULL, reference TEXT NOT NULL, narration TEXT NOT NULL,
  cancelled INTEGER NOT NULL, optional INTEGER NOT NULL, alter_id INTEGER NOT NULL, master_id INTEGER NOT NULL,
  postdated INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS vlines (
  voucher_guid TEXT NOT NULL REFERENCES vouchers(guid) ON DELETE CASCADE,
  seq INTEGER NOT NULL, ledger TEXT NOT NULL, amount INTEGER NOT NULL,
  PRIMARY KEY (voucher_guid, seq));
CREATE INDEX IF NOT EXISTS ix_v_date ON vouchers(date);
CREATE INDEX IF NOT EXISTS ix_v_party ON vouchers(party);
CREATE INDEX IF NOT EXISTS ix_v_alter ON vouchers(alter_id);
CREATE INDEX IF NOT EXISTS ix_l_ledger ON vlines(ledger);
CREATE TABLE IF NOT EXISTS party_codes (name TEXT PRIMARY KEY, code TEXT NOT NULL UNIQUE);
-- GST and residence details per ledger (latest registration in Tally); only ledgers that have any.
CREATE TABLE IF NOT EXISTS ledger_party (name TEXT PRIMARY KEY, gstin TEXT NOT NULL, state TEXT NOT NULL,
  reg_type TEXT NOT NULL, country TEXT NOT NULL, currency TEXT NOT NULL, transporter INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sync_log (
  id INTEGER PRIMARY KEY, started TEXT NOT NULL, finished TEXT, kind TEXT NOT NULL,
  chunks INTEGER, vouchers INTEGER, ledgers INTEGER, peak_rss_mb REAL, status TEXT, message TEXT);
`

// Open creates or opens the company's database under root.
func Open(root string, l Label) (*DB, error) {
	dir := l.Dir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "books.db")
	// WAL lets tools read while a sync writes; small cache keeps RAM low.
	db, err := sql.Open("sqlite", "file:"+p+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=cache_size(-8000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// Columns added after a database was first created, before the views that use them (fails
	// harmlessly on a new database, where the table does not exist yet).
	db.Exec(`ALTER TABLE vouchers ADD COLUMN postdated INTEGER NOT NULL DEFAULT 0`)
	if _, err := db.Exec(schema + mastersSchema + summarySchema + mapSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	// Columns added after a database was first created (ignored if already there).
	db.Exec(`ALTER TABLE stock_items ADD COLUMN opening_value INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE groups ADD COLUMN reserved TEXT NOT NULL DEFAULT ''`)
	s := &DB{DB: db, Label: l, Path: p}
	for k, v := range map[string]string{"company": l.Company, "gstin": l.GSTIN, "source": string(l.Source)} {
		if old, _ := s.Meta(k); old != "" && old != v {
			db.Close()
			return nil, fmt.Errorf("database %s belongs to %s=%q, not %q", p, k, old, v)
		}
		if err := s.SetMeta(k, v); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}

// List returns the labels of every company database under root.
func List(root string) ([]Label, error) {
	ents, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Label
	for _, e := range ents {
		p := filepath.Join(root, e.Name(), "books.db")
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		db, err := sql.Open("sqlite", "file:"+p+"?mode=ro")
		if err != nil {
			continue
		}
		m := map[string]string{}
		rows, err := db.Query(`SELECT key, value FROM meta WHERE key IN ('company','gstin','source')`)
		if err == nil {
			for rows.Next() {
				var k, v string
				rows.Scan(&k, &v)
				m[k] = v
			}
			rows.Close()
		}
		db.Close()
		if m["company"] != "" {
			out = append(out, Label{Company: m["company"], GSTIN: m["gstin"], Source: Source(m["source"])})
		}
	}
	return out, nil
}

// PartyCode returns the stable privacy-mode code (P001, P002 ...) for a party, creating
// it on first use. The mapping never leaves this PC.
func (s *DB) PartyCode(name string) (string, error) {
	var c string
	err := s.QueryRow(`SELECT code FROM party_codes WHERE name=?`, name).Scan(&c)
	if err == nil {
		return c, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	var n int
	s.QueryRow(`SELECT COUNT(*) FROM party_codes`).Scan(&n)
	c = fmt.Sprintf("P%03d", n+1)
	_, err = s.Exec(`INSERT INTO party_codes(name, code) VALUES(?,?)`, name, c)
	return c, err
}

// PartyName resolves a code back to the real name ("" if unknown).
func (s *DB) PartyName(code string) string {
	var n string
	s.QueryRow(`SELECT name FROM party_codes WHERE code = ? COLLATE NOCASE`, code).Scan(&n)
	return n
}

// Meta reads a meta value ("" if missing).
func (s *DB) Meta(k string) (string, error) {
	var v string
	err := s.QueryRow(`SELECT value FROM meta WHERE key=?`, k).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetMeta writes a meta value.
func (s *DB) SetMeta(k, v string) error {
	_, err := s.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

// ReplaceLedgers replaces the full ledger list in one transaction (ledgers missing
// from Tally are deleted).
func (s *DB) ReplaceLedgers(ls []tally.Ledger) error {
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM ledgers`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM ledger_party`); err != nil {
		return err
	}
	st, err := tx.Prepare(`INSERT INTO ledgers VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	pt, err := tx.Prepare(`INSERT OR REPLACE INTO ledger_party VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer pt.Close()
	for _, l := range ls {
		if _, err := st.Exec(l.GUID, l.Name, l.Parent, int64(l.Opening), int64(l.Closing), l.AlterID, l.MasterID); err != nil {
			return fmt.Errorf("ledger %q: %w", l.Name, err)
		}
		if p := l.Party; p != (tally.PartyInfo{}) {
			if _, err := pt.Exec(l.Name, strings.ToUpper(p.GSTIN), p.State, p.RegType, p.Country, p.Currency, b2i(p.Transporter)); err != nil {
				return fmt.Errorf("ledger %q: %w", l.Name, err)
			}
		}
	}
	return tx.Commit()
}

// VoucherTx writes one chunk of vouchers atomically.
type VoucherTx struct {
	tx         *sql.Tx
	del, v, ln *sql.Stmt
	N          int
	MaxAlter   int64
}

// BeginVouchers starts a chunk.
func (s *DB) BeginVouchers() (*VoucherTx, error) {
	tx, err := s.Begin()
	if err != nil {
		return nil, err
	}
	t := &VoucherTx{tx: tx}
	if t.del, err = tx.Prepare(`DELETE FROM vlines WHERE voucher_guid=?`); err == nil {
		if t.v, err = tx.Prepare(`INSERT INTO vouchers VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(guid) DO UPDATE SET
		  date=excluded.date,type=excluded.type,number=excluded.number,party=excluded.party,reference=excluded.reference,
		  narration=excluded.narration,cancelled=excluded.cancelled,optional=excluded.optional,alter_id=excluded.alter_id,master_id=excluded.master_id,postdated=excluded.postdated`); err == nil {
			t.ln, err = tx.Prepare(`INSERT INTO vlines VALUES(?,?,?,?)`)
		}
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return t, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Put inserts or replaces one voucher with its lines.
func (t *VoucherTx) Put(v tally.Voucher) error {
	if v.GUID == "" {
		return fmt.Errorf("voucher %s %s has no GUID", v.Type, v.Number)
	}
	if _, err := t.del.Exec(v.GUID); err != nil {
		return err
	}
	if _, err := t.v.Exec(v.GUID, v.Date.Format(time.DateOnly), v.Type, v.Number, v.Party, v.Reference, v.Narration,
		b2i(v.Cancelled), b2i(v.Optional), v.AlterID, v.MasterID, b2i(v.PostDated)); err != nil {
		return err
	}
	for i, l := range v.Lines {
		if _, err := t.ln.Exec(v.GUID, i, l.Ledger, int64(l.Amount)); err != nil {
			return err
		}
	}
	t.N++
	t.MaxAlter = max(t.MaxAlter, v.AlterID)
	return nil
}

// Delete removes a voucher (lines cascade) inside the chunk.
func (t *VoucherTx) Delete(guid string) error {
	_, err := t.tx.Exec(`DELETE FROM vouchers WHERE guid=?`, guid)
	return err
}

// Commit finishes the chunk.
func (t *VoucherTx) Commit() error { return t.tx.Commit() }

// Rollback abandons the chunk.
func (t *VoucherTx) Rollback() error { return t.tx.Rollback() }
