package write

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Rules memory: the user's own decisions for one client ("BANK CHG" -> Bank Charges).
// Kept in the client's database; used before past entries and name matching.
const rulesSchema = `
CREATE TABLE IF NOT EXISTS rules (id INTEGER PRIMARY KEY, match TEXT NOT NULL UNIQUE, ledger TEXT NOT NULL,
  gst TEXT NOT NULL DEFAULT '', tds TEXT NOT NULL DEFAULT '', cost_centre TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL, uses INTEGER NOT NULL DEFAULT 0);`

// Rule is one remembered decision. Match is lower-case words that must all appear.
type Rule struct {
	ID                                  int64
	Match, Ledger, GST, TDS, CostCentre string
	Uses                                int
	Broken                              bool // ledger no longer in Tally
}

// AddRule saves a rule after the user said yes. The ledger must exist.
func (b *Book) AddRule(r Rule) (Rule, error) {
	b.db.Exec(rulesSchema)
	r.Match = strings.Join(Words(r.Match), " ")
	if r.Match == "" {
		return r, fmt.Errorf("match needs at least one word of 3+ letters")
	}
	n, ok := b.lower[strings.ToLower(strings.TrimSpace(r.Ledger))]
	if !ok {
		return r, fmt.Errorf("ledger %q not found%s", r.Ledger, b.suggest(r.Ledger))
	}
	r.Ledger = n
	res, err := b.db.Exec(`INSERT INTO rules(match, ledger, gst, tds, cost_centre, created) VALUES(?,?,?,?,?,?)
		ON CONFLICT(match) DO UPDATE SET ledger=excluded.ledger, gst=excluded.gst, tds=excluded.tds, cost_centre=excluded.cost_centre`,
		r.Match, r.Ledger, r.GST, r.TDS, r.CostCentre, time.Now().Format(time.RFC3339))
	if err != nil {
		return r, err
	}
	r.ID, _ = res.LastInsertId()
	if r.ID == 0 {
		b.db.QueryRow(`SELECT id FROM rules WHERE match=?`, r.Match).Scan(&r.ID)
	}
	return r, nil
}

// DeleteRule removes a rule.
func (b *Book) DeleteRule(id int64) error {
	b.db.Exec(rulesSchema)
	res, err := b.db.Exec(`DELETE FROM rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("rule %d not found", id)
	}
	return nil
}

// Rules lists rules, marking those whose ledger is gone.
func (b *Book) Rules() ([]Rule, error) {
	b.db.Exec(rulesSchema)
	rows, err := b.db.Query(`SELECT id, match, ledger, gst, tds, cost_centre, uses FROM rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		rows.Scan(&r.ID, &r.Match, &r.Ledger, &r.GST, &r.TDS, &r.CostCentre, &r.Uses)
		_, ok := b.ledgers[r.Ledger]
		r.Broken = !ok
		out = append(out, r)
	}
	return out, rows.Err()
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// noise words in bank narrations that say nothing about the payee.
var noise = map[string]bool{"neft": true, "imps": true, "rtgs": true, "upi": true, "trf": true, "transfer": true,
	"chq": true, "cheque": true, "the": true, "and": true, "for": true, "from": true, "ltd": true, "pvt": true,
	"payment": true, "paid": true, "received": true, "inb": true, "mob": true, "ref": true, "txn": true, "via": true,
	"tally": true, "mcp": true, "guru": true, "posted": true}

// Words are the significant words of a narration: lower case, 3+ letters, no digits-only or noise.
func Words(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(nonWord.ReplaceAllString(strings.ToLower(s), " ")) {
		if len(w) < 3 || noise[w] || strings.Trim(w, "0123456789") == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// Suggestion is a guessed ledger with where it came from.
type Suggestion struct {
	Ledger, Source string
	Rule           *Rule
}

// Suggest picks a ledger for a narration: the client's rules first, then past entries with
// the same words (excluding the given ledger, e.g. the bank), then ledger names.
func (b *Book) Suggest(narration, exclude string) (Suggestion, bool) {
	ws := Words(narration)
	if len(ws) == 0 {
		return Suggestion{}, false
	}
	has := map[string]bool{}
	for _, w := range ws {
		has[w] = true
	}
	// 1. Rules: all words of the rule present; the longest rule wins.
	if rs, err := b.Rules(); err == nil {
		var best *Rule
		for i := range rs {
			r := &rs[i]
			if r.Broken {
				continue
			}
			all := true
			for _, w := range strings.Fields(r.Match) {
				if !has[w] {
					all = false
					break
				}
			}
			if all && (best == nil || len(r.Match) > len(best.Match)) {
				best = r
			}
		}
		if best != nil {
			b.db.Exec(`UPDATE rules SET uses=uses+1 WHERE id=?`, best.ID)
			return Suggestion{Ledger: best.Ledger, Source: fmt.Sprintf("rule %d", best.ID), Rule: best}, true
		}
	}
	// 2. Past entries: the other ledger in vouchers whose narration has the rarest shared word.
	type cnt struct {
		ledger string
		n      int
	}
	for _, w := range rarest(b.db, ws) {
		rows, err := b.db.Query(`SELECT vl.ledger, COUNT(*) FROM vouchers v JOIN vlines vl ON vl.voucher_guid=v.guid
			WHERE v.cancelled=0 AND lower(v.narration) LIKE ? AND vl.ledger<>? GROUP BY vl.ledger ORDER BY 2 DESC LIMIT 3`,
			"%"+w+"%", exclude)
		if err != nil {
			break
		}
		var cs []cnt
		total := 0
		for rows.Next() {
			var c cnt
			rows.Scan(&c.ledger, &c.n)
			cs = append(cs, c)
			total += c.n
		}
		rows.Close()
		if len(cs) > 0 && cs[0].n >= 2 && cs[0].n*10 >= total*6 {
			return Suggestion{Ledger: cs[0].ledger, Source: fmt.Sprintf("from %d past entries with %q", cs[0].n, w)}, true
		}
	}
	// 3. Ledger names containing a word of the narration (one clear hit only).
	var hits []string
	for _, l := range b.ledgers {
		if l.name == exclude {
			continue
		}
		lw := map[string]bool{}
		for _, w := range Words(l.name) {
			lw[w] = true
		}
		for _, w := range ws {
			if lw[w] && len(w) >= 4 {
				hits = append(hits, l.name)
				break
			}
		}
	}
	if len(hits) == 1 {
		return Suggestion{Ledger: hits[0], Source: "name match"}, true
	}
	return Suggestion{}, false
}

// rarest orders words so the most specific (fewest narrations) is tried first; common words
// like "salary" or "bank" come last.
func rarest(db *sql.DB, ws []string) []string {
	n := map[string]int{}
	for _, w := range ws {
		var c int
		db.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE lower(narration) LIKE ?`, "%"+w+"%").Scan(&c)
		n[w] = c
	}
	out := append([]string{}, ws...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := n[out[i]], n[out[j]]
		if (a == 0) != (b == 0) {
			return b == 0 // words never seen go last
		}
		return a < b
	})
	return out
}

// BankLedger returns the Tally spelling of a bank (or cash) ledger.
func (b *Book) BankLedger(name string) (string, bool) {
	n, ok := b.lower[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return name, false
	}
	l := b.ledgers[n]
	return n, l.bank || l.cash
}

// TypeFor returns a voucher type whose base is base (the base itself when it exists).
func (b *Book) TypeFor(base string) string {
	if _, ok := b.vtypes[base]; ok {
		return base
	}
	for n, bs := range b.vtypes {
		if bs == base {
			return n
		}
	}
	return base
}
