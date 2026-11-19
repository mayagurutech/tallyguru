package write

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Save stores a new draft and sets its ID.
func Save(db *sql.DB, v *Voucher) error {
	if _, err := db.Exec(Schema); err != nil {
		return err
	}
	res, err := db.Exec(`INSERT INTO drafts(created, kind, body, status) VALUES(?, 'voucher', '{}', 'draft')`, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}
	v.ID, _ = res.LastInsertId()
	b, _ := json.Marshal(v)
	_, err = db.Exec(`UPDATE drafts SET body=? WHERE id=?`, string(b), v.ID)
	return err
}

// SaveMaster stores a new master draft and sets its ID.
func SaveMaster(db *sql.DB, m *Master) error {
	db.Exec(Schema)
	res, err := db.Exec(`INSERT INTO drafts(created, kind, body, status) VALUES(?, 'master', '{}', 'draft')`, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	b, _ := json.Marshal(m)
	_, err = db.Exec(`UPDATE drafts SET body=? WHERE id=?`, string(b), m.ID)
	return err
}

// Draft is a stored draft: a voucher or a master.
type Draft struct {
	ID int64
	V  *Voucher
	M  *Master
}

func decode(id int64, kind, body string) (Draft, error) {
	d := Draft{ID: id}
	if kind == "change" {
		return d, fmt.Errorf("draft %d is a change: post it with change_voucher and the typed word", id)
	}
	if kind == "master" {
		d.M = &Master{}
		return d, json.Unmarshal([]byte(body), d.M)
	}
	d.V = &Voucher{}
	return d, json.Unmarshal([]byte(body), d.V)
}

// Load returns drafts by ID that are still waiting to be posted.
func Load(db *sql.DB, ids []int64) ([]Draft, error) {
	db.Exec(Schema)
	var out []Draft
	for _, id := range ids {
		var kind, body, status string
		if err := db.QueryRow(`SELECT kind, body, status FROM drafts WHERE id=?`, id).Scan(&kind, &body, &status); err != nil {
			return nil, fmt.Errorf("draft %d not found", id)
		}
		if status != "draft" {
			return nil, fmt.Errorf("draft %d is already %s", id, status)
		}
		d, err := decode(id, kind, body)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// Pending lists drafts waiting to be posted.
func Pending(db *sql.DB) ([]Draft, error) {
	db.Exec(Schema)
	rows, err := db.Query(`SELECT id, kind, body FROM drafts WHERE status='draft' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Draft
	for rows.Next() {
		var id int64
		var kind, body string
		rows.Scan(&id, &kind, &body)
		if d, err := decode(id, kind, body); err == nil {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// Mark sets a draft's status (posted, failed, discarded) with the Tally GUID or message.
func Mark(db *sql.DB, id int64, status, guid, msg string) error {
	_, err := db.Exec(`UPDATE drafts SET status=?, guid=?, message=? WHERE id=?`, status, guid, msg, id)
	return err
}

// FindPosted looks up the vouchers carrying each draft's marker after a sync.
func FindPosted(db *sql.DB, vs []Voucher) map[int64]string {
	out := map[int64]string{}
	for _, v := range vs {
		var g string
		if db.QueryRow(`SELECT guid FROM vouchers WHERE narration LIKE ? LIMIT 1`, "%"+Marker(v.ID)+"%").Scan(&g) == nil {
			out[v.ID] = g
		}
	}
	return out
}

// Log appends one line per write to writes.jsonl in the company folder.
func Log(dir string, entry map[string]any) error {
	entry["time"] = time.Now().Format(time.RFC3339)
	b, _ := json.Marshal(entry)
	f, err := os.OpenFile(filepath.Join(dir, "writes.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// IDs parses "3, 4, 7-9" or a JSON array into draft IDs.
func IDs(v any) ([]int64, error) {
	var out []int64
	add := func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		var a, b int64
		if n, _ := fmt.Sscanf(s, "%d-%d", &a, &b); n == 2 && b >= a && b-a < 500 {
			for i := a; i <= b; i++ {
				out = append(out, i)
			}
			return nil
		}
		if _, err := fmt.Sscan(s, &a); err != nil {
			return fmt.Errorf("draft id %q", s)
		}
		out = append(out, a)
		return nil
	}
	switch x := v.(type) {
	case []any:
		for _, y := range x {
			if err := add(fmt.Sprint(y)); err != nil {
				return nil, err
			}
		}
	case string:
		for _, s := range strings.Split(x, ",") {
			if err := add(s); err != nil {
				return nil, err
			}
		}
	case float64:
		out = append(out, int64(x))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("give draft ids, e.g. \"3, 4, 7-9\"")
	}
	return out, nil
}

// SetGroup labels a draft with an import group (e.g. "Sales Oct 2026").
func SetGroup(db *sql.DB, id int64, grp string) {
	db.Exec(`ALTER TABLE drafts ADD COLUMN grp TEXT NOT NULL DEFAULT ''`) // no-op error once added
	db.Exec(`UPDATE drafts SET grp=? WHERE id=?`, grp, id)
}

// GroupIDs returns waiting drafts of a group.
func GroupIDs(db *sql.DB, grp string) ([]int64, error) {
	db.Exec(`ALTER TABLE drafts ADD COLUMN grp TEXT NOT NULL DEFAULT ''`)
	rows, err := db.Query(`SELECT id FROM drafts WHERE status='draft' AND kind<>'change' AND lower(grp)=lower(?) ORDER BY id`, grp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no waiting drafts in group %q", grp)
	}
	return out, nil
}

// WaitItem is one waiting draft for the control panel.
type WaitItem struct {
	ID          int64
	Kind, Group string // voucher | master | change
	Date, Title string
	Preview     string
	Amount      int64
}

// Waiting lists every waiting draft (vouchers, masters and changes) with its preview.
func Waiting(db *sql.DB) ([]WaitItem, error) {
	db.Exec(Schema)
	db.Exec(`ALTER TABLE drafts ADD COLUMN grp TEXT NOT NULL DEFAULT ''`)
	rows, err := db.Query(`SELECT id, kind, body, grp FROM drafts WHERE status='draft' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WaitItem
	for rows.Next() {
		var it WaitItem
		var body string
		rows.Scan(&it.ID, &it.Kind, &body, &it.Group)
		switch it.Kind {
		case "change":
			var c Change
			if json.Unmarshal([]byte(body), &c) != nil {
				continue
			}
			c.ID = it.ID
			it.Date, it.Title, it.Preview = c.Before.Date, strings.ToUpper(c.Action)+" "+c.Before.Type+" "+c.Before.Number, c.Preview()
			// The word is shown in the preview text; the panel asks the user to type it.
		case "master":
			var m Master
			if json.Unmarshal([]byte(body), &m) != nil {
				continue
			}
			it.Title, it.Preview = strings.ReplaceAll(m.Kind, "_", " ")+" "+m.Name, m.Preview()
		default:
			var v Voucher
			if json.Unmarshal([]byte(body), &v) != nil {
				continue
			}
			it.Date, it.Title, it.Preview, it.Amount = v.Date, v.Type+" "+v.Party, v.Preview(), v.Total()
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Discard drops waiting drafts of any kind (vouchers, masters, changes).
func Discard(db *sql.DB, ids []int64) (int, error) {
	db.Exec(Schema)
	n := 0
	for _, id := range ids {
		var status string
		if err := db.QueryRow(`SELECT status FROM drafts WHERE id=?`, id).Scan(&status); err != nil {
			return n, fmt.Errorf("draft %d not found", id)
		}
		if status != "draft" {
			return n, fmt.Errorf("draft %d is already %s", id, status)
		}
		Mark(db, id, "discarded", "", "")
		n++
	}
	return n, nil
}
