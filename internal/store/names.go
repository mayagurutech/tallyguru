package store

import (
	"strings"
	"unicode"
)

// NameKey is how Tally compares master names: case, spaces and punctuation are ignored
// ("Swiss Pac Pvt. Ltd" and "SWISS PAC PVT LTD" are the same ledger to Tally). Voucher lines can
// carry such a variant of the ledger's name, e.g. typed before the ledger was renamed.
func NameKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ResolveNames rewrites ledger and party names in vouchers that are not exact ledger names but
// match exactly one ledger by NameKey, so every report and the trial-balance check join on the
// master's name. It returns how many distinct names were resolved.
func (s *DB) ResolveNames() (int, error) {
	rows, err := s.Query(`SELECT name FROM ledgers`)
	if err != nil {
		return 0, err
	}
	byKey := map[string]string{}
	dup := map[string]bool{}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		k := NameKey(n)
		if _, ok := byKey[k]; ok {
			dup[k] = true
		}
		byKey[k] = n
	}
	rows.Close()

	resolve := func(q, upd string) (map[string]bool, error) {
		r, err := s.Query(q)
		if err != nil {
			return nil, err
		}
		var odd []string
		for r.Next() {
			var n string
			r.Scan(&n)
			odd = append(odd, n)
		}
		r.Close()
		done := map[string]bool{}
		for _, n := range odd {
			k := NameKey(n)
			if m, ok := byKey[k]; ok && !dup[k] && k != "" {
				if _, err := s.Exec(upd, m, n); err != nil {
					return nil, err
				}
				done[n] = true
			}
		}
		return done, nil
	}
	a, err := resolve(`SELECT DISTINCT ledger FROM vlines WHERE ledger NOT IN (SELECT name FROM ledgers)`,
		`UPDATE vlines SET ledger = ? WHERE ledger = ?`)
	if err != nil {
		return 0, err
	}
	b, err := resolve(`SELECT DISTINCT party FROM vouchers WHERE party <> '' AND party NOT IN (SELECT name FROM ledgers)`,
		`UPDATE vouchers SET party = ? WHERE party = ?`)
	if err != nil {
		return 0, err
	}
	for n := range b {
		a[n] = true
	}
	return len(a), nil
}
