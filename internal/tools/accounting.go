package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/write"
)

// BatchSize is how many vouchers go to Tally in one request.
const BatchSize = 50

const idsProp = `"ids":{"type":"string","description":"e.g. 3, 5-7"}`

// Accounting returns the voucher entry tools (Phase 5). Every write is draft -> preview -> post by ID.
func Accounting(e *Env) []mcp.Tool {
	return []mcp.Tool{
		{Name: "draft_voucher",
			Description: "Prepare a voucher (not posted): checks it, returns preview + draft id. lines [{ledger, dr|cr}] in rupees; items for item invoice or stock journal (qty<0 = out).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp +
				`,"type":{"type":"string","description":"Payment, Receipt, Journal, Contra, Sales, Purchase, Credit Note..."}` +
				`,"date":{"type":"string"},"party":{"type":"string"},"reference":{"type":"string","description":"Bill no."}` +
				`,"narration":{"type":"string"}` +
				`,"lines":{"type":"array","items":{"type":"object","properties":{"ledger":{"type":"string"},"dr":{"type":"number"},"cr":{"type":"number"}}}}` +
				`,"items":{"type":"array","items":{"type":"object","properties":{"item":{"type":"string"},"qty":{"type":"number"},"rate":{"type":"number"},"ledger":{"type":"string"}}}}` +
				`,"override_duplicate":{"type":"boolean"}},"required":["type","date"]}`),
			Handler: e.draftVoucher},
		{Name: "drafts", Description: "Drafts: list (default), discard ids, or post ids to Tally only after the user confirms the preview.",
			InputSchema: schema(`"action":{"type":"string","enum":["list","post","discard"]}`, idsProp), Handler: e.drafts},
	}
}

// Masters returns the master create/alter tool (drafts post through post_drafts).
func Masters(e *Env) []mcp.Tool {
	return []mcp.Tool{{Name: "draft_master",
		Description: "Prepare a master create/alter (not posted); returns preview + draft id. Alter changes only the fields given.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp +
			`,"kind":{"type":"string","enum":["ledger","group","stock_item","unit","cost_centre"]},"action":{"type":"string","enum":["create","alter"]}` +
			`,"name":{"type":"string"},"new_name":{"type":"string"},"parent":{"type":"string"},"opening_dr":{"type":"number"},"opening_cr":{"type":"number"}` +
			`,"gstin":{"type":"string"},"state":{"type":"string"},"unit":{"type":"string"},"hsn":{"type":"string"}},"required":["kind","name"]}`),
		Handler: e.draftMaster}}
}

// openWrite opens a company for writing: the read checks plus the write guard.
func (e *Env) openWrite(a map[string]any) (*store.DB, error) {
	db, err := e.open(a, false)
	if err != nil {
		return nil, err
	}
	if err := config.CheckWrite(db.Label.Company); err != nil {
		db.Close()
		return nil, err
	}
	db.Exec(write.Schema)
	return db, nil
}

const postHint = "\nShow this to the user. Post only after they confirm (drafts action post). Take a Tally backup (Alt+Y) before posting."

func withWarnings(s string, warns []string) string {
	for _, w := range warns {
		s += "\nWarning: " + w
	}
	return s + postHint
}

func (e *Env) draftVoucher(a map[string]any) (string, error) {
	v, err := write.FromArgs(a)
	if err != nil {
		return "", err
	}
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		return "", err
	}
	errs, warns := bk.Check(&v)
	if len(errs) > 0 {
		return "Not drafted:\n- " + strings.Join(errs, "\n- "), nil
	}
	if err := write.Save(db.DB, &v); err != nil {
		return "", err
	}
	return withWarnings(v.Preview(), warns), nil
}

func (e *Env) draftMaster(a map[string]any) (string, error) {
	m, err := write.MasterFromArgs(a)
	if err != nil {
		return "", err
	}
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		return "", err
	}
	errs, warns := bk.CheckMaster(&m)
	if len(errs) > 0 {
		return "Not drafted:\n- " + strings.Join(errs, "\n- "), nil
	}
	if err := write.SaveMaster(db.DB, &m); err != nil {
		return "", err
	}
	return withWarnings(m.Preview(), warns), nil
}

func (e *Env) listDrafts(a map[string]any) (string, error) {
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	defer db.Close()
	if s := str(a, "discard"); s != "" {
		ids, err := write.IDs(s)
		if err != nil {
			return "", err
		}
		n, err := write.Discard(db.DB, ids) // any kind, change drafts too: discarding posts nothing
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Discarded %d draft(s).", n), nil
	}
	ds, err := write.Pending(db.DB)
	if err != nil {
		return "", err
	}
	if len(ds) == 0 {
		return "No drafts waiting.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d draft(s) waiting:\nId | Date | What | Party / name | Amount\n", len(ds))
	for i, d := range ds {
		if i == e.top(a) {
			fmt.Fprintf(&b, "... %d more\n", len(ds)-i)
			break
		}
		if d.M != nil {
			verb := "create"
			if d.M.Alter {
				verb = "alter"
			}
			fmt.Fprintf(&b, "%d | - | %s %s | %s | -\n", d.ID, verb, strings.ReplaceAll(d.M.Kind, "_", " "), d.M.Name)
			continue
		}
		v := d.V
		fmt.Fprintf(&b, "%d | %s | %s | %s | %s\n", v.ID, v.Date, v.Type, dash(v.Party), amt(v.Total()))
	}
	return strings.TrimSpace(b.String()), nil
}

// postDrafts posts masters first (one request each), refreshes the books, then re-checks and
// posts vouchers in batches. Each voucher is confirmed by its narration marker after the sync.
func (e *Env) postDrafts(a map[string]any) (string, error) {
	ids, err := write.IDs(a["ids"])
	if err != nil {
		return "", err
	}
	if e.Tally == nil {
		return "", fmt.Errorf("Tally is not connected")
	}
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	label, dir := db.Label, db.Label.Dir(e.Root)
	if label.Source != store.Live {
		db.Close()
		return "", fmt.Errorf("%s is a backup copy; posting needs the live company open in Tally", label.Company)
	}
	ds, err := write.Load(db.DB, ids)
	if err != nil {
		db.Close()
		return "", err
	}
	var ms []write.Master
	var vs []write.Voucher
	for _, d := range ds {
		if d.M != nil {
			ms = append(ms, *d.M)
		} else {
			vs = append(vs, *d.V)
		}
	}
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		db.Close()
		return "", err
	}
	var bad []string
	for i := range ms {
		if errs, _ := bk.CheckMaster(&ms[i]); len(errs) > 0 {
			bad = append(bad, fmt.Sprintf("draft %d: %s", ms[i].ID, strings.Join(errs, "; ")))
		}
	}
	if len(ms) == 0 { // with new masters in the same post, vouchers are checked after them
		for i := range vs {
			if errs, _ := bk.Check(&vs[i]); len(errs) > 0 {
				bad = append(bad, fmt.Sprintf("draft %d: %s", vs[i].ID, strings.Join(errs, "; ")))
			}
		}
	}
	db.Close()
	if len(bad) > 0 {
		return "Nothing posted. Fix or discard:\n- " + strings.Join(bad, "\n- "), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	post := func(body string) (write.Result, error) {
		var res write.Result
		err := e.Tally.Do(ctx, body, func(r io.Reader) error {
			var e2 error
			res, e2 = write.ParseResult(r)
			return e2
		})
		return res, err
	}
	var out []string
	var syncErr error
	var syncRes syncjob.Result
	resync := func() {
		syncRes, syncErr = syncjob.Run(ctx, e.Tally, syncjob.Job{Root: e.Root, Company: label.Company, Source: label.Source})
	}

	// 1. Masters, one request each so each result is known.
	var mOK, mBad []string
	for _, m := range ms {
		res, err := post(write.MasterXML(label.Company, m))
		ok := err == nil && res.Created+res.Altered > 0 && res.Errors+res.Exceptions == 0
		msg := fmt.Sprint(err)
		if err == nil {
			msg = res.String()
		}
		db2, _ := store.Open(e.Root, label)
		if ok {
			write.Mark(db2.DB, m.ID, "posted", "", "")
			mOK = append(mOK, fmt.Sprint(m.ID))
			write.Log(dir, map[string]any{"action": "master", "draft": m, "company": label.Company})
		} else {
			write.Mark(db2.DB, m.ID, "failed", "", msg)
			mBad = append(mBad, fmt.Sprintf("%d (%s)", m.ID, msg))
			write.Log(dir, map[string]any{"action": "master_failed", "draft": m, "reply": msg, "company": label.Company})
		}
		db2.Close()
	}
	if len(ms) > 0 {
		out = append(out, fmt.Sprintf("Masters posted %d of %d.", len(mOK), len(ms)))
		if len(mBad) > 0 {
			out = append(out, "Masters not posted: "+strings.Join(mBad, "; "))
		}
		resync()
	}

	// 2. Vouchers: re-check after new masters, then batches.
	if len(vs) > 0 {
		db, err = store.Open(e.Root, label)
		if err != nil {
			return "", err
		}
		if len(ms) > 0 {
			bk, _ = write.LoadBook(db.DB)
			var keep []write.Voucher
			for i := range vs {
				if errs, _ := bk.Check(&vs[i]); len(errs) > 0 {
					out = append(out, fmt.Sprintf("Draft %d not posted: %s", vs[i].ID, strings.Join(errs, "; ")))
				} else {
					keep = append(keep, vs[i])
				}
			}
			vs = keep
		}
		db.Close()
		var replies []string
		for i := 0; i < len(vs); i += BatchSize {
			res, err := post(write.VouchersXML(label.Company, vs[i:min(i+BatchSize, len(vs))]))
			if err != nil {
				replies = append(replies, fmt.Sprintf("batch %d: %v", i/BatchSize+1, err))
				break
			}
			if res.Errors+res.Exceptions > 0 {
				replies = append(replies, fmt.Sprintf("batch %d: %s", i/BatchSize+1, res))
			}
		}
		if len(vs) > 0 {
			resync()
			db, err = store.Open(e.Root, label)
			if err != nil {
				return "", err
			}
			found := write.FindPosted(db.DB, vs)
			var ok, failed []string
			for _, v := range vs {
				if g, hit := found[v.ID]; hit {
					write.Mark(db.DB, v.ID, "posted", g, "")
					ok = append(ok, fmt.Sprint(v.ID))
					write.Log(dir, map[string]any{"action": "create", "draft": v, "guid": g, "company": label.Company})
				} else {
					msg := strings.Join(replies, "; ")
					write.Mark(db.DB, v.ID, "failed", "", msg)
					failed = append(failed, fmt.Sprint(v.ID))
					write.Log(dir, map[string]any{"action": "create_failed", "draft": v, "reply": msg, "company": label.Company})
				}
			}
			db.Close()
			s := fmt.Sprintf("Vouchers posted %d of %d.", len(ok), len(vs))
			if len(failed) > 0 {
				s += fmt.Sprintf(" Not posted: %s. Tally said: %s", strings.Join(failed, ", "), dash(strings.Join(replies, "; ")))
			}
			out = append(out, s)
		}
	}
	out = append([]string{label.Company + ":"}, out...)
	if syncErr != nil {
		out = append(out, fmt.Sprintf("Warning: local books not refreshed (%v); run sync_now.", syncErr))
	} else if syncRes.Mode != "" {
		out = append(out, syncRes.TB.String())
	}
	return strings.Join(out, "\n"), nil
}

// PostTools is the drafts tool alone (masters without voucher entry).
func PostTools(e *Env) []mcp.Tool { return Accounting(e)[1:] }

func (e *Env) drafts(a map[string]any) (string, error) {
	if g, ok := strings.CutPrefix(str(a, "ids"), "group:"); ok {
		db, err := e.openWrite(a)
		if err != nil {
			return "", err
		}
		ids, err := write.GroupIDs(db.DB, strings.TrimSpace(g))
		db.Close()
		if err != nil {
			return "", err
		}
		var s []string
		for _, id := range ids {
			s = append(s, fmt.Sprint(id))
		}
		a["ids"] = strings.Join(s, ",")
	}
	switch str(a, "action") {
	case "post":
		return e.postDrafts(a)
	case "discard":
		a["discard"] = str(a, "ids")
	}
	return e.listDrafts(a)
}
