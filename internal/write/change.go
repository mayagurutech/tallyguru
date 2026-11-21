package write

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Change is a draft alter, cancel or delete of an existing voucher. It needs a typed
// confirmation word (shown in the preview) to post, and keeps the voucher as it was (Before)
// so it can be re-created or put back.
type Change struct {
	ID      int64    `json:"id"`
	Action  string   `json:"action"` // alter | cancel | delete
	GUID    string   `json:"guid"`
	Word    string   `json:"word"` // confirmation the user must type
	Before  Voucher  `json:"before"`
	After   *Voucher `json:"after,omitempty"` // alter only
	Altered bool     `json:"-"`
}

// Alterable voucher types. Sales/purchase/notes may carry stock lines that the local books do
// not hold, so altering them here could drop items: alter those in Tally.
var alterable = map[string]bool{"Payment": true, "Receipt": true, "Journal": true, "Contra": true}

// FindVoucher looks up one voucher in the synced books by type and number (and date if given).
func (b *Book) FindVoucher(typ, number, date string) (Voucher, string, error) {
	q := `SELECT guid, date, type, number, party, reference, narration FROM vouchers
		WHERE lower(type)=lower(?) AND number=? AND cancelled=0`
	args := []any{typ, number}
	if date != "" {
		q += ` AND date=?`
		args = append(args, date)
	}
	rows, err := b.db.Query(q, args...)
	if err != nil {
		return Voucher{}, "", err
	}
	var hits []Voucher
	var guids []string
	for rows.Next() {
		var v Voucher
		var g string
		rows.Scan(&g, &v.Date, &v.Type, &v.Number, &v.Party, &v.Reference, &v.Narr)
		hits, guids = append(hits, v), append(guids, g)
	}
	rows.Close()
	switch len(hits) {
	case 0:
		return Voucher{}, "", fmt.Errorf("no %s voucher no. %s found (cancelled vouchers are not changed)", typ, number)
	case 1:
	default:
		var ds []string
		for _, h := range hits {
			ds = append(ds, h.Date)
		}
		return Voucher{}, "", fmt.Errorf("%d %s vouchers numbered %s (dates %s): give the date", len(hits), typ, number, strings.Join(ds, ", "))
	}
	v := hits[0]
	v.Base = b.vtypes[v.Type]
	lr, err := b.db.Query(`SELECT ledger, amount FROM vlines WHERE voucher_guid=? ORDER BY seq`, guids[0])
	if err != nil {
		return v, "", err
	}
	defer lr.Close()
	for lr.Next() {
		var l Line
		lr.Scan(&l.Ledger, &l.Amount)
		v.Lines = append(v.Lines, l)
	}
	return v, guids[0], nil
}

// NewChange prepares a change. after is the new voucher for an alter (nil otherwise).
func (b *Book) NewChange(action string, before Voucher, guid string, after *Voucher) (Change, []string, []string) {
	c := Change{Action: action, GUID: guid, Before: before, After: after, Word: word(action)}
	var errs, warns []string
	switch action {
	case "cancel", "delete":
	case "alter":
		if !alterable[before.Base] {
			errs = append(errs, fmt.Sprintf("%s vouchers may hold stock lines: alter them in Tally (cancel or delete work here)", before.Type))
			break
		}
		if after == nil {
			errs = append(errs, "alter needs the new lines")
			break
		}
		after.Type, after.Number = before.Type, before.Number
		if after.Date == "" {
			after.Date = before.Date
		}
		if after.Party == "" {
			after.Party = before.Party
		}
		if after.Reference == "" {
			after.Reference = before.Reference
		}
		if after.Narr == "" {
			after.Narr = stripRemark(before.Narr)
		}
		after.Override = true // the same voucher, not a duplicate
		errs, warns = b.Check(after)
	default:
		errs = append(errs, "action must be alter, cancel or delete")
	}
	return c, errs, warns
}

// remarks are every narration remark the tool has used (old names too), stripped when altering.
var remarks = []string{Remark, "Altered via LedgerGuru", "Posted via Tally MCP by Guru", "Altered via Tally MCP by Guru"}

func stripRemark(n string) string {
	for _, r := range remarks {
		if i := strings.Index(n, r); i >= 0 {
			n = n[:i]
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(n), "."))
}

func word(action string) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(9000))
	return fmt.Sprintf("%s-%d", strings.ToUpper(action), 1000+n.Int64())
}

// Preview shows before and after and the word the user must type.
func (c *Change) Preview() string {
	var b strings.Builder
	v := c.Before
	fmt.Fprintf(&b, "Change draft %d: %s %s no. %s dated %s", c.ID, strings.ToUpper(c.Action), v.Type, v.Number, v.Date)
	if v.Party != "" {
		fmt.Fprintf(&b, ", party %s", v.Party)
	}
	b.WriteString("\nNow:\n")
	lines(&b, v.Lines)
	switch c.Action {
	case "alter":
		fmt.Fprintf(&b, "After (date %s):\n", c.After.Date)
		lines(&b, c.After.Lines)
	case "cancel":
		b.WriteString("After: the voucher stays in Tally marked cancelled (number kept, amounts become zero).\n")
	case "delete":
		b.WriteString("After: the voucher is removed from Tally. It is kept in the write log and can be re-created.\n")
	}
	fmt.Fprintf(&b, "To post, the user must type: %s", c.Word)
	return b.String()
}

func lines(b *strings.Builder, ls []Line) {
	for _, l := range ls {
		if l.Amount < 0 {
			fmt.Fprintf(b, "  Dr %s %s\n", l.Ledger, rs(l.Amount))
		} else {
			fmt.Fprintf(b, "  Cr %s %s\n", l.Ledger, rs(l.Amount))
		}
	}
}

// ChangeXML builds the alter/cancel/delete request. Tally finds the voucher by its date, type and
// number (TEST MCP trial 18 Nov 2026: TAGNAME="GUID" is not recognised and an alter by GUID
// created a new voucher instead).
func ChangeXML(company string, c Change) string {
	t, _ := time.Parse(time.DateOnly, c.Before.Date)
	d := t.Format("2-Jan-2006")
	act := map[string]string{"alter": "Alter", "cancel": "Cancel", "delete": "Delete"}[c.Action]
	head := fmt.Sprintf(`<TALLYMESSAGE><VOUCHER DATE="%s" TAGNAME="Voucher Number" TAGVALUE="%s" VCHTYPE="%s" ACTION="%s">`,
		d, esc(c.Before.Number), esc(c.Before.Type), act)
	if c.Action != "alter" {
		return envelope(company, "Vouchers", head+fmt.Sprintf("<DATE>%s</DATE><VOUCHERTYPENAME>%s</VOUCHERTYPENAME><VOUCHERNUMBER>%s</VOUCHERNUMBER></VOUCHER></TALLYMESSAGE>",
			d, esc(c.Before.Type), esc(c.Before.Number)))
	}
	a := *c.After
	a.ID = c.ID
	full := VouchersXML(company, []Voucher{a})
	// Reuse the create body, swapping the opening tag for the alter one.
	start := strings.Index(full, "<VOUCHER ")
	end := strings.Index(full[start:], ">") + start + 1
	body := full[end:strings.Index(full, "</VOUCHER>")]
	body = strings.Replace(body, Remark, "Altered via LedgerGuru", 1)
	return envelope(company, "Vouchers", head+body+"</VOUCHER></TALLYMESSAGE>")
}

// SaveChange stores a change draft and sets its ID.
func SaveChange(db *sql.DB, c *Change) error {
	db.Exec(Schema)
	res, err := db.Exec(`INSERT INTO drafts(created, kind, body, status) VALUES(?, 'change', '{}', 'draft')`, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	if c.After != nil {
		c.After.ID = c.ID
	}
	b, _ := json.Marshal(c)
	_, err = db.Exec(`UPDATE drafts SET body=? WHERE id=?`, string(b), c.ID)
	return err
}

// LoadChange returns a waiting change draft.
func LoadChange(db *sql.DB, id int64) (Change, error) {
	db.Exec(Schema)
	var kind, body, status string
	if err := db.QueryRow(`SELECT kind, body, status FROM drafts WHERE id=?`, id).Scan(&kind, &body, &status); err != nil {
		return Change{}, fmt.Errorf("draft %d not found", id)
	}
	if kind != "change" {
		return Change{}, fmt.Errorf("draft %d is not a change draft", id)
	}
	if status != "draft" {
		return Change{}, fmt.Errorf("draft %d is already %s", id, status)
	}
	var c Change
	return c, json.Unmarshal([]byte(body), &c)
}
