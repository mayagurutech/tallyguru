// Package syncjob is the one place a sync is started from (CLI, sync_now tool, auto-refresh):
// dev guard, company lookup, authorisation, smart sync, then the trial-balance check.
package syncjob

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/sync"
	"github.com/mayagurutech/tallyguru/internal/synclock"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tbcheck"
	"github.com/mayagurutech/tallyguru/internal/tbheal"
)

// Job describes one sync request.
type Job struct {
	Root    string
	Company string
	Source  store.Source
	Weekly  bool
	Full    bool // force the full monthly sweep
	// From changes which vouchers are kept: "" keeps the company's setting, "all" the whole
	// history, or a date (2025-04-01) / financial year (2025-26). See ParseFrom.
	From     string
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
	// Also one at a time across processes (every AI app runs its own copy of the server).
	lk, err := synclock.Acquire(ctx, j.Root, func() {
		say("Waiting: another LedgerGuru window is syncing TallyPrime; this sync starts when it finishes.")
	})
	if err != nil {
		return res, fmt.Errorf("waiting for another sync to finish: %w", err)
	}
	defer lk.Unlock()
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
	if j.From != "" {
		want, err := ParseFrom(j.From)
		if err != nil {
			return res, err
		}
		if old := mustMeta(db, "sync_from"); old != want {
			db.SetMeta("sync_from", want)
			j.Full = true // other months to read (or drop): full sweep
		}
	}
	if sf := mustMeta(db, "sync_from"); sf != "" && sf > co.BooksFrom.Format(time.DateOnly) {
		say("Vouchers from " + sf + " (earlier years are summed into the ledger openings)")
	}
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
		start := sync.VouchersFrom(db, sync.Options{From: from})
		cfg, _ := config.Load(j.Root)
		h, herr := tbheal.Run(ctx, e, res.TB, from, start, sync.FYEnd(time.Now()), cfg.ExportBase(), say)
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

// ParseFrom reads a "from" choice: "all" (whole history, stored as ""), a date 2025-04-01 or
// 01-04-2025, or a financial year 2025-26 / 2025-2026 / FY 25-26 (its 1 April).
func ParseFrom(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSpace(strings.TrimPrefix(s, "fy"))
	if s == "all" || s == "" {
		return "", nil
	}
	for _, f := range []string{time.DateOnly, "02-01-2006", "2-1-2006", "02/01/2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return t.Format(time.DateOnly), nil
		}
	}
	if a, _, ok := strings.Cut(s, "-"); ok {
		if y, err := strconv.Atoi(a); err == nil {
			if y < 100 {
				y += 2000
			}
			if y >= 1990 && y <= 2100 {
				return fmt.Sprintf("%d-04-01", y), nil
			}
		}
	}
	return "", fmt.Errorf("from %q: give a date (2025-04-01), a financial year (2025-26) or all", s)
}
