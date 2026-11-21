// Package syncjob is the one place a sync is started from (CLI, sync_now tool, auto-refresh):
// dev guard, company lookup, authorisation, smart sync, then the trial-balance check.
package syncjob

import (
	"context"
	"fmt"
	"io"
	gosync "sync"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/sync"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tbcheck"
	"github.com/mayagurutech/tallyguru/internal/tbheal"
)

// Job describes one sync request.
type Job struct {
	Root     string
	Company  string
	Source   store.Source
	Weekly   bool
	Full     bool // force the full monthly sweep
	Progress func(string)
}

// Result of a sync.
type Result struct {
	Mode  string
	Stats sync.Stats
	TB    tbcheck.Result
	Heal  tbheal.Result
}

func (r Result) String() string {
	if r.Mode == "unchanged" {
		return "No changes in Tally since the last sync. " + r.TB.String()
	}
	return fmt.Sprintf("Sync (%s): %s\n%s", r.Mode, r.Stats, r.TB)
}

// one sync at a time per process, so Tally never gets parallel requests from us.
var running gosync.Mutex

// Companies lists companies open in Tally.
func Companies(ctx context.Context, c *tally.Client) ([]tally.Company, error) {
	var cs []tally.Company
	err := c.Do(ctx, tally.CompanyListRequest().Build(), func(r io.Reader) error {
		return tally.ParseCompanies(r, func(co tally.Company) error { cs = append(cs, co); return nil })
	})
	return cs, err
}

// Run executes a job.
func Run(ctx context.Context, c *tally.Client, j Job) (Result, error) {
	running.Lock()
	defer running.Unlock()
	var res Result
	say := func(s string) {
		if j.Progress != nil {
			j.Progress(s)
		}
	}
	if j.Source != store.Live && j.Source != store.Backup {
		return res, fmt.Errorf("source must be live or backup")
	}
	cs, err := Companies(ctx, c)
	if err != nil {
		return res, err
	}
	var co *tally.Company
	for i := range cs {
		if cs[i].Name == j.Company {
			co = &cs[i]
		}
	}
	if co == nil {
		return res, fmt.Errorf("company %q is not open in TallyPrime", j.Company)
	}
	label := store.Label{Company: co.Name, GSTIN: co.GSTIN, Source: j.Source}
	box, err := crypt.LoadOrCreate(j.Root)
	if err != nil {
		return res, err
	}
	rec, err := auth.CoverFromAnnual(auth.Store{Dir: j.Root, Box: box}, auth.Store{Dir: label.Dir(j.Root), Box: box},
		co.Name, co.GSTIN, string(j.Source), time.Now())
	if err != nil {
		return res, err
	}
	say(fmt.Sprintf("Authorisation: %s, FY %s, recorded %s", rec.Kind, rec.FY, rec.AcceptedAt.Format("02-01-2006")))
	db, err := store.Open(j.Root, label)
	if err != nil {
		return res, err
	}
	defer db.Close()
	if err := sync.SetBooksFrom(db, co.BooksFrom); err != nil {
		return res, err
	}
	if co.State != "" {
		db.SetMeta("company_state", co.State) // IGST vs CGST+SGST check
	}
	say(fmt.Sprintf("Syncing %q (%s) into %s", co.Name, j.Source, db.Path))
	e := &sync.Engine{C: c, DB: db}
	o := sync.Options{Weekly: j.Weekly, Progress: func(s string) { say("  " + s) }}
	if j.Full {
		db.SetMeta("alt_vch_id", "0") // forget counters: forces the full sweep
	}
	res.Stats, res.Mode, err = e.Smart(ctx, o, *co)
	if err != nil {
		return res, err
	}
	if res.TB, err = tbcheck.Run(db); err != nil {
		return res, fmt.Errorf("trial-balance check: %w", err)
	}
	if !res.TB.OK() {
		// Self-heal: trace the differing ledgers month by month in Tally, read those months again.
		from, _ := time.Parse(time.DateOnly, mustMeta(db, "books_from"))
		cfg, _ := config.Load(j.Root)
		h, herr := tbheal.Run(ctx, e, res.TB, from, sync.FYEnd(time.Now()), cfg.ExportBase(), say)
		res.Heal = h
		if herr != nil {
			say("Self-check stopped: " + herr.Error())
		} else {
			res.TB = h.TB
			if !h.TB.OK() {
				db.SetMeta("tb_detail", h.String()+h.TB.String()) // shown by the panel and every blocked answer
			}
		}
	}
	if !res.TB.OK() {
		// Next sync must not take the "unchanged" shortcut.
		db.SetMeta("alt_vch_id", "0")
		return res, fmt.Errorf("sync finished, but the books here do not yet match Tally (details above); answers for this company wait until they match")
	}
	return res, nil
}

func mustMeta(db *store.DB, k string) string {
	v, _ := db.Meta(k)
	return v
}
