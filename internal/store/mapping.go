package store

import (
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/mapping"
)

// Mapping rows are keyed by ledger GUID, so a renamed ledger keeps its confirmed tags.
const mapSchema = `
CREATE TABLE IF NOT EXISTS ledger_map (
  guid TEXT NOT NULL, family TEXT NOT NULL, tag TEXT NOT NULL, conf TEXT NOT NULL,
  reason TEXT NOT NULL, source TEXT NOT NULL, confirmed TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (guid, family));`

// conf values: Auto = the tool decided from names, groups and the books; Ask = the books
// leave a material doubt and the user should answer. Confirmed rows are the user's.
const (
	Auto = "auto"
	Ask  = "ask"
)

// MapRow is one ledger's tag in one family.
type MapRow struct {
	GUID, Name, Parent, Family, Tag, Conf, Reason, Source, Confirmed string
}

// RefreshMapping (re)suggests tags for every ledger: confirmed rows are kept, unconfirmed
// rows are replaced with the current suggestion, rows of deleted ledgers or families that
// no longer apply (ledger moved group) are dropped. Cheap: names and groups only.
func (s *DB) RefreshMapping() error {
	if _, err := s.Exec(mapSchema); err != nil {
		return err
	}
	type led struct{ guid, name, parent string }
	rows, err := s.Query(`SELECT guid, name, parent FROM ledgers`)
	if err != nil {
		return err
	}
	var ls []led
	for rows.Next() {
		var l led
		rows.Scan(&l.guid, &l.name, &l.parent)
		ls = append(ls, l)
	}
	rows.Close()
	anc := map[string][]string{}
	rows, err = s.Query(`SELECT name, ancestor FROM group_anc ORDER BY name, ancestor <> name`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n, a string
		rows.Scan(&n, &a)
		anc[strings.ToLower(n)] = append(anc[strings.ToLower(n)], a)
	}
	rows.Close()

	ev, err := s.Evidence()
	if err != nil {
		return err
	}
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS map_keep (guid TEXT, family TEXT);
		DELETE FROM map_keep;`); err != nil {
		return err
	}
	up, err := tx.Prepare(`INSERT INTO ledger_map(guid,family,tag,conf,reason,source) VALUES(?,?,?,?,?,'rule')
		ON CONFLICT(guid,family) DO UPDATE SET tag=excluded.tag, conf=excluded.conf, reason=excluded.reason, source='rule'
		WHERE ledger_map.confirmed = ''`)
	if err != nil {
		return err
	}
	defer up.Close()
	keep, _ := tx.Prepare(`INSERT INTO map_keep VALUES(?,?)`)
	defer keep.Close()
	for _, l := range ls {
		gs := anc[strings.ToLower(l.parent)]
		if len(gs) == 0 {
			gs = []string{l.parent}
		}
		for _, sg := range mapping.Suggest(mapping.Ledger{Name: l.name, Groups: gs}) {
			d := mapping.Decide(sg, ev[strings.ToLower(l.name)])
			conf := Auto
			if d.Ask {
				conf = Ask
			}
			if _, err := up.Exec(l.guid, sg.Family, d.Tag, conf, d.Reason); err != nil {
				return err
			}
			keep.Exec(l.guid, sg.Family)
		}
	}
	if _, err := tx.Exec(`DELETE FROM ledger_map WHERE NOT EXISTS
		(SELECT 1 FROM map_keep k WHERE k.guid = ledger_map.guid AND k.family = ledger_map.family)`); err != nil {
		return err
	}
	return tx.Commit()
}

// MapRows returns mapping rows joined with current ledger names. family "" = all.
// pending: only unconfirmed rows, lowest confidence first.
func (s *DB) MapRows(family string, pending bool) ([]MapRow, error) {
	q := `SELECT m.guid, l.name, l.parent, m.family, m.tag, m.conf, m.reason, m.source, m.confirmed
		FROM ledger_map m JOIN ledgers l ON l.guid = m.guid WHERE (? = '' OR m.family = ?)`
	if pending {
		q += ` AND m.confirmed = ''`
	}
	q += ` ORDER BY CASE m.conf WHEN 'ask' THEN 0 ELSE 1 END, m.family, l.parent, l.name`
	rows, err := s.Query(q, family, family)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MapRow
	for rows.Next() {
		var r MapRow
		if err := rows.Scan(&r.GUID, &r.Name, &r.Parent, &r.Family, &r.Tag, &r.Conf, &r.Reason, &r.Source, &r.Confirmed); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetTag confirms a tag for a ledger (by name) in a family. source: "user" or "ai".
// Returns false if that ledger has no row in that family.
func (s *DB) SetTag(ledger, family, tag, source string, now time.Time) (bool, error) {
	r, err := s.Exec(`UPDATE ledger_map SET tag=?, source=?, confirmed=?, conf='high'
		WHERE family=? AND guid=(SELECT guid FROM ledgers WHERE name=? COLLATE NOCASE)`,
		tag, source, now.Format(time.RFC3339), family, ledger)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// SetTagGUID confirms a tag for a ledger by GUID (used by the local review page).
func (s *DB) SetTagGUID(guid, family, tag, source string, now time.Time) (bool, error) {
	r, err := s.Exec(`UPDATE ledger_map SET tag=?, source=?, confirmed=?, conf='high' WHERE guid=? AND family=?`,
		tag, source, now.Format(time.RFC3339), guid, family)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// AcceptSuggestions confirms all unconfirmed suggestions at or above a confidence level
// (family "" = all). Returns how many were confirmed.
func (s *DB) AcceptSuggestions(family, minConf string, now time.Time) (int64, error) {
	levels := map[string]string{mapping.High: `('high')`, mapping.Medium: `('high','medium')`, mapping.Low: `('high','medium','low')`}[minConf]
	r, err := s.Exec(`UPDATE ledger_map SET confirmed=?, source=source||'+accepted'
		WHERE confirmed='' AND conf IN `+levels+` AND (?='' OR family=?)`, now.Format(time.RFC3339), family, family)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
