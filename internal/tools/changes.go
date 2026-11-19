package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/write"
)

// Changes returns the alter/cancel/delete tool (separate module, off by default).
func Changes(e *Env) []mcp.Tool {
	return []mcp.Tool{{Name: "change_voucher",
		Description: "Alter, cancel or delete an existing voucher. Step 1: type, number (date if repeated), action; alter also lines [{ledger, dr|cr}]. " +
			"Shows before/after and a word. Step 2: draft_id + the word typed by the user.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + companyProp +
			`,"action":{"type":"string","enum":["alter","cancel","delete"]},"type":{"type":"string"},"number":{"type":"string"},"date":{"type":"string"}` +
			`,"new_date":{"type":"string"},"narration":{"type":"string"},"lines":{"type":"array","items":{"type":"object"}}` +
			`,"draft_id":{"type":"integer"},"confirm":{"type":"string"}}}`),
		Handler: e.changeVoucher}}
}

func (e *Env) changeVoucher(a map[string]any) (string, error) {
	db, err := e.openWrite(a)
	if err != nil {
		return "", err
	}
	if id, ok := a["draft_id"].(float64); ok {
		return e.postChange(db, int64(id), str(a, "confirm"))
	}
	defer db.Close()
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		return "", err
	}
	date := str(a, "date")
	if date != "" {
		if t, err := time.Parse("02-01-2006", date); err == nil {
			date = t.Format(time.DateOnly)
		}
	}
	before, guid, err := bk.FindVoucher(str(a, "type"), str(a, "number"), date)
	if err != nil {
		return "", err
	}
	var after *write.Voucher
	if str(a, "action") == "alter" {
		args := map[string]any{"type": before.Type, "date": str(a, "new_date"), "narration": str(a, "narration"), "lines": a["lines"]}
		if args["date"] == "" {
			args["date"] = before.Date
		}
		v, err := write.FromArgs(args)
		if err != nil {
			return "", err
		}
		after = &v
	}
	c, errs, warns := bk.NewChange(str(a, "action"), before, guid, after)
	if len(errs) > 0 {
		return "Not drafted:\n- " + strings.Join(errs, "\n- "), nil
	}
	if err := write.SaveChange(db.DB, &c); err != nil {
		return "", err
	}
	s := c.Preview()
	for _, w := range warns {
		s += "\nWarning: " + w
	}
	return s + "\nShow this to the user and ask them to type the word themselves. Take a Tally backup (Alt+Y) first.", nil
}

func (e *Env) postChange(db *store.DB, id int64, typed string) (string, error) {
	label, dir := db.Label, db.Label.Dir(e.Root)
	c, err := write.LoadChange(db.DB, id)
	if err != nil {
		db.Close()
		return "", err
	}
	if !strings.EqualFold(strings.TrimSpace(typed), c.Word) {
		db.Close()
		return fmt.Sprintf("Not posted: the confirmation word does not match. The user must type %s.", c.Word), nil
	}
	if label.Source != store.Live || e.Tally == nil {
		db.Close()
		return "", fmt.Errorf("posting needs the live company open in Tally")
	}
	// The voucher must still be as previewed.
	bk, err := write.LoadBook(db.DB)
	if err != nil {
		db.Close()
		return "", err
	}
	now, guid, err := bk.FindVoucher(c.Before.Type, c.Before.Number, c.Before.Date)
	db.Close()
	if err != nil || guid != c.GUID || !sameLines(now.Lines, c.Before.Lines) {
		return "Not posted: the voucher changed in Tally since the preview. Prepare the change again.", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var res write.Result
	err = e.Tally.Do(ctx, write.ChangeXML(label.Company, c), func(r io.Reader) error {
		var e2 error
		res, e2 = write.ParseResult(r)
		return e2
	})
	if err != nil {
		return "", err
	}
	sres, serr := syncjob.Run(ctx, e.Tally, syncjob.Job{Root: e.Root, Company: label.Company, Source: label.Source})
	db, err = store.Open(e.Root, label)
	if err != nil {
		return "", err
	}
	defer db.Close()
	// Confirm from the refreshed books, not only from Tally's counters.
	var cancelled, n int
	var narr string
	db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(cancelled),0), COALESCE(MAX(narration),'') FROM vouchers WHERE guid=?`, c.GUID).Scan(&n, &cancelled, &narr)
	done := false
	switch c.Action {
	case "delete":
		done = n == 0
	case "cancel":
		done = n == 1 && cancelled == 1
	case "alter":
		done = n == 1 && strings.Contains(narr, write.Marker(c.ID))
	}
	entry := map[string]any{"action": c.Action, "guid": c.GUID, "before": c.Before, "after": c.After, "reply": res.String(), "company": label.Company}
	if !done && c.Action == "alter" {
		// A failed alter can still create a new voucher in Tally: find it by the marker and say so.
		var typ, num, g string
		if db.QueryRow(`SELECT type, number, guid FROM vouchers WHERE narration LIKE ? AND guid<>?`, "%"+write.Marker(c.ID)+"%", c.GUID).Scan(&typ, &num, &g) == nil {
			write.Mark(db.DB, c.ID, "failed", g, "created new voucher instead of altering")
			entry["action"], entry["stray_guid"] = "alter_failed_created_new", g
			write.Log(dir, entry)
			return fmt.Sprintf("Not altered: Tally created a NEW voucher (%s no. %s) instead. Delete it (change_voucher delete), then report this.", typ, num), nil
		}
	}
	if !done {
		write.Mark(db.DB, c.ID, "failed", "", res.String())
		entry["action"] = c.Action + "_failed"
		write.Log(dir, entry)
		return fmt.Sprintf("Not done: Tally said %s. The voucher is unchanged in the books.", res), nil
	}
	write.Mark(db.DB, c.ID, "posted", c.GUID, "")
	write.Log(dir, entry)
	s := fmt.Sprintf("%s: %s %s no. %s done. Before-values kept in the write log.", label.Company, strings.ToUpper(c.Action[:1])+c.Action[1:], c.Before.Type, c.Before.Number)
	if serr != nil {
		s += fmt.Sprintf("\nWarning: local books not refreshed (%v).", serr)
	} else {
		s += "\n" + sres.TB.String()
	}
	return s, nil
}

func sameLines(a, b []write.Line) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
